import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FETCH_TIMEOUT_MS } from "./card-data";
import { loadWalletCardData } from "./wallet-card-data";

const ADDRESS = "0x00000000000000000000000000000000000000AB";
const BASE = "http://api:8080";
const LOWER = ADDRESS.toLowerCase();

function overview(over: Record<string, unknown> = {}) {
  return {
    address: LOWER,
    realized_pnl_wei: "-250000000000000000",
    portfolio_value_wei: "2000000000000000000",
    ...over,
  };
}

function fakeFetch(handlers: { wallet?: () => Response; positions?: () => Response }) {
  const calls: string[] = [];
  const fetcher = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    calls.push(url);
    if (url.includes("/positions"))
      return (handlers.positions ?? (() => Response.json({ items: [] })))();
    return (handlers.wallet ?? (() => Response.json(overview())))();
  }) as unknown as typeof fetch;
  return { fetcher, calls };
}

describe("loadWalletCardData", () => {
  beforeEach(() => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("returns generic for an invalid address without calling the API", async () => {
    const { fetcher, calls } = fakeFetch({});
    expect(await loadWalletCardData("nope", fetcher)).toEqual({ kind: "generic" });
    expect(calls).toEqual([]);
  });

  it("returns generic, marked as an API failure, on a non-200 overview response", async () => {
    const { fetcher } = fakeFetch({ wallet: () => new Response("x", { status: 500 }) });
    expect(await loadWalletCardData(ADDRESS, fetcher, BASE)).toEqual({
      kind: "generic",
      apiFailure: true,
    });
  });

  it("keeps the wallet card with no symbols when only positions fail", async () => {
    for (const positions of [
      () => new Response("x", { status: 500 }),
      () => {
        throw new TypeError("fetch failed");
      },
      () => Response.json({ items: "nope" }),
    ]) {
      const { fetcher } = fakeFetch({ positions });
      expect(await loadWalletCardData(ADDRESS, fetcher, BASE)).toEqual({
        kind: "wallet",
        address: LOWER,
        realizedPnlWei: "-250000000000000000",
        portfolioValueWei: "2000000000000000000",
        topSymbols: [],
      });
    }
  });

  it("returns generic when the fetch exceeds the 3 s timeout", async () => {
    vi.useFakeTimers();
    const hanging = vi.fn(
      (_input: RequestInfo | URL, init?: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => reject(new Error("aborted")));
        }),
    ) as unknown as typeof fetch;
    const pending = loadWalletCardData(ADDRESS, hanging, BASE);
    await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS + 1);
    expect(await pending).toEqual({ kind: "generic", apiFailure: true });
  });

  it("returns generic when an amount is not a canonical decimal string", async () => {
    const { fetcher } = fakeFetch({
      wallet: () => Response.json(overview({ realized_pnl_wei: "1.5" })),
    });
    expect(await loadWalletCardData(ADDRESS, fetcher, BASE)).toEqual({
      kind: "generic",
      apiFailure: true,
    });
  });

  it("cleans symbols (emoji and CJK dropped) and keeps at most three", async () => {
    const symbols = ["AAA", "🚀🚀", "漢字", "BBB", "CCC", "DDD"];
    const { fetcher } = fakeFetch({
      positions: () => Response.json({ items: symbols.map((symbol) => ({ symbol })) }),
    });
    const card = await loadWalletCardData(ADDRESS, fetcher, BASE);
    expect(card).toEqual({
      kind: "wallet",
      address: LOWER,
      realizedPnlWei: "-250000000000000000",
      portfolioValueWei: "2000000000000000000",
      topSymbols: ["AAA", "BBB", "CCC"],
    });
  });

  it("requests open positions only", async () => {
    const { fetcher, calls } = fakeFetch({});
    await loadWalletCardData(ADDRESS, fetcher, BASE);
    expect(calls.some((url) => url.includes(`/v1/wallets/${LOWER}/positions?closed=false`))).toBe(
      true,
    );
  });

  it("returns a wallet card with zero values for an empty wallet", async () => {
    const { fetcher } = fakeFetch({
      wallet: () => Response.json(overview({ realized_pnl_wei: "0", portfolio_value_wei: "0" })),
      positions: () => Response.json({ items: null }),
    });
    expect(await loadWalletCardData(ADDRESS, fetcher, BASE)).toEqual({
      kind: "wallet",
      address: LOWER,
      realizedPnlWei: "0",
      portfolioValueWei: "0",
      topSymbols: [],
    });
  });
});
