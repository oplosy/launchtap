import type { InboxItem } from "@/api/client";
import { formatDisplayAmount } from "@/amounts";

/** Page size the inbox page requests (server allows 1-50). */
export const INBOX_PAGE_SIZE = 20;

/** The server caps unread_count at 100; show "99+" above 99 (spec P0-C4). */
export function formatUnreadCount(count: number): string {
  if (!Number.isSafeInteger(count) || count <= 0) return "0";
  return count > 99 ? "99+" : String(count);
}

function ethAmount(wei: string): string | null {
  if (!/^\d+$/.test(wei)) return null;
  return formatDisplayAmount(BigInt(wei), 18, 4);
}

/** Display text for one inbox item. The symbol is user-controlled: render the result as text. */
export function describeInboxItem(item: InboxItem): string {
  const ticker = item.symbol.trim() || "Token";
  const amount = ethAmount(item.eth_wei);
  if (item.kind === "graduation")
    return amount ? `${ticker} graduated with ${amount} ETH to the pool` : `${ticker} graduated`;
  const side = item.side_buy === true ? "buy" : item.side_buy === false ? "sell" : "trade";
  const venue = item.source === "dex" ? " on the pool" : "";
  return amount
    ? `Large ${side} on ${ticker}${venue}: ${amount} ETH`
    : `Large ${side} on ${ticker}${venue}`;
}

/** Appends a later page, dropping ids already shown (a reload between pages can repeat one). */
export function appendInboxPage(
  current: readonly InboxItem[],
  page: readonly InboxItem[],
): InboxItem[] {
  const seen = new Set(current.map((item) => item.id));
  return [...current, ...page.filter((item) => !seen.has(item.id))];
}

/**
 * Unread items are the newest `unread` ones, because unread counts events after the read
 * position within the same watched, 30-day set the list shows newest first.
 */
export function isUnread(index: number, unread: number): boolean {
  return index < unread;
}
