import type { Metadata } from "next";
import Link from "next/link";
import type { LeaderboardResponse } from "@/api/client";
import { UnavailableState } from "@/components/primitives";
import { apiInternalBaseUrl } from "@/config/server";
import { LeaderboardView } from "@/portfolio/leaderboard-view";

export const revalidate = 60;
export const metadata: Metadata = { title: "Leaderboard" };

const FETCH_TIMEOUT_MS = 3_000;

async function loadLeaderboard(): Promise<LeaderboardResponse | null> {
  try {
    const response = await fetch(`${apiInternalBaseUrl()}/v1/leaderboard?limit=100`, {
      headers: { Accept: "application/json" },
      next: { revalidate: 60 },
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
    });
    if (!response.ok) return null;
    return (await response.json()) as LeaderboardResponse;
  } catch (cause) {
    console.error("leaderboard: unavailable", cause);
    return null;
  }
}

export default async function LeaderboardPage() {
  const board = await loadLeaderboard();
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
      {board ? (
        <section className="workspace-panel">
          <LeaderboardView items={board.items ?? []} />
        </section>
      ) : (
        <UnavailableState
          title="Leaderboard unavailable"
          description="The ranking could not be loaded. Try again in a minute."
        />
      )}
      <p className="ui-field-hint">
        <Link href="/analytics">Back to Analytics</Link>
      </p>
    </div>
  );
}
