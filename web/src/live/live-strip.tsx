"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import { ApiClient, resolveApiAssetUrl } from "@/api/client";
import { SafeImage } from "@/components/primitives";
import type { LiveEvent } from "@/api/types";
import { publicConfiguration } from "@/config/public";
import { UsdLine } from "@/market/usd-line";
import { mergeRecent, tradeToFeedItem, FEED_MAX_ITEMS, type FeedItem } from "./feed-store";
import { useLiveEvents } from "./live-events";
import {
  liveRefreshKind,
  reduceStripState,
  toStripItem,
  type StripState,
  type StripItemView,
} from "./live-strip-model";

const TICK_MS = 15_000;

/** Live trade strip: recent trades, launches and graduations, newest first, max 50. */
export function LiveStrip() {
  const configuration = publicConfiguration();
  const ready =
    configuration.status === "ready" &&
    Boolean(configuration.apiBaseUrl) &&
    configuration.chainId !== null &&
    configuration.deploymentId !== null;
  const [state, setState] = useState<StripState>({ items: [], receivedAt: {} });
  const [now, setNow] = useState(() => Date.now());
  const chainId = configuration.chainId ?? -1;
  const deploymentId = configuration.deploymentId ?? "";
  const baseUrl = configuration.apiBaseUrl;

  const fetchRecent = useCallback(
    async (signal?: AbortSignal): Promise<FeedItem[]> => {
      if (!ready || !baseUrl) return [];
      const body = await new ApiClient({ baseUrl }).listRecentTrades(FEED_MAX_ITEMS, signal);
      return (body.items ?? [])
        .filter((trade) => trade.chain_id === chainId && trade.deployment_id === deploymentId)
        .map(tradeToFeedItem);
    },
    [baseUrl, chainId, deploymentId, ready],
  );
  const refill = useCallback(
    async (signal?: AbortSignal) => {
      const recent = await fetchRecent(signal);
      if (!signal?.aborted)
        setState((current) => ({ ...current, items: mergeRecent(current.items, recent) }));
    },
    [fetchRecent],
  );

  useEffect(() => {
    const controller = new AbortController();
    fetchRecent(controller.signal)
      .then((recent) => {
        if (!controller.signal.aborted)
          setState((current) => ({ ...current, items: mergeRecent(current.items, recent) }));
      })
      .catch(() => undefined);
    return () => controller.abort();
  }, [fetchRecent]);

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), TICK_MS);
    return () => clearInterval(timer);
  }, []);

  const onEvent = useCallback(
    (event: LiveEvent) => {
      // The recovery fetch ran before the new stream opened; refill once more to close the gap.
      if (liveRefreshKind(event, { chainId, deploymentId }) === "reconnected") {
        void refill().catch(() => undefined);
        return;
      }
      setState((current) =>
        reduceStripState(current, event, { chainId, deploymentId }, Date.now()),
      );
    },
    [chainId, deploymentId, refill],
  );
  useLiveEvents(onEvent, refill);

  const views = useMemo(
    () => state.items.map((item) => toStripItem(item, now, state.receivedAt)),
    [state, now],
  );
  if (!ready || views.length === 0) return null;

  return (
    <section className="live-strip" aria-label="Live activity">
      <span className="live-strip-label">Live</span>
      <ul className="live-strip-track">
        {views.map((view) => (
          <li key={view.key} className="live-strip-item" data-pending={view.pending || undefined}>
            <StripEntry view={view} baseUrl={baseUrl} />
          </li>
        ))}
      </ul>
    </section>
  );
}

function StripEntry({ view, baseUrl }: { view: StripItemView; baseUrl: string | null }) {
  const imageUrl = resolveApiAssetUrl(baseUrl, view.imagePath);
  const body = (
    <>
      {/* Decorative: the ticker names the token. A fixed-size box keeps the layout stable while
          the image loads or when it falls back to a letter. */}
      <span className="live-strip-image-box" aria-hidden="true">
        <SafeImage
          src={imageUrl}
          alt={view.ticker === "—" ? "" : view.ticker}
          fallbackLabel="No artwork"
          className="live-strip-image"
        />
      </span>
      {view.badge ? (
        <span className="live-strip-badge" data-kind={view.kind}>
          {view.badge}
        </span>
      ) : (
        <span className="live-strip-arrow" data-side={view.side ?? undefined} aria-hidden="true">
          {view.arrow ?? "·"}
        </span>
      )}
      <span className="live-strip-ticker" title={view.name}>
        {view.ticker}
      </span>
      {view.side ? <span className="sr-only">{view.side}</span> : null}
      {view.eth ? (
        <span className="live-strip-eth mono">
          {view.eth}
          {view.wei ? <UsdLine wei={view.wei} /> : null}
        </span>
      ) : null}
    </>
  );
  // The token link and the trader link are siblings: anchors cannot nest.
  return (
    <span className="live-strip-link">
      {view.href ? (
        <Link className="live-strip-part" href={view.href as never}>
          {body}
        </Link>
      ) : (
        <span className="live-strip-part">{body}</span>
      )}
      {view.traderHref ? (
        <Link
          className="live-strip-trader live-strip-trader-link mono"
          href={view.traderHref as never}
        >
          {view.trader}
        </Link>
      ) : (
        <span className="live-strip-trader mono">{view.trader}</span>
      )}
      <span className="live-strip-time mono">{view.time}</span>
      {view.pending ? <span className="sr-only">Updating</span> : null}
    </span>
  );
}
