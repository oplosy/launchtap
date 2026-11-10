import Link from "next/link";
import type { LeaderboardResponse } from "@/api/client";
import { EmptyState } from "@/components/primitives";
import { shortAddress } from "@/token/address";
import { formatSignedEth, pnlTone, walletPath } from "./wallet-model";

/** Ranked realized-PnL table; a plain server-renderable view with no hooks or client state. */
export function LeaderboardView({ items }: { items: NonNullable<LeaderboardResponse["items"]> }) {
  if (!items.length)
    return (
      <EmptyState
        title="No ranked wallets yet"
        description="Wallets appear here once they have realized PnL."
      />
    );
  return (
    <div className="analytics-table" role="table" aria-label="Leaderboard">
      <div className="analytics-row leaderboard-row analytics-heading" role="row">
        <span>Rank</span>
        <span>Wallet</span>
        <span>Realized PnL</span>
        <span>Tokens traded</span>
      </div>
      {items.map((item) => (
        <div className="analytics-row leaderboard-row" role="row" key={item.address}>
          <span className="mono">{item.rank.toLocaleString("en-US")}</span>
          <span className="mono">
            <Link
              href={walletPath(item.address) as never}
              title={item.username ? shortAddress(item.address) : item.address}
            >
              {item.username ? `@${item.username}` : shortAddress(item.address)}
            </Link>
          </span>
          <span className={`mono pnl pnl-${pnlTone(item.realized_pnl_wei)}`}>
            {formatSignedEth(item.realized_pnl_wei)}
          </span>
          <span className="mono">{item.tokens_traded.toLocaleString("en-US")}</span>
        </div>
      ))}
    </div>
  );
}
