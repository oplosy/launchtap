import type { ApiEvent, TradeFeedItem } from "@/api/types";

export type FeedFinality = "observed" | "safe" | "finalized";

type FeedBase = {
  /** Stable identity used for de-duplication. */
  key: string;
  block_number: number;
  tx_hash: string;
  token: string;
  name: string;
  symbol: string;
  finality: FeedFinality;
};

export type TradeFeedEntry = FeedBase & { kind: "trade" } & Omit<
    TradeFeedItem,
    "block_number" | "tx_hash" | "token" | "name" | "symbol" | "finality"
  >;
export type GraduationFeedEntry = FeedBase & { kind: "graduation"; pair: string };
export type LaunchFeedEntry = FeedBase & { kind: "launch"; creator: string };

export type FeedItem = TradeFeedEntry | GraduationFeedEntry | LaunchFeedEntry;

export const FEED_MAX_ITEMS = 50;

const FINALITY_RANK: Record<FeedFinality, number> = { observed: 0, safe: 1, finalized: 2 };

function toFinality(value: string): FeedFinality {
  return value === "safe" || value === "finalized" ? value : "observed";
}

/** Converts a trade payload (SSE `trade` or a REST recent-trades item) into a feed item. */
export function tradeToFeedItem(trade: TradeFeedItem): TradeFeedEntry {
  return {
    ...trade,
    kind: "trade",
    key: `${trade.tx_hash}:${trade.log_index}`,
    finality: toFinality(trade.finality),
  };
}

function insertFirst(items: FeedItem[], item: FeedItem, max: number): FeedItem[] {
  const id = item.key.toLowerCase();
  if (items.some((existing) => existing.key.toLowerCase() === id)) return items;
  return [item, ...items].slice(0, max);
}

/** Pure reducer applying one stream event to the feed. Amounts stay decimal strings. */
export function applyFeedEvent(
  items: FeedItem[],
  event: ApiEvent,
  max: number = FEED_MAX_ITEMS,
): FeedItem[] {
  switch (event.event) {
    case "trade":
      return insertFirst(items, tradeToFeedItem(event.data), max);
    case "graduation":
      return insertFirst(
        items,
        {
          kind: "graduation",
          key: `graduation:${event.data.tx_hash}:${event.data.token.toLowerCase()}`,
          block_number: event.data.block_number,
          tx_hash: event.data.tx_hash,
          token: event.data.token,
          name: event.data.name,
          symbol: event.data.symbol,
          pair: event.data.pair,
          finality: "observed",
        },
        max,
      );
    case "new_token":
      return insertFirst(
        items,
        {
          kind: "launch",
          key: `launch:${event.data.tx_hash}:${event.data.token.toLowerCase()}`,
          block_number: event.data.block_number,
          tx_hash: event.data.tx_hash,
          token: event.data.token,
          name: event.data.name,
          symbol: event.data.symbol,
          creator: event.data.creator,
          finality: "observed",
        },
        max,
      );
    case "reorg": {
      const kept = items.filter((item) => item.block_number <= event.data.common_ancestor);
      return kept.length === items.length ? items : kept;
    }
    case "finality": {
      const { safe_block: safe, finalized_block: finalized } = event.data;
      let changed = false;
      const next = items.map((item) => {
        const target: FeedFinality =
          item.block_number <= finalized
            ? "finalized"
            : item.block_number <= safe
              ? "safe"
              : "observed";
        if (FINALITY_RANK[target] <= FINALITY_RANK[item.finality]) return item;
        changed = true;
        return { ...item, finality: target };
      });
      return changed ? next : items;
    }
    default:
      return items;
  }
}

function chainOrder(a: FeedItem, b: FeedItem) {
  if (a.block_number !== b.block_number) return b.block_number - a.block_number;
  if (a.kind === "trade" && b.kind === "trade") {
    if (a.transaction_index !== b.transaction_index) {
      return b.transaction_index - a.transaction_index;
    }
    return b.log_index - a.log_index;
  }
  return 0;
}

/**
 * Merges a REST recent-trades snapshot into the feed after a reconnect. REST is canonical for
 * every block up to the newest block in `recent`: live items at or below it are replaced by the
 * snapshot (so a trade reorged out during the outage disappears) and only newer live trades
 * are kept; live launch and graduation items are kept because REST does not carry them. One copy per key (higher finality wins), newest first by chain position, capped.
 */
export function mergeRecent(
  items: FeedItem[],
  recent: FeedItem[],
  max: number = FEED_MAX_ITEMS,
): FeedItem[] {
  const newest = recent.reduce((top, item) => Math.max(top, item.block_number), -1);
  // REST only returns trades, so live launch/graduation items are always kept (reorg pruning
  // happens in applyFeedEvent); only trades at or below the snapshot are replaced by it.
  const kept =
    recent.length === 0
      ? items
      : items.filter((item) => item.kind !== "trade" || item.block_number > newest);
  const byKey = new Map<string, FeedItem>();
  for (const item of [...kept, ...recent]) {
    const id = item.key.toLowerCase();
    const existing = byKey.get(id);
    if (!existing || FINALITY_RANK[item.finality] > FINALITY_RANK[existing.finality]) {
      byKey.set(id, item);
    }
  }
  return [...byKey.values()].sort(chainOrder).slice(0, max);
}
