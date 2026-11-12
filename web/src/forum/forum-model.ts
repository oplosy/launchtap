import type { ForumPost } from "@/api/client";
import { parseTokenAddress } from "@/token/address";

/** Forum tabs in the URL (`?tab=`), spec P0-C5. */
export const FORUM_TABS = ["new", "best", "hot", "top"] as const;
export type ForumTab = (typeof FORUM_TABS)[number];
export const FORUM_TAB_LABELS: Record<ForumTab, string> = {
  new: "New",
  best: "Best",
  hot: "Hot",
  top: "Top",
};
export const FORUM_PAGE_SIZE = 20;
export const BIGGEST_LAUNCHES = 10;

/** Reads `?tab=`; anything unknown (or repeated) falls back to New. */
export function parseForumTab(raw: string | string[] | undefined): ForumTab {
  return typeof raw === "string" && (FORUM_TABS as readonly string[]).includes(raw)
    ? (raw as ForumTab)
    : "new";
}

export function forumTabHref(tab: ForumTab): string {
  return tab === "new" ? "/forum" : `/forum?tab=${tab}`;
}

/** `s/TICKER`; the symbol is user-controlled, so callers render the result as text. */
export function communityLabel(symbol: string): string {
  return `s/${symbol.trim() || "TOKEN"}`;
}

/** Token page link, or null when the API returned a malformed address. */
export function tokenHref(address: string): string | null {
  const parsed = parseTokenAddress(address);
  return parsed ? `/token/${parsed}` : null;
}

/** Appends a later page, dropping posts already shown (a new post shifts nothing: keyset). */
export function appendForumPage(
  current: readonly ForumPost[],
  page: readonly ForumPost[],
): ForumPost[] {
  const seen = new Set(current.map((post) => post.id));
  return [...current, ...page.filter((post) => !seen.has(post.id))];
}
