import { ApiClient } from "@/api/client";
import type { LiveEvent } from "@/api/types";
import { publicConfiguration } from "@/config/public";
import { OWN_BURST_REPLAY_MS, tradeKey, type RuleState } from "./notification-rules";

/** Same 3 s bound as the share-card fetches; those live in a server-only module, so it is repeated. */
export const HOLDINGS_FETCH_TIMEOUT_MS = 3000;

/** Watchlist (signed-in) plus holdings (connected): lowercased, deduped, sorted. */
export function mergeWatched(watchlist: readonly string[], holdings: readonly string[]): string[] {
  return [...new Set([...watchlist, ...holdings].map((address) => address.toLowerCase()))].sort();
}

/**
 * When the held-token set may have changed: the stream reconnected (events were missed), one of
 * the connected wallet's own trades was just seen, or one just became safe. A trade therefore
 * causes at most two loads (seen, then safe); an already-safe trade causes one, and a replayed
 * row that is still pending (republished after a server reconnect) causes none. A `trade_burst`
 * entry listing the wallet counts as seen (it has no tx hash, so it never becomes "safe" here);
 * a truncated trader list is unknown and does not. The first non-pending own row on a token within
 * OWN_BURST_REPLAY_MS of such a burst is taken as that burst's replay and does not load again (a
 * genuinely new trade then still gets its "safe" load). Call before `evaluate` so a `finality` event
 * still sees the pending own trades it is about to release, and a first sighting is not yet
 * pending.
 */
export function holdingsRefreshReason(
  event: LiveEvent,
  wallet: string | null,
  rules: RuleState,
  now: number = Date.now(),
): "reconnected" | "own_trade_seen" | "own_trade_safe" | null {
  if (!wallet) return null;
  if (event.event === "reconnected") return "reconnected";
  const lower = wallet.toLowerCase();
  if (event.event === "trade") {
    const trade = event.data;
    if (trade.trader?.toLowerCase() !== lower) return null;
    if (trade.finality === "safe" || trade.finality === "finalized") return "own_trade_safe";
    if (rules.pendingOwn.has(tradeKey(trade))) return null;
    // A recent burst that listed the wallet on this token already caused the "seen" load; its
    // individual row (republished after a server reconnect) consumes that sighting instead.
    const token = trade.token.toLowerCase();
    const sighted = rules.ownBurstSeen.get(token);
    if (sighted !== undefined) {
      rules.ownBurstSeen.delete(token);
      if (now - sighted <= OWN_BURST_REPLAY_MS) return null;
    }
    return "own_trade_seen";
  }
  if (event.event === "trade_burst") {
    for (const item of event.data.items ?? []) {
      if (item.traders_truncated) continue;
      if (item.traders.some((trader) => trader.toLowerCase() === lower)) return "own_trade_seen";
    }
    return null;
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
    // Open mode ignores `limit` and returns every open position in one response, capped at 500
    // (`truncated` beyond that), so no limit is sent.
    const response = await new ApiClient({ baseUrl, fetch: fetcher }).listWalletPositions(
      wallet.toLowerCase(),
      { closed: false },
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

/**
 * Open-position token addresses of `wallet` (the API's single open-mode response, up to 500;
 * positions past a `truncated` cap are not watched), lowercased; [] on any failure.
 */
export async function loadHoldings(
  ...args: Parameters<typeof loadHoldingsResult>
): Promise<string[]> {
  return (await loadHoldingsResult(...args)) ?? [];
}
