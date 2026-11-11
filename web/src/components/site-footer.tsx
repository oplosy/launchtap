"use client";

import Link from "next/link";
import {
  ArrowUpRight,
  CaretDown,
  DiscordLogo,
  FacebookLogo,
  GithubLogo,
  InstagramLogo,
  LinkedinLogo,
  MediumLogo,
  RedditLogo,
  TelegramLogo,
  ThreadsLogo,
  TiktokLogo,
  TwitchLogo,
  XLogo,
  YoutubeLogo,
  type Icon,
} from "@phosphor-icons/react";
import { SafeExternalLink } from "./primitives";
import { publicConfiguration } from "@/config/public";
import { CoinGeckoAttribution } from "@/market/usd-line";

/** Accounts are not set up yet: the icons are shown, not linked. */
const SOCIAL: ReadonlyArray<{ label: string; icon: Icon }> = [
  { label: "X", icon: XLogo },
  { label: "Telegram", icon: TelegramLogo },
  { label: "Discord", icon: DiscordLogo },
  { label: "GitHub", icon: GithubLogo },
  { label: "YouTube", icon: YoutubeLogo },
  { label: "Instagram", icon: InstagramLogo },
  { label: "TikTok", icon: TiktokLogo },
  { label: "Reddit", icon: RedditLogo },
  { label: "LinkedIn", icon: LinkedinLogo },
  { label: "Facebook", icon: FacebookLogo },
  { label: "Threads", icon: ThreadsLogo },
  { label: "Twitch", icon: TwitchLogo },
  { label: "Medium", icon: MediumLogo },
];

/**
 * One footer row: a hairline-separated label. Open, its links fill the column to the right of
 * the label, one per row, with the chevron kept on the first row.
 */
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
      <div className="footer-main">
        <Link href="/" className="footer-brand" aria-label="Launchpad home">
          Launchpad
        </Link>
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
          <FooterSection label="Learn">
            <Link href={"/docs#overview" as never}>How it works</Link>
            <Link href={"/docs#curve" as never}>The bonding curve</Link>
            <Link href={"/docs#graduation" as never}>Graduation</Link>
          </FooterSection>
          <FooterSection label="Protocol">
            <Link href={"/docs#economics" as never}>Fees and economics</Link>
            <Link href={"/docs#token" as never}>Token rules</Link>
            <Link href={"/docs#governance" as never}>Pauses and governance</Link>
          </FooterSection>
          <FooterSection label="Account">
            <Link href="/profile">Profile</Link>
            <Link href={"/explore?view=watchlist" as never}>Watchlist</Link>
            <Link href="/notifications">Notifications</Link>
          </FooterSection>
        </div>
        <aside className="footer-aside" aria-label="About Launchpad">
          <div className="footer-aside-block">
            <h2>Non-custodial</h2>
            <p>
              Fixed-supply launches on Robinhood Chain. Your selected wallet signs every
              transaction; Launchpad never takes custody.
            </p>
          </div>
          <div className="footer-aside-block">
            <h2>Get started</h2>
            <Link href="/create">
              Launch a token <ArrowUpRight size={12} aria-hidden="true" />
            </Link>
            <Link href={"/explore" as never}>
              Explore launches <ArrowUpRight size={12} aria-hidden="true" />
            </Link>
          </div>
        </aside>
        {/* Wide layout: in the brand's column, its last line level with the last section's rule. */}
        <div className="footer-aside-block footer-follow">
          <h2>Follow</h2>
          <ul className="footer-social" aria-label="Social media">
            {SOCIAL.map(({ label, icon: SocialIcon }) => (
              <li key={label}>
                <span role="img" aria-label={`${label} (coming soon)`} title={label}>
                  <SocialIcon size={18} aria-hidden="true" />
                </span>
              </li>
            ))}
          </ul>
        </div>
      </div>
      <div className="footer-bottom">
        <p className="footer-meta">
          <strong>© 2026 Launchpad</strong> <CoinGeckoAttribution />
        </p>
      </div>
    </footer>
  );
}
