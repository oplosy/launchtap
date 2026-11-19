import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";
import { Skeleton } from "@/components/primitives";
import { apiInternalBaseUrl } from "@/config/server";
import {
  LEADERBOARD_WINDOW_LABELS,
  loadLeaderboard,
  parseLeaderboardWindow,
  type LeaderboardWindow,
} from "@/portfolio/leaderboard-data";
import { LeaderboardView } from "@/portfolio/leaderboard-view";
import { LeaderboardWindowSelect } from "@/portfolio/leaderboard-window-select";

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

export default async function LeaderboardPage({
  searchParams,
}: {
  searchParams?: Promise<{ window?: string | string[] }>;
}) {
  const query = searchParams ? await searchParams : undefined;
  const period = parseLeaderboardWindow(query?.window);
  return (
    <div className="page-stack leaderboard-page">
      {/* One heading for every window: a per-window heading wrapped to a second line on mobile
          and pushed the whole board down on each switch. The period dropdown names the window. */}
      <h1 className="sr-only">Leaderboard</h1>
      <section
        className="leaderboard-board"
        aria-label={`Ranking, ${LEADERBOARD_WINDOW_LABELS[period].toLowerCase()}`}
      >
        <LeaderboardWindowSelect active={period} />
        {/* Not keyed by period: a window switch is a router transition, so React keeps the
            current table until the new one streams instead of collapsing to the skeleton,
            which shifted the page footer (CLS 0.9 measured on a window switch). */}
        <Suspense
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
      <footer className="leaderboard-note">
        <p>
          Wallets ranked by realized profit in ETH. Pool trades are attributed to the transaction
          sender. Windows count profit booked by sells inside the period.
        </p>
        <Link href="/analytics">
          Back to Analytics <span aria-hidden="true">→</span>
        </Link>
      </footer>
    </div>
  );
}
