import { describe, expect, it } from "vitest";
import type { ApiEvent, TradeFeedItem } from "@/api/types";
import {
  LARGE_TRADE_WEI,
  PENDING_OWN_MAX,
  bellVisible,
  createRuleState,
  evaluate,
  identityChanged,
} from "./notification-rules";

const TOKEN = "0x00000000000000000000000000000000000000Aa";
const WALLET = "0x1111111111111111111111111111111111111111";
const base = { chain_id: 1, deployment_id: "local" };

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
const watched = new Set([TOKEN.toLowerCase()]);

describe("evaluate", () => {
  it("notifies when a watched token graduates", () => {
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
    const out = evaluate(event, { watched }, createRuleState(), 7);
    expect(out).toHaveLength(1);
    expect(out[0]).toMatchObject({ kind: "graduation", symbol: "TKN", at: 7 });
    expect(evaluate(event, { watched: new Set() }, createRuleState())).toEqual([]);
  });

  it("notifies a watched-token trade at exactly 0.5 ETH", () => {
    const out = evaluate(
      trade({ eth_volume: LARGE_TRADE_WEI.toString() }),
      { watched },
      createRuleState(),
    );
    expect(out.map((n) => n.kind)).toEqual(["large_trade"]);
  });

  it("does not notify at 0.499999999999999999 ETH", () => {
    const out = evaluate(
      trade({ eth_volume: "499999999999999999" }),
      { watched },
      createRuleState(),
    );
    expect(out).toEqual([]);
  });

  it("ignores malformed amounts", () => {
    expect(evaluate(trade({ eth_volume: "1e30" }), { watched }, createRuleState())).toEqual([]);
  });

  it("ignores trades on unwatched tokens", () => {
    const out = evaluate(
      trade({ eth_volume: "9".repeat(30) }),
      { watched: new Set() },
      createRuleState(),
    );
    expect(out).toEqual([]);
  });

  it("notifies own trade once when finality covers its block", () => {
    const state = createRuleState();
    const ctx = { watched: new Set<string>(), wallet: WALLET };
    expect(evaluate(trade({ trader: WALLET, block_number: 10 }), ctx, state)).toEqual([]);
    expect(evaluate(finality(9), ctx, state)).toEqual([]);
    const out = evaluate(finality(10), ctx, state);
    expect(out).toHaveLength(1);
    expect(out[0]?.kind).toBe("own_trade_safe");
    expect(evaluate(finality(11), ctx, state)).toEqual([]);
  });

  it("does not notify own trade before finality", () => {
    const state = createRuleState();
    const out = evaluate(trade({ trader: WALLET }), { watched: new Set(), wallet: WALLET }, state);
    expect(out).toEqual([]);
    expect(state.pendingOwn.size).toBe(1);
  });

  it("null trader never matches own trade", () => {
    const state = createRuleState();
    evaluate(trade({ trader: null }), { watched: new Set(), wallet: WALLET }, state);
    expect(state.pendingOwn.size).toBe(0);
    evaluate(trade({ trader: null }), { watched: new Set(), wallet: undefined }, state);
    expect(state.pendingOwn.size).toBe(0);
  });

  it("address comparison is case-insensitive", () => {
    const state = createRuleState();
    const checksummed = "0xAbCdEf0123456789aBcDeF0123456789AbCdEf01";
    evaluate(
      trade({ trader: checksummed }),
      { watched: new Set(), wallet: checksummed.toLowerCase() },
      state,
    );
    expect(state.pendingOwn.size).toBe(1);
    const grad: ApiEvent = {
      event: "graduation",
      data: {
        ...base,
        token: TOKEN,
        name: "N",
        symbol: "T",
        pair: "0x1",
        block_number: 5,
        tx_hash: "0xg",
      },
    };
    expect(evaluate(grad, { watched }, createRuleState())).toHaveLength(1);
  });

  it("drops pending own trades above the common ancestor on reorg", () => {
    const state = createRuleState();
    const ctx = { watched: new Set<string>(), wallet: WALLET };
    evaluate(trade({ trader: WALLET, block_number: 10, tx_hash: "0x1" }), ctx, state);
    evaluate(trade({ trader: WALLET, block_number: 20, tx_hash: "0x2" }), ctx, state);
    evaluate({ event: "reorg", data: { ...base, common_ancestor: 15 } }, ctx, state);
    expect([...state.pendingOwn.values()].map((t) => t.tx_hash)).toEqual(["0x1"]);
  });

  it("caps pending own trades, evicting the oldest", () => {
    const state = createRuleState();
    const ctx = { watched: new Set<string>(), wallet: WALLET };
    for (let i = 0; i < PENDING_OWN_MAX + 5; i++) {
      evaluate(trade({ trader: WALLET, tx_hash: `0x${i}` }), ctx, state);
    }
    expect(state.pendingOwn.size).toBe(PENDING_OWN_MAX);
    expect([...state.pendingOwn.values()][0]?.tx_hash).toBe("0x5");
  });
});

describe("bellVisible", () => {
  it("hides the bell when neither signed in nor connected", () => {
    expect(bellVisible({ signedIn: false, connected: false })).toBe(false);
    expect(bellVisible({ signedIn: true, connected: false })).toBe(true);
    expect(bellVisible({ signedIn: false, connected: true })).toBe(true);
  });
});

describe("already-safe own trade", () => {
  it("notifies immediately and only once when a later finality also covers it", () => {
    const state = createRuleState();
    const ctx = { watched: new Set<string>(), wallet: WALLET };
    const first = evaluate(trade({ trader: WALLET, finality: "safe" }), ctx, state);
    expect(first.map((n) => n.kind)).toEqual(["own_trade_safe"]);
    expect(state.pendingOwn.size).toBe(0);
    expect(evaluate(finality(50), ctx, state)).toEqual([]);
  });
});

describe("identityChanged", () => {
  it("detects wallet change (case-insensitive), disconnect and sign-out", () => {
    const base = { signedIn: true, wallet: WALLET };
    expect(
      identityChanged(base, { ...base, wallet: WALLET.toUpperCase().replace("0X", "0x") }),
    ).toBe(false);
    expect(identityChanged(base, { signedIn: true })).toBe(true);
    expect(identityChanged(base, { signedIn: false, wallet: WALLET })).toBe(true);
    expect(
      identityChanged(base, { ...base, wallet: "0x3333333333333333333333333333333333333333" }),
    ).toBe(true);
  });
});
