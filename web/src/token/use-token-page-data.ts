"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ApiClient, type TokenDetailResponse } from "@/api/client";
import type { LiveEvent } from "@/api/types";
import type { publicConfiguration } from "@/config/public";
import { useLiveEvents } from "@/live/live-events";
import type { FeedItem } from "@/live/feed-store";
import { liveRefreshKind, reduceTokenTrades } from "@/live/live-strip-model";
import { transformCandles, type CandleChartPoint } from "./chart-data";
import {
  appendTokenCollectionPage,
  type TokenCollection,
  type TokenCollectionFetcher,
} from "./pagination";
import { createCoalescedRefresh } from "./coalesced-refresh";
import { isTokenEventForAddress } from "./sse-scope";
import { PRICE_FLASH_MS, priceDirection } from "./price-flash";

export type Tab = "trades" | "holders";
export const intervals = ["1m", "5m", "1h", "1d", "6h", "all"] as const;
export type CandleInterval = (typeof intervals)[number];
export const CANDLE_LIMIT = 100;

type Configuration = ReturnType<typeof publicConfiguration>;

/**
 * The token page's REST and live state: the token, its candles, the active history tab's pages
 * and the live trade rows. Requests are tagged with the route and a per-kind request id, so a
 * response for a previous token or a superseded request never lands.
 */
export function useTokenPageData(
  tokenAddress: `0x${string}`,
  configuration: Configuration,
  useFixture: boolean,
) {
  const [token, setToken] = useState<TokenDetailResponse | null>(null);
  const [tokenError, setTokenError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(true);
  const [interval, setInterval] = useState<(typeof intervals)[number]>("1h");
  const [chartPoints, setChartPoints] = useState<CandleChartPoint[]>([]);
  const [chartLoading, setChartLoading] = useState(false);
  const [chartError, setChartError] = useState<Error | null>(null);
  const [tab, setTab] = useState<Tab>("trades");
  const [pages, setPages] = useState<TokenCollection[]>([]);
  const [collectionLoading, setCollectionLoading] = useState(false);
  const [collectionError, setCollectionError] = useState<Error | null>(null);
  const [resetNotice, setResetNotice] = useState(false);
  const [liveFeed, setLiveFeed] = useState<{ address: string; items: FeedItem[] }>({
    address: "",
    items: [],
  });
  // A spot-price change is hinted for PRICE_FLASH_MS; never under reduced motion.
  const [priceFlash, setPriceFlash] = useState<{ direction: "up" | "down"; id: number } | null>(
    null,
  );
  // Below 1024 px the trade card opens as a sheet from the bottom bar; the side picks its tab.
  const clientRef = useRef<ApiClient | null>(null);
  const tokenRef = useRef<TokenDetailResponse | null>(null);
  const tokenAbortRef = useRef<AbortController | null>(null);
  const candleAbortRef = useRef<AbortController | null>(null);
  const collectionAbortRef = useRef<AbortController | null>(null);
  const requestIds = useRef({ token: 0, candle: 0, collection: 0 });
  const pagesRef = useRef<TokenCollection[]>([]);
  const tabRef = useRef<Tab>("trades");
  const loadersRef = useRef<{
    loadToken: typeof loadToken;
    loadCandles: typeof loadCandles;
    loadCollection: typeof loadCollection;
  } | null>(null);

  const loadToken = useCallback(
    async (externalSignal?: AbortSignal) => {
      const requestId = ++requestIds.current.token;
      tokenAbortRef.current?.abort();
      if (configuration.status !== "ready" || !configuration.apiBaseUrl) {
        setLoading(false);
        tokenRef.current = null;
        return null;
      }
      const controller = new AbortController();
      const forwardAbort = () => controller.abort();
      externalSignal?.addEventListener("abort", forwardAbort, { once: true });
      if (externalSignal?.aborted) controller.abort();
      const client = new ApiClient({ baseUrl: configuration.apiBaseUrl });
      clientRef.current = client;
      tokenAbortRef.current = controller;
      setLoading(true);
      setTokenError(null);
      try {
        const next = await client.getToken(tokenAddress, controller.signal);
        if (
          !controller.signal.aborted &&
          requestId === requestIds.current.token &&
          activeRouteRef.current === tokenAddress
        ) {
          const previous = tokenRef.current;
          const direction =
            previous && previous.address === next.address
              ? priceDirection(previous.spot_price_eth, next.spot_price_eth)
              : null;
          if (direction && !window.matchMedia("(prefers-reduced-motion: reduce)").matches)
            setPriceFlash({ direction, id: Date.now() });
          tokenRef.current = next;
          setToken(next);
        }
        return next;
      } catch (cause) {
        if (
          !controller.signal.aborted &&
          requestId === requestIds.current.token &&
          activeRouteRef.current === tokenAddress
        )
          setTokenError(cause instanceof Error ? cause : new Error("Token unavailable"));
        throw cause;
      } finally {
        if (
          !controller.signal.aborted &&
          requestId === requestIds.current.token &&
          activeRouteRef.current === tokenAddress
        )
          setLoading(false);
        if (tokenAbortRef.current === controller) tokenAbortRef.current = null;
        externalSignal?.removeEventListener("abort", forwardAbort);
      }
    },
    [configuration.apiBaseUrl, configuration.status, tokenAddress],
  );

  const loadCandles = useCallback(
    async (externalSignal?: AbortSignal) => {
      const requestId = ++requestIds.current.candle;
      candleAbortRef.current?.abort();
      const client = clientRef.current;
      if (!client || !tokenRef.current) return null;
      const controller = new AbortController();
      const forwardAbort = () => controller.abort();
      externalSignal?.addEventListener("abort", forwardAbort, { once: true });
      if (externalSignal?.aborted) controller.abort();
      candleAbortRef.current = controller;
      setChartLoading(true);
      setChartError(null);
      try {
        const response = await client.getCandles(
          tokenAddress,
          { interval, limit: CANDLE_LIMIT },
          controller.signal,
        );
        if (
          !controller.signal.aborted &&
          requestId === requestIds.current.candle &&
          activeRouteRef.current === tokenAddress
        )
          setChartPoints(transformCandles(response.items));
        return response;
      } catch (cause) {
        if (
          !controller.signal.aborted &&
          requestId === requestIds.current.candle &&
          activeRouteRef.current === tokenAddress
        )
          setChartError(cause instanceof Error ? cause : new Error("Chart unavailable"));
        throw cause;
      } finally {
        if (
          !controller.signal.aborted &&
          requestId === requestIds.current.candle &&
          activeRouteRef.current === tokenAddress
        )
          setChartLoading(false);
        if (candleAbortRef.current === controller) candleAbortRef.current = null;
        externalSignal?.removeEventListener("abort", forwardAbort);
      }
    },
    [interval, tokenAddress],
  );

  const loadCollection = useCallback(
    async (selectedTab: Tab, cursor?: string, append = false, externalSignal?: AbortSignal) => {
      const requestId = ++requestIds.current.collection;
      collectionAbortRef.current?.abort();
      const client = clientRef.current;
      if (!client) return null;
      const controller = new AbortController();
      const forwardAbort = () => controller.abort();
      externalSignal?.addEventListener("abort", forwardAbort, { once: true });
      if (externalSignal?.aborted) controller.abort();
      collectionAbortRef.current = controller;
      const fetchPage: TokenCollectionFetcher = (nextCursor, signal) =>
        selectedTab === "trades"
          ? client.getTrades(tokenAddress, { cursor: nextCursor, limit: 25 }, signal)
          : client.getHolders(tokenAddress, { cursor: nextCursor, limit: 25 }, signal);
      const currentPages = append && selectedTab === tabRef.current ? pagesRef.current : [];
      setCollectionLoading(true);
      setCollectionError(null);
      try {
        const result =
          append && cursor
            ? await appendTokenCollectionPage(fetchPage, currentPages, cursor, controller.signal)
            : { pages: [await fetchPage(undefined, controller.signal)], reset: false };
        if (
          !controller.signal.aborted &&
          requestId === requestIds.current.collection &&
          activeRouteRef.current === tokenAddress &&
          selectedTab === tabRef.current
        ) {
          pagesRef.current = result.pages;
          setPages(result.pages);
          setResetNotice(result.reset);
        }
      } catch (cause) {
        if (
          !controller.signal.aborted &&
          requestId === requestIds.current.collection &&
          activeRouteRef.current === tokenAddress &&
          selectedTab === tabRef.current
        )
          setCollectionError(cause instanceof Error ? cause : new Error("History unavailable"));
        throw cause;
      } finally {
        if (
          !controller.signal.aborted &&
          requestId === requestIds.current.collection &&
          activeRouteRef.current === tokenAddress &&
          selectedTab === tabRef.current
        )
          setCollectionLoading(false);
        if (collectionAbortRef.current === controller) collectionAbortRef.current = null;
        externalSignal?.removeEventListener("abort", forwardAbort);
      }
    },
    [tokenAddress],
  );

  const activeRouteRef = useRef<string | null>(null);

  useEffect(() => {
    tabRef.current = tab;
  }, [tab]);
  useEffect(() => {
    loadersRef.current = { loadToken, loadCandles, loadCollection };
  }, [loadCandles, loadCollection, loadToken]);

  useEffect(() => {
    // The route identity is an external-request guard, not render state.
    // eslint-disable-next-line react-hooks/immutability
    activeRouteRef.current = tokenAddress;
    tokenAbortRef.current?.abort();
    candleAbortRef.current?.abort();
    collectionAbortRef.current?.abort();
    tokenRef.current = null;
    clientRef.current = null;
    pagesRef.current = [];
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setToken(null);
    setTokenError(null);
    setChartPoints([]);
    setChartError(null);
    setPages([]);
    setResetNotice(false);
    let active = true;
    void (async () => {
      const next = await loadToken();
      if (!active || !next) return;
      // Candles and history depend only on the loaded token, not on each other.
      await Promise.all([loadCandles(), loadCollection(tabRef.current)]);
    })().catch(() => undefined);
    return () => {
      active = false;
      tokenAbortRef.current?.abort();
      candleAbortRef.current?.abort();
      collectionAbortRef.current?.abort();
    };
    // This is one route-entry fetch sequence. Interval and tab changes use their own effects.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadToken, tokenAddress]);

  useEffect(() => {
    if (!tokenRef.current) return;
    void loadCandles().catch(() => undefined);
  }, [loadCandles]);

  useEffect(() => {
    if (!tokenRef.current) return;
    pagesRef.current = [];
    setPages([]);
    void loadCollection(tab).catch(() => undefined);
  }, [loadCollection, tab]);

  // Hints that arrive during a refresh collapse into one follow-up refresh; an abort (a reconnect
  // reload cancelled it) is swallowed there, not surfaced as an unhandled rejection.
  const refreshAllRef = useRef<(() => void) | null>(null);
  // Bumped on each `comment` hint for this token and on reconnect; the panel coalesces refetches.
  const [commentSignal, setCommentSignal] = useState(0);
  const onLiveEvent = useCallback(
    (event: LiveEvent) => {
      if (useFixture || configuration.status !== "ready") return;
      if (event.event === "comment") {
        if (
          event.data.chain_id === configuration.chainId &&
          event.data.deployment_id === configuration.deploymentId &&
          isTokenEventForAddress(event, tokenAddress)
        )
          setCommentSignal((n) => n + 1);
        return;
      }
      let refreshKind: ReturnType<typeof liveRefreshKind> = null;
      if (configuration.chainId !== null && configuration.deploymentId !== null) {
        // Trades prepend client-side from the shared stream; no refetch is needed.
        const scope = { chainId: configuration.chainId, deploymentId: configuration.deploymentId };
        refreshKind = liveRefreshKind(event, scope);
        setLiveFeed((current) => {
          // Live rows belong to one token; never carry them across a route change.
          const base = current.address === tokenAddress ? current.items : [];
          const items = reduceTokenTrades(base, event, tokenAddress, scope);
          // Unrelated stream events must not re-render the whole page.
          return items === current.items && current.address === tokenAddress
            ? current
            : { address: tokenAddress, items };
        });
      }
      if (refreshKind === "reconnected") {
        // The stream has no replay: a comment published while disconnected needs a refetch.
        setCommentSignal((n) => n + 1);
        // The recovery fetch ran before the stream reopened; reload trades to close that gap.
        const loaders = loadersRef.current;
        if (loaders && tokenRef.current)
          void loaders.loadCollection(tabRef.current).catch(() => undefined);
        return;
      }
      if (refreshKind !== "reorg") {
        if (event.event !== "launch" && event.event !== "token") return;
        if (
          event.data.chain_id !== configuration.chainId ||
          event.data.deployment_id !== configuration.deploymentId
        )
          return;
        // The shared stream is unfiltered, so scope hints to this token client-side.
        if (!isTokenEventForAddress(event, tokenAddress)) return;
      }
      if (!tokenRef.current) return;
      refreshAllRef.current ??= createCoalescedRefresh(async () => {
        const loaders = loadersRef.current;
        if (!loaders) return;
        // The token is already loaded here, so all three reads are independent.
        await Promise.all([
          loaders.loadToken(),
          loaders.loadCandles(),
          loaders.loadCollection(tabRef.current),
        ]);
      });
      refreshAllRef.current();
    },
    [
      configuration.chainId,
      configuration.deploymentId,
      configuration.status,
      tokenAddress,
      useFixture,
    ],
  );
  const recoverLive = useCallback(
    async (signal?: AbortSignal) => {
      if (useFixture || configuration.status !== "ready") return;
      const loaders = loadersRef.current;
      if (!loaders) return;
      const next = await loaders.loadToken(signal);
      if (!next) throw new Error("Token unavailable");
      await Promise.all([
        loaders.loadCandles(signal),
        loaders.loadCollection(tabRef.current, undefined, false, signal),
      ]);
      // REST is canonical again after recovery; live rows it already covers are deduplicated.
      setLiveFeed({ address: tokenAddress, items: [] });
    },
    [configuration.status, tokenAddress, useFixture],
  );
  useLiveEvents(onLiveEvent, recoverLive);

  useEffect(() => {
    const refreshAfterReorg = (event: Event) => {
      const detail = (event as CustomEvent<{ tokenAddress?: string }>).detail;
      if (detail?.tokenAddress?.toLowerCase() !== tokenAddress) return;
      const loaders = loadersRef.current;
      if (!loaders) return;
      void Promise.all([
        loaders.loadToken(),
        loaders.loadCandles(),
        loaders.loadCollection("trades"),
        loaders.loadCollection("holders"),
      ]).catch(() => undefined);
    };
    window.addEventListener("launchpad:canonical-reorg", refreshAfterReorg);
    return () => window.removeEventListener("launchpad:canonical-reorg", refreshAfterReorg);
  }, [tokenAddress]);

  useEffect(() => {
    if (!priceFlash) return;
    const timer = window.setTimeout(() => setPriceFlash(null), PRICE_FLASH_MS);
    return () => window.clearTimeout(timer);
  }, [priceFlash]);

  const selectTab = (value: Tab) => {
    setTab(value);
    setPages([]);
    setResetNotice(false);
  };

  return {
    token,
    tokenError,
    loading,
    interval,
    setInterval,
    chartPoints,
    chartLoading,
    chartError,
    tab,
    selectTab,
    pages,
    collectionLoading,
    collectionError,
    resetNotice,
    liveFeed,
    priceFlash,
    commentSignal,
    loadToken,
    loadCandles,
    loadCollection,
  };
}
