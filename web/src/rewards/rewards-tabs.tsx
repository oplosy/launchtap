"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { SlidingIndicator } from "@/components/sliding-indicator";

const TABS = [
  { href: "/rewards", label: "Rewards" },
  { href: "/rewards/leaderboard", label: "Leaderboard" },
] as const;

/**
 * The rewards page's two sections as routes, so each loads only what it needs: Rewards reads the
 * account and loads the wallet stack, Leaderboard stays a read-only page. Drawn like the forum tabs.
 */
export function RewardsTabs() {
  const pathname = usePathname();
  return (
    <nav className="forum-tabs rewards-tabs" aria-label="Rewards sections">
      {TABS.map(({ href, label }) => (
        <Link
          key={href}
          href={href as never}
          className="forum-tab"
          aria-current={pathname === href ? "page" : undefined}
        >
          {label}
        </Link>
      ))}
      <SlidingIndicator />
    </nav>
  );
}
