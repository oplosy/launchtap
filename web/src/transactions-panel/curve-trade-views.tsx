"use client";

import { useEffect, useState, type ReactNode } from "react";
import { formatBaseUnits } from "@/amounts";
import { ApiClient } from "@/api/client";
import type { snipeTaxLine } from "@/anti-snipe";
import type { V4Addresses } from "@/engine";
import type { curvePriceImpact } from "@/price-impact";
import { parseQuoteQuantity, type ReviewedWriteIntent } from "@/transactions";
import type { publicConfiguration } from "@/config/public";
import { UsdLine } from "@/market/usd-line";
import { Badge, UnavailableState } from "@/components/primitives";
import { type Address, eth, QUOTE_DEBOUNCE_MS } from "./shared";
import { V2CreatorPanel } from "./v2-creator-panel";
import { V4SwapPanel } from "./v4-swap-panel";
import { GraduatedSwapPanel } from "./graduated-swap-panel";

export type CurveQuote = {
  output: bigint;
  fee: bigint;
  protocolFee: bigint;
  source: "backend" | "contract";
};

/**
 * The informational backend quote shown while typing; signing never uses it. The setters let the
 * authoritative contract quote replace it at review and right before signing.
 */
export function useCurveQuote(
  configuration: ReturnType<typeof publicConfiguration>,
  tokenAddress: string,
  side: "buy" | "sell",
  inputUnits: bigint | null,
) {
  const token = { address: tokenAddress };
  // fee is protocol + creator; the confirmation lists the two shares separately.
  const [quote, setQuote] = useState<{
    output: bigint;
    fee: bigint;
    protocolFee: bigint;
    source: "backend" | "contract";
  } | null>(null);
  const [quoteError, setQuoteError] = useState<string | null>(null);
  const [taxQuote, setTaxQuote] = useState<{ snipeTax: bigint; taxBps: number } | null>(null);
  // ETH a buy actually spends (input minus a graduation-cap refund), or the tokens a sell spends.
  const [quoteInputUsed, setQuoteInputUsed] = useState<bigint | null>(null);
  useEffect(() => {
    let cancelled = false;
    if (
      !inputUnits ||
      inputUnits <= 0n ||
      configuration.status !== "ready" ||
      !configuration.apiBaseUrl
    )
      return;
    const client = new ApiClient({ baseUrl: configuration.apiBaseUrl });
    const controller = new AbortController();
    const timer = setTimeout(() => {
      void client
        .getQuote(token.address, { amount: inputUnits.toString(), side }, controller.signal)
        .then((result) => {
          if (!cancelled) {
            if (parseQuoteQuantity(result.input, "quote input") !== inputUnits)
              throw new Error("QuoteChanged");
            const output = parseQuoteQuantity(result.output, "quote output");
            const refund = parseQuoteQuantity(result.refund, "quote refund");
            if (refund > inputUnits) throw new Error("QuoteChanged");
            parseQuoteQuantity(result.next_virtual_eth, "next virtual ETH");
            parseQuoteQuantity(result.next_virtual_token, "next virtual token");
            parseQuoteQuantity(result.reserve_source_block.toString(), "quote source block");
            const protocolFee = parseQuoteQuantity(result.protocol_fee, "protocol fee");
            setQuote({
              output,
              fee: protocolFee + parseQuoteQuantity(result.creator_fee, "creator fee"),
              protocolFee,
              source: "backend",
            });
            setTaxQuote({
              snipeTax: parseQuoteQuantity(result.snipe_tax, "snipe tax"),
              taxBps: result.tax_bps,
            });
            setQuoteInputUsed(side === "buy" ? inputUnits - refund : inputUnits);
            setQuoteError(null);
          }
        })
        .catch(() => {
          if (!cancelled) {
            setQuote(null);
            setTaxQuote(null);
            setQuoteInputUsed(null);
            setQuoteError("Informational quote unavailable. Refresh before trading.");
          }
        });
    }, QUOTE_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
      controller.abort();
    };
  }, [configuration.apiBaseUrl, configuration.status, inputUnits, side, token.address]);
  return {
    quote,
    setQuote,
    quoteError,
    setQuoteError,
    taxQuote,
    setTaxQuote,
    quoteInputUsed,
    setQuoteInputUsed,
  };
}

export function CurveQuoteSummary({
  quote,
  side,
  taxLine,
  minimum,
  priceImpact,
}: {
  quote: CurveQuote;
  side: "buy" | "sell";
  taxLine: ReturnType<typeof snipeTaxLine> | null;
  minimum: bigint | null;
  priceImpact: ReturnType<typeof curvePriceImpact> | null;
}) {
  return (
    <div className="quote-summary">
      <span>
        Estimated output <strong className="mono">{formatBaseUnits(quote.output, 18, 6)}</strong>
        {side === "sell" ? <UsdLine wei={quote.output} /> : null}
      </span>
      <span>
        Fees <strong className="mono">{eth(quote.fee)}</strong>
        <UsdLine wei={quote.fee} />
      </span>
      {taxLine ? (
        <span>
          Anti-snipe tax ({taxLine.rate}) <strong className="mono">{eth(taxLine.amount)}</strong>
          {taxLine.timeLeft ? <small> ends in {taxLine.timeLeft}</small> : null}
        </span>
      ) : null}
      <span>
        Minimum output{" "}
        <strong className="mono">{minimum === null ? "—" : formatBaseUnits(minimum, 18, 6)}</strong>
        {side === "sell" && minimum !== null ? <UsdLine wei={minimum} /> : null}
      </span>
      {priceImpact ? (
        <span>
          Price impact <strong className="mono">{priceImpact.percent}</strong>
        </span>
      ) : null}
      {priceImpact?.high ? (
        <p className="transaction-warning" role="status">
          This trade moves the price by {priceImpact.percent}. A smaller amount gets a better
          average price.
        </p>
      ) : null}
      <small>
        {quote.source === "backend"
          ? "Informational API quote; contract quote is authoritative before signing."
          : "Authoritative contract quote, read immediately before simulation."}
        {taxLine
          ? " The tax only decreases, so a later block never gives less than the minimum output."
          : null}
        {priceImpact ? " Price impact is measured against the last indexed spot price." : null}
      </small>
    </div>
  );
}

export type CurveReviewDetails = {
  side: "buy" | "sell";
  inputUnits: bigint;
  output: bigint;
  fee: bigint;
  protocolFee: bigint;
};

/** The reviewed curve write, field by field, as the wallet will be asked to sign it. */
export function CurveTradeDetails({
  token,
  side,
  input,
  reviewDetails,
  reviewIntent,
  slippage,
  slippageBps,
  configuration,
  curveExplorer,
}: {
  token: { name: string; symbol: string; curve: string };
  side: "buy" | "sell";
  input: string;
  reviewDetails: CurveReviewDetails | null;
  reviewIntent: ReviewedWriteIntent | null;
  slippage: string;
  slippageBps: bigint | null;
  configuration: ReturnType<typeof publicConfiguration>;
  curveExplorer: string | null;
}) {
  return (
    <dl>
      <dt>Token</dt>
      <dd>
        {token.name} ({token.symbol})
      </dd>
      <dt>Input</dt>
      <dd className="mono">
        {reviewDetails ? formatBaseUnits(reviewDetails.inputUnits, 18, 6) : input} ·{" "}
        {side === "buy" ? "ETH" : token.symbol}
      </dd>
      <dt>Wallet</dt>
      <dd className="mono">{reviewIntent?.account ?? "Unavailable"}</dd>
      <dt>Chain ID</dt>
      <dd className="mono">{reviewIntent?.chainId ?? "Unavailable"}</dd>
      <dt>Minimum output</dt>
      <dd className="mono">
        {reviewIntent?.minimumOutput === undefined
          ? "—"
          : formatBaseUnits(reviewIntent.minimumOutput, 18, 6)}
      </dd>
      <dt>Protocol fee</dt>
      <dd className="mono">{reviewDetails ? eth(reviewDetails.protocolFee) : "—"}</dd>
      <dt>Creator fee</dt>
      <dd className="mono">
        {reviewDetails ? eth(reviewDetails.fee - reviewDetails.protocolFee) : "—"}
      </dd>
      <dt>Network fee</dt>
      <dd>Shown and paid in your wallet</dd>
      <dt>Target</dt>
      <dd className="mono">{reviewIntent?.target ?? "Unavailable"}</dd>
      <dt>Function and arguments</dt>
      <dd className="mono">
        {reviewIntent
          ? `${reviewIntent.functionName}(${reviewIntent.args.map(String).join(", ")})`
          : "Unavailable"}
      </dd>
      <dt>Native value</dt>
      <dd className="mono">
        {reviewIntent
          ? `${eth(reviewIntent.value)} (${reviewIntent.value.toString()} wei)`
          : "Unavailable"}
      </dd>
      <dt>Slippage</dt>
      <dd className="mono">{slippageBps === null ? "Invalid" : `${slippage}%`}</dd>
      <dt>Deadline</dt>
      <dd className="mono">{reviewIntent?.deadline?.toString() ?? "Unavailable"} (Unix seconds)</dd>
      <dt>Network</dt>
      <dd>{configuration.deployment?.name}</dd>
      <dt>Contract</dt>
      <dd className="mono">
        {curveExplorer ? (
          <a href={curveExplorer} target="_blank" rel="noreferrer">
            {token.curve}
          </a>
        ) : (
          token.curve
        )}
      </dd>
      {side === "sell" ? (
        <>
          <dt>Approval spender</dt>
          <dd className="mono">
            {curveExplorer ? (
              <a href={curveExplorer} target="_blank" rel="noreferrer">
                {token.curve}
              </a>
            ) : (
              token.curve
            )}{" "}
            (exact sell amount only)
          </dd>
        </>
      ) : null}
    </dl>
  );
}

/** A graduated token trades through the reviewed router (engine 1) or V4 pool (engine 2). */
export function GraduatedRoute({
  token,
  tokenAddress,
  curveAddress,
  engineVersion,
  graduated,
  reviewedV4,
  v4PoolReady,
  reviewedRouter,
  reviewedWeth,
  claimRow,
}: {
  token: Parameters<typeof V4SwapPanel>[0]["token"];
  tokenAddress: Address;
  curveAddress: Address;
  engineVersion: number;
  graduated: boolean;
  reviewedV4: V4Addresses | null;
  v4PoolReady: boolean;
  reviewedRouter: Address | null;
  reviewedWeth: Address | null;
  claimRow: ReactNode;
}) {
  return (
    <section className="workspace-panel transaction-panel" aria-labelledby="graduated-trade-title">
      <div className="panel-head">
        <div>
          <p className="panel-kicker">Graduated route</p>
          <h2 id="graduated-trade-title">Router handoff</h2>
        </div>
        <Badge tone="success">Graduated</Badge>
      </div>
      {engineVersion === 2 ? (
        reviewedV4 && v4PoolReady ? (
          <V4SwapPanel token={token} addresses={reviewedV4} />
        ) : (
          <UnavailableState
            title="Pool unavailable"
            description="No reviewed Uniswap V4 deployment is enabled for this chain, or the indexed pool does not match the reviewed pool key. Trading is safely disabled."
          />
        )
      ) : reviewedRouter && reviewedWeth ? (
        <GraduatedSwapPanel token={token} router={reviewedRouter} weth={reviewedWeth} />
      ) : (
        <UnavailableState
          title="Router unavailable"
          description="No enabled reviewed Uniswap v2 router is configured for this chain. Trading is safely disabled."
        />
      )}
      {engineVersion === 2 ? claimRow : null}
      {engineVersion === 2 ? (
        <V2CreatorPanel
          tokenAddress={tokenAddress}
          curveAddress={curveAddress}
          locker={reviewedV4?.locker ?? null}
          graduated={graduated}
        />
      ) : null}
    </section>
  );
}
