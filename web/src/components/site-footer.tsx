"use client";

import Link from "next/link";
import { CaretDown } from "@phosphor-icons/react";
import { SafeExternalLink } from "./primitives";
import { publicConfiguration } from "@/config/public";
import { CoinGeckoAttribution } from "@/market/usd-line";

/** One footer row: a hairline-separated label that opens to its links. */
function FooterSection({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <details className="footer-section">
      <summary>
        <span>{label}</span>
        <CaretDown size={16} aria-hidden="true" />
      </summary>
      <nav className="footer-section-links" aria-label={label}>
        {children}
      </nav>
    </details>
  );
}

export function SiteFooter() {
  const explorerBase = publicConfiguration().deployment?.explorerBase;
  return (
    <footer className="app-footer">
      <div className="footer-sections">
        <FooterSection label="Product">
          <Link href={"/explore" as never}>Explore</Link>
          <Link href="/create">Create</Link>
          <Link href="/forum">Forum</Link>
        </FooterSection>
        <FooterSection label="Data">
          <Link href="/leaderboard">Leaderboard</Link>
          <Link href="/analytics">Analytics</Link>
        </FooterSection>
        <FooterSection label="Resources">
          <Link href="/docs">Docs</Link>
          <Link href="/docs#risk">Risk and finality</Link>
          {explorerBase ? (
            <SafeExternalLink href={explorerBase}>Block explorer ↗</SafeExternalLink>
          ) : (
            <span className="ui-disabled-link">Explorer unavailable</span>
          )}
        </FooterSection>
        <FooterSection label="Account">
          <Link href="/profile">Profile</Link>
          <Link href={"/explore?view=watchlist" as never}>Watchlist</Link>
          <Link href="/notifications">Notifications</Link>
        </FooterSection>
      </div>
      <div className="footer-bottom">
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
