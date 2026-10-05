import { ApiProblem } from "@/api/problems";
import type { CandleResponse, HoldersResponse, TradesResponse } from "@/api/client";

export type TokenCollection = TradesResponse | HoldersResponse;
export type TokenCollectionFetcher = (
  cursor?: string,
  signal?: AbortSignal,
) => Promise<TokenCollection>;

type CollectionItem = NonNullable<TokenCollection["items"]>[number];

function collectionItemKey(item: CollectionItem) {
  return "tx_hash" in item ? `${item.tx_hash}:${item.log_index}` : item.address.toLowerCase();
}

/**
 * Append one keyset page. The indexer tip advancing between pages is expected and keeps the
 * accumulated pages; only a reorg-invalidated or malformed cursor starts page one again. Rows
 * that moved across the page boundary while the tip advanced are dropped as duplicates.
 */
export async function appendTokenCollectionPage(
  fetchPage: TokenCollectionFetcher,
  pages: TokenCollection[],
  cursor: string,
  signal?: AbortSignal,
) {
  let response: TokenCollection;
  try {
    response = await fetchPage(cursor, signal);
  } catch (cause) {
    if (
      !(cause instanceof ApiProblem) ||
      !["cursor_invalidated", "invalid_cursor"].includes(cause.code)
    )
      throw cause;
    return { pages: [await fetchPage(undefined, signal)], reset: true };
  }
  const itemsOf = (page: TokenCollection): CollectionItem[] => page.items ?? [];
  const seen = new Set(pages.flatMap((page) => itemsOf(page).map(collectionItemKey)));
  const items = itemsOf(response).filter((item) => !seen.has(collectionItemKey(item)));
  return { pages: [...pages, { ...response, items } as TokenCollection], reset: false };
}

export function collectionItems(response: TokenCollection | CandleResponse | undefined) {
  return response?.items ?? [];
}
