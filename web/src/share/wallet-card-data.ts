/**
 * Wallet share-card data loader. Server-side only (reads the private API base); no React, no Next.
 * Symbols are user input: they are cleaned with cardText so the renderer never needs a fallback
 * font. Money stays a decimal string here and is formatted with bigint math at render time.
 */
import { apiInternalBaseUrl } from "@/config/server";
import { parseTokenAddress } from "@/token/address";
import { CARD_REVALIDATE_SECONDS, FETCH_TIMEOUT_MS, cardText } from "./card-data";

export type WalletCardData =
  | {
      kind: "wallet";
      address: string;
      realizedPnlWei: string;
      portfolioValueWei: string;
      /** At most three open-position symbols in the API's value order, cardText-cleaned. */
      topSymbols: string[];
    }
  /**
   * `apiFailure`: the API failed or answered malformed data (transient), as opposed to an invalid
   * address (a stable answer); the route then serves the card with a short cache lifetime.
   */
  | { kind: "generic"; apiFailure?: true };

const MAX_SYMBOL_LENGTH = 16;
const MAX_TOP_SYMBOLS = 3;
const SIGNED_WEI = /^-?(?:0|[1-9]\d*)$/;

async function getJson(url: string, fetcher: typeof fetch): Promise<unknown> {
  // A manual timer (not AbortSignal.timeout) so the 3 s bound holds under fake timers too.
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS);
  try {
    const response = await fetcher(url, {
      signal: controller.signal,
      redirect: "error",
      headers: { Accept: "application/json" },
      next: { revalidate: CARD_REVALIDATE_SECONDS },
    } as RequestInit);
    if (!response.ok) throw new Error(`API responded ${response.status}`);
    return await response.json();
  } finally {
    clearTimeout(timer);
  }
}

function amount(value: unknown): string {
  if (typeof value !== "string" || !SIGNED_WEI.test(value))
    throw new Error("wallet response has a non-canonical amount");
  return value;
}

export async function loadWalletCardData(
  address: string,
  fetcher: typeof fetch = fetch,
  baseUrl?: string,
): Promise<WalletCardData> {
  const parsed = parseTokenAddress(address);
  if (!parsed) return { kind: "generic" };
  try {
    const base = baseUrl ?? apiInternalBaseUrl();
    // Positions only supply the optional symbol row: their failure must not cost the whole card.
    const [overview, topSymbols] = await Promise.all([
      getJson(`${base}/v1/wallets/${parsed}`, fetcher) as Promise<Record<string, unknown>>,
      loadTopSymbols(base, parsed, fetcher),
    ]);
    return {
      kind: "wallet",
      address: parsed,
      realizedPnlWei: amount(overview.realized_pnl_wei),
      portfolioValueWei: amount(overview.portfolio_value_wei),
      topSymbols,
    };
  } catch (cause) {
    console.error("wallet share card: falling back to the generic card", cause);
    return { kind: "generic", apiFailure: true };
  }
}

/** Up to three cleaned open-position symbols; [] (logged) when the positions read fails. */
async function loadTopSymbols(base: string, wallet: string, fetcher: typeof fetch) {
  try {
    const positions = (await getJson(
      `${base}/v1/wallets/${wallet}/positions?closed=false`,
      fetcher,
    )) as Record<string, unknown> | null;
    const items = positions?.items;
    if (items !== null && items !== undefined && !Array.isArray(items))
      throw new Error("positions response is malformed");
    const topSymbols: string[] = [];
    for (const item of items ?? []) {
      if (topSymbols.length === MAX_TOP_SYMBOLS) break;
      const symbol = cardText((item as { symbol?: unknown } | null)?.symbol, MAX_SYMBOL_LENGTH);
      if (symbol) topSymbols.push(symbol);
    }
    return topSymbols;
  } catch (cause) {
    console.error("wallet share card: positions unavailable, showing no symbols", cause);
    return [];
  }
}
