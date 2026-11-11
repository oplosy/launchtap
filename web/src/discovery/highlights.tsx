"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { getHighlights, type TokenHighlights } from "@/api/client";
import type { components } from "@/api/generated";
import { EmptyState } from "@/components/primitives";
import { UsdLine } from "@/market/usd-line";
import { TokenCard, TokenListHead, displayAmount } from "./token-card";

export type TrendingItem = components["schemas"]["TrendingDTO"];

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
