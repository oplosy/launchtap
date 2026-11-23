import Link from "next/link";
import type { LeaderboardResponse } from "@/api/client";
import { EmptyState } from "@/components/primitives";
import { shortAddress } from "@/token/address";
import type { LeaderboardWindow } from "./leaderboard-data";
import { formatSignedEth, pnlTone, walletPath } from "./wallet-model";

/** Ranked realized-PnL table; a plain server-renderable view with no hooks or client state. */
export function LeaderboardView({
  items,
  period = "all",
}: {
  items: NonNullable<LeaderboardResponse["items"]>;
  period?: LeaderboardWindow;
}) {
  if (!items.length)
    return (
      <EmptyState
        title="No ranked wallets yet"
        description={
          period === "all"
            ? "Wallets appear here once they have realized PnL."
            : `No wallet has a positive realized PnL in the last ${period === "7d" ? "7" : "30"} days.`
        }
      />
    );
  return (
    <div className="leaderboard-table" role="table" aria-label="Leaderboard">
      <div className="leaderboard-row leaderboard-heading" role="row">
        <span role="cell" className="leaderboard-rank">
          Rank
        </span>
        <span role="cell">Wallet</span>
        <span role="cell" className="leaderboard-pnl">
          Realized PnL
        </span>
        <span role="cell" className="leaderboard-tokens">
          Tokens traded
        </span>
      </div>
      {items.map((item) => (
        // Keyed by rank, not address: a window switch then updates each row in place. Keyed by
        // address, React moved a wallet's row to its new rank, which Chrome counts as a layout
        // shift (CLS 0.3-0.6 measured per switch).
        <div className="leaderboard-row" role="row" key={item.rank}>
          <span role="cell" className="mono leaderboard-rank">
            {String(item.rank).padStart(2, "0")}
          </span>
          <span role="cell" className="leaderboard-wallet">
            <Link
              href={walletPath(item.address) as never}
              title={item.username ? shortAddress(item.address) : item.address}
            >
              {item.username ? `@${item.username}` : shortAddress(item.address)}
            </Link>
          </span>
          <span
            role="cell"
            className={`mono leaderboard-pnl pnl pnl-${pnlTone(item.realized_pnl_wei)}`}
          >
            {formatSignedEth(item.realized_pnl_wei)}
          </span>
          <span role="cell" className="mono leaderboard-tokens">
            {item.tokens_traded.toLocaleString("en-US")}
            <span className="leaderboard-unit">
              {item.tokens_traded === 1 ? " token" : " tokens"}
            </span>
          </span>
        </div>
      ))}
    </div>
  );
}
