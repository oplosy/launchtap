import { ApiClient } from "@/api/client";
import type { LiveEvent } from "@/api/types";
import { publicConfiguration } from "@/config/public";
import type { RuleState } from "./notification-rules";

/** Same 3 s bound as the share-card fetches; those live in a server-only module, so it is repeated. */
export const HOLDINGS_FETCH_TIMEOUT_MS = 3000;
const HOLDINGS_LIMIT = 100;

/** Watchlist (signed-in) plus holdings (connected): lowercased, deduped, sorted. */
export function mergeWatched(watchlist: readonly string[], holdings: readonly string[]): string[] {
  return [...new Set([...watchlist, ...holdings].map((address) => address.toLowerCase()))].sort();
}

/**
 * When the held-token set may have changed: the stream reconnected (events were missed), or one of
 * the connected wallet's own trades just became safe. Call before `evaluate` so a `finality` event
 * still sees the pending own trades it is about to release.
 */
export function holdingsRefreshReason(
  event: LiveEvent,
  wallet: string | null,
  rules: RuleState,
): "reconnected" | "own_trade_safe" | null {
  if (!wallet) return null;
  if (event.event === "reconnected") return "reconnected";
  const lower = wallet.toLowerCase();
  if (event.event === "trade") {
    const trade = event.data;
    const own = trade.trader?.toLowerCase() === lower;
    return own && (trade.finality === "safe" || trade.finality === "finalized")
      ? "own_trade_safe"
      : null;
  }
  if (event.event === "finality") {
    for (const trade of rules.pendingOwn.values()) {
      if (trade.block_number <= event.data.safe_block) return "own_trade_safe";
    }
  }
  return null;
}

/** A failed refresh keeps the previous set; only a successful answer replaces it. */
export function applyHoldingsResult(previous: string[], result: string[] | null): string[] {
  return result ?? previous;
}

/** Like `loadHoldings`, but null (not []) when the load failed, so callers can keep what they had. */
export async function loadHoldingsResult(
  wallet: string,
  fetcher: typeof fetch = (input, init) => globalThis.fetch(input, init),
  baseUrl: string | null = publicConfiguration().apiBaseUrl,
  timeoutMs: number = HOLDINGS_FETCH_TIMEOUT_MS,
): Promise<string[] | null> {
  if (!baseUrl) return null;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await new ApiClient({ baseUrl, fetch: fetcher }).listWalletPositions(
      wallet.toLowerCase(),
      { closed: false, limit: HOLDINGS_LIMIT },
      controller.signal,
    );
    if (!Array.isArray(response.items)) return null;
    const tokens: string[] = [];
    for (const item of response.items) {
      if (typeof item?.token !== "string") return null;
      tokens.push(item.token.toLowerCase());
    }
    return tokens;
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
}

/** Open-position token addresses of `wallet` (first page only), lowercased; [] on any failure. */
export async function loadHoldings(
  ...args: Parameters<typeof loadHoldingsResult>
): Promise<string[]> {
  return (await loadHoldingsResult(...args)) ?? [];
}
