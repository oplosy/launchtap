import type { components } from "@/api/generated";
import { apiFallbackMessage, apiMessages, contextMessages } from "@/errors/catalog";
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

/** The comment-context sentence for an API problem code (errors/catalog.ts). */
export function problemMessage(code: string | undefined): string {
  const words = contextMessages.comment;
  return (code && (words[code] ?? apiMessages[code])) || words.fallback || apiFallbackMessage;
}

/** Vote values: 1 up, -1 down, 0 none (docs/specs/2026-11-12-comment-votes-design.md). */
export type VoteValue = -1 | 0 | 1;

/** Clicking the active arrow clears the vote; the other arrow switches to it. */
export function nextVote(current: VoteValue, clicked: 1 | -1): VoteValue {
  return current === clicked ? 0 : clicked;
}

export function scoreLabel(score: number): string {
  return `${score} ${score === 1 ? "point" : "points"}`;
}

/** Vote refusals in vote terms; other codes read as in problemMessage. */
export function voteProblemMessage(code: string | undefined): string {
  return (code && contextMessages.vote[code]) || problemMessage(code);
}
