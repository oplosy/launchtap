"use client";

import Link from "next/link";
import { useState, type CSSProperties, type ReactNode } from "react";
import { resolveApiAssetUrl } from "@/api/client";
import type { components } from "@/api/generated";
import { formatDisplayAmount } from "@/amounts";
import { SafeImage } from "@/components/primitives";
import { relativeTime } from "@/live/live-strip-model";
import { UsdLine } from "@/market/usd-line";
import { addressHue } from "@/token/address";
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

/**
 * One token in a list. The same markup is a ~72 px hairline row in a list and an image-first tile
 * in a grid; the parent `.token-list[data-layout]` decides. The whole item is a link (the name's
 * link stretches over it) while the star stays its own button above that link.
 */
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
  // The age is read once when the item mounts; lists re-mount items on refresh.
  const [now] = useState(Date.now);
  const imageUrl = resolveApiAssetUrl(
    apiBaseUrl,
    `/v1/tokens/${encodeURIComponent(token.address)}/image`,
  );
  return (
    <article className="token-card" style={{ "--hue": addressHue(token.address) } as CSSProperties}>
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
          {token.phase === "graduated" ? <span className="token-phase">Graduated</span> : null}
        </p>
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
        <div>
          <dt>Age</dt>
          <dd>{relativeTime(token.launch_time, now)}</dd>
        </div>
      </dl>
      <StarButton address={token.address} label={label} />
    </article>
  );
}
