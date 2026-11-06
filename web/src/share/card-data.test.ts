import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { loadCardData, MAX_IMAGE_BYTES, MAX_NAME_LENGTH } from "./card-data";

const ADDRESS = "0x0000000000000000000000000000000000000001";
const BASE = "http://api:8080";
const wad = (value: string) => `${BigInt(value) * 10n ** 18n}`;
const PNG = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0]);

function token(overrides: Record<string, unknown> = {}) {
  return {
    name: "Fixture Route",
    symbol: "FIX",
    market_cap_eth: wad("500"),
    graduation_progress_bps: 7143,
    phase: "curve",
    image_url: "https://evil.example/steal.png",
    ...overrides,
  };
}

type Handlers = {
  token?: () => Response;
  image?: () => Response;
  usd?: () => Response;
};

function fakeFetch(handlers: Handlers) {
  const calls: string[] = [];
  const fetcher = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    calls.push(url);
    if (url.endsWith("/image")) return (handlers.image ?? (() => new Response(PNG)))();
    if (url.endsWith("/v1/reference/eth-usd"))
      return (handlers.usd ?? (() => Response.json({ price: "2000.00" })))();
    return (handlers.token ?? (() => Response.json(token())))();
  }) as unknown as typeof fetch;
  return { fetcher, calls };
}

describe("loadCardData", () => {
  beforeEach(() => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
  });
  afterEach(() => vi.restoreAllMocks());

  it("returns generic for an invalid address without calling the API", async () => {
    const { fetcher, calls } = fakeFetch({});
    expect(await loadCardData("not-an-address", fetcher, BASE)).toEqual({ kind: "generic" });
    expect(await loadCardData("0x123", fetcher, BASE)).toEqual({ kind: "generic" });
    expect(calls).toEqual([]);
  });

  it("returns generic when the API fails", async () => {
    const { fetcher } = fakeFetch({ token: () => new Response("boom", { status: 500 }) });
    expect(await loadCardData(ADDRESS, fetcher, BASE)).toEqual({ kind: "generic" });
    expect(console.error).toHaveBeenCalled();
    const throwing = vi.fn(async () => {
      throw new Error("network down");
    }) as unknown as typeof fetch;
    expect(await loadCardData(ADDRESS, throwing, BASE)).toEqual({ kind: "generic" });
  });

  it("formats amounts from decimal strings and keeps USD null when unavailable", async () => {
    const ok = await loadCardData(ADDRESS, fakeFetch({}).fetcher, BASE);
    expect(ok).toMatchObject({
      kind: "token",
      symbol: "FIX",
      marketCapEth: "500",
      marketCapUsd: "$1,000,000.00",
      phase: "bonding",
      progressBps: 7143,
    });
    const noUsd = await loadCardData(
      ADDRESS,
      fakeFetch({ usd: () => new Response("x", { status: 503 }) }).fetcher,
      BASE,
    );
    expect(noUsd).toMatchObject({ kind: "token", marketCapUsd: null });
  });

  it("imageUrl points only at the API image route", async () => {
    const { fetcher, calls } = fakeFetch({});
    const card = await loadCardData(ADDRESS, fetcher, BASE);
    expect(card).toMatchObject({ kind: "token", imageUrl: `${BASE}/v1/tokens/${ADDRESS}/image` });
    for (const url of calls) expect(url.startsWith(`${BASE}/v1/`)).toBe(true);
    expect(calls.join(" ")).not.toContain("evil.example");
  });

  it("truncates names over 32 characters", async () => {
    const { fetcher } = fakeFetch({
      token: () => Response.json(token({ name: "N".repeat(80) })),
    });
    const card = await loadCardData(ADDRESS, fetcher, BASE);
    if (card.kind !== "token") throw new Error("expected a token card");
    expect(Array.from(card.name)).toHaveLength(MAX_NAME_LENGTH);
    expect(card.name.endsWith("…")).toBe(true);
  });

  it("imageUrl is null when the image request fails", async () => {
    const failed = await loadCardData(
      ADDRESS,
      fakeFetch({ image: () => new Response("", { status: 404 }) }).fetcher,
      BASE,
    );
    expect(failed).toMatchObject({ kind: "token", imageUrl: null, imageDataUri: null });
    const timedOut = await loadCardData(
      ADDRESS,
      fakeFetch({
        image: () => {
          throw new DOMException("timeout", "TimeoutError");
        },
      }).fetcher,
      BASE,
    );
    expect(timedOut).toMatchObject({ kind: "token", imageUrl: null });
  });

  it("rejects oversized and non-image bodies", async () => {
    const huge = new Response(new Uint8Array(MAX_IMAGE_BYTES + 1).fill(0x41));
    const tooBig = await loadCardData(ADDRESS, fakeFetch({ image: () => huge }).fetcher, BASE);
    expect(tooBig).toMatchObject({ kind: "token", imageUrl: null });
    const declared = new Response(PNG, {
      headers: { "content-length": String(MAX_IMAGE_BYTES + 1) },
    });
    expect(
      await loadCardData(ADDRESS, fakeFetch({ image: () => declared }).fetcher, BASE),
    ).toMatchObject({ imageUrl: null });
    const html = new Response("<html></html>");
    expect(
      await loadCardData(ADDRESS, fakeFetch({ image: () => html }).fetcher, BASE),
    ).toMatchObject({ imageUrl: null });
  });

  it("marks graduated tokens and strips control characters from text", async () => {
    const { fetcher } = fakeFetch({
      token: () =>
        Response.json(token({ phase: "graduated", name: "A\u0000B\nC", symbol: "<b>X</b>" })),
    });
    const card = await loadCardData(ADDRESS, fetcher, BASE);
    expect(card).toMatchObject({ phase: "graduated", name: "A B C", symbol: "<b>X</b>" });
  });
});
