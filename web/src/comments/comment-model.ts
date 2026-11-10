import type { components } from "@/api/generated";
import { walletPath } from "@/portfolio/wallet-model";
import { shortAddress } from "@/token/address";

export type CommentItem = components["schemas"]["CommentDTO"];

function compareIdDesc(a: CommentItem, b: CommentItem) {
  const x = BigInt(a.id);
  const y = BigInt(b.id);
  return x === y ? 0 : x > y ? -1 : 1;
}

/**
 * Replaces the newest page with a fresh copy and keeps the older pages already loaded: the result
 * is `firstPage` plus every current item older than the oldest first-page item, newest first and
 * deduplicated. A comment hidden since the last fetch disappears because it is absent from the
 * fresh first page.
 */
export function mergeFirstPage(
  current: readonly CommentItem[],
  firstPage: readonly CommentItem[],
): CommentItem[] {
  let oldest: bigint | null = null;
  for (const item of firstPage) {
    const id = BigInt(item.id);
    if (oldest === null || id < oldest) oldest = id;
  }
  if (oldest === null) return [];
  const floor = oldest;
  const seen = new Set<string>();
  const merged: CommentItem[] = [];
  for (const item of [...firstPage, ...current.filter((c) => BigInt(c.id) < floor)]) {
    if (seen.has(item.id)) continue;
    seen.add(item.id);
    merged.push(item);
  }
  return merged.sort(compareIdDesc);
}

/** Appends an older page, deduplicated, newest first. */
export function appendPage(
  current: readonly CommentItem[],
  page: readonly CommentItem[],
): CommentItem[] {
  const seen = new Set(current.map((c) => c.id));
  return [...current, ...page.filter((c) => !seen.has(c.id))].sort(compareIdDesc);
}

/** The author line: "@name" when a username is shown, else the short posting wallet. */
export function authorLabel(author: CommentItem["author"]): {
  primary: string;
  walletHref: string | null;
} {
  const wallet = author.wallet ?? null;
  const primary = author.username
    ? `@${author.username}`
    : wallet
      ? shortAddress(wallet)
      : "Anonymous";
  return { primary, walletHref: wallet ? walletPath(wallet) : null };
}

const PROBLEM_MESSAGES: Record<string, string> = {
  authentication_failed: "Sign in again to continue.",
  wallet_not_linked: "That wallet is not linked to your account.",
  not_holder: "Only holders and the creator of this token can comment.",
  banned: "Your account cannot post or report comments.",
  token_not_found: "This token is no longer available.",
  comment_not_found: "That comment is no longer available.",
  invalid_comment: "This comment breaks a posting rule.",
  invalid_cursor: "The comment list changed. Refresh to continue.",
  invalid_limit: "The comment list request was invalid.",
  invalid_report: "Choose a report reason.",
  own_comment: "You cannot report your own comment.",
  rate_limited: "You are posting too fast. Try again shortly.",
  username_taken: "That username is taken.",
  invalid_username: "Usernames are 3-20 characters of a-z, 0-9 and _.",
  username_reserved: "That username is reserved.",
  too_many_public_wallets: "You can publish at most 5 wallets.",
};

/** One sentence for each API problem code the comment and profile routes return. */
export function problemMessage(code: string | undefined): string {
  return (code && PROBLEM_MESSAGES[code]) || "Something went wrong. Try again.";
}
