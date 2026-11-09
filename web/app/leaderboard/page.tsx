import type { Metadata } from "next";
import Link from "next/link";
import { apiInternalBaseUrl } from "@/config/server";
import { loadLeaderboard } from "@/portfolio/leaderboard-data";
import { LeaderboardView } from "@/portfolio/leaderboard-view";

export const revalidate = 60;
export const metadata: Metadata = { title: "Leaderboard" };

export default async function LeaderboardPage() {
  // Throws when the API fails: Next then keeps the last good render (or cached data), and only a
  // route that never rendered shows the friendly state in ./error.tsx.
  const board = await loadLeaderboard(fetch, apiInternalBaseUrl());
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
        <LeaderboardView items={board.items ?? []} />
      </section>
      <p className="ui-field-hint">
        <Link href="/analytics">Back to Analytics</Link>
      </p>
    </div>
  );
}
