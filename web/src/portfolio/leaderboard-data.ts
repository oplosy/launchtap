/**
 * Leaderboard loader for the server-rendered /leaderboard route. Server-side only (the caller
 * passes the private API base). It throws on any failure instead of returning an "unavailable"
 * value: a thrown render is never cached, so Next keeps serving the last good page or data-cache
 * entry, and only a route that never rendered successfully falls through to
 * app/leaderboard/error.tsx.
 */
import type { LeaderboardResponse } from "@/api/client";

export const LEADERBOARD_TIMEOUT_MS = 3_000;
export const LEADERBOARD_REVALIDATE_SECONDS = 60;

export async function loadLeaderboard(
  fetcher: typeof fetch,
  baseUrl: string,
): Promise<LeaderboardResponse> {
  const response = await fetcher(`${baseUrl}/v1/leaderboard?limit=100`, {
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
