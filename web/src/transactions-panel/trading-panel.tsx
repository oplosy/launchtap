"use client";

import { useState } from "react";
import type { Abi } from "viem";
import { reviewedV4Addresses } from "@/engine";
import { poolMatchesApi } from "@/v4-swap";
import { browserAbis } from "@/contracts/generated";
import { addressExplorerUrl } from "@/wallet/explorer";
import { decodeTransactionError, reviewedRouterAddress, transactionDeadline } from "@/transactions";
import { Badge, UnavailableState } from "@/components/primitives";
import {
  DEFAULT_TTL,
  type Address,
  safeAddress,
  currentUnixSeconds,
  TradeSideTabs,
  ReadinessGate,
  TransactionProgress,
} from "./shared";
import { TradeAmountFields, useReviewedTrade, WrongNetworkBanner } from "./reviewed-trade";
import { CurveClaimRow, useCurveClaims } from "./curve-claims";
import { CurveReviewStep } from "./curve-review-step";
import { readCurveTradeIntent, useCurveTradeFigures, type TradingToken } from "./curve-trade-logic";
import { V2CreatorPanel } from "./v2-creator-panel";
import {
  CurveQuoteSummary,
  GraduatedRoute,
  useCurveQuote,
  type CurveReviewDetails,
} from "./curve-trade-views";

export function TradingPanel({ token }: { token: TradingToken }) {
  return (
    <ReadinessGate>
      <TradingPanelReady token={token} />
    </ReadinessGate>
  );
}

function TradingPanelReady({ token }: { token: TradingToken }) {
  const tokenAddress = safeAddress(token.address);
  const curveAddress = safeAddress(token.curve);
  const safeTokenAddress =
    tokenAddress ?? ("0x0000000000000000000000000000000000000000" as Address);
  const safeCurveAddress =
    curveAddress ?? ("0x0000000000000000000000000000000000000000" as Address);
  const graduated = token.phase.toLowerCase() === "graduated";
  // Engine 2 trades the curve through BondingCurveV2 (Plan v2-C Task 3); any other engine
  // value leaves the panel unavailable instead of guessing an ABI.
  const engineVersion = token.engineVersion ?? 1;
  const curveAbi = (engineVersion === 2 ? browserAbis.curveV2 : browserAbis.curve) as Abi;
  const trade = useReviewedTrade(tokenAddress, !graduated);
  const {
    account,
    readiness,
    publicClient,
    walletClient,
    configuration,
    chainId,
    transaction,
    approvalTransaction,
    state,
    approvalHash,
    side,
    inputUnits,
    slippageBps,
    executionBusy,
    confirming,
    reviewIntent,
  } = trade;
  const {
    quote,
    setQuote,
    quoteError,
    setQuoteError,
    taxQuote,
    setTaxQuote,
    quoteInputUsed,
    setQuoteInputUsed,
  } = useCurveQuote(configuration, token.address, side, inputUnits);
  const [reviewDetails, setReviewDetails] = useState<CurveReviewDetails | null>(null);
  const curveExplorer = addressExplorerUrl(configuration, safeCurveAddress);
  const claims = useCurveClaims({
    configuration,
    account,
    readiness,
    chainId,
    publicClient,
    walletClient,
    tokenAddress,
    curveAddress,
    safeCurveAddress,
    curveAbi,
    engineVersion,
    graduated,
  });
  const claimRow = <CurveClaimRow claims={claims} />;
  const reviewedRouter = reviewedRouterAddress(configuration.deployment!, configuration.chainId);
  const reviewedV4 =
    engineVersion === 2
      ? reviewedV4Addresses(configuration.deployment!, configuration.chainId)
      : null;
  // Plan v2-C D5: trade only the pool whose locally computed id equals the indexed one.
  const v4PoolReady = Boolean(
    reviewedV4 &&
    tokenAddress &&
    poolMatchesApi(
      tokenAddress,
      reviewedV4.hook,
      token.v2PoolId ? { pool_id: token.v2PoolId } : null,
    ),
  );
  const reviewedWeth = safeAddress(configuration.deployment?.weth);

  const { taxLine, minimum, priceImpact } = useCurveTradeFigures({
    quote: inputUnits && inputUnits > 0n ? quote : null,
    side,
    slippageBps,
    taxQuote,
    quoteInputUsed,
    antiSnipeEndsAt: engineVersion === 2 && !graduated ? (token.antiSnipeEndsAt ?? null) : null,
    spotPriceWei: token.spotPriceWei,
    publicClient,
  });
  const readCurveIntent = async (
    accountAddress: Address,
    quoteSide: "buy" | "sell",
    quantity: bigint,
    toleranceBps: bigint,
    deadline: bigint,
  ) => {
    const fresh = await readCurveTradeIntent({
      publicClient: publicClient!,
      curve: safeCurveAddress,
      abi: curveAbi,
      engineVersion,
      chainId: configuration.chainId,
      account: accountAddress,
      side: quoteSide,
      quantity,
      toleranceBps,
      deadline,
    });
    setTaxQuote(fresh.taxQuote);
    setQuoteInputUsed(fresh.inputUsed);
    return fresh;
  };
  const showContractQuote = (
    quoteSide: "buy" | "sell",
    quantity: bigint,
    fresh: { output: bigint; fee: bigint; protocolFee: bigint },
  ) => {
    setQuote({
      output: fresh.output,
      fee: fresh.fee,
      protocolFee: fresh.protocolFee,
      source: "contract",
    });
    setReviewDetails({
      side: quoteSide,
      inputUnits: quantity,
      output: fresh.output,
      fee: fresh.fee,
      protocolFee: fresh.protocolFee,
    });
  };
  const readPhase = () =>
    publicClient!.readContract({
      address: safeCurveAddress,
      abi: curveAbi,
      functionName: "phase",
    });
  const prepareTradeReview = async () => {
    if (!trade.walletReady || !inputUnits || slippageBps === null || trade.unresolved) return;
    try {
      await trade.assertWalletForReview();
      if (Number(await readPhase()) !== 0) throw new Error("WrongPhase");
      const deadline = transactionDeadline(currentUnixSeconds(), DEFAULT_TTL);
      const fresh = await readCurveIntent(
        account.address as Address,
        side,
        inputUnits,
        slippageBps,
        deadline,
      );
      showContractQuote(side, inputUnits, fresh);
      trade.openReview(fresh.intent);
      setQuoteError(null);
    } catch (cause) {
      trade.closeReview(false);
      setQuoteError(decodeTransactionError(cause).message);
    }
  };
  const execute = async () => {
    if (graduated) return;
    const resumingApproval = side === "sell" && approvalHash !== undefined;
    if (
      !publicClient ||
      !walletClient ||
      !account.address ||
      !inputUnits ||
      slippageBps === null ||
      !reviewIntent ||
      !reviewDetails ||
      !transaction.ready ||
      (reviewDetails.side === "sell" && !approvalTransaction.ready) ||
      (!resumingApproval && trade.unresolved) ||
      chainId !== configuration.chainId ||
      !readiness.selectedAccountVerified
    ) {
      trade.setReviewNotice(
        "Wallet, network, or review changed. Refresh the review before signing.",
      );
      return;
    }
    const reviewed = reviewIntent;
    const reviewedSide = reviewDetails.side;
    const reviewedInput = reviewDetails.inputUnits;
    const requote = async () => {
      const fresh = await readCurveIntent(
        reviewed.account as Address,
        reviewedSide,
        reviewedInput,
        slippageBps,
        reviewed.deadline!,
      );
      return {
        intent: fresh.intent,
        replace: () => showContractQuote(reviewedSide, reviewedInput, fresh),
      };
    };
    await trade.runExecution(async (run) => {
      // A failure before anything is submitted keeps reporting the last known hash.
      run.setFallbackHash(state.hash as Address | undefined);
      // Independent reads go out together; the checks keep their original order.
      const [phase, tokenBalance, nativeBalance] = await Promise.all([
        readPhase(),
        publicClient.readContract({
          address: safeTokenAddress,
          abi: browserAbis.token,
          functionName: "balanceOf",
          args: [reviewed.account as Address],
        }),
        reviewedSide === "buy"
          ? publicClient.getBalance({ address: reviewed.account as Address })
          : Promise.resolve(null),
      ]);
      if (Number(phase) !== 0) throw new Error("WrongPhase");
      if (reviewedSide === "sell" && tokenBalance < reviewedInput)
        throw new Error("ERC20InsufficientBalance");
      if (nativeBalance !== null && nativeBalance < reviewedInput)
        throw new Error("InsufficientETHBalance");
      await run.ensureWallet(reviewed);
      if (!run.unchanged(reviewed, await requote())) return;
      if (reviewedSide === "sell") {
        const allowance = await publicClient.readContract({
          address: safeTokenAddress,
          abi: browserAbis.token,
          functionName: "allowance",
          args: [reviewed.account as Address, safeCurveAddress],
        });
        if (allowance < reviewedInput) {
          await run.approve(reviewed, {
            address: safeTokenAddress,
            abi: browserAbis.token as Abi,
            functionName: "approve",
            args: [safeCurveAddress, reviewedInput],
          });
          run.approvalsDone(
            "The curve allowance is set for this exact sell. Review again to sign.",
          );
          return;
        }
      }
      await run.sign(reviewed, {
        abi: curveAbi,
        requote,
        beforeSimulate: async () => {
          if (Number(await readPhase()) !== 0) throw new Error("WrongPhase");
        },
      });
    });
  };
  const canTrade = Boolean(
    inputUnits &&
    inputUnits > 0n &&
    minimum !== null &&
    readiness.selectedAccountVerified &&
    chainId === configuration.chainId,
  );
  const canSubmit = canTrade && confirming && (side !== "sell" || approvalTransaction.ready);
  if (!tokenAddress || !curveAddress || (engineVersion !== 1 && engineVersion !== 2))
    return (
      <UnavailableState
        title="Trading unavailable"
        description="This token contract is not valid for the selected network."
      />
    );
  if (graduated)
    return (
      <GraduatedRoute
        token={token}
        tokenAddress={tokenAddress}
        curveAddress={curveAddress}
        engineVersion={engineVersion}
        graduated={graduated}
        reviewedV4={reviewedV4}
        v4PoolReady={v4PoolReady}
        reviewedRouter={reviewedRouter}
        reviewedWeth={reviewedWeth}
        claimRow={claimRow}
      />
    );
  return (
    <section className="workspace-panel transaction-panel" aria-labelledby="trade-title">
      <div className="panel-head">
        <div>
          <h2 id="trade-title">Trade {token.symbol}</h2>
        </div>
        <Badge tone={token.phase === "graduated" ? "success" : "accent"}>{token.phase}</Badge>
      </div>
      <WrongNetworkBanner write={trade} />
      <TradeSideTabs
        side={side}
        label="Trade side"
        disabled={executionBusy}
        onChange={(next) => trade.changeSide(next)}
      >
        <div className="transaction-form">
          <TradeAmountFields
            trade={trade}
            symbol={token.symbol}
            inputHint="Up to 18 decimal places."
            slippageHint="Minimum output is rounded down on-chain. At most 50%."
          />
          {quoteError ? (
            <p className="transaction-error" role="alert">
              {quoteError}
            </p>
          ) : null}
          {quote ? (
            <CurveQuoteSummary
              quote={quote}
              side={side}
              taxLine={taxLine}
              minimum={minimum}
              priceImpact={priceImpact}
            />
          ) : null}
        </div>
        <p className="transaction-risk">
          Non-custodial: your selected wallet signs directly. Transactions are irreversible and may
          lose value. Contract{" "}
          {curveExplorer ? (
            <a href={curveExplorer} target="_blank" rel="noreferrer" className="mono">
              {token.curve}
            </a>
          ) : (
            <span className="mono">{token.curve}</span>
          )}
          .
        </p>
        <CurveReviewStep
          trade={trade}
          token={token}
          reviewDetails={reviewDetails}
          curveExplorer={curveExplorer}
          canTrade={canTrade}
          canSubmit={canSubmit}
          onReview={() => void prepareTradeReview()}
          onSign={() => void execute()}
        />
      </TradeSideTabs>
      <TransactionProgress
        state={state}
        tone={
          state.status === "reverted" || state.status === "rejected-signature"
            ? "danger"
            : state.status === "indexing"
              ? "warning"
              : "success"
        }
      />
      <TransactionProgress
        state={approvalTransaction.state}
        tone="warning"
        prefix="Approval "
        refreshNotice={false}
      />
      {claimRow}
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
