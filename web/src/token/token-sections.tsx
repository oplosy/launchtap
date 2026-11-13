"use client";

import { ArrowUpRight, X } from "@phosphor-icons/react";
import { CopyAddress } from "@/components/copy-address";
import { useState, type RefObject } from "react";
import type { components } from "@/api/generated";
import type { TokenDetailResponse } from "@/api/client";
import type { publicConfiguration } from "@/config/public";
import type { FeedItem } from "@/live/feed-store";
import { liveTradeRows } from "@/live/live-strip-model";
import { CoinGeckoAttribution, UsdLine } from "@/market/usd-line";
import { StarButton } from "@/watchlist/star-button";
import { WatchCount } from "@/watchlist/watch-count";
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
import { TradingPanel } from "@/transactions-panel";
import { shortAddress } from "./address";
import type { CandleChartPoint } from "./chart-data";
import type { TokenCollection } from "./pagination";
import { MarketChart } from "./market-chart";
import type { marketLinks } from "./market-links";
import {
  amount,
  finalityTone,
  finalityLabel,
  safeTokenAmount,
  safeMarketValue,
} from "./token-format";
import { Metric } from "./facts-panels";
import { Trades, Holders, CreatorLine, EarlyBuyers } from "./token-activity";
import type { CandleInterval, Tab } from "./use-token-page-data";

type Configuration = ReturnType<typeof publicConfiguration>;

/** Display order, shortest to longest. */
const INTERVAL_ORDER: readonly CandleInterval[] = ["1m", "5m", "1h", "6h", "1d", "all"];

export function TokenHeader({
  token,
  title,
  currentImageUrl,
  priceFlash,
  configuration,
  useFixture,
  tokenAddress,
  explorer,
}: {
  token: TokenDetailResponse;
  title: string;
  currentImageUrl: string | null | undefined;
  priceFlash: { direction: "up" | "down"; id: number } | null;
  configuration: Configuration;
  useFixture: boolean;
  tokenAddress: `0x${string}`;
  explorer: string | null;
}) {
  const snapshot = token.snapshot;
  const change = token.price_change_24h_bps / 100;
  return (
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
          <SafeExternalLink href={explorer} className="ui-button ui-button-secondary ui-button-sm">
            Explorer <ArrowUpRight size={14} aria-hidden="true" />
          </SafeExternalLink>
        ) : (
          <span className="token-link-unavailable">Explorer unavailable</span>
        )}
      </div>
    </header>
  );
}

export function ChartPanel({
  tokenAddress,
  interval,
  setInterval,
  points,
  loading,
  error,
  onRetry,
}: {
  tokenAddress: `0x${string}`;
  interval: CandleInterval;
  setInterval: (value: CandleInterval) => void;
  points: CandleChartPoint[];
  loading: boolean;
  error: Error | null;
  onRetry: () => void;
}) {
  const [mode, setMode] = useState<"area" | "candles">("area");
  return (
    <section className="market-panel" aria-labelledby="market-title">
      <h2 id="market-title" className="sr-only">
        Price chart
      </h2>
      <div className="chart-controls">
        <label className="chart-range">
          <span className="sr-only">Timeframe</span>
          <select
            value={interval}
            onChange={(event) => setInterval(event.target.value as CandleInterval)}
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
      {loading ? (
        <div className="chart-loading">
          <Skeleton className="chart-skeleton" />
        </div>
      ) : error ? (
        <ErrorState
          title="Chart unavailable"
          description="Price history could not be loaded."
          action={<Button onClick={onRetry}>Retry</Button>}
        />
      ) : (
        <MarketChart key={tokenAddress} points={points} mode={mode} />
      )}
    </section>
  );
}

export function MarketStats({
  token,
  liquidityIndexed,
}: {
  token: TokenDetailResponse;
  liquidityIndexed: boolean;
}) {
  const progress = Math.max(0, Math.min(100, token.graduation_progress_bps / 100));
  return (
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
          <Metric label="Liquidity" value={amount(token.liquidity_eth)} eth={token.liquidity_eth} />
        ) : (
          <Metric label="Pool liquidity" value="Not indexed yet" />
        )}
        <Metric label="Holders" value={token.holder_count.toLocaleString("en-US")} />
        {liquidityIndexed ? (
          <>
            <Metric label="ETH reserve" value={amount(token.eth_reserve)} eth={token.eth_reserve} />
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
  );
}

export function ActivityPanel({
  token,
  tokenAddress,
  configuration,
  tab,
  selectTab,
  pages,
  liveFeed,
  collectionLoading,
  collectionError,
  resetNotice,
  loadMore,
}: {
  token: TokenDetailResponse;
  tokenAddress: `0x${string}`;
  configuration: Configuration;
  tab: Tab;
  selectTab: (tab: Tab) => void;
  pages: TokenCollection[];
  liveFeed: { address: string; items: FeedItem[] };
  collectionLoading: boolean;
  collectionError: Error | null;
  resetNotice: boolean;
  loadMore: (cursor: string) => void;
}) {
  const collectionSnapshot = pages[0]?.snapshot;
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
  return (
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
        onChange={(value) => selectTab(value as Tab)}
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
                  apiBaseUrl={configuration.status === "ready" ? configuration.apiBaseUrl : null}
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
            onClick={() => loadMore(nextCursor)}
          >
            Load more
          </Button>
        </div>
      ) : null}
    </section>
  );
}

export function AboutPanel({
  token,
  explorer,
  market,
  configuration,
}: {
  token: TokenDetailResponse;
  explorer: string | null;
  market: ReturnType<typeof marketLinks>;
  configuration: Configuration;
}) {
  return (
    <section className="token-metadata" aria-labelledby="metadata-title">
      <div className="token-section-head">
        <div>
          <h2 id="metadata-title">About this token</h2>
          <p className="section-description">Supplied by the creator, not verified.</p>
        </div>
      </div>
      <p className="token-description">{token.description.trim() || "No description supplied."}</p>
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
  );
}

/** The trade card: a column on wide screens, a modal sheet below 1024 px. */
export function TradeSheet({
  token,
  sheetSide,
  setSheetSide,
  asideRef,
}: {
  token: TokenDetailResponse;
  sheetSide: "buy" | "sell" | null;
  setSheetSide: (side: "buy" | "sell" | null) => void;
  asideRef: RefObject<HTMLElement | null>;
}) {
  return (
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
  );
}

/** The bottom bar that opens the trade sheet on narrow screens, with its backdrop. */
export function TradeBar({
  sheetSide,
  setSheetSide,
}: {
  sheetSide: "buy" | "sell" | null;
  setSheetSide: (side: "buy" | "sell" | null) => void;
}) {
  return (
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
  );
}
