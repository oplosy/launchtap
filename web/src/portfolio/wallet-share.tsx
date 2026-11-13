"use client";

import { useEffect, useState } from "react";
import { SafeExternalLink } from "@/components/primitives";
import { Check, Copy, ShareNetwork } from "@/components/icons";
import { publicWebOrigin } from "@/share/share-links";
import { walletPath } from "./wallet-model";

/** "Share on X" and "Copy link" for the wallet page; the canonical URL is the public web origin. */
export function WalletShare({ address }: { address: string }) {
  const [status, setStatus] = useState<"idle" | "copied" | "failed">("idle");
  useEffect(() => {
    if (status === "idle") return;
    const timer = window.setTimeout(() => setStatus("idle"), 2400);
    return () => window.clearTimeout(timer);
  }, [status]);
  const origin = publicWebOrigin();
  const canonical = origin ? `${origin}${walletPath(address)}` : "";
  const shareUrl = canonical
    ? `https://x.com/intent/post?${new URLSearchParams({ text: "Wallet on Launchpad", url: canonical })}`
    : "";
  return (
    <div className="share-actions">
      {shareUrl ? (
        <SafeExternalLink href={shareUrl} className="ui-button ui-button-secondary ui-button-sm">
          <ShareNetwork size={14} aria-hidden="true" /> Share on X
        </SafeExternalLink>
      ) : null}
      <button
        type="button"
        className="ui-button ui-button-secondary ui-button-sm"
        onClick={() => {
          const write = navigator.clipboard?.writeText(canonical || window.location.href);
          if (!write) {
            setStatus("failed");
            return;
          }
          write.then(
            () => setStatus("copied"),
            () => setStatus("failed"),
          );
        }}
      >
        {status === "copied" ? (
          <Check size={14} aria-hidden="true" />
        ) : (
          <Copy size={14} aria-hidden="true" />
        )}{" "}
        Copy link
      </button>
      <span className="share-status" role="status" aria-live="polite">
        {status === "copied"
          ? "Link copied"
          : status === "failed"
            ? "Copy is unavailable here; copy the address bar instead"
            : ""}
      </span>
    </div>
  );
}
