"use client";

import Link from "next/link";
import { useState } from "react";
import { riskAcknowledgementCookie } from "./documents";

/**
 * First-visit risk notice (docs/specs/2026-11-18-legal-pages-preflight.md). Non-blocking: it sits
 * above the page content until the visitor acknowledges it, which sets a one-year cookie the root
 * layout reads on the server.
 */
export function RiskNotice({ initiallyOpen }: { initiallyOpen: boolean }) {
  const [open, setOpen] = useState(initiallyOpen);
  if (!open) return null;
  const acknowledge = () => {
    document.cookie = riskAcknowledgementCookie(window.location.protocol === "https:");
    setOpen(false);
  };
  return (
    <section className="risk-notice" aria-label="Risk notice">
      <p>
        Launched tokens are highly speculative and most lose their value. Transactions are final,
        and your wallet signs every one; Launchpad never holds your funds.{" "}
        <Link href={"/docs#risk" as never}>Read the risks</Link> and the{" "}
        <Link href={"/terms" as never}>terms</Link>.
      </p>
      <button
        type="button"
        className="ui-button ui-button-secondary ui-button-sm"
        onClick={acknowledge}
      >
        I understand
      </button>
    </section>
  );
}
