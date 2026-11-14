"use client";

import { ArrowUpRight, X } from "@phosphor-icons/react";
import { CopyAddress } from "@/components/copy-address";
import { PriceText } from "@/components/price-text";
import { startTransition, useEffect, useState, type RefObject } from "react";
import type { TokenDetailResponse } from "@/api/client";
import type { publicConfiguration } from "@/config/public";
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
} from "@/components/primitives";
import { SlidingIndicator } from "@/components/sliding-indicator";
import { TradingPanel } from "@/transactions-panel";
import type { CandleChartPoint } from "./chart-data";
import { MarketChart } from "./market-chart";
import {
  amount,
  finalityTone,
  finalityLabel,
  safeTokenAmount,
  safeMarketValue,
} from "./token-format";
import { Metric } from "./facts-panels";
import type { CandleInterval } from "./use-token-page-data";

export { AboutPanel, ActivityPanel } from "./token-panels";
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
          <PriceText value={safeMarketValue(token.spot_price_eth)} />
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
  const [mode, setChartMode] = useState<"area" | "candles">("area");
  // A mode switch rebuilds the whole chart in MarketChart's effect. As a transition that work
  // runs after the click's next paint instead of before it (INP 206 ms measured on a mobile
  // profile with the synchronous switch).
  const setMode = (next: "area" | "candles") => startTransition(() => setChartMode(next));
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

function useStickyHeight(ref: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    const observer = new ResizeObserver(() =>
      element.style.setProperty("--aside-height", `${element.offsetHeight}px`),
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, [ref]);
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
  useStickyHeight(asideRef);
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
