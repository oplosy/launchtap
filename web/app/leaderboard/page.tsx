import { permanentRedirect } from "next/navigation";
import { parseLeaderboardWindow } from "@/portfolio/leaderboard-data";

/** The leaderboard is a tab of the rewards page; old links keep their period. */
export default async function LeaderboardPage({
  searchParams,
}: {
  searchParams?: Promise<{ window?: string | string[] }>;
}) {
  const query = searchParams ? await searchParams : undefined;
  const period = parseLeaderboardWindow(query?.window);
  permanentRedirect(
    (period === "all" ? "/rewards/leaderboard" : `/rewards/leaderboard?window=${period}`) as never,
  );
}
