import type { Metadata } from "next";
import { LegalPage } from "@/legal/legal-page";

export const metadata: Metadata = { title: "Privacy Policy" };

// Draft: counsel replaces this text and sets src/legal/documents.json to "approved"
// (docs/specs/2026-11-18-legal-pages-preflight.md). Every statement below must match the code.
export default function PrivacyPage() {
  return (
    <LegalPage id="privacy">
      <section>
        <h2>Public blockchain data</h2>
        <p>
          Wallet addresses, transactions and balances on Robinhood Chain are public. Launchpad
          indexes them to show prices, trades, holders and leaderboards; it cannot remove them from
          the chain.
        </p>
      </section>
      <section>
        <h2>What you give us when you sign in</h2>
        <ul className="docs-list">
          <li>
            Sign-in runs through Privy, which issues the account identifier Launchpad stores. Privy
            processes your login details under its own policy.
          </li>
          <li>
            Data you choose to add: a username, wallets you publish on your profile, comments, forum
            posts, votes and reports, your watchlist, and, if you link it, a Telegram chat for
            alerts.
          </li>
          <li>Token creators: the description, links and image they upload for their token.</li>
        </ul>
      </section>
      <section>
        <h2>What the site collects on its own</h2>
        <ul className="docs-list">
          <li>
            Two functional cookies: your colour theme and whether you dismissed the risk notice.
          </li>
          <li>
            Anonymous error and page-speed counts: the page area, the error type and timing numbers,
            with no address, URL, message or identifier.
          </li>
          <li>
            Your IP address is used in memory to apply rate limits and is not stored by the
            application. The hosting provider&apos;s logs are covered below.
          </li>
        </ul>
        <p>No third-party analytics or advertising trackers run on this site.</p>
      </section>
      <section>
        <h2>To be completed by counsel</h2>
        <p>
          The controller and contact, legal bases, retention periods, hosting and log processors,
          international transfers, and how to exercise your rights.
        </p>
      </section>
    </LegalPage>
  );
}
