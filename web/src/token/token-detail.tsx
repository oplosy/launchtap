"use client";
/* The callbacks intentionally coordinate external REST/SSE state and are not compiler memoization candidates. */
/* eslint-disable react-hooks/preserve-manual-memoization */

import { ArrowUpRight, Check, Copy, X } from "@phosphor-icons/react";
import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import type { components } from "@/api/generated";
import { ApiClient, resolveApiAssetUrl, type TokenDetailResponse } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import type { LiveEvent } from "@/api/types";
import { useLiveEvents } from "@/live/live-events";
import type { FeedItem } from "@/live/feed-store";
import { liveRefreshKind, liveTradeRows, reduceTokenTrades } from "@/live/live-strip-model";
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
  ErrorState,
  SafeExternalLink,
  SafeImage,
  Skeleton,
  Tabs,
} from "@/components/primitives";
import { SlidingIndicator } from "@/components/sliding-indicator";
import { addressHue, shortAddress } from "./address";
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
import { launchFacts } from "./launch-facts";
import { v2Facts } from "./v2-facts";
import {
  amount,
  finalityTone,
  finalityLabel,
  safeTokenAmount,
  safeMarketValue,
} from "./token-format";
import { V2FactsPanel, LaunchFactsPanel, Metric } from "./facts-panels";
import { Trades, Holders, CreatorLine, EarlyBuyers } from "./token-activity";
import { TokenUnavailable, TokenMissing, TokenLoadError, TokenSkeleton } from "./token-states";

type TokenDetailProps = { address: `0x${string}` };
type Tab = "trades" | "holders";
export const intervals = ["1m", "5m", "1h", "1d", "6h", "all"] as const;
/** Display order, shortest to longest. */
const INTERVAL_ORDER: readonly (typeof intervals)[number][] = ["1m", "5m", "1h", "6h", "1d", "all"];
export const CANDLE_LIMIT = 100;

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
