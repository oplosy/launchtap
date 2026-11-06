import { afterEach, describe, expect, it, vi } from "vitest";
import type { EventSourceLike } from "@/api/sse";
import type { LiveEvent } from "@/api/types";
import { createLiveController } from "./live-controller";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function harness() {
  const sources: { source: EventSourceLike; closed: boolean }[] = [];
  const listeners = new Map<string, (event: Event) => void>();
  const factory = () => {
    const index = sources.length;
    const entry = { closed: false, source: null as unknown as EventSourceLike };
    entry.source = {
      addEventListener: (name, listener) => {
        listeners.set(`${index}:${name}`, listener);
      },
      close: () => {
        entry.closed = true;
      },
    };
    sources.push(entry);
    return entry.source;
  };
  const emit = (name: string, data: unknown) =>
    listeners.get(`${sources.length - 1}:${name}`)?.(
      new MessageEvent(name, { data: JSON.stringify(data) }),
    );
  const error = () => listeners.get(`${sources.length - 1}:error`)?.(new Event("error"));
  return { factory, sources, emit, error };
}

const url = "https://api.example/v1/events";
const base = { chain_id: 1, deployment_id: "local" };

/** Fails the stream repeatedly until its retry budget (5 attempts) is exhausted. */
async function exhaust(h: ReturnType<typeof harness>) {
  for (let i = 0; i < 4; i++) {
    h.error();
    await vi.advanceTimersByTimeAsync(2_500);
  }
  // The fifth failure exhausts the budget and starts the slow retry timer.
  h.error();
}

describe("live controller", () => {
  it("opens one EventSource for many subscribers and fans events out", () => {
    const h = harness();
    const c = createLiveController({ url, eventSourceFactory: h.factory });
    const a: string[] = [];
    const b: string[] = [];
    c.subscribe((e) => a.push(e.event));
    const offB = c.subscribe((e) => b.push(e.event));
    const stop = c.start();
    h.emit("trade", base);
    expect(h.sources).toHaveLength(1);
    expect(a).toEqual(["trade"]);
    expect(b).toEqual(["trade"]);
    offB();
    h.emit("finality", base);
    expect(a).toEqual(["trade", "finality"]);
    expect(b).toEqual(["trade"]);
    stop();
    expect(h.sources[0]?.closed).toBe(true);
  });

  it("logs a throwing subscriber and still delivers to the others", () => {
    const h = harness();
    const errors = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const c = createLiveController({ url, eventSourceFactory: h.factory });
    const seen: string[] = [];
    c.subscribe(() => {
      throw new Error("boom");
    });
    c.subscribe((e) => seen.push(e.event));
    const stop = c.start();
    h.emit("trade", base);
    expect(seen).toEqual(["trade"]);
    expect(errors).toHaveBeenCalled();
    stop();
  });

  it("reconnects even if one subscriber recovery rejects, then emits reconnected", async () => {
    vi.useFakeTimers();
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    const h = harness();
    const c = createLiveController({ url, eventSourceFactory: h.factory });
    const events: LiveEvent["event"][] = [];
    let healthyRan = false;
    c.addRecovery(async () => {
      throw new Error("token 404");
    });
    c.addRecovery(async () => {
      healthyRan = true;
    });
    c.subscribe((e) => events.push(e.event));
    const stop = c.start();
    h.error();
    await vi.advanceTimersByTimeAsync(250);
    expect(healthyRan).toBe(true);
    expect(h.sources).toHaveLength(2);
    expect(events).toEqual(["reconnected"]);
    expect(warn).toHaveBeenCalled();
    stop();
  });

  it("keeps retrying slowly after the retry budget is exhausted", async () => {
    vi.useFakeTimers();
    const h = harness();
    const c = createLiveController({ url, eventSourceFactory: h.factory, slowRetryMs: 30_000 });
    const events: string[] = [];
    c.subscribe((e) => events.push(e.event));
    const stop = c.start();
    await exhaust(h);
    const before = h.sources.length;
    await vi.advanceTimersByTimeAsync(29_000);
    expect(h.sources.length).toBe(before);
    await vi.advanceTimersByTimeAsync(2_000);
    expect(h.sources.length).toBe(before + 1);
    expect(events).toContain("reconnected");
    stop();
  });

  it("retries immediately on visibilitychange; stop cancels the slow timer and listener", async () => {
    vi.useFakeTimers();
    const doc = new EventTarget() as EventTarget & { visibilityState: string };
    doc.visibilityState = "visible";
    vi.stubGlobal("document", doc);
    const h = harness();
    const c = createLiveController({ url, eventSourceFactory: h.factory, slowRetryMs: 30_000 });
    const stop = c.start();
    await exhaust(h);
    const before = h.sources.length;
    doc.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(0);
    expect(h.sources.length).toBe(before + 1);

    await exhaust(h);
    const afterExhaust = h.sources.length;
    stop();
    await vi.advanceTimersByTimeAsync(60_000);
    doc.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(0);
    expect(h.sources.length).toBe(afterExhaust);
  });
});
