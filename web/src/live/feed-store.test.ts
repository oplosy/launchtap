import { describe, expect, it } from "vitest";
import type { ApiEvent, TradeFeedItem } from "@/api/types";
import { applyFeedEvent, mergeRecent, tradeToFeedItem, type FeedItem } from "./feed-store";

function trade(n: number, over: Partial<TradeFeedItem> = {}): TradeFeedItem {
  return {
    chain_id: 1,
    deployment_id: "local",
    token: "0xAbC0000000000000000000000000000000000001",
    name: "Coin",
    symbol: "C",
    source: "curve",
    trader: null,
    side: "buy",
    eth_volume: "1000000000000000000",
    token_volume: "5",
    block_number: n,
    transaction_index: 0,
    tx_hash: `0x${n.toString(16).padStart(64, "0")}`,
    log_index: 0,
    time: "2026-01-01T00:00:00Z",
    finality: "observed",
    ...over,
  };
}
const tradeEvent = (t: TradeFeedItem): ApiEvent => ({ event: "trade", data: t });

describe("feed store", () => {
  it("applies trade events newest first and caps at 50", () => {
    let items: FeedItem[] = [];
    for (let n = 1; n <= 60; n++) items = applyFeedEvent(items, tradeEvent(trade(n)));
    expect(items).toHaveLength(50);
    expect(items[0]?.block_number).toBe(60);
    expect(items[49]?.block_number).toBe(11);
  });

  it("drops duplicate tx_hash:log_index", () => {
    const t = trade(1);
    let items = applyFeedEvent([], tradeEvent(t));
    items = applyFeedEvent(items, tradeEvent({ ...t }));
    expect(items).toHaveLength(1);
    expect(items[0]?.key).toBe(`${t.tx_hash}:0`);
  });

  it("reorg removes items above the common ancestor", () => {
    let items: FeedItem[] = [];
    for (const n of [1, 2, 3]) items = applyFeedEvent(items, tradeEvent(trade(n)));
    items = applyFeedEvent(items, {
      event: "reorg",
      data: { chain_id: 1, deployment_id: "local", common_ancestor: 2 },
    });
    expect(items.map((i) => i.block_number)).toEqual([2, 1]);
  });

  it("finality upgrades observed items at or below safe_block", () => {
    let items: FeedItem[] = [];
    for (const n of [1, 5, 9]) items = applyFeedEvent(items, tradeEvent(trade(n)));
    items = applyFeedEvent(items, {
      event: "finality",
      data: { chain_id: 1, deployment_id: "local", safe_block: 5, finalized_block: 1 },
    });
    const by = Object.fromEntries(items.map((i) => [i.block_number, i.finality]));
    expect(by).toEqual({ 1: "finalized", 5: "safe", 9: "observed" });
  });

  it("builds graduation and launch items observed, keyed by kind", () => {
    const base = { chain_id: 1, deployment_id: "local", name: "N", symbol: "S" };
    let items = applyFeedEvent([], {
      event: "graduation",
      data: { ...base, block_number: 4, pair: "0xp", token: "0xT", tx_hash: "0xh" },
    });
    items = applyFeedEvent(items, {
      event: "new_token",
      data: { ...base, block_number: 3, creator: "0xc", token: "0xT", tx_hash: "0xh" },
    });
    expect(items.map((i) => [i.kind, i.key, i.finality])).toEqual([
      ["launch", "launch:0xh:0xt", "observed"],
      ["graduation", "graduation:0xh:0xt", "observed"],
    ]);
  });

  it("mergeRecent after reconnect keeps one copy of each item", () => {
    const live = [3, 2].map((n) => tradeToFeedItem(trade(n)));
    const recent = [4, 3, 2, 1].map((n) =>
      tradeToFeedItem(trade(n, n === 2 ? { finality: "safe" } : {})),
    );
    const merged = mergeRecent(live, recent);
    expect(merged.map((i) => i.block_number)).toEqual([4, 3, 2, 1]);
    expect(new Set(merged.map((i) => i.key)).size).toBe(4);
    expect(merged.find((i) => i.block_number === 2)?.finality).toBe("safe");
    expect(mergeRecent(merged, recent, 2)).toHaveLength(2);
  });
});
