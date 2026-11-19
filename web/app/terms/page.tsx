import type { Metadata } from "next";
import Link from "next/link";
import { LegalPage } from "@/legal/legal-page";

export const metadata: Metadata = { title: "Terms of Service" };

// Draft: counsel replaces this text and sets src/legal/documents.json to "approved"
// (docs/specs/2026-11-18-legal-pages-preflight.md). Only product facts are stated here.
export default function TermsPage() {
  return (
    <LegalPage id="terms">
      <section>
        <h2>What Launchpad is</h2>
        <p>
          Launchpad is an interface to smart contracts on Robinhood Chain that launch fixed-supply
          tokens on a bonding curve and move them to a Uniswap pool at graduation. The interface
          does not hold, transfer or control your ETH or tokens. Your wallet signs every
          transaction, and the contracts, not Launchpad, execute it.
        </p>
      </section>
      <section>
        <h2>Your responsibilities</h2>
        <p>
          You are responsible for your wallet, its keys and every transaction you sign. On-chain
          transactions cannot be reversed. Tokens launched here are created by their users, not by
          Launchpad, and Launchpad does not endorse any token.
        </p>
      </section>
      <section>
        <h2>Risks</h2>
        <p>
          Trading launched tokens carries a high risk of loss. Read{" "}
          <Link href={"/docs#risk" as never}>Risks and custody</Link> before you trade.
        </p>
      </section>
      <section>
        <h2>To be completed by counsel</h2>
        <p>
          Eligibility and excluded jurisdictions, prohibited use, the operating entity, liability,
          disputes and governing law, and changes to these terms.
        </p>
      </section>
    </LegalPage>
  );
}
