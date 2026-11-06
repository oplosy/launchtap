import type { ApiEvent, TradeFeedItem } from "@/api/types";

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
  txHash: string;
  at: number;
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

const tradeKey = (trade: TradeFeedItem) => `${trade.tx_hash}:${trade.log_index}`.toLowerCase();

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

/** The bell appears only for a signed-in reader (watchlist) or a connected wallet (own trades). */
export function bellVisible(identity: { signedIn: boolean; connected: boolean }): boolean {
  return identity.signedIn || identity.connected;
}
