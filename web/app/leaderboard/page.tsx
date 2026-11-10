import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";
import { Skeleton } from "@/components/primitives";
import { apiInternalBaseUrl } from "@/config/server";
import {
  loadLeaderboard,
  parseLeaderboardWindow,
  type LeaderboardWindow,
} from "@/portfolio/leaderboard-data";
import { LeaderboardView, LeaderboardWindowTabs } from "@/portfolio/leaderboard-view";

export const revalidate = 60;
export const metadata: Metadata = { title: "Leaderboard" };

/**
 * Throws when the API fails: Next then keeps the last good render (or cached data), and only a
 * route that never rendered shows the friendly state in ./error.tsx. It sits behind Suspense so
 * the page shell streams first and a failure reaches the error boundary without turning the
 * document into an HTTP 500.
 */
async function Board({ period }: { period: LeaderboardWindow }) {
  const board = await loadLeaderboard(fetch, apiInternalBaseUrl(), period);
  return <LeaderboardView items={board.items ?? []} period={period} />;
}

const HEADINGS: Record<LeaderboardWindow, string> = {
  all: "Top realized PnL.",
  "7d": "Top realized PnL, last 7 days.",
  "30d": "Top realized PnL, last 30 days.",
};

export default async function LeaderboardPage({
  searchParams,
}: {
  searchParams?: Promise<{ window?: string | string[] }>;
}) {
  const query = searchParams ? await searchParams : undefined;
  const period = parseLeaderboardWindow(query?.window);
  return (
    <div className="page-stack leaderboard-page">
      <section className="page-hero">
        <div>
          <div className="section-kicker">Leaderboard</div>
          <h1>{HEADINGS[period]}</h1>
          <p className="hero-summary">
            Wallets ranked by realized profit in ETH. Pool trades are attributed to the transaction
            sender. Windows count profit booked by sells inside the period.
          </p>
        </div>
      </section>
      <section className="workspace-panel">
        <LeaderboardWindowTabs active={period} />
        <Suspense
          key={period}
          fallback={
            <div className="history-state">
              <Skeleton />
              <Skeleton />
              <Skeleton />
            </div>
          }
        >
          <Board period={period} />
        </Suspense>
      </section>
      <p className="ui-field-hint">
        <Link href="/analytics">Back to Analytics</Link>
      </p>
    </div>
  );
}
