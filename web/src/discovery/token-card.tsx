"use client";

import Link from "next/link";
import type { CSSProperties, PointerEvent, ReactNode } from "react";
import { resolveApiAssetUrl } from "@/api/client";
import type { components } from "@/api/generated";
import { formatDisplayAmount } from "@/amounts";
import { Badge, SafeImage } from "@/components/primitives";
import { UsdLine } from "@/market/usd-line";
import { addressHue, shortAddress } from "@/token/address";
import { StarButton } from "@/watchlist/star-button";

export type TokenCardData = components["schemas"]["TokenDTO"];

export function displayAmount(raw: string, decimals = 18) {
  try {
    return formatDisplayAmount(BigInt(raw), decimals, 4);
  } catch {
    return "Unavailable";
  }
}

export function tokenLabel(token: TokenCardData) {
  return token.name.trim() || token.symbol.trim() || "Unnamed token";
}

// Feeds the card's cursor-following highlight; CSS reads --mx/--my.
export function trackSpotlight(event: PointerEvent<HTMLElement>) {
  const box = event.currentTarget.getBoundingClientRect();
  event.currentTarget.style.setProperty("--mx", `${event.clientX - box.left}px`);
  event.currentTarget.style.setProperty("--my", `${event.clientY - box.top}px`);
}

export function TokenCard({
  token,
  apiBaseUrl,
  extraMetrics,
}: {
  token: TokenCardData;
  apiBaseUrl: string | null;
  /** Additional `<div><dt/><dd/></div>` rows for the metrics list. */
  extraMetrics?: ReactNode;
}) {
  const label = tokenLabel(token);
  const imageUrl = resolveApiAssetUrl(
    apiBaseUrl,
    `/v1/tokens/${encodeURIComponent(token.address)}/image`,
  );
  return (
    <article
      className="token-card"
      onPointerMove={trackSpotlight}
      style={{ "--hue": addressHue(token.address) } as CSSProperties}
    >
      <div className="token-card-main">
        <SafeImage
          src={imageUrl}
          alt={`${label} token`}
          fallbackLabel="No artwork"
          className="token-image"
        />
        <div className="token-identity">
          <h3>
            <Link href={`/token/${token.address}` as never}>{label}</Link>
          </h3>
          <p className="token-symbol">
            <span>{token.symbol || "—"}</span>
            <span className="mono token-address">{shortAddress(token.address, 4)}</span>
          </p>
        </div>
        <Badge tone={token.phase === "graduated" ? "success" : "accent"}>{token.phase}</Badge>
        <StarButton address={token.address} label={label} />
      </div>
      <dl className="token-metrics">
        <div>
          <dt>Market cap</dt>
          <dd>
            {displayAmount(token.market_cap_eth)} ETH
            <UsdLine wei={token.market_cap_eth} />
          </dd>
        </div>
        <div>
          <dt>24h volume</dt>
          <dd>
            {displayAmount(token.volume_24h_eth)} ETH
            <UsdLine wei={token.volume_24h_eth} />
          </dd>
        </div>
        <div>
          <dt>Holders</dt>
          <dd>{token.holder_count.toLocaleString("en-US")}</dd>
        </div>
        {extraMetrics}
      </dl>
    </article>
  );
}
