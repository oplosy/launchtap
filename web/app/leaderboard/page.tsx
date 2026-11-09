import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";
import { Skeleton } from "@/components/primitives";
import { apiInternalBaseUrl } from "@/config/server";
import { loadLeaderboard } from "@/portfolio/leaderboard-data";
import { LeaderboardView } from "@/portfolio/leaderboard-view";

export const revalidate = 60;
export const metadata: Metadata = { title: "Leaderboard" };

/**
 * Throws when the API fails: Next then keeps the last good render (or cached data), and only a
 * route that never rendered shows the friendly state in ./error.tsx. It sits behind Suspense so
 * the page shell streams first and a failure reaches the error boundary without turning the
 * document into an HTTP 500.
 */
async function Board() {
  const board = await loadLeaderboard(fetch, apiInternalBaseUrl());
  return <LeaderboardView items={board.items ?? []} />;
}

export default function LeaderboardPage() {
  return (
    <div className="page-stack leaderboard-page">
      <section className="page-hero">
        <div>
          <div className="section-kicker">Leaderboard</div>
          <h1>Top realized PnL.</h1>
          <p className="hero-summary">
            Wallets ranked by realized profit in ETH. Pool trades are attributed to the transaction
            sender.
          </p>
        </div>
      </section>
      <section className="workspace-panel">
        <Suspense
          fallback={
            <div className="history-state">
              <Skeleton />
              <Skeleton />
              <Skeleton />
            </div>
          }
        >
          <Board />
        </Suspense>
      </section>
      <p className="ui-field-hint">
        <Link href="/analytics">Back to Analytics</Link>
      </p>
    </div>
  );
}
