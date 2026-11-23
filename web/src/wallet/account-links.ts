import { walletPath } from "@/portfolio/wallet-model";

export type AccountLink = { href: string; label: string };

/**
 * The account pages a connected wallet reaches from the navbar's account menu. On desktop the
 * header shows none of them otherwise; the footer and the phone sheet still list some.
 */
export function accountLinks(address: string): AccountLink[] {
  return [
    { href: walletPath(address), label: "My wallet" },
    { href: "/profile", label: "Profile" },
    { href: "/rewards", label: "Rewards" },
    { href: "/explore?view=watchlist", label: "Watchlist" },
    { href: "/notifications", label: "Notifications" },
  ];
}
