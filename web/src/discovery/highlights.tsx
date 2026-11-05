"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import { getHighlights, resolveApiAssetUrl, type TokenHighlights } from "@/api/client";
import type { components } from "@/api/generated";
import { Badge, EmptyState, SafeImage } from "@/components/primitives";
import { UsdLine } from "@/market/usd-line";
import { addressHue } from "@/token/address";
import { StarButton } from "@/watchlist/star-button";
import { TokenCard, displayAmount, tokenLabel, type TokenCardData } from "./token-card";

/**
 * The API flattens the token fields into each highlight item, but the generated schema only
 * lists the highlight-specific fields, so the token part is declared here.
 */
export type TrendingItem = TokenCardData & components["schemas"]["TrendingDTO"];
export type KingItem = TokenCardData & components["schemas"]["KingDTO"];

export const HIGHLIGHTS_REFRESH_MS = 30_000;

/** Basis points as a one-decimal percentage, clamped to 0-100. */
export function progressPercent(bps: number) {
  const clamped = Math.min(10_000, Math.max(0, Number.isFinite(bps) ? bps : 0));
  return `${(Math.floor(clamped / 10) / 10).toFixed(1)}%`;
}

export type HighlightsState = {
  data: TokenHighlights | null;
  loading: boolean;
  failed: boolean;
  retry: () => void;
};

export function useHighlights(apiBaseUrl: string | null): HighlightsState {
  const [data, setData] = useState<TokenHighlights | null>(null);
  const [loading, setLoading] = useState(apiBaseUrl !== null);
  const [failed, setFailed] = useState(false);
  const controllerRef = useRef<AbortController | null>(null);

  const load = useCallback(
    async (initial: boolean) => {
      if (!apiBaseUrl) return;
      controllerRef.current?.abort();
      const controller = (controllerRef.current = new AbortController());
      if (initial) setLoading(true);
      try {
        const next = await getHighlights(apiBaseUrl, controller.signal);
        if (controller.signal.aborted) return;
        setData(next);
        setFailed(false);
      } catch {
        if (controller.signal.aborted) return;
        // Keep the last good highlights through a transient refresh failure.
        if (initial) setFailed(true);
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    },
    [apiBaseUrl],
  );

  useEffect(() => {
    // Fetching is the intended synchronization with the external API.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load(true);
    const timer = setInterval(() => void load(false), HIGHLIGHTS_REFRESH_MS);
    return () => {
      clearInterval(timer);
      controllerRef.current?.abort();
    };
  }, [load]);

  const retry = useCallback(() => {
    setFailed(false);
    void load(true);
  }, [load]);

  return { data, loading, failed, retry };
}

export function KingOfTheHill({
  king,
  apiBaseUrl,
}: {
  king: KingItem | null | undefined;
  apiBaseUrl: string | null;
}) {
  if (!king) return null;
  const label = tokenLabel(king);
  const percent = progressPercent(king.graduation_progress_bps);
  const imageUrl = resolveApiAssetUrl(
    apiBaseUrl,
    `/v1/tokens/${encodeURIComponent(king.address)}/image`,
  );
  return (
    <article
      className="king-card"
      aria-label="King of the hill"
      style={{ "--hue": addressHue(king.address) } as CSSProperties}
    >
      <p className="panel-kicker">King of the hill</p>
      <div className="king-card-main">
        <SafeImage
          src={imageUrl}
          alt={`${label} token`}
          fallbackLabel="No artwork"
          className="token-image"
        />
        <div className="token-identity">
          <h3>
            <Link href={`/token/${king.address}` as never}>{label}</Link>
          </h3>
          <p className="token-symbol">
            <span>{king.symbol || "—"}</span>
          </p>
        </div>
        <Badge tone="accent">{percent} to graduation</Badge>
        <StarButton address={king.address} label={label} />
      </div>
      <div
        className="king-progress"
        role="progressbar"
        aria-label={`${label} graduation progress`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(king.graduation_progress_bps / 100)}
        aria-valuetext={percent}
      >
        <span style={{ width: percent }} />
      </div>
      <p className="king-remaining">
        {displayAmount(king.remaining_eth)} ETH remaining
        <UsdLine wei={king.remaining_eth} />
      </p>
    </article>
  );
}

export function TrendingList({
  items,
  apiBaseUrl,
}: {
  items: readonly TrendingItem[];
  apiBaseUrl: string | null;
}) {
  if (items.length === 0)
    return (
      <div className="discovery-state">
        <EmptyState
          title="Nothing is trending yet"
          description="Tokens with recent buying activity will appear here."
        />
      </div>
    );
  return (
    <div className="token-list" aria-live="polite">
      {items.map((item) => (
        <TokenCard
          key={item.address}
          token={item}
          apiBaseUrl={apiBaseUrl}
          extraMetrics={
            <>
              <div>
                <dt>Recent volume</dt>
                <dd>
                  {displayAmount(item.window_volume_eth)} ETH
                  <UsdLine wei={item.window_volume_eth} />
                </dd>
              </div>
              <div>
                <dt>Buyers</dt>
                <dd>{item.distinct_buyers.toLocaleString("en-US")}</dd>
              </div>
            </>
          }
        />
      ))}
    </div>
  );
}
