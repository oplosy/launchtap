"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import { getHighlights, resolveApiAssetUrl, type TokenHighlights } from "@/api/client";
import type { components } from "@/api/generated";
import { EmptyState, SafeImage } from "@/components/primitives";
import { UsdLine } from "@/market/usd-line";
import { addressHue } from "@/token/address";
import { StarButton } from "@/watchlist/star-button";
import { TokenCard, TokenListHead, displayAmount, tokenLabel } from "./token-card";

export type TrendingItem = components["schemas"]["TrendingDTO"];
export type KingItem = components["schemas"]["KingDTO"];

export const HIGHLIGHTS_REFRESH_MS = 30_000;

/** Basis points as a one-decimal percentage, clamped to 0-100. */
export function progressPercent(bps: number) {
  const clamped = Math.min(10_000, Math.max(0, Number.isFinite(bps) ? bps : 0));
  if (clamped > 0 && clamped < 10) return "<0.1%";
  return `${(Math.floor(clamped / 10) / 10).toFixed(1)}%`;
}

/** Kept highlights older than this (two missed refreshes) are flagged as possibly out of date. */
export const HIGHLIGHTS_STALE_MS = 2 * HIGHLIGHTS_REFRESH_MS;

export function highlightsStale(lastSuccessAt: number | null, now: number) {
  return lastSuccessAt !== null && now - lastSuccessAt > HIGHLIGHTS_STALE_MS;
}

type VisibilityDocument = Pick<
  Document,
  "visibilityState" | "addEventListener" | "removeEventListener"
>;

/**
 * Calls `tick` every `intervalMs` while the document is visible, skips ticks while it is hidden,
 * and refreshes immediately when it becomes visible again. Returns the cleanup function.
 */
export function startVisiblePolling(
  tick: () => void,
  intervalMs: number,
  doc: VisibilityDocument | null,
) {
  const timer = setInterval(() => {
    if (doc?.visibilityState !== "hidden") tick();
  }, intervalMs);
  const onVisibility = () => {
    if (doc?.visibilityState !== "hidden") tick();
  };
  doc?.addEventListener("visibilitychange", onVisibility);
  return () => {
    clearInterval(timer);
    doc?.removeEventListener("visibilitychange", onVisibility);
  };
}

export type HighlightsState = {
  data: TokenHighlights | null;
  loading: boolean;
  failed: boolean;
  /** True when the shown data is kept from an earlier success because refreshes keep failing. */
  stale: boolean;
  retry: () => void;
};

export function useHighlights(apiBaseUrl: string | null): HighlightsState {
  const [data, setData] = useState<TokenHighlights | null>(null);
  const [loading, setLoading] = useState(apiBaseUrl !== null);
  const [failed, setFailed] = useState(false);
  const [stale, setStale] = useState(false);
  const controllerRef = useRef<AbortController | null>(null);
  const lastSuccessRef = useRef<number | null>(null);

  const load = useCallback(
    async (initial: boolean) => {
      if (!apiBaseUrl) return;
      controllerRef.current?.abort();
      const controller = (controllerRef.current = new AbortController());
      if (initial) setLoading(true);
      try {
        const next = await getHighlights(apiBaseUrl, controller.signal);
        if (controller.signal.aborted) return;
        lastSuccessRef.current = Date.now();
        setData(next);
        setFailed(false);
        setStale(false);
      } catch {
        if (controller.signal.aborted) return;
        // Keep the last good highlights through a transient refresh failure, but say so once
        // they are old enough that the ranking may have changed.
        if (initial && lastSuccessRef.current === null) setFailed(true);
        else setStale(highlightsStale(lastSuccessRef.current, Date.now()));
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
    const stop = startVisiblePolling(
      () => void load(false),
      HIGHLIGHTS_REFRESH_MS,
      typeof document === "undefined" ? null : document,
    );
    return () => {
      stop();
      controllerRef.current?.abort();
    };
  }, [load]);

  const retry = useCallback(() => {
    setFailed(false);
    void load(true);
  }, [load]);

  return { data, loading, failed, stale, retry };
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
  const href = `/token/${king.address}`;
  return (
    <article
      className="king-card"
      aria-label="King of the hill"
      style={{ "--hue": addressHue(king.address) } as CSSProperties}
    >
      <SafeImage
        src={imageUrl}
        alt={`${label} token`}
        fallbackLabel="No artwork"
        className="token-image king-art"
      />
      <div className="king-body">
        <div className="king-head">
          <div className="token-identity">
            <p className="panel-kicker">King of the hill</p>
            <h3>
              <Link href={href as never}>{label}</Link>
            </h3>
            <p className="token-symbol">
              <span>{king.symbol || "—"}</span>
            </p>
          </div>
          <StarButton address={king.address} label={label} />
        </div>
        <dl className="king-stats">
          <div>
            <dt>Market cap</dt>
            <dd className="mono">{displayAmount(king.market_cap_eth)} ETH</dd>
          </div>
          <div>
            <dt>Holders</dt>
            <dd className="mono">{king.holder_count.toLocaleString()}</dd>
          </div>
          <div>
            <dt>To graduate</dt>
            <dd className="mono">
              {displayAmount(king.remaining_eth)} ETH
              <UsdLine wei={king.remaining_eth} />
            </dd>
          </div>
        </dl>
        <div className="king-progress-row">
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
          <span className="king-percent mono">{percent} to graduation</span>
        </div>
        <Link
          className="ui-button ui-button-primary ui-button-md king-trade"
          href={href as never}
          aria-label={`Trade ${label}`}
        >
          Trade
        </Link>
      </div>
    </article>
  );
}

export function TrendingList({
  items,
  apiBaseUrl,
  layout = "list",
  className,
}: {
  items: readonly TrendingItem[];
  apiBaseUrl: string | null;
  layout?: "list" | "grid";
  /** Extra class on the list, e.g. a fixed column count for a home section. */
  className?: string;
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
    <div
      className={className ? `token-list ${className}` : "token-list"}
      data-layout={layout}
      aria-live="polite"
    >
      <TokenListHead extra={["Recent volume", "Buyers"]} />
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
