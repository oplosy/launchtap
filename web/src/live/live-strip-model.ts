import type { components } from "@/api/generated";
import type { ApiEvent, LiveEvent } from "@/api/types";
import { formatCanonicalBaseUnits } from "@/amounts";
import { walletPath } from "@/portfolio/wallet-model";
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
  /** Wallet page of the trader (or launch creator), or null when there is no valid address. */
  traderHref: string | null;
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

/**
 * What a live event asks of REST-backed views. `reorg`: an in-scope reorg (it has no token field)
 * means REST rows above the ancestor may be gone, so refetch. `reconnected`: the stream is open
 * again after the recovery fetch, so refetch once more to close the gap between the two.
 */
export function liveRefreshKind(
  event: LiveEvent,
  scope: ChainScope,
): "reorg" | "reconnected" | null {
  if (event.event === "reconnected") return "reconnected";
  if (event.event === "reorg" && isEventInScope(event, scope)) return "reorg";
  return null;
}

/** Short relative time ("now", "5s", "3m", "2h", "4d") from an ISO timestamp. */
export function relativeTime(at: string | number | undefined, now: number): string {
  if (at === undefined || at === "") return "now";
  const then = typeof at === "number" ? at : Date.parse(at);
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

function traderHref(trader: string | null | undefined): string | null {
  const address = parseTokenAddress(trader);
  return address ? walletPath(address) : null;
}

/** Client receive times (ms) for items that carry no chain timestamp, keyed by feed key. */
export type ReceivedAt = Record<string, number>;

export function toStripItem(
  item: FeedItem,
  now: number,
  receivedAt: ReceivedAt = {},
): StripItemView {
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
      traderHref: traderHref(item.trader),
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
    traderHref: item.kind === "launch" ? traderHref(item.creator) : null,
    time: relativeTime(receivedAt[item.key], now),
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

export type StripState = { items: FeedItem[]; receivedAt: ReceivedAt };

/** Like `reduceStripFeed`, but stamps newly arrived launch/graduation items with `now`. */
export function reduceStripState(
  state: StripState,
  event: LiveEvent,
  scope: ChainScope,
  now: number,
): StripState {
  const items = reduceStripFeed(state.items, event, scope);
  if (items === state.items) return state;
  const receivedAt: ReceivedAt = {};
  for (const item of items) {
    if (item.kind === "trade") continue;
    receivedAt[item.key] = state.receivedAt[item.key] ?? now;
  }
  return { items, receivedAt };
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

function comparePosition(a: [number, number, number], b: [number, number, number]): number {
  return a[0] - b[0] || a[1] - b[1] || a[2] - b[2];
}

type TradeDto = components["schemas"]["TradeDTO"];

/**
 * Live trades not yet present in the REST pages, newest first, shaped as table rows.
 * REST rows are canonical: a live item with the same tx hash and log index is dropped.
 * Prices are not on the stream, so they stay empty.
 */
export function liveTradeRows(items: FeedItem[], rest: TradeDto[]): TradeDto[] {
  const known = new Set(rest.map((row) => `${row.tx_hash}:${row.log_index}`.toLowerCase()));
  // Same rule as mergeRecent: REST is canonical up to its newest row, so only live trades
  // strictly newer in chain order may sit above it. Otherwise a page-1 reload would differ.
  let newest: [number, number, number] | null = null;
  for (const row of rest) {
    const position: [number, number, number] = [
      row.block_number,
      row.transaction_index,
      row.log_index,
    ];
    if (!newest || comparePosition(position, newest) > 0) newest = position;
  }
  const rows: TradeDto[] = [];
  for (const item of items) {
    if (item.kind !== "trade") continue;
    if (known.has(item.key.toLowerCase())) continue;
    if (
      newest &&
      comparePosition([item.block_number, item.transaction_index, item.log_index], newest) <= 0
    )
      continue;
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
