import Link from "next/link";
import type { LeaderboardResponse } from "@/api/client";
import { EmptyState } from "@/components/primitives";
import { shortAddress } from "@/token/address";
import {
  LEADERBOARD_WINDOW_LABELS,
  LEADERBOARD_WINDOWS,
  type LeaderboardWindow,
} from "./leaderboard-data";
import { formatSignedEth, pnlTone, walletPath } from "./wallet-model";

/** Window tabs as plain links, so the choice lives in the URL and works without JavaScript. */
export function LeaderboardWindowTabs({ active }: { active: LeaderboardWindow }) {
  return (
    <nav className="explore-tabs" aria-label="Leaderboard period">
      {LEADERBOARD_WINDOWS.map((option) => (
        <Link
          key={option}
          className="explore-tab"
          href={(option === "all" ? "/leaderboard" : `/leaderboard?window=${option}`) as never}
          aria-current={active === option ? "page" : undefined}
        >
          {LEADERBOARD_WINDOW_LABELS[option]}
        </Link>
      ))}
    </nav>
  );
}

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
    <div className="analytics-table" role="table" aria-label="Leaderboard">
      <div className="analytics-row leaderboard-row analytics-heading" role="row">
        <span className="leaderboard-rank">Rank</span>
        <span>Wallet</span>
        <span>Realized PnL</span>
        <span>Tokens traded</span>
      </div>
      {items.map((item) => (
        <div className="analytics-row leaderboard-row" role="row" key={item.address}>
          <span className="mono leaderboard-rank">{item.rank.toLocaleString("en-US")}</span>
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
