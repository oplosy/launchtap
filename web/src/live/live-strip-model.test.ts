import { describe, expect, it } from "vitest";
import type { LiveEvent, TradeFeedItem } from "@/api/types";
import { mergeRecent, tradeToFeedItem, type FeedItem } from "./feed-store";
import {
  liveRefreshKind,
  liveTradeRows,
  reduceStripFeed,
  reduceStripState,
  reduceTokenTrades,
  relativeTime,
  toStripItem,
} from "./live-strip-model";

const scope = { chainId: 1, deploymentId: "local" };
const TOKEN = "0xAbC0000000000000000000000000000000000001";
const OTHER = "0xAbC0000000000000000000000000000000000002";
const NOW = Date.parse("2026-01-01T00:10:00Z");

function trade(n: number, over: Partial<TradeFeedItem> = {}): TradeFeedItem {
  return {
    chain_id: 1,
    deployment_id: "local",
    token: TOKEN,
    name: "Coin",
    symbol: "C",
    source: "curve",
    trader: "0x1111111111111111111111111111111111111111",
    side: "buy",
    eth_volume: "1500000000000000000",
    token_volume: "5",
    block_number: n,
    transaction_index: 0,
    tx_hash: `0x${n.toString(16).padStart(64, "0")}`,
    log_index: 0,
    time: "2026-01-01T00:09:00Z",
    finality: "observed",
    ...over,
  };
}
function first(items: FeedItem[]): FeedItem {
  const [head] = items;
  if (!head) throw new Error("expected at least one item");
  return head;
}
const ev = (t: TradeFeedItem): LiveEvent => ({ event: "trade", data: t });

describe("live strip model", () => {
  it("renders recent trades from the initial fetch", () => {
    const items = mergeRecent([], [trade(2), trade(1)].map(tradeToFeedItem));
    const views = items.map((item) => toStripItem(item, NOW));
    expect(views.map((v) => v.ticker)).toEqual(["C", "C"]);
    expect(views[0]).toMatchObject({
      arrow: "▲",
      side: "buy",
      eth: "1.5 ETH",
      time: "1m",
      href: `/token/${TOKEN.toLowerCase()}`,
      badge: null,
    });
  });

  it("prepends a live trade", () => {
    const first = reduceStripFeed([], ev(trade(1)), scope);
    const next = reduceStripFeed(first, ev(trade(2)), scope);
    expect(next.map((i) => i.block_number)).toEqual([2, 1]);
  });

  it("drops events from another chain or deployment", () => {
    expect(reduceStripFeed([], ev(trade(1, { chain_id: 9 })), scope)).toEqual([]);
    expect(reduceStripFeed([], ev(trade(1, { deployment_id: "x" })), scope)).toEqual([]);
  });

  it("links a valid trader to its wallet page and nothing else", () => {
    const linked = toStripItem(
      tradeToFeedItem(trade(1, { trader: "0xAbCdEf0000000000000000000000000000000001" })),
      NOW,
    );
    expect(linked.traderHref).toBe("/wallet/0xabcdef0000000000000000000000000000000001");
    expect(toStripItem(tradeToFeedItem(trade(1, { trader: null })), NOW).traderHref).toBeNull();
  });

  it("shows a dash for a null trader", () => {
    const view = toStripItem(tradeToFeedItem(trade(1, { trader: null })), NOW);
    expect(view.trader).toBe("—");
  });

  it("shortens a trader address and marks a sell", () => {
    const view = toStripItem(tradeToFeedItem(trade(1, { side: "sell" })), NOW);
    expect(view.arrow).toBe("▼");
    expect(view.trader).toBe("0x1111…1111");
  });

  it("keeps a 64-character emoji name whole for CSS ellipsis truncation", () => {
    const name = "🚀".repeat(64);
    const view = toStripItem(tradeToFeedItem(trade(1, { name })), NOW);
    expect(view.name).toBe(name);
    expect(Array.from(view.name)).toHaveLength(64);
  });

  it("marks observed items pending until finality", () => {
    let items: FeedItem[] = reduceStripFeed([], ev(trade(5)), scope);
    expect(toStripItem(first(items), NOW).pending).toBe(true);
    items = reduceStripFeed(
      items,
      {
        event: "finality",
        data: { chain_id: 1, deployment_id: "local", safe_block: 5, finalized_block: 5 },
      } as LiveEvent,
      scope,
    );
    expect(toStripItem(first(items), NOW).pending).toBe(false);
  });

  it("badges launch and graduation items", () => {
    let items = reduceStripFeed(
      [],
      {
        event: "new_token",
        data: {
          chain_id: 1,
          deployment_id: "local",
          token: TOKEN,
          name: "N",
          symbol: "N",
          creator: OTHER,
          block_number: 3,
          tx_hash: "0xaa",
        },
      } as LiveEvent,
      scope,
    );
    items = reduceStripFeed(
      items,
      {
        event: "graduation",
        data: {
          chain_id: 1,
          deployment_id: "local",
          token: OTHER,
          name: "G",
          symbol: "G",
          pair: TOKEN,
          block_number: 4,
          tx_hash: "0xbb",
        },
      } as LiveEvent,
      scope,
    );
    expect(items.map((i) => toStripItem(i, NOW).badge)).toEqual(["Graduated", "Launch"]);
  });

  it("removes items above the reorg ancestor", () => {
    let items = reduceStripFeed([], ev(trade(1)), scope);
    items = reduceStripFeed(items, ev(trade(2)), scope);
    items = reduceStripFeed(
      items,
      {
        event: "reorg",
        data: { chain_id: 1, deployment_id: "local", common_ancestor: 1 },
      } as LiveEvent,
      scope,
    );
    expect(items.map((i) => i.block_number)).toEqual([1]);
  });

  it("formats relative time", () => {
    expect(relativeTime(undefined, NOW)).toBe("now");
    expect(relativeTime("2026-01-01T00:09:58Z", NOW)).toBe("now");
    expect(relativeTime("2026-01-01T00:09:30Z", NOW)).toBe("30s");
    expect(relativeTime("2026-01-01T07:00:00Z", Date.parse("2026-01-01T10:00:00Z"))).toBe("3h");
    expect(relativeTime("garbage", NOW)).toBe("—");
  });
});

describe("launch and graduation age", () => {
  const launch = (token: string): LiveEvent =>
    ({
      event: "new_token",
      data: {
        chain_id: 1,
        deployment_id: "local",
        token,
        name: "N",
        symbol: "N",
        creator: OTHER,
        block_number: 3,
        tx_hash: "0xaa",
      },
    }) as LiveEvent;

  it("ages from the client receive time", () => {
    const t0 = Date.parse("2026-01-01T00:00:00Z");
    let state = reduceStripState({ items: [], receivedAt: {} }, launch(TOKEN), scope, t0);
    const item = first(state.items);
    expect(toStripItem(item, t0, state.receivedAt).time).toBe("now");
    expect(toStripItem(item, t0 + 125_000, state.receivedAt).time).toBe("2m");
    // A later event keeps the original receive time.
    state = reduceStripState(state, ev(trade(9)), scope, t0 + 60_000);
    expect(
      toStripItem(
        first(state.items.filter((i) => i.kind === "launch")),
        t0 + 125_000,
        state.receivedAt,
      ).time,
    ).toBe("2m");
  });
});

describe("token page live trades", () => {
  it("keeps only live trades newer than REST so a reload yields the same order", () => {
    let live: FeedItem[] = [];
    for (let n = 1; n <= 30; n += 1) live = reduceTokenTrades(live, ev(trade(n)), TOKEN, scope);
    // REST page 1 holds the newest 25 rows (blocks 6..30) except one the stream has not seen.
    const rest = Array.from({ length: 25 }, (_, i) => ({
      ...trade(30 - i),
      execution_price: "1",
      spot_price: "1",
    }));
    expect(liveTradeRows(live, rest)).toEqual([]);
    // A live trade newer than every REST row stays on top; older ones are dropped.
    const newer = reduceTokenTrades(live, ev(trade(31)), TOKEN, scope);
    const rows = liveTradeRows(newer, rest);
    expect(rows.map((r) => r.block_number)).toEqual([31]);
    const combined = [...rows, ...rest].map((r) => r.block_number);
    expect(combined).toEqual([...combined].sort((a, b) => b - a));
  });

  it("orders by transaction and log index inside one block", () => {
    const live = reduceTokenTrades(
      [],
      ev(trade(5, { transaction_index: 2, log_index: 1, tx_hash: "0x" + "ab".repeat(32) })),
      TOKEN,
      scope,
    );
    const older = {
      ...trade(5, { transaction_index: 2, log_index: 0 }),
      execution_price: "1",
      spot_price: "1",
    };
    const newerRest = {
      ...trade(5, { transaction_index: 3, log_index: 0 }),
      execution_price: "1",
      spot_price: "1",
    };
    expect(liveTradeRows(live, [older])).toHaveLength(1);
    expect(liveTradeRows(live, [newerRest])).toHaveLength(0);
  });

  it("prepends a trade event for this token (case-insensitive) without refetching", () => {
    const lower = TOKEN.toLowerCase();
    let items: FeedItem[] = [];
    items = reduceTokenTrades(items, ev(trade(1)), lower, scope);
    items = reduceTokenTrades(items, ev(trade(2)), lower, scope);
    expect(items.map((i) => i.block_number)).toEqual([2, 1]);
  });

  it("ignores trade events for other tokens", () => {
    const items = reduceTokenTrades([], ev(trade(1, { token: OTHER })), TOKEN, scope);
    expect(items).toEqual([]);
  });

  it("ignores graduation events", () => {
    const items = reduceTokenTrades(
      [],
      {
        event: "graduation",
        data: {
          chain_id: 1,
          deployment_id: "local",
          token: TOKEN,
          name: "G",
          symbol: "G",
          pair: OTHER,
          block_number: 4,
          tx_hash: "0xbb",
        },
      } as LiveEvent,
      TOKEN,
      scope,
    );
    expect(items).toEqual([]);
  });

  it("drops live rows already covered by REST", () => {
    const items = reduceTokenTrades([], ev(trade(1)), TOKEN, scope);
    const rest = [{ ...trade(1), execution_price: "1", spot_price: "1" }];
    expect(liveTradeRows(items, rest)).toEqual([]);
    expect(liveTradeRows(items, [])).toHaveLength(1);
  });

  it("prices a live row from the trade's own amounts and leaves spot unknown", () => {
    const items = reduceTokenTrades(
      [],
      ev(trade(1, { eth_volume: "1500000000000000000", token_volume: "5000000000000000000" })),
      TOKEN,
      scope,
    );
    expect(liveTradeRows(items, [])[0]).toMatchObject({
      execution_price: "300000000000000000",
      spot_price: "",
    });
  });

  it("gives each strip item its token image path, or null for an invalid token", () => {
    expect(toStripItem(tradeToFeedItem(trade(1)), NOW).imagePath).toBe(
      `/v1/tokens/${TOKEN.toLowerCase()}/image`,
    );
    expect(toStripItem(tradeToFeedItem(trade(1, { token: "nope" })), NOW).imagePath).toBeNull();
  });
  it("decides which live events refresh REST-backed views", () => {
    const reorg = (over: object = {}) =>
      ({
        event: "reorg",
        data: { chain_id: 1, deployment_id: "local", common_ancestor: 1, ...over },
      }) as LiveEvent;
    expect(liveRefreshKind(reorg(), scope)).toBe("reorg");
    expect(liveRefreshKind(reorg({ chain_id: 2 }), scope)).toBeNull();
    expect(liveRefreshKind(reorg({ deployment_id: "other" }), scope)).toBeNull();
    expect(liveRefreshKind({ event: "reconnected" }, scope)).toBe("reconnected");
    expect(liveRefreshKind(ev(trade(1)), scope)).toBeNull();
  });

  it("shows the replacement trade after a reorg once REST is reloaded", () => {
    // Before the reload REST still holds the orphaned block-2 row and hides a replacement at 2.
    const staleRest = [
      { ...trade(2, { tx_hash: "0xold" }), execution_price: "1", spot_price: "1" },
    ];
    const replacement = reduceTokenTrades([], ev(trade(2, { tx_hash: "0xnew" })), TOKEN, scope);
    expect(liveTradeRows(replacement, staleRest)).toEqual([]);
    // After the reload REST no longer has 0xold; the replacement is covered by REST or shown live.
    const freshRest = [
      { ...trade(2, { tx_hash: "0xnew" }), execution_price: "1", spot_price: "1" },
    ];
    expect(liveTradeRows(replacement, freshRest)).toEqual([]);
    expect(liveTradeRows(replacement, [])).toHaveLength(1);
    // Live rows above the ancestor are pruned by the reorg event itself.
    const pruned = reduceTokenTrades(
      replacement,
      {
        event: "reorg",
        data: { chain_id: 1, deployment_id: "local", common_ancestor: 1 },
      } as LiveEvent,
      TOKEN,
      scope,
    );
    expect(liveTradeRows(pruned, [])).toEqual([]);
  });
});
