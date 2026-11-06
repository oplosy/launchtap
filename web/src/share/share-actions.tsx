"use client";

import { Check, Copy, ShareNetwork } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { SafeExternalLink } from "@/components/primitives";
import { canonicalTokenUrl, publicWebOrigin, shareOnXUrl } from "./share-links";

type Status = "idle" | "copied" | "failed";

/** "Share on X" and "Copy link" for a token page. The canonical URL is the public web origin. */
export function ShareActions({ address, symbol }: { address: string; symbol: string }) {
  const [status, setStatus] = useState<Status>("idle");
  useEffect(() => {
    if (status === "idle") return;
    const timer = window.setTimeout(() => setStatus("idle"), 2400);
    return () => window.clearTimeout(timer);
  }, [status]);
  const origin = publicWebOrigin();
  const canonical = origin ? canonicalTokenUrl(origin, address) : "";
  return (
    <div className="share-actions">
      {canonical ? (
        <SafeExternalLink
          href={shareOnXUrl(symbol, canonical)}
          className="ui-button ui-button-secondary ui-button-sm"
        >
          <ShareNetwork size={14} aria-hidden="true" /> Share on X
        </SafeExternalLink>
      ) : null}
      <button
        type="button"
        className="ui-button ui-button-secondary ui-button-sm"
        onClick={() => {
          const target = canonical || window.location.href;
          const write = navigator.clipboard?.writeText(target);
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
