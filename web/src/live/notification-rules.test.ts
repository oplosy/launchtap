import { describe, expect, it } from "vitest";
import type { ApiEvent, TradeFeedItem } from "@/api/types";
import {
  BURST_TOKENS_MAX,
  LARGE_TRADE_WEI,
  PENDING_OWN_MAX,
  bellVisible,
  createRuleState,
  describeNotification,
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

describe("trade_burst", () => {
  const OTHER = "0x00000000000000000000000000000000000000bb";
  function burst(
    items: Array<{
      token?: string;
      count?: number;
      max_eth_wei?: string;
      traders?: string[];
      traders_truncated?: boolean;
    }>,
  ): ApiEvent {
    return {
      event: "trade_burst",
      data: {
        ...base,
        window_start: "2026-01-01T00:00:00Z",
        window_end: "2026-01-01T00:00:01Z",
        items: items.map((item) => ({
          token: TOKEN,
          count: 3,
          max_eth_wei: "0",
          traders: [],
          traders_truncated: false,
          ...item,
        })),
      },
    };
  }

  it("raises one large-trade notification per watched entry at or above the threshold", () => {
    const out = evaluate(
      burst([
        { max_eth_wei: LARGE_TRADE_WEI.toString(), count: 4 },
        { token: OTHER, max_eth_wei: "9".repeat(30) },
      ]),
      { watched },
      createRuleState(),
      9,
    );
    expect(out).toHaveLength(1);
    expect(out[0]).toMatchObject({
      kind: "large_trade",
      token: TOKEN,
      at: 9,
      burst: { count: 4, maxEthWei: LARGE_TRADE_WEI.toString() },
    });
    expect(describeNotification(out[0]!)).toBe("Large trade on Token: 4 trades, largest 0.5 ETH");
  });

  it("ignores entries below the threshold, with unknown or malformed max", () => {
    for (const max of ["499999999999999999", "0", "1e30", ""]) {
      expect(evaluate(burst([{ max_eth_wei: max }]), { watched }, createRuleState())).toEqual([]);
    }
  });

  it("raises an unconfirmed own-trade notification when traders lists the wallet", () => {
    const state = createRuleState();
    const out = evaluate(
      burst([{ token: OTHER, traders: ["0x2222222222222222222222222222222222222222", WALLET] }]),
      { watched: new Set(), wallet: WALLET.toUpperCase().replace("0X", "0x") },
      state,
    );
    expect(out).toHaveLength(1);
    expect(out[0]).toMatchObject({ kind: "own_trade_safe", token: OTHER, unconfirmed: true });
    expect(describeNotification(out[0]!)).toBe("Your Token trade was seen (not yet safe)");
    // A burst has no tx hash, so nothing is tracked toward finality.
    expect(state.pendingOwn.size).toBe(0);
  });

  it("does not raise own-trade when traders is truncated (unknown)", () => {
    const out = evaluate(
      burst([{ traders: [WALLET], traders_truncated: true }]),
      { watched: new Set(), wallet: WALLET },
      createRuleState(),
    );
    expect(out).toEqual([]);
  });

  it("does not raise own-trade without a wallet or when the wallet is absent", () => {
    expect(
      evaluate(burst([{ traders: [WALLET] }]), { watched: new Set() }, createRuleState()),
    ).toEqual([]);
    expect(
      evaluate(
        burst([{ traders: ["0x2222222222222222222222222222222222222222"] }]),
        { watched: new Set(), wallet: WALLET },
        createRuleState(),
      ),
    ).toEqual([]);
  });

  it("gives the same burst the same ids, so a replay does not double-notify", () => {
    const event = burst([{ max_eth_wei: LARGE_TRADE_WEI.toString(), traders: [WALLET] }]);
    const ctx = { watched, wallet: WALLET };
    const a = evaluate(event, ctx, createRuleState()).map((n) => n.id);
    const b = evaluate(event, ctx, createRuleState()).map((n) => n.id);
    expect(a).toHaveLength(2);
    expect(new Set(a).size).toBe(2);
    expect(b).toEqual(a);
  });

  it("tolerates a null items list", () => {
    const event: ApiEvent = {
      event: "trade_burst",
      data: { ...base, window_start: "a", window_end: "b", items: null },
    };
    expect(evaluate(event, { watched, wallet: WALLET }, createRuleState())).toEqual([]);
  });
});

describe("burst then replayed rows", () => {
  const LARGE = LARGE_TRADE_WEI.toString();
  function burstAt(start: string, end: string, item: Record<string, unknown>): ApiEvent {
    return {
      event: "trade_burst",
      data: {
        ...base,
        window_start: start,
        window_end: end,
        items: [
          {
            token: TOKEN,
            count: 3,
            max_eth_wei: "0",
            traders: [],
            traders_truncated: false,
            ...item,
          },
        ],
      },
    } as ApiEvent;
  }

  it("does not re-notify a large trade replayed individually inside the burst window", () => {
    const state = createRuleState();
    const ctx = { watched, wallet: WALLET };
    const burst = burstAt("2026-01-01T00:00:00Z", "2026-01-01T00:00:01Z", { max_eth_wei: LARGE });
    expect(evaluate(burst, ctx, state).map((n) => n.kind)).toEqual(["large_trade"]);
    const replay = trade({
      eth_volume: (LARGE_TRADE_WEI + 1n).toString(),
      time: "2026-01-01T00:00:00.500Z",
    });
    expect(evaluate(replay, ctx, state)).toEqual([]);
  });

  it("does not re-notify a replayed trade whose amount is the burst's largest", () => {
    const state = createRuleState();
    evaluate(
      burstAt("2026-01-01T00:00:10Z", "2026-01-01T00:00:11Z", { max_eth_wei: LARGE }),
      { watched },
      state,
    );
    // Block time precedes the (wall-clock) publish window, but the amount identifies it.
    const replay = trade({ eth_volume: LARGE, time: "2026-01-01T00:00:05Z" });
    expect(evaluate(replay, { watched }, state)).toEqual([]);
  });

  it("still notifies a different large trade outside the window, and keeps own-trade safe", () => {
    const state = createRuleState();
    const ctx = { watched, wallet: WALLET };
    evaluate(
      burstAt("2026-01-01T00:00:00Z", "2026-01-01T00:00:01Z", { max_eth_wei: LARGE }),
      ctx,
      state,
    );
    const later = trade({
      eth_volume: (LARGE_TRADE_WEI * 3n).toString(),
      time: "2026-01-01T00:05:00Z",
      tx_hash: "0xnew",
    });
    expect(evaluate(later, ctx, state).map((n) => n.kind)).toEqual(["large_trade"]);
    const ownInside = trade({
      trader: WALLET,
      finality: "safe",
      eth_volume: LARGE,
      time: "2026-01-01T00:00:00.500Z",
      tx_hash: "0xown",
    });
    expect(evaluate(ownInside, ctx, state).map((n) => n.kind)).toEqual(["own_trade_safe"]);
  });

  it("does not re-notify a replay re-folded into a different burst window", () => {
    const state = createRuleState();
    const ctx = { watched };
    const first = burstAt("2026-01-01T00:00:00Z", "2026-01-01T00:00:01Z", {
      max_eth_wei: LARGE,
    });
    const refolded = burstAt("2026-01-01T00:00:30Z", "2026-01-01T00:00:31Z", {
      max_eth_wei: LARGE,
    });
    expect(evaluate(first, ctx, state)).toHaveLength(1);
    expect(evaluate(refolded, ctx, state)).toEqual([]);
    const bigger = burstAt("2026-01-01T00:01:00Z", "2026-01-01T00:01:01Z", {
      max_eth_wei: (LARGE_TRADE_WEI * 2n).toString(),
    });
    expect(evaluate(bigger, ctx, state)).toHaveLength(1);
  });

  it("bounds the recorded burst windows", () => {
    const state = createRuleState();
    for (let i = 0; i < BURST_TOKENS_MAX + 10; i++) {
      evaluate(
        burstAt("2026-01-01T00:00:00Z", "2026-01-01T00:00:01Z", {
          token: `0x${i.toString(16).padStart(40, "0")}`,
          max_eth_wei: LARGE,
        }),
        { watched },
        state,
      );
    }
    expect(state.burstWindows.size).toBe(BURST_TOKENS_MAX);
  });
});

describe("describeNotification", () => {
  it("keeps the single-event wording", () => {
    const n = { id: "x", token: TOKEN, symbol: "TKN", txHash: "0x1", at: 0 };
    expect(describeNotification({ ...n, kind: "graduation" })).toBe("TKN graduated");
    expect(describeNotification({ ...n, kind: "large_trade" })).toBe("Large trade on TKN");
    expect(describeNotification({ ...n, kind: "own_trade_safe" })).toBe("Your TKN trade is safe");
  });
});
