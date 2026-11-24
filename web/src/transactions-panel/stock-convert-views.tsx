"use client";

import { formatBaseUnits } from "@/amounts";
import type { ImpactLevel, StockAsset } from "@/stock-routing";
import type { ReviewedWriteIntent } from "@/transactions";
import { type Address, eth } from "./shared";

/** The jurisdiction, custody and two-transaction notice above the conversion form. */
export function StockConvertRisk({
  router,
  routerExplorer,
}: {
  router: Address;
  routerExplorer: string | null;
}) {
  return (
    <p className="transaction-risk">
      Stock tokens are debt securities issued by Robinhood Assets (Jersey) Limited and may not be
      offered to persons in the United States, Canada, the United Kingdom or Switzerland. They carry
      no shareholder rights. Conversions route through Uniswap v4 pools via the reviewed Universal
      Router{" "}
      {routerExplorer ? (
        <a href={routerExplorer} target="_blank" rel="noreferrer" className="mono">
          {router}
        </a>
      ) : (
        <span className="mono">{router}</span>
      )}
      . Buying a launch token with a stock token takes two transactions: convert to ETH here, then
      trade; the price can move in between.
    </p>
  );
}

export function StockQuoteSummary({
  amountOut,
  impactBps,
  minimum,
  outputSymbol,
  hops,
  level,
}: {
  amountOut: bigint;
  impactBps: bigint;
  minimum: bigint | null;
  outputSymbol: string;
  hops: number;
  level: ImpactLevel;
}) {
  return (
    <div className="quote-summary">
      <span>
        Route output{" "}
        <strong className="mono">
          {formatBaseUnits(amountOut, 18, 6)} {outputSymbol}
        </strong>
      </span>
      <span>
        Minimum output{" "}
        <strong className="mono">
          {minimum === null ? "—" : formatBaseUnits(minimum, 18, 6)} {outputSymbol}
        </strong>
      </span>
      <span>
        Price impact <strong className="mono">{(Number(impactBps) / 100).toFixed(2)}%</strong>
      </span>
      <small>
        {hops === 1 ? "One pool" : `${hops} pools`}; pool fees are included in this quote.
      </small>
      {level === "warn" ? (
        <p className="transaction-warning" role="status">
          Price impact is above 3%. Stock token pools are thin; consider a smaller amount.
        </p>
      ) : null}
      {level === "block" ? (
        <p className="transaction-error" role="alert">
          Price impact is above 15%. This conversion cannot be reviewed.
        </p>
      ) : null}
    </div>
  );
}

export function StockConvertDetails({
  networkName,
  router,
  permit2,
  asset,
  side,
  reviewInput,
  reviewIntent,
  slippage,
  slippageValid,
}: {
  networkName: string | undefined;
  router: Address;
  permit2: Address;
  asset: StockAsset;
  side: "buy" | "sell";
  reviewInput: bigint | null;
  reviewIntent: ReviewedWriteIntent | null;
  slippage: string;
  slippageValid: boolean;
}) {
  const outputSymbol = side === "buy" ? asset.symbol : "ETH";
  return (
    <dl>
      <dt>Network</dt>
      <dd>{networkName}</dd>
      <dt>Contract</dt>
      <dd className="mono">{router}</dd>
      <dt>Stock token</dt>
      <dd className="mono">
        {asset.symbol} {asset.address}
      </dd>
      <dt>Input / minimum</dt>
      <dd className="mono">
        {reviewInput === null ? "—" : formatBaseUnits(reviewInput, 18, 6)}{" "}
        {side === "buy" ? "ETH" : asset.symbol} /{" "}
        {reviewIntent?.minimumOutput === undefined
          ? "—"
          : formatBaseUnits(reviewIntent.minimumOutput, 18, 6)}{" "}
        {outputSymbol}
      </dd>
      <dt>Wallet</dt>
      <dd className="mono">{reviewIntent?.account ?? "Unavailable"}</dd>
      <dt>Chain ID</dt>
      <dd className="mono">{reviewIntent?.chainId ?? "Unavailable"}</dd>
      <dt>Function</dt>
      <dd className="mono">execute(V4_SWAP: exact input, settle all, take all)</dd>
      <dt>Native value</dt>
      <dd className="mono">
        {reviewIntent
          ? `${eth(reviewIntent.value)} (${reviewIntent.value.toString()} wei)`
          : "Unavailable"}
      </dd>
      <dt>Slippage</dt>
      <dd className="mono">{slippageValid ? `${slippage}%` : "Invalid"}</dd>
      <dt>Deadline</dt>
      <dd className="mono">{reviewIntent?.deadline?.toString() ?? "Unavailable"} (Unix seconds)</dd>
      {side === "sell" ? (
        <>
          <dt>Allowances</dt>
          <dd className="mono">
            approve({permit2}, {reviewInput?.toString() ?? "—"}) on {asset.address};
            Permit2.approve({asset.address}, {router}, {reviewInput?.toString() ?? "—"},{" "}
            {reviewIntent?.deadline?.toString() ?? "—"}). Already exact allowances are skipped.
          </dd>
        </>
      ) : null}
    </dl>
  );
}
