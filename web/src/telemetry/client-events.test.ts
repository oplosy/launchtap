import { describe, expect, it, vi } from "vitest";
import {
  FLUSH_DELAY_MS,
  MAX_BATCH,
  MAX_ERRORS_PER_LOAD,
  browserSender,
  createClientEventReporter,
  errorClass,
} from "./client-events";
import { metricPathname, sameOriginError } from "./client-telemetry";
import { ROUTE_GROUPS, routeGroup } from "./route-group";

function harness() {
  const sent: string[] = [];
  const pending: Array<() => void> = [];
  const reporter = createClientEventReporter({
    send: (body) => sent.push(body),
    schedule: (flush, delay) => {
      expect(delay).toBe(FLUSH_DELAY_MS);
      pending.push(flush);
    },
  });
  const events = () => sent.flatMap((body) => JSON.parse(body).events);
  return { reporter, sent, pending, events };
}

describe("routeGroup", () => {
  it("keeps only the top-level route group of a path", () => {
    expect(routeGroup("/")).toBe("home");
    expect(routeGroup("/token/0x" + "a".repeat(40))).toBe("token");
    expect(routeGroup("/wallet/0xabc?tab=pnl#x")).toBe("wallet");
    expect(routeGroup("/v1/transactions/0x" + "b".repeat(64))).toBe("transactions");
    expect(routeGroup("/unknown/path")).toBe("other");
    expect(routeGroup("/home")).toBe("other");
    expect(routeGroup("/other")).toBe("other");
    expect(routeGroup("/transactions")).toBe("other");
    expect(routeGroup("/e2e/rpc")).toBe("other");
  });

  it("only produces values the API accepts", () => {
    for (const path of ["/", "/explore", "/docs/x", "/create", "/zzz", "/v1/x"])
      expect(ROUTE_GROUPS).toContain(routeGroup(path));
  });
});

describe("errorClass", () => {
  it("keeps known error names and folds everything else into other", () => {
    expect(errorClass(new TypeError("0xdeadbeef secret"))).toBe("TypeError");
    expect(errorClass(new Error("x"))).toBe("Error");
    const chunk = new Error("Loading chunk 7 failed");
    chunk.name = "ChunkLoadError";
    expect(errorClass(chunk)).toBe("ChunkLoadError");
    expect(errorClass(new Error("Failed to load chunk /_next/static/chunks/a.js"))).toBe(
      "ChunkLoadError",
    );
    expect(errorClass(new Error("Loading chunk 12 failed."))).toBe("ChunkLoadError");
    const custom = new Error("x");
    custom.name = "UserRejectedRequestError";
    expect(errorClass(custom)).toBe("other");
    expect(errorClass("a string")).toBe("other");
    expect(errorClass(undefined)).toBe("other");
  });
});

describe("createClientEventReporter", () => {
  it("batches events and sends only closed-set fields", () => {
    const { reporter, sent, pending, events } = harness();
    reporter.reportError("window_error", new TypeError("token 0x1234 failed"), "/token/0x1234");
    reporter.reportVital("LCP", 1234.5, "v1-1", "/token/0x1234");
    expect(sent).toHaveLength(0);
    expect(pending).toHaveLength(1);
    pending[0]?.();
    expect(events()).toEqual([
      { type: "error", source: "window_error", route: "token", class: "TypeError" },
      { type: "vital", name: "LCP", route: "token", value: 1234.5 },
    ]);
    expect(sent[0]).not.toContain("0x1234");
  });

  it("reports each error kind once and at most the per-load cap", () => {
    const { reporter, events } = harness();
    for (let i = 0; i < 3; i++) reporter.reportError("window_error", new TypeError(), "/explore");
    const paths = ["/", "/explore", "/token/x", "/create", "/wallet/x", "/profile", "/forum"];
    for (const path of paths) {
      reporter.reportError("unhandled_rejection", new Error(), path);
      reporter.reportError("route_boundary", new RangeError(), path);
    }
    reporter.flush();
    expect(events()).toHaveLength(MAX_ERRORS_PER_LOAD);
    expect(events().filter((e: { source: string }) => e.source === "window_error")).toHaveLength(1);
  });

  it("drops unknown, duplicate and invalid vitals and clamps large values", () => {
    const { reporter, events } = harness();
    reporter.reportVital("Next.js-hydration", 10, "a", "/");
    reporter.reportVital("FID", 10, "b", "/");
    reporter.reportVital("LCP", Number.NaN, "c", "/");
    reporter.reportVital("LCP", -1, "d", "/");
    reporter.reportVital("TTFB", 50, "e", "/");
    reporter.reportVital("TTFB", 60, "e", "/");
    reporter.reportVital("INP", 90_000, "f", "/");
    reporter.reportVital("CLS", 25, "g", "/");
    reporter.flush();
    expect(events()).toEqual([
      { type: "vital", name: "TTFB", route: "home", value: 50 },
      { type: "vital", name: "INP", route: "home", value: 60_000 },
      { type: "vital", name: "CLS", route: "home", value: 10 },
    ]);
  });

  it("splits a full queue into batches the API accepts", () => {
    const { reporter, sent } = harness();
    for (let i = 0; i < MAX_BATCH + 3; i++) reporter.reportVital("TTFB", i, `id-${i}`, "/");
    reporter.flush();
    expect(sent.map((body) => JSON.parse(body).events.length)).toEqual([MAX_BATCH, 3]);
  });

  it("never throws when sending fails and does nothing when disabled", () => {
    const failing = createClientEventReporter({
      send: () => {
        throw new Error("offline");
      },
      schedule: () => undefined,
    });
    failing.reportVital("LCP", 1, "x", "/");
    expect(() => failing.flush()).not.toThrow();
    const schedule = vi.fn();
    const disabled = createClientEventReporter({ send: null, schedule });
    disabled.reportError("window_error", new Error(), "/");
    disabled.flush();
    expect(schedule).not.toHaveBeenCalled();
  });
});

describe("browserSender", () => {
  const ready = { status: "ready", apiBaseUrl: "https://api.example" };

  it("posts text/plain keepalive batches without credentials", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    const send = browserSender(ready, false, fetchImpl as unknown as typeof fetch);
    send?.('{"events":[]}');
    expect(fetchImpl).toHaveBeenCalledWith("https://api.example/v1/client-events", {
      method: "POST",
      body: '{"events":[]}',
      keepalive: true,
      credentials: "omit",
      headers: { "Content-Type": "text/plain;charset=UTF-8" },
    });
  });

  it("swallows a rejected request", async () => {
    const fetchImpl = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
    const send = browserSender(ready, false, fetchImpl as unknown as typeof fetch);
    expect(() => send?.("{}")).not.toThrow();
    await Promise.resolve();
  });

  it("is off for E2E fixtures, fail-closed builds and a missing API", () => {
    const fetchImpl = vi.fn() as unknown as typeof fetch;
    expect(browserSender(ready, true, fetchImpl)).toBeNull();
    expect(browserSender({ ...ready, status: "fail-closed" }, false, fetchImpl)).toBeNull();
    expect(browserSender({ ...ready, apiBaseUrl: null }, false, fetchImpl)).toBeNull();
    expect(browserSender(ready, false, null)).toBeNull();
  });
});

describe("client telemetry helpers", () => {
  it("ignores errors from other origins and extensions", () => {
    const origin = "https://launch.example";
    expect(sameOriginError({ filename: `${origin}/_next/static/chunk.js` }, origin)).toBe(true);
    expect(sameOriginError({ filename: "" }, origin)).toBe(false);
    expect(sameOriginError({ filename: "chrome-extension://abc/inject.js" }, origin)).toBe(false);
    expect(sameOriginError({ filename: `${origin}.evil.example/x.js` }, origin)).toBe(false);
  });

  it("uses the metric's own navigation URL when it has one", () => {
    expect(metricPathname({ navigationURL: "https://launch.example/token/0x1" }, "/explore")).toBe(
      "/token/0x1",
    );
    expect(metricPathname({}, "/explore")).toBe("/explore");
  });
});

describe("API contract drift", () => {
  it("matches the API's closed sets", async () => {
    const { readFile } = await import("node:fs/promises");
    const source = await readFile(
      new URL("../../../backend/internal/apiserver/client_event_routes.go", import.meta.url),
      "utf8",
    );
    const closedSet = (name: string) => {
      const match = new RegExp(String.raw`${name}\s*=\s*closedSet\(([^)]*)\)`).exec(source);
      return [...(match?.[1] ?? "").matchAll(/"([^"]+)"/g)].map((m) => m[1]).sort();
    };
    expect(closedSet("clientRouteGroups")).toEqual([...ROUTE_GROUPS].sort());
    expect(closedSet("clientVitalNames")).toEqual(["CLS", "FCP", "INP", "LCP", "TTFB"]);
    expect(closedSet("clientErrorClasses")).toEqual([
      "ChunkLoadError",
      "Error",
      "RangeError",
      "ReferenceError",
      "SyntaxError",
      "TypeError",
      "other",
    ]);
  });
});
