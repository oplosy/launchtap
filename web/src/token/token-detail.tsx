"use client";
/* The callbacks intentionally coordinate external REST/SSE state and are not compiler memoization candidates. */
/* eslint-disable react-hooks/preserve-manual-memoization */

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import type { components } from "@/api/generated";
import { ApiClient, resolveApiAssetUrl, type TokenDetailResponse } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { queryKeys } from "@/api/types";
import { SseInvalidationStream } from "@/api/sse";
import { formatCanonicalBaseUnits } from "@/amounts";
import { publicConfiguration } from "@/config/public";
import { reviewedDeployments } from "@/contracts/generated";
import { addressExplorerUrl } from "@/wallet/explorer";
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
import { shortAddress } from "./address";
import { transformCandles, type CandleChartPoint } from "./chart-data";
import {
  appendTokenCollectionPage,
  type TokenCollection,
  type TokenCollectionFetcher,
} from "./pagination";
import { MarketChart } from "./market-chart";
import { isTokenEventForAddress } from "./sse-scope";
import { TradingPanel } from "@/transactions-panel";
import { MetadataEditor } from "./metadata-editor";

type TokenDetailProps = { address: `0x${string}` };
type Tab = "trades" | "holders";
export const intervals = ["1m", "5m", "1h", "1d", "6h", "all"] as const;
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
        apiBaseUrl: "http://127.0.0.1:3000",
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

  useEffect(() => {
    if (useFixture || configuration.status !== "ready" || !configuration.apiBaseUrl) return;
    let refreshing = false;
    let refreshAgain = false;
    const stream = new SseInvalidationStream({
      // The server forwards only this token's hints (plus reorgs) and coalesces bursts.
      url: `${configuration.apiBaseUrl}/v1/events?token=${tokenAddress}`,
      queryClient: {
        invalidateQueries: async () => {
          // Collapse hints that arrive during a refresh into one follow-up refresh.
          if (refreshing) {
            refreshAgain = true;
            return;
          }
          refreshing = true;
          try {
            do {
              refreshAgain = false;
              const loaders = loadersRef.current;
              if (!loaders) return;
              await loaders.loadToken();
              await loaders.loadCandles();
              await loaders.loadCollection(tabRef.current);
            } while (refreshAgain);
          } finally {
            refreshing = false;
          }
        },
      },
      queryKeyForEvent: (event) => {
        if (
          event.data.chain_id !== configuration.chainId ||
          event.data.deployment_id !== configuration.deploymentId
        )
          return undefined;
        if (!isTokenEventForAddress(event, tokenAddress)) return undefined;
        const currentToken = tokenRef.current;
        if (!currentToken) return undefined;
        return queryKeys.token(
          configuration.chainId,
          configuration.deploymentId,
          tokenAddress,
          currentToken.snapshot,
        );
      },
      refetchSnapshot: async (signal) => {
        const loaders = loadersRef.current;
        if (!loaders) return;
        const next = await loaders.loadToken(signal);
        if (!next) throw new Error("Token unavailable");
        await loaders.loadCandles(signal);
        await loaders.loadCollection(tabRef.current, undefined, false, signal);
      },
    });
    return stream.start();
  }, [
    configuration.apiBaseUrl,
    configuration.chainId,
    configuration.deploymentId,
    configuration.status,
    tokenAddress,
    useFixture,
  ]);

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

  if (configuration.status !== "ready") return <TokenUnavailable address={tokenAddress} />;
  if (loading) return <TokenSkeleton />;
  if (tokenError instanceof ApiProblem && tokenError.status === 404)
    return <TokenMissing address={tokenAddress} />;
  if (tokenError || !token) return <TokenLoadError onRetry={() => window.location.reload()} />;

  const progress = Math.max(0, Math.min(100, token.graduation_progress_bps / 100));
  const pagesItems: unknown[] = [];
  for (const page of pages) {
    for (const item of page.items ?? []) pagesItems.push(item);
  }
  const tradeItems = pagesItems as unknown as components["schemas"]["TradeDTO"][];
  const holderItems = pagesItems as unknown as components["schemas"]["HolderDTO"][];
  const nextCursor = pages.at(-1)?.next_cursor;
  const snapshot = token.snapshot;
  return (
    <div className="page-stack token-page">
      <section className="token-hero" aria-labelledby="token-title">
        <div className="token-hero-identity">
          <SafeImage
            src={currentImageUrl}
            alt={`${token.name || token.symbol || "Token"} token`}
            className="token-detail-image"
            fallbackLabel="No artwork"
          />
          <div>
            <p className="section-kicker">Token overview</p>
            <h1 id="token-title">{token.name.trim() || token.symbol.trim() || "Unnamed token"}</h1>
            <p className="token-detail-symbol mono">
              {token.symbol || "—"} · {shortAddress(token.address)}
            </p>
            <div className="token-badges">
              <Badge tone={token.phase === "graduated" ? "success" : "accent"}>
                {token.phase || "Phase unavailable"}
              </Badge>
              <Badge tone={finalityTone(snapshot.finality)}>
                {finalityLabel(snapshot.finality)}
              </Badge>
            </div>
          </div>
        </div>
        <div className="token-hero-actions">
          {explorer ? (
            <SafeExternalLink
              href={explorer}
              className="ui-button ui-button-secondary ui-button-md"
            >
              Open explorer
            </SafeExternalLink>
          ) : (
            <span className="mono token-link-unavailable">Explorer unavailable</span>
          )}
          <p>Non-custodial read surface. Signing and execution remain in your selected wallet.</p>
        </div>
      </section>

      {snapshot.finality === "stale" || snapshot.finality === "provisional" ? (
        <p className="token-trust-notice" role="status">
          This data is still updating and may change as the chain settles.
        </p>
      ) : null}
      <section
        className="workspace-panel token-summary-panel"
        aria-labelledby="token-summary-title"
      >
        <div className="panel-head">
          <div>
            <p className="panel-kicker">Token stats</p>
            <h2 id="token-summary-title">Market and reserves</h2>
          </div>
          <span className="mono token-block">Live values</span>
        </div>
        <dl className="token-summary-grid">
          <Metric label="Spot price" value={amount(token.spot_price_eth)} />
          <Metric label="Market cap" value={amount(token.market_cap_eth)} />
          <Metric label="24h volume" value={amount(token.volume_24h_eth)} />
          <Metric label="Liquidity" value={amount(token.liquidity_eth)} />
          <Metric label="ETH reserve" value={amount(token.eth_reserve)} />
          <Metric label="Token reserve" value={safeTokenAmount(token.token_reserve)} />
          <Metric label="Holders" value={token.holder_count.toLocaleString("en-US")} />
          <Metric label="24h change" value={`${(token.price_change_24h_bps / 100).toFixed(2)}%`} />
        </dl>
        <div className="graduation-progress" aria-label={`Graduation progress ${progress}%`}>
          <div className="graduation-progress-head">
            <span>Graduation progress</span>
            <strong>{progress.toFixed(1)}%</strong>
          </div>
          <div className="progress-track">
            <span style={{ width: `${progress}%` }} />
          </div>
          <p className="mono">
            {amount(token.real_curve_eth)} of {amount(token.graduation_eth)} target · source{" "}
            {token.reserve_source || "unavailable"}
          </p>
        </div>
      </section>

      {!useFixture ? (
        <TradingPanel
          token={{
            address: token.address,
            curve: token.curve,
            pair: token.pair,
            phase: token.phase,
            name: token.name,
            symbol: token.symbol,
          }}
        />
      ) : null}

      <section className="workspace-panel market-panel" aria-labelledby="market-title">
        <div className="panel-head market-panel-head">
          <div>
            <p className="panel-kicker">Price history</p>
            <h2 id="market-title">Market activity</h2>
          </div>
          <div className="chart-controls">
            <label className="chart-control-label">
              Timeframe
              <select
                className="ui-input"
                value={interval}
                onChange={(event) => setInterval(event.target.value as (typeof intervals)[number])}
              >
                {intervals.map((item) => (
                  <option key={item}>{item}</option>
                ))}
              </select>
            </label>
            <div className="chart-toggle" role="group" aria-label="Chart type">
              <button
                type="button"
                className={mode === "area" ? "is-active" : ""}
                onClick={() => setMode("area")}
              >
                Area
              </button>
              <button
                type="button"
                className={mode === "candles" ? "is-active" : ""}
                onClick={() => setMode("candles")}
              >
                Candles
              </button>
            </div>
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

      <section className="workspace-panel token-history-panel" aria-labelledby="history-title">
        <div className="panel-head">
          <div>
            <p className="panel-kicker">Activity</p>
            <h2 id="history-title">Recent activity</h2>
          </div>
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
                <Trades items={tradeItems} loading={collectionLoading} error={collectionError} />
              ),
            },
            {
              value: "holders",
              label: "Holders",
              panel: (
                <Holders items={holderItems} loading={collectionLoading} error={collectionError} />
              ),
            },
          ]}
        />
        {nextCursor ? (
          <div className="history-pagination">
            <span className="mono">{pagesItems.length} rows</span>
            <Button
              loading={collectionLoading}
              onClick={() => void loadCollection(tab, nextCursor, true)}
            >
              Load more
            </Button>
          </div>
        ) : null}
      </section>

      <section className="token-metadata" aria-labelledby="metadata-title">
        <div>
          <p className="panel-kicker">Untrusted metadata</p>
          <h2 id="metadata-title">About this token</h2>
        </div>
        <p>{token.description.trim() || "No description supplied."}</p>
        <div className="token-links">
          <SafeExternalLink href={token.x_url}>X profile</SafeExternalLink>
          <SafeExternalLink href={token.telegram_url}>Telegram</SafeExternalLink>
          {explorer ? (
            <SafeExternalLink href={explorer}>
              Contract {shortAddress(token.address)}
            </SafeExternalLink>
          ) : null}
        </div>
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
  );
}

function safeTokenAmount(raw: string) {
  return formatCanonicalBaseUnits(raw, 3) ?? "Unavailable";
}

function safeMarketValue(raw: string) {
  return formatCanonicalBaseUnits(raw, 6) ?? "Unavailable";
}
function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd className="mono">{value}</dd>
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
        <span>Side</span>
        <span>Token volume</span>
        <span>ETH volume</span>
        <span>Execution / spot</span>
        <span>Finality</span>
      </div>
      {items.map((item) => (
        <div className="history-row" role="row" key={`${item.tx_hash}-${item.log_index}`}>
          <span className="mono">{safeDate(item.time)}</span>
          <span className={item.side === "buy" ? "trade-buy" : "trade-sell"}>
            {item.side || "—"}
          </span>
          <span className="mono">{safeTokenAmount(item.token_volume)}</span>
          <span className="mono">{amount(item.eth_volume)}</span>
          <span className="mono trade-prices">
            {safeMarketValue(item.execution_price)} / {safeMarketValue(item.spot_price)}
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
  loading,
  error,
}: {
  items: components["schemas"]["HolderDTO"][];
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
  return (
    <div className="history-table" role="table" aria-label="Token holders">
      <div className="history-row history-heading" role="row">
        <span>Address</span>
        <span>Balance</span>
        <span>First acquired block</span>
      </div>
      {items.map((item) => (
        <div className="history-row holder-row" role="row" key={item.address}>
          <span className="mono">{shortAddress(item.address)}</span>
          <span className="mono">{safeTokenAmount(item.balance)}</span>
          <span className="mono">
            {Number.isSafeInteger(item.first_acquired_block)
              ? item.first_acquired_block.toLocaleString("en-US")
              : "Unavailable"}
          </span>
        </div>
      ))}
    </div>
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
      <Link className="ui-button ui-button-secondary ui-button-md" href="/">
        Return to Explore
      </Link>
    </section>
  );
}
function TokenSkeleton() {
  return (
    <div className="page-stack token-page">
      <section className="token-skeleton">
        <Skeleton className="token-detail-image" />
        <div>
          <Skeleton className="token-skeleton-title" />
          <Skeleton className="token-skeleton-copy" />
        </div>
      </section>
      <section className="workspace-panel">
        <div className="token-summary-grid">
          {Array.from({ length: 8 }, (_, index) => (
            <Skeleton key={index} />
          ))}
        </div>
      </section>
      <section className="workspace-panel">
        <Skeleton className="chart-skeleton" />
      </section>
    </div>
  );
}
