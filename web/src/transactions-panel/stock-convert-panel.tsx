"use client";

import { useCallback, useEffect, useState } from "react";
import type { Abi } from "viem";
import { engineDeployment, publicConfiguration } from "@/config/public";
import { browserAbis } from "@/contracts/generated";
import { reviewedV4Addresses, type V4Addresses } from "@/engine";
import {
  STOCK_ASSETS,
  encodeV4ExactIn,
  impactLevel,
  priceImpactBps,
  quoteExactParams,
  referenceInput,
  stockPath,
  stockRoutingEnabled,
  type ConversionQuote,
  type StockAsset,
  type StockDirection,
} from "@/stock-routing";
import { planPermit2Sell } from "@/v4-swap";
import {
  decodeTransactionError,
  minimumOutput,
  parseRuntimeQuantity,
  transactionDeadline,
  type ReviewedWriteIntent,
} from "@/transactions";
import { addressExplorerUrl } from "@/wallet/explorer";
import { Badge, Button } from "@/components/primitives";
import { readPermit2Allowance, sellReviewDeadline } from "./permit2-allowance";
import { StockConvertDetails, StockConvertRisk, StockQuoteSummary } from "./stock-convert-views";
import {
  DEFAULT_TTL,
  QUOTE_DEBOUNCE_MS,
  type Address,
  currentUnixSeconds,
  ReadinessGate,
  TradeSideTabs,
  TransactionProgress,
} from "./shared";
import {
  ConnectOr,
  ReviewActions,
  ReviewNotice,
  TradeAmountFields,
  useReviewedTrade,
  WrongNetworkBanner,
} from "./reviewed-trade";

/**
 * Converts a Robinhood stock token to ETH, or ETH to a stock token, through the reviewed
 * Universal Router (docs/specs/2026-11-24-stock-token-routing-preflight.md). It renders only
 * behind NEXT_PUBLIC_STOCK_ROUTING on Robinhood Chain mainnet with the engine-2 V4 addresses.
 */
export function StockConvertPanel() {
  const configuration = publicConfiguration();
  const deployment = engineDeployment(configuration, 2);
  const addresses = deployment ? reviewedV4Addresses(deployment, configuration.chainId) : null;
  if (!addresses || !stockRoutingEnabled(configuration.chainId, true)) return null;
  return (
    <ReadinessGate>
      <StockConvertSection addresses={addresses} />
    </ReadinessGate>
  );
}

function StockConvertSection({ addresses }: { addresses: V4Addresses }) {
  const [asset, setAsset] = useState<StockAsset>(STOCK_ASSETS[0]!);
  return (
    <section className="workspace-panel transaction-panel" aria-labelledby="stock-convert-title">
      <div className="panel-head">
        <div>
          <h2 id="stock-convert-title">Convert stock tokens</h2>
        </div>
        <Badge tone="accent">Beta</Badge>
      </div>
      <label className="explore-sort">
        <span>Stock token</span>
        <select
          className="ui-input"
          value={asset.symbol}
          onChange={(event) =>
            setAsset(STOCK_ASSETS.find((option) => option.symbol === event.target.value) ?? asset)
          }
        >
          {STOCK_ASSETS.map((option) => (
            <option key={option.symbol} value={option.symbol}>
              {option.symbol} · {option.name}
            </option>
          ))}
        </select>
      </label>
      {/* Keyed by asset so the amount, quote and persisted state start fresh per token. */}
      <StockConvertForm key={asset.address} asset={asset} addresses={addresses} />
    </section>
  );
}

/** "buy" spends ETH for the stock token; "sell" spends the stock token for ETH. */
const directionOf = (value: "buy" | "sell"): StockDirection =>
  value === "buy" ? "eth-to-stock" : "stock-to-eth";

function StockConvertForm({ asset, addresses }: { asset: StockAsset; addresses: V4Addresses }) {
  const trade = useReviewedTrade(asset.address, true, "stock-convert");
  const {
    publicClient,
    configuration,
    state,
    approvalTransaction,
    approvalHash,
    side,
    inputUnits,
    slippage,
    slippageBps,
    executionBusy,
    confirming,
    reviewIntent,
    reviewNotice,
  } = trade;
  const router = addresses.universalRouter;
  const routerExplorer = addressExplorerUrl(configuration, router);
  const [storedQuote, setQuote] = useState<ConversionQuote | null>(null);
  const [reviewInput, setReviewInput] = useState<bigint | null>(null);

  const quoteExactIn = useCallback(
    async (direction: StockDirection, amountIn: bigint): Promise<ConversionQuote> => {
      const route = stockPath(asset, direction);
      const read = async (amount: bigint) => {
        const { result } = await publicClient!.simulateContract({
          address: addresses.v4Quoter,
          abi: browserAbis.v4Quoter,
          functionName: "quoteExactInput",
          args: [quoteExactParams(route, amount)],
        } as never);
        return parseRuntimeQuantity((result as readonly unknown[])[0], "V4 route quote output");
      };
      const referenceIn = referenceInput(amountIn);
      const [amountOut, referenceOut] = await Promise.all([read(amountIn), read(referenceIn)]);
      return {
        direction,
        amountIn,
        amountOut,
        impactBps: priceImpactBps({ amountIn, amountOut, referenceIn, referenceOut }),
      };
    },
    [addresses.v4Quoter, asset, publicClient],
  );

  useEffect(() => {
    let cancelled = false;
    if (!inputUnits || inputUnits <= 0n || !publicClient) return;
    const timer = setTimeout(() => {
      void quoteExactIn(directionOf(side), inputUnits)
        .then((next) => {
          if (!cancelled) setQuote(next);
        })
        .catch(() => {
          if (!cancelled) setQuote(null);
        });
    }, QUOTE_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [inputUnits, publicClient, quoteExactIn, side]);

  // A quote read for an earlier or cleared amount is never shown next to the current one.
  const quote =
    storedQuote !== null &&
    storedQuote.amountIn === inputUnits &&
    storedQuote.direction === directionOf(side)
      ? storedQuote
      : null;
  const level = quote ? impactLevel(quote.impactBps) : null;
  const minimum =
    quote !== null && slippageBps !== null ? minimumOutput(quote.amountOut, slippageBps) : null;
  const outputSymbol = side === "buy" ? asset.symbol : "ETH";

  const readConversionIntent = async (
    accountAddress: Address,
    quoteSide: "buy" | "sell",
    quantity: bigint,
    toleranceBps: bigint,
    deadline: bigint,
  ) => {
    const intentChainId = configuration.chainId;
    if (intentChainId === null) throw new Error("ChainUnavailable");
    const direction = directionOf(quoteSide);
    const fresh = await quoteExactIn(direction, quantity);
    if (impactLevel(fresh.impactBps) === "block") throw new Error("StockRoutePriceImpactTooHigh");
    const minimumAmount = minimumOutput(fresh.amountOut, toleranceBps);
    const encoded = encodeV4ExactIn({
      ...stockPath(asset, direction),
      amountIn: quantity,
      amountOutMinimum: minimumAmount,
    });
    return {
      intent: {
        account: accountAddress,
        target: router,
        value: quoteSide === "buy" ? quantity : 0n,
        args: [encoded.commands, encoded.inputs, deadline],
        deadline,
        minimumOutput: minimumAmount,
        chainId: intentChainId,
        functionName: "execute",
      } satisfies ReviewedWriteIntent,
      replace: () => {
        setQuote(fresh);
        setReviewInput(quantity);
      },
    };
  };

  const prepareReview = async () => {
    if (!trade.walletReady || !inputUnits || slippageBps === null || trade.unresolved) return;
    const review = trade.beginReview();
    try {
      await trade.assertWalletForReview();
      let deadline = transactionDeadline(currentUnixSeconds(), DEFAULT_TTL);
      if (side === "sell" && publicClient) {
        // A conversion resumed after its approvals reuses the exact Permit2 allowance; the swap
        // deadline must not outlive it.
        deadline = await sellReviewDeadline(publicClient, {
          permit2: addresses.permit2,
          owner: trade.account.address as Address,
          token: asset.address,
          spender: router,
          amount: inputUnits,
          nowSeconds: currentUnixSeconds(),
          ttlSeconds: DEFAULT_TTL,
        });
      }
      const fresh = await readConversionIntent(
        trade.account.address as Address,
        side,
        inputUnits,
        slippageBps,
        deadline,
      );
      if (review.open(fresh.intent)) fresh.replace();
    } catch (cause) {
      if (!review.current()) return;
      trade.closeReview(false);
      trade.setReviewNotice(decodeTransactionError(cause).message);
    }
  };

  const execute = async () => {
    if (
      !publicClient ||
      inputUnits === null ||
      slippageBps === null ||
      !reviewIntent ||
      reviewInput === null ||
      !trade.walletReady ||
      trade.unresolved
    )
      return;
    const reviewed = reviewIntent;
    const reviewedSide = side;
    const reviewedInput = reviewInput;
    const requote = () =>
      readConversionIntent(
        reviewed.account as Address,
        reviewedSide,
        reviewedInput,
        slippageBps,
        reviewed.deadline!,
      );
    await trade.runExecution(async (run) => {
      await run.ensureWallet(reviewed);
      if (!run.unchanged(reviewed, await requote())) return;
      if (reviewedSide === "buy") {
        const balance = await publicClient.getBalance({ address: reviewed.account as Address });
        if (balance < reviewedInput) throw new Error("InsufficientETHBalance");
      } else {
        const [balance, erc20Allowance, permit2] = await Promise.all([
          publicClient.readContract({
            address: asset.address,
            abi: browserAbis.tokenV2,
            functionName: "balanceOf",
            args: [reviewed.account as Address],
          }),
          publicClient.readContract({
            address: asset.address,
            abi: browserAbis.tokenV2,
            functionName: "allowance",
            args: [reviewed.account as Address, addresses.permit2],
          }),
          readPermit2Allowance(publicClient, {
            permit2: addresses.permit2,
            owner: reviewed.account as Address,
            token: asset.address,
            spender: router,
          }),
        ]);
        if ((balance as bigint) < reviewedInput) throw new Error("ERC20InsufficientBalance");
        const steps = planPermit2Sell({
          amount: reviewedInput,
          erc20Allowance: erc20Allowance as bigint,
          permit2,
          nowSeconds: currentUnixSeconds(),
          ttlSeconds: DEFAULT_TTL,
          deadline: reviewed.deadline!,
        });
        if (steps.length > 1) {
          const tokenWrite = (amount: bigint) => ({
            address: asset.address,
            abi: browserAbis.tokenV2 as Abi,
            functionName: "approve",
            args: [addresses.permit2, amount] as const,
          });
          for (const step of steps) {
            if (step === "reset-erc20") await run.approve(reviewed, tokenWrite(0n));
            if (step === "approve-erc20") await run.approve(reviewed, tokenWrite(reviewedInput));
            if (step === "approve-permit2")
              await run.approve(reviewed, {
                address: addresses.permit2,
                abi: browserAbis.permit2 as Abi,
                functionName: "approve",
                // Exact amount, expiring with the reviewed swap deadline (≤ the TTL).
                args: [asset.address, router, reviewedInput, Number(reviewed.deadline!)],
              });
          }
          run.approvalsDone(
            "Allowances are set for this exact conversion. Review it again to sign.",
          );
          return;
        }
      }
      await run.sign(reviewed, { abi: browserAbis.universalRouter as Abi, requote });
    });
  };

  const ready = Boolean(
    inputUnits &&
    inputUnits > 0n &&
    minimum !== null &&
    level !== "block" &&
    trade.walletReady &&
    !trade.unresolved,
  );
  const hops = stockPath(asset, directionOf(side)).path.length;
  return (
    <div className="graduated-swap">
      <WrongNetworkBanner write={trade} />
      <StockConvertRisk router={router} routerExplorer={routerExplorer} />
      <TradeSideTabs
        side={side}
        label="Conversion side"
        disabled={executionBusy}
        onChange={(next) => {
          trade.changeSide(next);
          setQuote(null);
        }}
      >
        <div className="transaction-form">
          <TradeAmountFields trade={trade} symbol={asset.symbol} />
          {quote !== null && level !== null ? (
            <StockQuoteSummary
              amountOut={quote.amountOut}
              impactBps={quote.impactBps}
              minimum={minimum}
              outputSymbol={outputSymbol}
              hops={hops}
              level={level}
            />
          ) : null}
        </div>
        {confirming ? (
          <div className="transaction-confirmation">
            <h3>Confirm conversion</h3>
            <ReviewNotice notice={reviewNotice} />
            <StockConvertDetails
              networkName={configuration.deployment?.name}
              router={router}
              permit2={addresses.permit2}
              asset={asset}
              side={side}
              reviewInput={reviewInput}
              reviewIntent={reviewIntent}
              slippage={slippage}
              slippageValid={slippageBps !== null}
            />
            <ReviewActions
              write={trade}
              label="Sign conversion"
              canSign={ready}
              onSign={() => void execute()}
            />
          </div>
        ) : (
          <>
            <ReviewNotice notice={reviewNotice} />
            <ConnectOr write={trade}>
              <Button
                variant="primary"
                size="lg"
                disabled={executionBusy || !ready}
                onClick={() => void prepareReview()}
              >
                {side === "sell" && approvalHash ? "Resume conversion" : "Review conversion"}
              </Button>
            </ConnectOr>
          </>
        )}
      </TradeSideTabs>
      <TransactionProgress
        state={state}
        tone={state.status === "reverted" ? "danger" : "warning"}
      />
      <TransactionProgress
        state={approvalTransaction.state}
        tone="warning"
        prefix="Approval "
        refreshNotice={false}
      />
    </div>
  );
}
