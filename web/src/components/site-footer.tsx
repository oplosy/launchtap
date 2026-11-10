"use client";

import Link from "next/link";
import { SafeExternalLink } from "./primitives";
import { originOf, setThemePreference, useThemePreference, type ThemePreference } from "./theme";
import { publicConfiguration } from "@/config/public";
import { CoinGeckoAttribution } from "@/market/usd-line";

const THEME_OPTIONS: ReadonlyArray<{ value: ThemePreference; label: string }> = [
  { value: "system", label: "System" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
];

/** Three-way theme selector: one sliding pill under System / Light / Dark. */
export function ThemeSwitcher() {
  const preference = useThemePreference();
  const index = THEME_OPTIONS.findIndex((option) => option.value === preference);
  return (
    <div
      className="theme-switcher"
      role="group"
      aria-label="Color theme"
      style={{ "--index": index } as React.CSSProperties}
    >
      <span className="theme-switcher-indicator" aria-hidden="true" />
      {THEME_OPTIONS.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={preference === option.value}
          onClick={(event) => setThemePreference(option.value, originOf(event.currentTarget))}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

export function SiteFooter() {
  const explorerBase = publicConfiguration().deployment?.explorerBase;
  return (
    <footer className="app-footer">
      <div className="footer-columns">
        <nav className="footer-column" aria-labelledby="footer-product">
          <p id="footer-product">Product</p>
          <Link href={"/explore" as never}>Explore</Link>
          <Link href="/create">Create</Link>
          <Link href="/forum">Forum</Link>
        </nav>
        <nav className="footer-column" aria-labelledby="footer-data">
          <p id="footer-data">Data</p>
          <Link href="/leaderboard">Leaderboard</Link>
          <Link href="/analytics">Analytics</Link>
        </nav>
        <nav className="footer-column" aria-labelledby="footer-resources">
          <p id="footer-resources">Resources</p>
          <Link href="/docs">Docs</Link>
          <Link href="/docs#risk">Risk and finality</Link>
          {explorerBase ? (
            <SafeExternalLink href={explorerBase}>Block explorer ↗</SafeExternalLink>
          ) : (
            <span className="ui-disabled-link">Explorer unavailable</span>
          )}
        </nav>
        <nav className="footer-column" aria-labelledby="footer-account">
          <p id="footer-account">Account</p>
          <Link href="/profile">Profile</Link>
          <Link href={"/explore?view=watchlist" as never}>Watchlist</Link>
          <Link href="/notifications">Notifications</Link>
        </nav>
      </div>
      <div className="footer-bottom">
        <ThemeSwitcher />
        <p className="footer-note">
          Non-custodial. Your selected wallet signs every transaction; Launchpad never takes
          custody.
        </p>
        <p className="footer-meta">
          © 2026 Launchpad <CoinGeckoAttribution />
        </p>
        <p className="footer-risk">
          On-chain transactions may be irreversible. Tokens can be volatile or lose all value.
        </p>
      </div>
    </footer>
  );
}
