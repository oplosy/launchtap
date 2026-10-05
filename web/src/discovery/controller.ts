import type { TokenListQuery, TokenListResponse } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import type { TokenListQueryState } from "./query-state";
import { tokenListFilters } from "./query-state";

export type DiscoveryPageFetcher = (
  query: TokenListQuery,
  signal?: AbortSignal,
) => Promise<TokenListResponse>;

export type AppendRecoveryResult = {
  pages: TokenListResponse[];
  reset: boolean;
};

export type TokenDiscoveryPageOptions = {
  fetchPage: DiscoveryPageFetcher;
  state: TokenListQueryState;
  cursor?: string;
  existingPages: TokenListResponse[];
  signal?: AbortSignal;
};

/**
 * Append one keyset page. The indexer tip advancing between pages keeps the accumulated pages;
 * only a reorg-invalidated or malformed cursor recovers through one page-one request. Tokens
 * that moved across the page boundary in the meantime are dropped as duplicates.
 */
export async function appendTokenListPage(
  options: TokenDiscoveryPageOptions & { cursor: string },
): Promise<AppendRecoveryResult> {
  const { fetchPage, state, cursor, existingPages, signal } = options;
  let response: TokenListResponse;
  try {
    response = await fetchPage({ ...tokenListFilters(state), cursor }, signal);
  } catch (cause) {
    if (
      !(cause instanceof ApiProblem) ||
      (cause.code !== "cursor_invalidated" && cause.code !== "invalid_cursor")
    ) {
      throw cause;
    }
    return {
      pages: [await fetchPage({ ...tokenListFilters(state), cursor: undefined }, signal)],
      reset: true,
    };
  }
  const seen = new Set(
    existingPages.flatMap((page) => (page.items ?? []).map((item) => item.address.toLowerCase())),
  );
  const items = (response.items ?? []).filter((item) => !seen.has(item.address.toLowerCase()));
  return { pages: [...existingPages, { ...response, items }], reset: false };
}

/** The production page coordinator used by TokenDiscovery for initial and cursor loads. */
export async function loadTokenDiscoveryPage(
  options: TokenDiscoveryPageOptions,
): Promise<AppendRecoveryResult> {
  if (options.cursor) return appendTokenListPage({ ...options, cursor: options.cursor });
  const response = await options.fetchPage(
    { ...tokenListFilters(options.state), cursor: undefined },
    options.signal,
  );
  return { pages: [response], reset: false };
}
