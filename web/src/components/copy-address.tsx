"use client";

import { useEffect, useState } from "react";
import { Check, Copy } from "@/components/icons";
import { shortAddress } from "@/token/address";

/** Short address with one-click copy; stays a plain label where the clipboard is unavailable. */
export function CopyAddress({ address }: { address: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1600);
    return () => window.clearTimeout(timer);
  }, [copied]);
  return (
    <button
      type="button"
      className="copy-address mono"
      title={address}
      aria-label={copied ? "Address copied" : `Copy address ${address}`}
      onClick={() =>
        void navigator.clipboard
          ?.writeText(address)
          .then(() => setCopied(true))
          .catch(() => undefined)
      }
    >
      {shortAddress(address)}
      {copied ? <Check size={13} aria-hidden="true" /> : <Copy size={13} aria-hidden="true" />}
    </button>
  );
}
