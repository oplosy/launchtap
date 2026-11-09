import type { ApiEvent, TradeFeedItem } from "@/api/types";
import { formatDisplayAmount } from "@/amounts";

/** A trade of at least this much gross ETH (0.5 ETH, in wei) on a watched token notifies. */
export const LARGE_TRADE_WEI = 500_000_000_000_000_000n;

/** Upper bound on own trades awaiting finality, so a long session cannot grow it without limit. */
export const PENDING_OWN_MAX = 100;

export type NotificationKind = "graduation" | "large_trade" | "own_trade_safe";

export type Notification = {
  id: string;
  kind: NotificationKind;
  token: string;
  /** User-controlled text: render as text only. */
  symbol: string;
  /** Empty for notifications raised from a `trade_burst` entry, which carries no tx hash. */
  txHash: string;
  at: number;
  /** Set when raised from a `trade_burst` entry: the folded trade count and the largest amount. */
  burst?: { count: number; maxEthWei: string };
  /**
   * Own trade seen in a `trade_burst` entry. A burst has no tx hash or block, so the trade cannot
   * be tracked to `safe` finality: the notification reuses the own-trade kind with "pending"
   * semantics (seen, not yet safe) instead of waiting for a finality event that cannot match it.
   */
  unconfirmed?: true;
};

export type RuleState = { pendingOwn: Map<string, TradeFeedItem> };

export type RuleContext = {
  /** Lowercase watched token addresses. */
  watched: ReadonlySet<string>;
  wallet?: string;
};

export function createRuleState(): RuleState {
  return { pendingOwn: new Map() };
}

/** Key of a trade in `RuleState.pendingOwn`. */
export const tradeKey = (trade: TradeFeedItem) =>
  `${trade.tx_hash}:${trade.log_index}`.toLowerCase();

/** Exact wei comparison; a malformed amount never notifies. */
export function isLargeTrade(ethVolume: string): boolean {
  if (!/^\d+$/.test(ethVolume)) return false;
  return BigInt(ethVolume) >= LARGE_TRADE_WEI;
}

function isOwnTrade(trade: TradeFeedItem, wallet: string | undefined): boolean {
  if (!wallet || !trade.trader) return false;
  return trade.trader.toLowerCase() === wallet.toLowerCase();
}

function ownTradeNotification(trade: TradeFeedItem, at: number): Notification {
  return {
    id: `own:${tradeKey(trade)}`,
    kind: "own_trade_safe",
    token: trade.token,
    symbol: trade.symbol,
    txHash: trade.tx_hash,
    at,
  };
}

/**
 * Turns one in-scope stream event into notifications and updates `state` (own trades waiting for
 * finality). Callers must already have dropped events from another chain or deployment.
 */
export function evaluate(
  event: ApiEvent,
  ctx: RuleContext,
  state: RuleState,
  now: number = Date.now(),
): Notification[] {
  switch (event.event) {
    case "graduation": {
      const data = event.data;
      if (!ctx.watched.has(data.token.toLowerCase())) return [];
      return [
        {
          id: `graduation:${data.tx_hash}:${data.token}`.toLowerCase(),
          kind: "graduation",
          token: data.token,
          symbol: data.symbol,
          txHash: data.tx_hash,
          at: now,
        },
      ];
    }
    case "trade": {
      const trade = event.data;
      const out: Notification[] = [];
      if (ctx.watched.has(trade.token.toLowerCase()) && isLargeTrade(trade.eth_volume)) {
        out.push({
          id: `large:${tradeKey(trade)}`,
          kind: "large_trade",
          token: trade.token,
          symbol: trade.symbol,
          txHash: trade.tx_hash,
          at: now,
        });
      }
      if (isOwnTrade(trade, ctx.wallet)) {
        if (trade.finality === "safe" || trade.finality === "finalized") {
          out.push(ownTradeNotification(trade, now));
        } else {
          const key = tradeKey(trade);
          state.pendingOwn.delete(key);
          state.pendingOwn.set(key, trade);
          while (state.pendingOwn.size > PENDING_OWN_MAX) {
            const oldest = state.pendingOwn.keys().next().value;
            if (oldest === undefined) break;
            state.pendingOwn.delete(oldest);
          }
        }
      }
      return out;
    }
    case "trade_burst": {
      const data = event.data;
      const window = `${data.window_start}:${data.window_end}`;
      const wallet = ctx.wallet?.toLowerCase();
      const out: Notification[] = [];
      for (const item of data.items ?? []) {
        const token = item.token.toLowerCase();
        const burst = { count: item.count, maxEthWei: item.max_eth_wei };
        if (ctx.watched.has(token) && isLargeTrade(item.max_eth_wei)) {
          out.push({
            id: `large-burst:${window}:${token}`.toLowerCase(),
            kind: "large_trade",
            token: item.token,
            symbol: "",
            txHash: "",
            at: now,
            burst,
          });
        }
        // A truncated trader list means "unknown": never claim an own trade from it.
        if (
          wallet &&
          !item.traders_truncated &&
          item.traders.some((trader) => trader.toLowerCase() === wallet)
        ) {
          out.push({
            id: `own-burst:${window}:${token}`.toLowerCase(),
            kind: "own_trade_safe",
            token: item.token,
            symbol: "",
            txHash: "",
            at: now,
            burst,
            unconfirmed: true,
          });
        }
      }
      return out;
    }
    case "finality": {
      const out: Notification[] = [];
      for (const [key, trade] of state.pendingOwn) {
        if (trade.block_number > event.data.safe_block) continue;
        state.pendingOwn.delete(key);
        out.push(ownTradeNotification(trade, now));
      }
      return out;
    }
    case "reorg": {
      for (const [key, trade] of state.pendingOwn) {
        if (trade.block_number > event.data.common_ancestor) state.pendingOwn.delete(key);
      }
      return [];
    }
    default:
      return [];
  }
}

/** Display text for a notification. The symbol is user-controlled: render the result as text. */
export function describeNotification(notification: Notification): string {
  const ticker = notification.symbol.trim() || "Token";
  switch (notification.kind) {
    case "graduation":
      return `${ticker} graduated`;
    case "large_trade": {
      const burst = notification.burst;
      if (!burst) return `Large trade on ${ticker}`;
      const largest = formatDisplayAmount(BigInt(burst.maxEthWei), 18, 4);
      return `Large trade on ${ticker}: ${burst.count} trades, largest ${largest} ETH`;
    }
    case "own_trade_safe":
      return notification.unconfirmed
        ? `Your ${ticker} trade was seen (not yet safe)`
        : `Your ${ticker} trade is safe`;
  }
}

/** The bell appears only for a signed-in reader (watchlist) or a connected wallet (own trades). */
export function bellVisible(identity: { signedIn: boolean; connected: boolean }): boolean {
  return identity.signedIn || identity.connected;
}

/** A different wallet, or signing in or out, starts a fresh notification session. */
export function identityChanged(
  a: { signedIn: boolean; wallet?: string },
  b: { signedIn: boolean; wallet?: string },
): boolean {
  return a.signedIn !== b.signedIn || a.wallet?.toLowerCase() !== b.wallet?.toLowerCase();
}
