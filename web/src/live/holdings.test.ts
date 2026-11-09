import { describe, expect, it } from "vitest";
import type { ApiEvent, LiveEvent, TradeFeedItem } from "@/api/types";
import {
  applyHoldingsResult,
  holdingsRefreshReason,
  loadHoldings,
  loadHoldingsResult,
  mergeWatched,
} from "./holdings";
import { createRuleState, evaluate } from "./notification-rules";

const TOKEN = "0x00000000000000000000000000000000000000Aa";
const WALLET = "0x1111111111111111111111111111111111111111";
const base = { chain_id: 1, deployment_id: "local" };
const BASE_URL = "http://api.test";

function trade(over: Partial<TradeFeedItem> = {}): ApiEvent {
  return {
    event: "trade",
    data: {
      ...base,
      token: TOKEN,
      name: "Name",
      symbol: "TKN",
      source: "curve",
      trader: "0x2222222222222222222222222222222222222222",
      side: "buy",
      eth_volume: "1000",
      token_volume: "1",
      block_number: 10,
      transaction_index: 0,
      tx_hash: "0xaaa",
      log_index: 0,
      time: "2026-01-01T00:00:00Z",
      finality: "observed",
      ...over,
    },
  };
}
const finality = (safe: number): ApiEvent => ({
  event: "finality",
  data: { ...base, safe_block: safe, finalized_block: 0 },
});

describe("mergeWatched", () => {
  it("lowercases, dedupes and sorts", () => {
    const out = mergeWatched(["0xBB", "0xaa"], ["0xAA", "0xcc"]);
    expect(out).toEqual(["0xaa", "0xbb", "0xcc"]);
  });
});

describe("holdings as watched tokens", () => {
  const watched = new Set(mergeWatched([], [TOKEN]));

  it("notifies a graduation of a held token", () => {
    const event: ApiEvent = {
      event: "graduation",
      data: {
        ...base,
        token: TOKEN,
        name: "N",
        symbol: "TKN",
        pair: "0x1",
        block_number: 5,
        tx_hash: "0xg",
      },
    };
    expect(evaluate(event, { watched }, createRuleState())).toHaveLength(1);
  });

  it("notifies a 0.5 ETH trade on a held token", () => {
    const out = evaluate(
      trade({ eth_volume: "500000000000000000" }),
      { watched },
      createRuleState(),
    );
    expect(out.map((n) => n.kind)).toEqual(["large_trade"]);
  });
});

describe("own dex trades", () => {
  it("raises own_trade_safe when a pending dex trade reaches safe", () => {
    const state = createRuleState();
    const ctx = { watched: new Set<string>(), wallet: WALLET };
    const dex = trade({ source: "dex", trader: WALLET.toUpperCase().replace("0X", "0x") });
    expect(evaluate(dex, ctx, state)).toEqual([]);
    expect(evaluate(finality(10), ctx, state).map((n) => n.kind)).toEqual(["own_trade_safe"]);
  });

  it("never matches a null trader", () => {
    const state = createRuleState();
    const ctx = { watched: new Set<string>(), wallet: WALLET };
    expect(evaluate(trade({ source: "dex", trader: null }), ctx, state)).toEqual([]);
    expect(state.pendingOwn.size).toBe(0);
  });
});

describe("holdingsRefreshReason", () => {
  it("returns own_trade_safe for an own dex trade that is already safe", () => {
    const event = trade({ source: "dex", trader: WALLET, finality: "safe" });
    expect(holdingsRefreshReason(event, WALLET, createRuleState())).toBe("own_trade_safe");
  });

  it("returns own_trade_safe when a finality event releases a pending own trade", () => {
    const state = createRuleState();
    evaluate(
      trade({ source: "dex", trader: WALLET }),
      { watched: new Set(), wallet: WALLET },
      state,
    );
    expect(holdingsRefreshReason(finality(10), WALLET, state)).toBe("own_trade_safe");
    expect(holdingsRefreshReason(finality(9), WALLET, state)).toBeNull();
  });

  it("returns own_trade_seen for an own trade that is not yet safe", () => {
    const event = trade({ trader: WALLET.toUpperCase().replace("0X", "0x") });
    expect(holdingsRefreshReason(event, WALLET, createRuleState())).toBe("own_trade_seen");
  });

  it("loads at most twice for one own trade: when seen and when safe, not on a replay", () => {
    const state = createRuleState();
    const ctx = { watched: new Set<string>(), wallet: WALLET };
    const reasons: Array<string | null> = [];
    // The provider's order: reason first, then evaluate (which records the pending own trade).
    const feed = (event: LiveEvent) => {
      reasons.push(holdingsRefreshReason(event, WALLET, state));
      if (event.event !== "reconnected") evaluate(event, ctx, state);
    };
    const own = trade({ trader: WALLET, block_number: 10 });
    feed(own);
    feed(own); // republished after a LISTEN reconnect: already pending, no second load
    feed(finality(9));
    feed(finality(10));
    feed(finality(11));
    expect(reasons.filter((reason) => reason !== null)).toEqual([
      "own_trade_seen",
      "own_trade_safe",
    ]);
  });

  it("returns own_trade_seen once for a burst that lists the wallet, never when truncated", () => {
    const burst = (traders: string[], truncated = false): LiveEvent => ({
      event: "trade_burst",
      data: {
        ...base,
        window_start: "a",
        window_end: "b",
        items: [
          {
            token: TOKEN,
            count: 2,
            max_eth_wei: "0",
            traders,
            traders_truncated: truncated,
          },
        ],
      },
    });
    expect(holdingsRefreshReason(burst([WALLET]), WALLET, createRuleState())).toBe(
      "own_trade_seen",
    );
    expect(holdingsRefreshReason(burst([WALLET], true), WALLET, createRuleState())).toBeNull();
    expect(
      holdingsRefreshReason(
        burst(["0x2222222222222222222222222222222222222222"]),
        WALLET,
        createRuleState(),
      ),
    ).toBeNull();
  });

  it("returns reconnected for a reconnect and null for unrelated events", () => {
    const reconnected: LiveEvent = { event: "reconnected" };
    expect(holdingsRefreshReason(reconnected, WALLET, createRuleState())).toBe("reconnected");
    expect(holdingsRefreshReason(trade(), WALLET, createRuleState())).toBeNull();
    expect(holdingsRefreshReason(reconnected, null, createRuleState())).toBeNull();
  });
});

describe("loadHoldings", () => {
  const json = (body: unknown, status = 200) =>
    (async () => new Response(JSON.stringify(body), { status })) as unknown as typeof fetch;

  it("returns lowercased token addresses", async () => {
    const calls: string[] = [];
    const fetcher = (async (input: RequestInfo | URL) => {
      calls.push(String(input));
      return new Response(JSON.stringify({ items: [{ token: TOKEN }] }), { status: 200 });
    }) as unknown as typeof fetch;
    expect(await loadHoldings(WALLET, fetcher, BASE_URL)).toEqual([TOKEN.toLowerCase()]);
    expect(calls[0]?.endsWith(`/v1/wallets/${WALLET}/positions?closed=false`)).toBe(true);
  });

  it("returns [] on a 500", async () => {
    expect(await loadHoldings(WALLET, json({}, 500), BASE_URL)).toEqual([]);
  });

  it("returns [] on a network error and a malformed body", async () => {
    const failing = (async () => {
      throw new Error("offline");
    }) as unknown as typeof fetch;
    expect(await loadHoldings(WALLET, failing, BASE_URL)).toEqual([]);
    expect(await loadHoldings(WALLET, json({ items: "nope" }), BASE_URL)).toEqual([]);
    expect(await loadHoldings(WALLET, json({ items: [{ token: 5 }] }), BASE_URL)).toEqual([]);
  });

  it("returns [] when the request outlives the timeout", async () => {
    const hanging = ((_: unknown, init?: RequestInit) =>
      new Promise((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => reject(new Error("aborted")));
      })) as unknown as typeof fetch;
    expect(await loadHoldings(WALLET, hanging, BASE_URL, 10)).toEqual([]);
  });
});

describe("loadHoldingsResult", () => {
  const reply = (body: unknown, status = 200) =>
    (async () => new Response(JSON.stringify(body), { status })) as unknown as typeof fetch;

  it("returns the list on success and null on every failure", async () => {
    expect(
      await loadHoldingsResult(WALLET, reply({ items: [{ token: TOKEN }] }), BASE_URL),
    ).toEqual([TOKEN.toLowerCase()]);
    expect(await loadHoldingsResult(WALLET, reply({ items: [] }), BASE_URL)).toEqual([]);
    expect(await loadHoldingsResult(WALLET, reply({}, 500), BASE_URL)).toBeNull();
    expect(await loadHoldingsResult(WALLET, reply({ items: "x" }), BASE_URL)).toBeNull();
    const failing = (async () => {
      throw new Error("offline");
    }) as unknown as typeof fetch;
    expect(await loadHoldingsResult(WALLET, failing, BASE_URL)).toBeNull();
    const hanging = ((_: unknown, init?: RequestInit) =>
      new Promise((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => reject(new Error("aborted")));
      })) as unknown as typeof fetch;
    expect(await loadHoldingsResult(WALLET, hanging, BASE_URL, 10)).toBeNull();
  });
});

describe("applyHoldingsResult", () => {
  it("keeps the previous holdings when a refresh failed", () => {
    const held = [TOKEN.toLowerCase()];
    const afterOk = applyHoldingsResult([], held);
    expect(afterOk).toEqual(held);
    expect(applyHoldingsResult(afterOk, null)).toBe(afterOk);
    expect(applyHoldingsResult(afterOk, [])).toEqual([]);
  });
});
