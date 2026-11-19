"use client";

import { useRouter } from "next/navigation";
import { useTransition } from "react";
import {
  LEADERBOARD_WINDOW_LABELS,
  LEADERBOARD_WINDOWS,
  type LeaderboardWindow,
} from "./leaderboard-data";

/** The ranking's period as a dropdown, like Explore's sort; the choice lives in the URL. */
export function LeaderboardWindowSelect({ active }: { active: LeaderboardWindow }) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  return (
    <label className="explore-sort leaderboard-window">
      <span>Period</span>
      <select
        className="ui-input"
        value={active}
        aria-busy={pending || undefined}
        onChange={(event) => {
          const next = event.target.value as LeaderboardWindow;
          startTransition(() =>
            router.replace(
              (next === "all" ? "/leaderboard" : `/leaderboard?window=${next}`) as never,
              { scroll: false },
            ),
          );
        }}
      >
        {LEADERBOARD_WINDOWS.map((option) => (
          <option key={option} value={option}>
            {LEADERBOARD_WINDOW_LABELS[option]}
          </option>
        ))}
      </select>
    </label>
  );
}
