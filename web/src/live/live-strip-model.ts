import type { components } from "@/api/generated";
import type { ApiEvent, LiveEvent } from "@/api/types";
import { formatCanonicalBaseUnits } from "@/amounts";
import { parseTokenAddress, shortAddress } from "@/token/address";
import { applyFeedEvent, FEED_MAX_ITEMS, type FeedItem } from "./feed-store";

export type StripItemView = {
  key: string;
  kind: FeedItem["kind"];
  href: string | null;
  ticker: string;
  /** Full token name; rendered with CSS ellipsis and exposed as the tooltip. */
  name: string;
  side: "buy" | "sell" | null;
  arrow: "▲" | "▼" | null;
  /** Formatted ETH amount, or null when the item carries none or it is malformed. */
  eth: string | null;
  /** Raw wei for the optional USD line. */
  wei: string | null;
  trader: string;
  time: string;
  badge: "Launch" | "Graduated" | null;
  pending: boolean;
};

export type ChainScope = { chainId: number; deploymentId: string };

/** True when the event belongs to the app's configured chain and deployment. */
export function isEventInScope(event: LiveEvent, scope: ChainScope): boolean {
  if (event.event === "reconnected") return true;
  const data = event.data as { chain_id?: number; deployment_id?: string };
  return data.chain_id === scope.chainId && data.deployment_id === scope.deploymentId;
}

/** Short relative time ("now", "5s", "3m", "2h", "4d") from an ISO timestamp. */
export function relativeTime(iso: string | undefined, now: number): string {
  if (!iso) return "now";
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "—";
  const seconds = Math.max(0, Math.floor((now - then) / 1000));
  if (seconds < 5) return "now";
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}

function traderLabel(trader: string | null | undefined): string {
  if (!trader) return "—";
  const short = shortAddress(trader, 4);
  return short === "Unavailable" ? "—" : short;
}

export function toStripItem(item: FeedItem, now: number): StripItemView {
  const address = parseTokenAddress(item.token);
  const base = {
    key: item.key,
    kind: item.kind,
    href: address ? `/token/${address}` : null,
    ticker: item.symbol.trim() || "—",
    name: item.name,
    pending: item.finality === "observed",
  };
  if (item.kind === "trade") {
    const formatted = formatCanonicalBaseUnits(item.eth_volume, 4);
    const side = item.side === "buy" ? "buy" : item.side === "sell" ? "sell" : null;
    return {
      ...base,
      side,
      arrow: side === "buy" ? "▲" : side === "sell" ? "▼" : null,
      eth: formatted === null ? null : `${formatted} ETH`,
      wei: formatted === null ? null : item.eth_volume,
      trader: traderLabel(item.trader),
      time: relativeTime(item.time, now),
      badge: null,
    };
  }
  return {
    ...base,
    side: null,
    arrow: null,
    eth: null,
    wei: null,
    trader: item.kind === "launch" ? traderLabel(item.creator) : "—",
    time: "now",
    badge: item.kind === "launch" ? "Launch" : "Graduated",
  };
}

/**
 * Reduces one stream event into the strip feed after dropping events from another chain or
 * deployment. The strip shows trades, launches and graduations; other events are ignored.
 */
export function reduceStripFeed(
  items: FeedItem[],
  event: LiveEvent,
  scope: ChainScope,
): FeedItem[] {
  if (event.event === "reconnected" || !isEventInScope(event, scope)) return items;
  return applyFeedEvent(items, event as ApiEvent, FEED_MAX_ITEMS);
}

function sameToken(a: string, b: string): boolean {
  return a.toLowerCase() === b.toLowerCase();
}

/**
 * Token-page reducer over the shared unfiltered stream: trade events for other tokens are
 * ignored, launch/graduation events are ignored, reorg and finality events apply to kept trades.
 */
export function reduceTokenTrades(
  items: FeedItem[],
  event: LiveEvent,
  address: string,
  scope: ChainScope,
): FeedItem[] {
  if (event.event === "reconnected" || !isEventInScope(event, scope)) return items;
  if (event.event === "trade") {
    if (!sameToken(event.data.token, address)) return items;
    return applyFeedEvent(items, event, FEED_MAX_ITEMS);
  }
  if (event.event === "reorg" || event.event === "finality") {
    return applyFeedEvent(items, event, FEED_MAX_ITEMS);
  }
  return items;
}

type TradeDto = components["schemas"]["TradeDTO"];

/**
 * Live trades not yet present in the REST pages, newest first, shaped as table rows.
 * REST rows are canonical: a live item with the same tx hash and log index is dropped.
 * Prices are not on the stream, so they stay empty.
 */
export function liveTradeRows(items: FeedItem[], rest: TradeDto[]): TradeDto[] {
  const known = new Set(rest.map((row) => `${row.tx_hash}:${row.log_index}`.toLowerCase()));
  const rows: TradeDto[] = [];
  for (const item of items) {
    if (item.kind !== "trade") continue;
    if (known.has(item.key.toLowerCase())) continue;
    rows.push({
      block_number: item.block_number,
      eth_volume: item.eth_volume,
      execution_price: "",
      finality: item.finality,
      log_index: item.log_index,
      side: item.side,
      source: item.source,
      spot_price: "",
      time: item.time,
      token_volume: item.token_volume,
      trader: item.trader,
      transaction_index: item.transaction_index,
      tx_hash: item.tx_hash,
    });
  }
  return rows;
}
