/**
 * Leaderboard loader for the server-rendered /leaderboard route. Server-side only (the caller
 * passes the private API base). It throws on any failure instead of returning an "unavailable"
 * value: a thrown render is never cached, so Next keeps serving the last good page or data-cache
 * entry, and only a route that never rendered successfully falls through to
 * app/leaderboard/error.tsx.
 */
import type { LeaderboardResponse } from "@/api/client";

export const LEADERBOARD_WINDOWS = ["all", "7d", "30d"] as const;
export type LeaderboardWindow = (typeof LEADERBOARD_WINDOWS)[number];
export const LEADERBOARD_WINDOW_LABELS: Record<LeaderboardWindow, string> = {
  all: "All time",
  "7d": "7d",
  "30d": "30d",
};

/** Reads `?window=`; anything unknown (or repeated) falls back to all time. */
export function parseLeaderboardWindow(raw: string | string[] | undefined): LeaderboardWindow {
  return typeof raw === "string" && (LEADERBOARD_WINDOWS as readonly string[]).includes(raw)
    ? (raw as LeaderboardWindow)
    : "all";
}

export const LEADERBOARD_TIMEOUT_MS = 3_000;
export const LEADERBOARD_REVALIDATE_SECONDS = 60;

export async function loadLeaderboard(
  fetcher: typeof fetch,
  baseUrl: string,
  period: LeaderboardWindow = "all",
): Promise<LeaderboardResponse> {
  const query = period === "all" ? "limit=100" : `limit=100&window=${period}`;
  const response = await fetcher(`${baseUrl}/v1/leaderboard?${query}`, {
    headers: { Accept: "application/json" },
    next: { revalidate: LEADERBOARD_REVALIDATE_SECONDS },
    signal: AbortSignal.timeout(LEADERBOARD_TIMEOUT_MS),
  } as RequestInit);
  if (!response.ok) throw new Error(`leaderboard: API responded ${response.status}`);
  const body = (await response.json()) as LeaderboardResponse;
  if (
    typeof body !== "object" ||
    body === null ||
    !(body.items === null || body.items === undefined || Array.isArray(body.items))
  )
    throw new Error("leaderboard: malformed response");
  return body;
}
