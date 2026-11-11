"use client";

import Link from "next/link";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
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
/** Marquee speed in px per second, the same however many items there are. */
const SPEED_PX_PER_S = 40;

/**
 * Scrolls the rail left at a constant speed and loops on the width of the first copy. The offset
 * is kept in pixels, so a new trade at the front never restarts the loop: after the list changes,
 * the item that was at the left edge is found again and the offset follows it.
 */
function useMarquee(itemsKey: string) {
  const railRef = useRef<HTMLDivElement>(null);
  const trackRef = useRef<HTMLUListElement>(null);
  const offset = useRef(0);
  const paused = useRef(false);
  const anchor = useRef<{ key: string; left: number } | null>(null);

  // The strip renders nothing until it has items, so the loop starts once the rail exists.
  const mounted = itemsKey !== "";
  useEffect(() => {
    const rail = railRef.current;
    const track = trackRef.current;
    if (!mounted || !rail || !track) return;
    const motion = window.matchMedia("(prefers-reduced-motion: reduce)");
    let frame = 0;
    let last = 0;
    const step = (time: number) => {
      // A hidden tab pauses frames: never jump by the whole time it was away.
      const delta = last ? Math.min(time - last, 100) : 0;
      last = time;
      const width = track.offsetWidth;
      if (!motion.matches && width > 0) {
        if (!paused.current) offset.current += (SPEED_PX_PER_S * delta) / 1000;
        offset.current %= width;
        rail.style.transform = `translate3d(${-offset.current}px, 0, 0)`;
        // Remember the leftmost visible item so a list change can keep it in place.
        const items = track.children;
        for (let index = 0; index < items.length; index++) {
          const item = items[index] as HTMLElement;
          if (item.offsetLeft + item.offsetWidth > offset.current) {
            anchor.current = { key: item.dataset.key ?? "", left: item.offsetLeft };
            break;
          }
        }
      } else {
        rail.style.transform = "";
      }
      frame = requestAnimationFrame(step);
    };
    frame = requestAnimationFrame(step);
    return () => cancelAnimationFrame(frame);
  }, [mounted]);

  useLayoutEffect(() => {
    const track = trackRef.current;
    const kept = anchor.current;
    if (!track || !kept) return;
    const item = [...track.children].find(
      (child) => (child as HTMLElement).dataset.key === kept.key,
    ) as HTMLElement | undefined;
    if (item) offset.current += item.offsetLeft - kept.left;
  }, [itemsKey]);

  const pause = useCallback(() => {
    paused.current = true;
  }, []);
  const resume = useCallback(() => {
    paused.current = false;
  }, []);
  return { railRef, trackRef, pause, resume };
}

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
  const { railRef, trackRef, pause, resume } = useMarquee(views.map((view) => view.key).join("|"));

  if (!ready || views.length === 0) return null;

  // The rail holds the list and an identical, inert copy behind it, so scrolling by the width of
  // the list loops without a seam.
  return (
    <section
      className="live-strip"
      aria-label="Live activity"
      onMouseEnter={pause}
      onMouseLeave={resume}
      onFocus={pause}
      onBlur={resume}
    >
      <div className="live-strip-viewport">
        <div className="live-strip-rail" ref={railRef}>
          <ul className="live-strip-track" ref={trackRef}>
            {views.map((view) => (
              <li
                key={view.key}
                data-key={view.key}
                className="live-strip-item"
                data-pending={view.pending || undefined}
              >
                <StripEntry view={view} baseUrl={baseUrl} />
              </li>
            ))}
          </ul>
          <div className="live-strip-clone" aria-hidden="true" inert>
            {views.map((view) => (
              <div
                key={view.key}
                className="live-strip-item"
                data-pending={view.pending || undefined}
              >
                <StripEntry view={view} baseUrl={baseUrl} />
              </div>
            ))}
          </div>
        </div>
      </div>
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
      <span className="live-strip-ticker" title={view.name}>
        {view.ticker}
      </span>
      {view.badge ? (
        <span className="live-strip-badge" data-kind={view.kind}>
          {view.badge}
        </span>
      ) : null}
      {view.side ? <span className="sr-only">{view.side}</span> : null}
      {/* Direction and size read as one coloured value: green for a buy, red for a sell. */}
      {view.arrow || view.eth ? (
        <span className="live-strip-value mono" data-side={view.side ?? undefined}>
          {view.arrow ? (
            <span className="live-strip-arrow" aria-hidden="true">
              {view.arrow}
            </span>
          ) : null}
          {view.eth ? <span className="live-strip-eth">{view.eth}</span> : null}
          {view.eth && view.wei ? <UsdLine wei={view.wei} /> : null}
        </span>
      ) : null}
    </>
  );
  return (
    <span className="live-strip-link">
      {view.href ? (
        <Link className="live-strip-part" href={view.href as never}>
          {body}
        </Link>
      ) : (
        <span className="live-strip-part">{body}</span>
      )}
      {view.pending ? <span className="sr-only">Updating</span> : null}
    </span>
  );
}
