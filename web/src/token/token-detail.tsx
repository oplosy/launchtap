"use client";
/* The callbacks intentionally coordinate external REST/SSE state and are not compiler memoization candidates. */
/* eslint-disable react-hooks/preserve-manual-memoization */

import Link from "next/link";
import { ArrowUpRight, Check, Copy, X } from "@phosphor-icons/react";
import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import type { components } from "@/api/generated";
import { ApiClient, resolveApiAssetUrl, type TokenDetailResponse } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import type { LiveEvent } from "@/api/types";
import { useLiveEvents } from "@/live/live-events";
import type { FeedItem } from "@/live/feed-store";
import { liveRefreshKind, liveTradeRows, reduceTokenTrades } from "@/live/live-strip-model";
import { formatCanonicalBaseUnits, formatSignificantPrice } from "@/amounts";
import { CoinGeckoAttribution, UsdLine } from "@/market/usd-line";
import { DEFAULT_E2E_WEB_URL, publicConfiguration } from "@/config/public";
import { reviewedDeployments } from "@/contracts/generated";
import { addressExplorerUrl } from "@/wallet/explorer";
import { marketLinks } from "./market-links";
import { StarButton, WatchlistToaster } from "@/watchlist/star-button";
import { WatchCount } from "@/watchlist/watch-count";
import { CommentPanel } from "@/comments/comment-panel";
import { ShareActions } from "@/share/share-actions";
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  SafeExternalLink,
  SafeImage,
  Skeleton,
  Tabs,
} from "@/components/primitives";
import { SlidingIndicator } from "@/components/sliding-indicator";
import { walletPath } from "@/portfolio/wallet-model";
import { addressHue, parseTokenAddress, shortAddress } from "./address";
import { transformCandles, type CandleChartPoint } from "./chart-data";
import {
  appendTokenCollectionPage,
  type TokenCollection,
  type TokenCollectionFetcher,
} from "./pagination";
import { MarketChart } from "./market-chart";
import { createCoalescedRefresh } from "./coalesced-refresh";
import { isTokenEventForAddress } from "./sse-scope";
import { TradingPanel } from "@/transactions-panel";
import { MetadataEditor } from "./metadata-editor";
import { PRICE_FLASH_MS, priceDirection } from "./price-flash";
import { creatorHistoryLine } from "./creator-history";
import { earlyBuyersView, type EarlyBuyersView } from "./early-buyers";
import { launchBlockHolders, topHolderShare } from "./holder-concentration";
import { launchFacts, type LaunchFacts } from "./launch-facts";
import { v2Facts, type V2Facts } from "./v2-facts";

type TokenDetailProps = { address: `0x${string}` };
type Tab = "trades" | "holders";
export const intervals = ["1m", "5m", "1h", "1d", "6h", "all"] as const;
/** Display order, shortest to longest. */
const INTERVAL_ORDER: readonly (typeof intervals)[number][] = ["1m", "5m", "1h", "6h", "1d", "all"];
export const CANDLE_LIMIT = 100;

function amount(raw: string) {
  const value = formatCanonicalBaseUnits(raw, 5);
  return value === null ? "Unavailable" : `${value} ETH`;
}

function finalityTone(value: string): "success" | "warning" | "neutral" {
  return value === "safe" || value === "finalized"
    ? "success"
    : value === "provisional" || value === "stale"
      ? "warning"
      : "neutral";
}

function finalityLabel(value: string) {
  return value === "safe" || value === "finalized" ? "Verified" : "Updating";
}

export function TokenDetail({
  address,
  fixture = false,
}: TokenDetailProps & { fixture?: boolean }) {
  const baseConfiguration = publicConfiguration();
  const useFixture = fixture && process.env.NEXT_PUBLIC_E2E_FIXTURE === "1";
  const configuration = useFixture
    ? {
        ...baseConfiguration,
        status: "ready" as const,
        apiBaseUrl: process.env.NEXT_PUBLIC_TASK6_ANVIL_WEB_URL || DEFAULT_E2E_WEB_URL,
        chainId: 4663,
        deploymentId: "robinhood-mainnet",
        deployment: reviewedDeployments[0] ?? null,
      }
    : baseConfiguration;
  const [token, setToken] = useState<TokenDetailResponse | null>(null);
  const [tokenError, setTokenError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(true);
  const [interval, setInterval] = useState<(typeof intervals)[number]>("1h");
  const [mode, setMode] = useState<"area" | "candles">("area");
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
  const [sheetSide, setSheetSide] = useState<"buy" | "sell" | null>(null);
  const asideRef = useRef<HTMLElement>(null);
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
  const tokenAddress = address.toLowerCase() as `0x${string}`;
  const collectionSnapshot = pages[0]?.snapshot;
  const explorer = addressExplorerUrl(configuration, tokenAddress);
  const market = marketLinks(configuration.deployment, token?.pair ?? null, token?.phase ?? "");
  const currentImageUrl = resolveApiAssetUrl(configuration.apiBaseUrl, token?.image_url);

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
      await loadCandles();
      await loadCollection(tabRef.current);
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
        await loaders.loadToken();
        await loaders.loadCandles();
        await loaders.loadCollection(tabRef.current);
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
      await loaders.loadCandles(signal);
      await loaders.loadCollection(tabRef.current, undefined, false, signal);
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

  useEffect(() => {
    if (!sheetSide) return;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const close = () => setSheetSide(null);
    const aside = asideRef.current;
    // Land on the requested side's tab; re-selecting the active side would clear the amount.
    const tab =
      aside?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[sheetSide === "buy" ? 0 : 1];
    if (tab && !tab.disabled && tab.getAttribute("aria-selected") !== "true") tab.click();
    else
      (tab && !tab.disabled
        ? tab
        : aside?.querySelector<HTMLElement>(".token-sheet-close")
      )?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        close();
        return;
      }
      const current = asideRef.current;
      if (event.key !== "Tab" || !current) return;
      // Focus stays inside the sheet while it is modal.
      const focusable = Array.from(
        current.querySelectorAll<HTMLElement>(
          'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
        ),
      ).filter((element) => element.getClientRects().length > 0);
      const first = focusable[0];
      const last = focusable.at(-1);
      if (!first || !last) return;
      if (!current.contains(document.activeElement)) {
        event.preventDefault();
        first.focus();
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    // The sheet only exists below 1024 px; widening the window returns the card to the column.
    const wide = window.matchMedia("(min-width: 1024px)");
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", onKeyDown);
    wide.addEventListener("change", close);
    return () => {
      document.body.style.overflow = overflow;
      document.removeEventListener("keydown", onKeyDown);
      wide.removeEventListener("change", close);
      opener?.focus();
    };
  }, [sheetSide]);

  if (configuration.status !== "ready") return <TokenUnavailable address={tokenAddress} />;
  if (loading) return <TokenSkeleton />;
  if (tokenError instanceof ApiProblem && tokenError.status === 404)
    return <TokenMissing address={tokenAddress} />;
  if (tokenError || !token) return <TokenLoadError onRetry={() => window.location.reload()} />;

  const progress = Math.max(0, Math.min(100, token.graduation_progress_bps / 100));
  const facts = v2Facts(token);
  const engineOneFacts = launchFacts(token);
  const liquidityIndexed = facts?.liquidityIndexed ?? true;
  const pagesItems: unknown[] = [];
  for (const page of pages) {
    for (const item of page.items ?? []) pagesItems.push(item);
  }
  // Both tab panels stay mounted, but `pages` holds only the active tab's rows.
  const restTrades = (tab === "trades"
    ? pagesItems
    : []) as unknown as components["schemas"]["TradeDTO"][];
  const tradeItems =
    tab === "trades"
      ? [
          ...liveTradeRows(liveFeed.address === tokenAddress ? liveFeed.items : [], restTrades),
          ...restTrades,
        ]
      : [];
  const holderItems = (tab === "holders"
    ? pagesItems
    : []) as unknown as components["schemas"]["HolderDTO"][];
  const nextCursor = pages.at(-1)?.next_cursor;
  const snapshot = token.snapshot;
  const title = token.name.trim() || token.symbol.trim() || "Unnamed token";
  const change = token.price_change_24h_bps / 100;
  const showTrade = !useFixture;
  return (
    <div
      className="token-page"
      data-trade-bar={showTrade ? "" : undefined}
      style={{ "--hue": addressHue(token.address) } as CSSProperties}
    >
      <header className="token-header" aria-labelledby="token-title">
        <SafeImage
          src={currentImageUrl}
          alt={`${title} token`}
          className="token-detail-image"
          fallbackLabel="No artwork"
        />
        <h1 id="token-title">{title}</h1>
        <p className="token-detail-symbol">
          <span>{token.symbol || "—"}</span>
          <CopyAddress address={token.address} />
        </p>
        <div className="market-price">
          <span className="market-price-label">Spot price</span>
          <strong
            className="market-price-value"
            data-flash={priceFlash?.direction}
            key={priceFlash?.id}
          >
            {safeMarketValue(token.spot_price_eth)}
            <small> ETH</small>
          </strong>
          <UsdLine wei={token.spot_price_eth} />
          <span className={`market-change ${change < 0 ? "is-down" : "is-up"}`}>
            {change >= 0 ? "+" : ""}
            {change.toFixed(2)}% <span>24h</span>
          </span>
        </div>
        <div className="token-header-actions">
          <Badge tone={token.phase === "graduated" ? "success" : "neutral"}>
            {token.phase || "Phase unavailable"}
          </Badge>
          <Badge tone={finalityTone(snapshot.finality)}>{finalityLabel(snapshot.finality)}</Badge>
          <StarButton address={token.address} label={title} />
          {!useFixture && configuration.status === "ready" && configuration.apiBaseUrl ? (
            <WatchCount
              key={tokenAddress}
              address={token.address}
              apiBaseUrl={configuration.apiBaseUrl}
            />
          ) : null}
          <ShareActions address={token.address} symbol={token.symbol} />
          {explorer ? (
            <SafeExternalLink
              href={explorer}
              className="ui-button ui-button-secondary ui-button-sm"
            >
              Explorer <ArrowUpRight size={14} aria-hidden="true" />
            </SafeExternalLink>
          ) : (
            <span className="token-link-unavailable">Explorer unavailable</span>
          )}
        </div>
      </header>
      <WatchlistToaster />

      {snapshot.finality === "stale" || snapshot.finality === "provisional" ? (
        <p className="token-trust-notice" role="status">
          This data is still updating and may change as the chain settles.
        </p>
      ) : null}

      <div className="token-layout" data-single={showTrade ? undefined : ""}>
        <div className="token-main">
          <section className="market-panel" aria-labelledby="market-title">
            <h2 id="market-title" className="sr-only">
              Price chart
            </h2>
            <div className="chart-controls">
              <label className="chart-range">
                <span className="sr-only">Timeframe</span>
                <select
                  value={interval}
                  onChange={(event) =>
                    setInterval(event.target.value as (typeof intervals)[number])
                  }
                >
                  {INTERVAL_ORDER.map((item) => (
                    <option key={item} value={item}>
                      {item === "all" ? "All" : item}
                    </option>
                  ))}
                </select>
              </label>
              <div className="chart-toggle" role="group" aria-label="Chart type">
                <button
                  type="button"
                  className={mode === "area" ? "is-active" : ""}
                  aria-pressed={mode === "area"}
                  onClick={() => setMode("area")}
                >
                  Area
                </button>
                <button
                  type="button"
                  className={mode === "candles" ? "is-active" : ""}
                  aria-pressed={mode === "candles"}
                  onClick={() => setMode("candles")}
                >
                  Candles
                </button>
                <SlidingIndicator />
              </div>
            </div>
            {chartLoading ? (
              <div className="chart-loading">
                <Skeleton className="chart-skeleton" />
              </div>
            ) : chartError ? (
              <ErrorState
                title="Chart unavailable"
                description="Price history could not be loaded."
                action={<Button onClick={() => void loadCandles()}>Retry</Button>}
              />
            ) : (
              <MarketChart key={tokenAddress} points={chartPoints} mode={mode} />
            )}
          </section>

          <section className="token-stats" aria-labelledby="token-summary-title">
            <div className="token-section-head">
              <h2 id="token-summary-title">Market and reserves</h2>
              <CoinGeckoAttribution />
            </div>
            <dl className="token-summary-grid">
              <Metric
                label="Market cap"
                value={amount(token.market_cap_eth)}
                eth={token.market_cap_eth}
              />
              <Metric
                label="24h volume"
                value={amount(token.volume_24h_eth)}
                eth={token.volume_24h_eth}
              />
              {liquidityIndexed ? (
                <Metric
                  label="Liquidity"
                  value={amount(token.liquidity_eth)}
                  eth={token.liquidity_eth}
                />
              ) : (
                <Metric label="Pool liquidity" value="Not indexed yet" />
              )}
              <Metric label="Holders" value={token.holder_count.toLocaleString("en-US")} />
              {liquidityIndexed ? (
                <>
                  <Metric
                    label="ETH reserve"
                    value={amount(token.eth_reserve)}
                    eth={token.eth_reserve}
                  />
                  <Metric label="Token reserve" value={safeTokenAmount(token.token_reserve)} />
                </>
              ) : null}
            </dl>
            <div
              className="graduation-progress"
              role="group"
              aria-label={`Graduation progress ${progress}%`}
            >
              <div className="graduation-progress-head">
                <span>Graduation</span>
                <strong>{progress.toFixed(1)}%</strong>
              </div>
              <div className="progress-track">
                <span style={{ width: `${progress}%` }} />
              </div>
              <p>
                {amount(token.real_curve_eth)} of {amount(token.graduation_eth)} raised · source{" "}
                {token.reserve_source || "unavailable"}
              </p>
            </div>
          </section>

          {facts ? <V2FactsPanel facts={facts} configuration={configuration} /> : null}
          {engineOneFacts ? (
            <LaunchFactsPanel token={token} facts={engineOneFacts} configuration={configuration} />
          ) : null}

          <section className="token-history-panel" aria-labelledby="history-title">
            <div className="token-section-head">
              <h2 id="history-title">Recent activity</h2>
              <CoinGeckoAttribution />
              {collectionSnapshot ? (
                <Badge tone={finalityTone(collectionSnapshot.finality)}>
                  {finalityLabel(collectionSnapshot.finality)}
                </Badge>
              ) : null}
            </div>
            {resetNotice ? (
              <p className="discovery-notice" role="status">
                The activity list was refreshed with the latest available data.
              </p>
            ) : null}
            <Tabs
              ariaLabel="Token history"
              value={tab}
              onChange={(value) => {
                setTab(value as Tab);
                setPages([]);
                setResetNotice(false);
              }}
              tabs={[
                {
                  value: "trades",
                  label: "Recent trades",
                  panel: (
                    <Trades
                      items={tradeItems}
                      loading={collectionLoading}
                      error={collectionError}
                    />
                  ),
                },
                {
                  value: "holders",
                  label: "Holders",
                  panel: (
                    <>
                      <Holders
                        items={holderItems}
                        totalSupply={token.total_supply}
                        launchBlock={token.launch_block}
                        hasMore={Boolean(nextCursor)}
                        loading={collectionLoading}
                        error={collectionError}
                      />
                      <EarlyBuyers
                        key={token.address}
                        tokenAddress={token.address}
                        totalSupply={token.total_supply}
                        apiBaseUrl={
                          configuration.status === "ready" ? configuration.apiBaseUrl : null
                        }
                      />
                    </>
                  ),
                },
              ]}
            />
            {nextCursor ? (
              <div className="history-pagination">
                <span>{pagesItems.length} rows</span>
                <Button
                  variant="secondary"
                  loading={collectionLoading}
                  onClick={() => void loadCollection(tab, nextCursor, true)}
                >
                  Load more
                </Button>
              </div>
            ) : null}
          </section>

          {!useFixture && configuration.status === "ready" && configuration.apiBaseUrl ? (
            <CommentPanel
              key={tokenAddress}
              tokenAddress={tokenAddress}
              apiBaseUrl={configuration.apiBaseUrl}
              liveSignal={commentSignal}
            />
          ) : null}

          <section className="token-metadata" aria-labelledby="metadata-title">
            <div className="token-section-head">
              <div>
                <h2 id="metadata-title">About this token</h2>
                <p className="section-description">Supplied by the creator, not verified.</p>
              </div>
            </div>
            <p className="token-description">
              {token.description.trim() || "No description supplied."}
            </p>
            <div className="token-links">
              <SafeExternalLink href={token.x_url}>X profile</SafeExternalLink>
              <SafeExternalLink href={token.telegram_url}>Telegram</SafeExternalLink>
              {explorer ? (
                <SafeExternalLink href={explorer}>
                  Contract {shortAddress(token.address)}
                </SafeExternalLink>
              ) : null}
              {market.dexscreener ? (
                <SafeExternalLink href={market.dexscreener}>Dexscreener</SafeExternalLink>
              ) : null}
              {market.geckoterminal ? (
                <SafeExternalLink href={market.geckoterminal}>GeckoTerminal</SafeExternalLink>
              ) : null}
            </div>
            <CreatorLine
              key={`${token.creator}:${token.address}`}
              creator={token.creator}
              tokenAddress={token.address}
              apiBaseUrl={configuration.status === "ready" ? configuration.apiBaseUrl : null}
            />
          </section>
          {!useFixture ? (
            <MetadataEditor
              token={{
                address: token.address,
                description: token.description,
                x_url: token.x_url,
                telegram_url: token.telegram_url,
                image_url: token.image_url,
              }}
              onConflict={loadToken}
            />
          ) : null}
        </div>

        {showTrade ? (
          <aside
            ref={asideRef}
            className="token-aside"
            data-open={sheetSide ? "" : undefined}
            {...(sheetSide
              ? { role: "dialog", "aria-modal": true, "aria-labelledby": "trade-sheet-title" }
              : { "aria-label": "Trade" })}
          >
            <div className="token-sheet-head">
              <h2 id="trade-sheet-title">Trade</h2>
              <button
                type="button"
                className="ui-icon-button token-sheet-close"
                aria-label="Close trade"
                onClick={() => setSheetSide(null)}
              >
                <X size={18} aria-hidden="true" />
              </button>
            </div>
            <TradingPanel
              token={{
                address: token.address,
                curve: token.curve,
                pair: token.pair,
                phase: token.phase,
                name: token.name,
                symbol: token.symbol,
                engineVersion: token.engine_version,
                antiSnipeEndsAt: token.v2?.anti_snipe.ends_at ?? null,
                v2PoolId: token.v2?.pool?.pool_id ?? null,
                spotPriceWei: token.spot_price_eth,
              }}
            />
            <p className="token-aside-note">
              Non-custodial. Signing and execution stay in your selected wallet.
            </p>
          </aside>
        ) : null}
      </div>

      {showTrade ? (
        <>
          {sheetSide ? (
            <div
              className="token-sheet-backdrop"
              aria-hidden="true"
              onClick={() => setSheetSide(null)}
            />
          ) : null}
          <div className="token-trade-bar" role="region" aria-label="Quick trade">
            <Button variant="primary" onClick={() => setSheetSide("buy")}>
              Buy
            </Button>
            <Button variant="secondary" onClick={() => setSheetSide("sell")}>
              Sell
            </Button>
          </div>
        </>
      ) : null}
    </div>
  );
}

/** Short address with one-click copy; stays a plain label where the clipboard is unavailable. */
function CopyAddress({ address }: { address: string }) {
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

function safeTokenAmount(raw: string) {
  return formatCanonicalBaseUnits(raw, 3) ?? "Unavailable";
}

function safeMarketValue(raw: string) {
  return formatSignificantPrice(raw) ?? "Unavailable";
}
/** Engine-2 launch, pool and fee recipient facts (Plan v2-C Task 8), from the indexed API. */
function V2FactsPanel({
  facts,
  configuration,
}: {
  facts: V2Facts;
  configuration: Parameters<typeof addressExplorerUrl>[0];
}) {
  const link = (address: string) => {
    const href = addressExplorerUrl(configuration, address as `0x${string}`);
    return href ? (
      <a className="mono" href={href} target="_blank" rel="noreferrer" title={address}>
        {address}
      </a>
    ) : (
      <span className="mono" title={address}>
        {address}
      </span>
    );
  };
  return (
    <section className="token-v2-facts" aria-labelledby="v2-facts-title">
      <div className="token-section-head">
        <h2 id="v2-facts-title">Engine v2</h2>
      </div>
      <dl className="token-summary-grid">
        <div>
          <dt>Anti-snipe tax</dt>
          <dd title={facts.antiSnipe}>{facts.antiSnipe}</dd>
        </div>
        <div>
          <dt>Tax ends</dt>
          <dd>
            <time dateTime={facts.antiSnipeEndsAt}>{facts.antiSnipeEndsAt}</time>
          </dd>
        </div>
        <div>
          <dt>Pool fee creator share</dt>
          <dd>{facts.lpCreatorShare}</dd>
        </div>
        <div>
          <dt>Fee recipient</dt>
          <dd>{link(facts.feeRecipient)}</dd>
        </div>
        {facts.pendingFeeRecipient ? (
          <div>
            <dt>Pending recipient</dt>
            <dd>{link(facts.pendingFeeRecipient)}</dd>
          </div>
        ) : null}
        {facts.pool ? (
          <>
            <div>
              <dt>Pool id</dt>
              <dd className="mono" title={facts.pool.poolId}>
                {facts.pool.poolId}
              </dd>
            </div>
            <div>
              <dt>Pool hook</dt>
              <dd>{link(facts.pool.hook)}</dd>
            </div>
          </>
        ) : null}
        {facts.lockerOwed.map((owed) => (
          <div key={owed.account}>
            <dt title={owed.account}>Locker owes {owed.account.slice(0, 10)}…</dt>
            <dd>
              {amount(owed.eth)} + {safeTokenAmount(owed.token)} tokens
            </dd>
          </div>
        ))}
      </dl>
      {facts.pool ? (
        <p className="token-aside-note">
          Pool liquidity is not indexed yet; price comes from the indexed spot price.
        </p>
      ) : null}
    </section>
  );
}

/**
 * Engine-1 launch facts: what LaunchToken and BondingCurveV1 guarantee by construction, with
 * the curve and pair as evidence links, and the pair the curve seeds at graduation.
 */
function LaunchFactsPanel({
  token,
  facts,
  configuration,
}: {
  token: {
    curve: string;
    pair: string;
    phase: string;
    total_supply: string;
    curve_tokens: string;
    lp_tokens: string;
  };
  facts: LaunchFacts;
  configuration: Parameters<typeof addressExplorerUrl>[0];
}) {
  const graduated = token.phase.toLowerCase() === "graduated";
  const link = (address: string) => {
    const href = addressExplorerUrl(configuration, address as `0x${string}`);
    return href ? (
      <a className="mono" href={href} target="_blank" rel="noreferrer" title={address}>
        {address}
      </a>
    ) : (
      <span className="mono" title={address}>
        {address}
      </span>
    );
  };
  const openingPrice = formatSignificantPrice(facts.graduation.openingPriceWad);
  return (
    <section className="token-launch-facts" aria-labelledby="launch-facts-title">
      <div className="token-section-head">
        <h2 id="launch-facts-title">Launch facts</h2>
      </div>
      <dl className="token-summary-grid">
        <div>
          <dt>Fixed supply</dt>
          <dd>{safeTokenAmount(token.total_supply)}</dd>
        </div>
        <div>
          <dt>On the curve</dt>
          <dd>{safeTokenAmount(token.curve_tokens)}</dd>
        </div>
        <div>
          <dt>Seeded into the pool</dt>
          <dd>{safeTokenAmount(token.lp_tokens)}</dd>
        </div>
        <div>
          <dt>Curve trade fee</dt>
          <dd title={facts.feeSplit}>{facts.tradeFee}</dd>
        </div>
        <div>
          <dt>Pool opens at</dt>
          <dd>{openingPrice === null ? "Unavailable" : `${openingPrice} ETH`}</dd>
        </div>
        <div>
          <dt>Market cap at graduation</dt>
          <dd>
            {amount(facts.graduation.marketCapWei)}
            <UsdLine wei={facts.graduation.marketCapWei} />
          </dd>
        </div>
        {facts.graduation.remainingEthWei !== null ? (
          <div>
            <dt>Left to graduate</dt>
            <dd>{amount(facts.graduation.remainingEthWei)}</dd>
          </div>
        ) : null}
        <div>
          <dt>Curve</dt>
          <dd>{link(token.curve)}</dd>
        </div>
        <div>
          <dt>Pool</dt>
          <dd>{link(token.pair)}</dd>
        </div>
      </dl>
      <ul className="token-launch-guarantees">
        <li>The whole supply was minted once at launch; the token has no mint function.</li>
        <li>The token contract has no owner, no transfer tax and no blacklist.</li>
        <li>
          {graduated
            ? "Until graduation only the curve could move tokens to or from itself or the pool, so nobody could seed the pool early."
            : "Until graduation only the curve can move tokens to or from itself or the pool, so nobody can seed the pool early."}
        </li>
        <li>
          {graduated
            ? "At graduation the pool's LP tokens were minted to 0x…dEaD, so the liquidity can never be withdrawn."
            : "At graduation the pool's LP tokens are minted to 0x…dEaD, so the liquidity can never be withdrawn."}
        </li>
        <li>Curve fee split: {facts.feeSplit}.</li>
      </ul>
    </section>
  );
}

function Metric({ label, value, eth }: { label: string; value: string; eth?: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>
        {value}
        {eth ? <UsdLine wei={eth} /> : null}
      </dd>
    </div>
  );
}
function Trades({
  items,
  loading,
  error,
}: {
  items: components["schemas"]["TradeDTO"][];
  loading: boolean;
  error: Error | null;
}) {
  if (loading && !items.length)
    return (
      <div className="history-state">
        <Skeleton />
        <Skeleton />
        <Skeleton />
      </div>
    );
  if (error)
    return (
      <ErrorState title="Trades unavailable" description="Trade history could not be loaded." />
    );
  if (!items.length)
    return (
      <EmptyState title="No trades yet" description="This token has no recorded trades yet." />
    );
  return (
    <div className="history-table" role="table" aria-label="Recent trades">
      <div className="history-row history-heading" role="row">
        <span>Time</span>
        <span>Trader</span>
        <span>Side</span>
        <span>Token volume</span>
        <span>ETH volume</span>
        <span>Execution / spot</span>
        <span>Finality</span>
      </div>
      {items.map((item) => (
        <div
          className="history-row"
          role="row"
          key={`${item.tx_hash}-${item.log_index}`}
          data-pending={item.finality === "observed" ? "" : undefined}
        >
          <span className="mono">{safeDate(item.time)}</span>
          <span className="mono">
            <WalletLink address={item.trader} />
          </span>
          <span className={item.side === "buy" ? "trade-buy" : "trade-sell"}>
            {item.side || "—"}
          </span>
          <span className="mono">{safeTokenAmount(item.token_volume)}</span>
          <span className="mono">
            {amount(item.eth_volume)}
            <UsdLine wei={item.eth_volume} />
          </span>
          <span className="mono trade-prices">
            {item.execution_price === "" ? (
              "—"
            ) : (
              <>
                {safeMarketValue(item.execution_price)} /{" "}
                {/* A live row has no spot price until the next REST refresh. */}
                {item.spot_price === "" ? "—" : safeMarketValue(item.spot_price)}
                <UsdLine wei={item.execution_price} />
              </>
            )}
          </span>
          <span>
            <Badge tone={finalityTone(item.finality)}>{finalityLabel(item.finality)}</Badge>
          </span>
        </div>
      ))}
    </div>
  );
}
function Holders({
  items,
  totalSupply,
  launchBlock,
  hasMore,
  loading,
  error,
}: {
  items: components["schemas"]["HolderDTO"][];
  totalSupply: string;
  launchBlock: number | undefined;
  hasMore: boolean;
  loading: boolean;
  error: Error | null;
}) {
  if (loading && !items.length)
    return (
      <div className="history-state">
        <Skeleton />
        <Skeleton />
        <Skeleton />
      </div>
    );
  if (error)
    return (
      <ErrorState title="Holders unavailable" description="Holder data could not be loaded." />
    );
  if (!items.length)
    return (
      <EmptyState title="No holders yet" description="No holder balances are available yet." />
    );
  // The list arrives largest first with the curve, pool, locker and burn addresses excluded.
  const concentration = topHolderShare(
    items.map((item) => item.balance),
    totalSupply,
  );
  const launchLine = launchBlockHolders(items, launchBlock, totalSupply, hasMore);
  return (
    <>
      {concentration ? (
        <p className="holder-concentration">
          Top {concentration.holders} {concentration.holders === 1 ? "holder owns" : "holders own"}{" "}
          <strong>{concentration.percent}</strong> of the supply (curve, pool and burned tokens
          excluded from the list).
        </p>
      ) : null}
      {launchLine ? <p className="holder-concentration">{launchLine}</p> : null}
      <div className="history-table" role="table" aria-label="Token holders">
        <div className="history-row history-heading" role="row">
          <span>Address</span>
          <span>Balance</span>
          <span>First acquired block</span>
        </div>
        {items.map((item) => (
          <div className="history-row holder-row" role="row" key={item.address}>
            <span className="mono">
              <WalletLink address={item.address} />
            </span>
            <span className="mono">{safeTokenAmount(item.balance)}</span>
            <span className="mono">
              {Number.isSafeInteger(item.first_acquired_block)
                ? item.first_acquired_block.toLocaleString("en-US")
                : "Unavailable"}
            </span>
          </div>
        ))}
      </div>
    </>
  );
}
/**
 * Who launched the token and what else that wallet launched here, from the public created-tokens
 * list. Keyed by creator and token, so a new token starts without the previous history.
 */
function CreatorLine({
  creator,
  tokenAddress,
  apiBaseUrl,
}: {
  creator: string;
  tokenAddress: string;
  apiBaseUrl: string | null;
}) {
  const parsed = parseTokenAddress(creator);
  const [history, setHistory] = useState<string | null>(null);
  useEffect(() => {
    if (!parsed || !apiBaseUrl) return;
    const controller = new AbortController();
    void new ApiClient({ baseUrl: apiBaseUrl })
      .listWalletCreated(parsed, { limit: 100 }, controller.signal)
      .then((page) => {
        if (!controller.signal.aborted)
          setHistory(creatorHistoryLine(page.items ?? [], tokenAddress, Boolean(page.next_cursor)));
      })
      // The history is optional context; the creator link stays without it.
      .catch(() => undefined);
    return () => controller.abort();
  }, [apiBaseUrl, parsed, tokenAddress]);
  if (!parsed) return null;
  return (
    <p className="token-creator">
      Created by <WalletLink address={parsed} />.{history ? ` ${history}` : null}
    </p>
  );
}
/**
 * Early wallets linked by same-transaction deliveries, token transfers or a shared funding
 * source (early-buyer clusters specs, Levels 1 and 2). Keyed by token, so a new token starts empty; an unavailable read shows nothing.
 */
function EarlyBuyers({
  tokenAddress,
  totalSupply,
  apiBaseUrl,
}: {
  tokenAddress: string;
  totalSupply: string;
  apiBaseUrl: string | null;
}) {
  const [view, setView] = useState<EarlyBuyersView | null>(null);
  useEffect(() => {
    if (!apiBaseUrl) return;
    const controller = new AbortController();
    void new ApiClient({ baseUrl: apiBaseUrl })
      .getEarlyBuyers(tokenAddress, controller.signal)
      .then((body) => {
        if (!controller.signal.aborted) setView(earlyBuyersView(body, totalSupply));
      })
      // Optional context under the holders list; the list stays without it.
      .catch(() => undefined);
    return () => controller.abort();
  }, [apiBaseUrl, tokenAddress, totalSupply]);
  if (!view) return null;
  return (
    <section className="early-buyers" aria-labelledby="early-buyers-title">
      <h3 id="early-buyers-title">Linked early wallets</h3>
      <p>{view.summary}</p>
      {view.groups.length ? (
        <ul className="early-buyers-groups">
          {view.groups.map((group, index) => (
            <li key={group.wallets[0]?.address ?? index}>
              <details>
                <summary>
                  <strong>{group.title}</strong>
                  {group.links ? <span>: {group.links}</span> : null}
                  {group.includesCreator ? (
                    <>
                      {" "}
                      <span className="early-buyers-creator">Creator</span>
                    </>
                  ) : null}
                </summary>
                <ul className="early-buyers-wallets">
                  {group.wallets.map((wallet) => (
                    <li key={wallet.address}>
                      <WalletLink address={wallet.address} />
                      <span className="mono">{safeTokenAmount(wallet.balance)}</span>
                    </li>
                  ))}
                </ul>
                {group.unlisted ? <p>and {group.unlisted} more</p> : null}
              </details>
            </li>
          ))}
        </ul>
      ) : null}
      {view.emptyGroups ? <p>{view.emptyGroups}</p> : null}
      {view.moreGroups ? <p>and {view.moreGroups} more linked groups</p> : null}
      {view.funding ? <p>{view.funding}</p> : null}
      <p className="early-buyers-note">
        Links are on-chain facts: tokens delivered by the curve to several wallets in one
        transaction, tokens moved between those wallets
        {view.funding ? ", or first ETH sent to them by the same wallet" : ""}. A link does not
        prove one owner.
      </p>
    </section>
  );
}
/** Links an address to its wallet page; a missing or malformed address stays a dash. */
function WalletLink({ address }: { address: string | null | undefined }) {
  const parsed = parseTokenAddress(address);
  if (!parsed) return <>—</>;
  return (
    <Link href={walletPath(parsed) as never} title={parsed}>
      {shortAddress(parsed)}
    </Link>
  );
}
function safeDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.valueOf())
    ? "Unavailable"
    : date.toLocaleString("en-US", { dateStyle: "medium", timeStyle: "short" });
}
function TokenUnavailable({ address }: { address: string }) {
  return (
    <div className="page-stack token-page">
      <UnavailableBlock
        title="Token unavailable"
        description={`The reviewed deployment is not configured. ${shortAddress(address)} was not loaded.`}
      />
    </div>
  );
}
function TokenMissing({ address }: { address: string }) {
  return (
    <div className="page-stack token-page">
      <UnavailableBlock
        title="Token not found"
        description={`No token was found for ${shortAddress(address)}.`}
      />
    </div>
  );
}
function TokenLoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="page-stack token-page">
      <ErrorState
        title="Token unavailable"
        description="This token could not be loaded. Try again."
        action={<Button onClick={onRetry}>Retry</Button>}
      />
    </div>
  );
}
function UnavailableBlock({ title, description }: { title: string; description: string }) {
  return (
    <section className="workspace-panel token-unavailable">
      <p className="panel-kicker">Token route</p>
      <h1>{title}</h1>
      <p>{description}</p>
      <Link className="ui-button ui-button-secondary ui-button-md" href="/explore">
        Return to Explore
      </Link>
    </section>
  );
}
function TokenSkeleton() {
  return (
    <div className="token-page" aria-busy="true">
      <section className="token-header token-skeleton">
        <Skeleton className="token-detail-image" />
        <Skeleton className="token-skeleton-title" />
        <Skeleton className="token-skeleton-copy" />
      </section>
      <div className="token-layout">
        <div className="token-main">
          <Skeleton className="chart-skeleton" />
        </div>
        <div className="token-aside">
          <Skeleton className="token-skeleton-trade" />
        </div>
      </div>
    </div>
  );
}
