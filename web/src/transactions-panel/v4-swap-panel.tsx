"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { formatBaseUnits } from "@/amounts";
import type { Abi } from "viem";
import { type V4Addresses } from "@/engine";
import { encodeV4ExactInSingle, planPermit2Sell, poolId, poolKey } from "@/v4-swap";
import { browserAbis } from "@/contracts/generated";
import { addressExplorerUrl } from "@/wallet/explorer";
import {
  decodeTransactionError,
  minimumOutput,
  parseRuntimeQuantity,
  transactionDeadline,
  type ReviewedWriteIntent,
} from "@/transactions";
import { UsdLine } from "@/market/usd-line";
import { Button, UnavailableState } from "@/components/primitives";
import {
  DEFAULT_TTL,
  QUOTE_DEBOUNCE_MS,
  type Address,
  eth,
  safeAddress,
  currentUnixSeconds,
  TradeSideTabs,
  TransactionProgress,
} from "./shared";
import {
  ReviewActions,
  ReviewNotice,
  TradeAmountFields,
  useReviewedTrade,
  WrongNetworkBanner,
} from "./reviewed-trade";

/**
 * Graduated engine-2 trading through the reviewed Universal Router (Plan v2-C Tasks 4 and 5).
 * The pool key is rebuilt from spec constants and checked against the API's pool id before
 * this panel renders; quotes come from the reviewed V4Quoter immediately before review and
 * again before signing; sells go through exact ERC-20 → Permit2 → router allowances (D3).
 */
export function V4SwapPanel({
  token,
  addresses,
}: {
  token: { address: string; name: string; symbol: string };
  addresses: V4Addresses;
}) {
  const tokenAddress = safeAddress(token.address);
  const trade = useReviewedTrade(tokenAddress);
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
  const key = useMemo(
    () => (tokenAddress ? poolKey(tokenAddress, addresses.hook) : null),
    [addresses.hook, tokenAddress],
  );
  const [output, setOutput] = useState<bigint | null>(null);
  const [reviewInput, setReviewInput] = useState<bigint | null>(null);
  const quoteExactIn = useCallback(
    async (zeroForOne: boolean, amountIn: bigint) => {
      const { result } = await publicClient!.simulateContract({
        address: addresses.v4Quoter,
        abi: browserAbis.v4Quoter,
        functionName: "quoteExactInputSingle",
        args: [{ poolKey: key!, zeroForOne, exactAmount: amountIn, hookData: "0x" }],
      } as never);
      return parseRuntimeQuantity((result as readonly unknown[])[0], "V4 quote output");
    },
    [addresses.v4Quoter, key, publicClient],
  );
  useEffect(() => {
    let cancelled = false;
    if (!key || !inputUnits || inputUnits <= 0n || !publicClient) return;
    const timer = setTimeout(() => {
      void quoteExactIn(side === "buy", inputUnits)
        .then((amount) => {
          if (!cancelled) setOutput(amount);
        })
        .catch(() => {
          if (!cancelled) setOutput(null);
        });
    }, QUOTE_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [inputUnits, key, publicClient, quoteExactIn, side]);
  const minimum =
    output !== null && slippageBps !== null ? minimumOutput(output, slippageBps) : null;
  const readSwapIntent = async (
    accountAddress: Address,
    quoteSide: "buy" | "sell",
    quantity: bigint,
    toleranceBps: bigint,
    deadline: bigint,
  ) => {
    const intentChainId = configuration.chainId;
    if (intentChainId === null || !key) throw new Error("ChainUnavailable");
    const outputAmount = await quoteExactIn(quoteSide === "buy", quantity);
    const minimumAmount = minimumOutput(outputAmount, toleranceBps);
    const encoded = encodeV4ExactInSingle({
      key,
      zeroForOne: quoteSide === "buy",
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
        setOutput(outputAmount);
        setReviewInput(quantity);
      },
    };
  };
  const prepareSwapReview = async () => {
    if (!key || !trade.walletReady || !inputUnits || slippageBps === null || trade.unresolved)
      return;
    try {
      await trade.assertWalletForReview();
      const deadline = transactionDeadline(currentUnixSeconds(), DEFAULT_TTL);
      const fresh = await readSwapIntent(
        trade.account.address as Address,
        side,
        inputUnits,
        slippageBps,
        deadline,
      );
      fresh.replace();
      trade.openReview(fresh.intent);
    } catch (cause) {
      trade.closeReview(false);
      trade.setReviewNotice(decodeTransactionError(cause).message);
    }
  };
  const execute = async () => {
    if (
      !tokenAddress ||
      !key ||
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
      readSwapIntent(
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
        const [balance, erc20Allowance, permit2Allowance] = await Promise.all([
          publicClient.readContract({
            address: tokenAddress,
            abi: browserAbis.tokenV2,
            functionName: "balanceOf",
            args: [reviewed.account as Address],
          }),
          publicClient.readContract({
            address: tokenAddress,
            abi: browserAbis.tokenV2,
            functionName: "allowance",
            args: [reviewed.account as Address, addresses.permit2],
          }),
          publicClient.readContract({
            address: addresses.permit2,
            abi: browserAbis.permit2,
            functionName: "allowance",
            args: [reviewed.account as Address, tokenAddress, router],
          }),
        ]);
        if ((balance as bigint) < reviewedInput) throw new Error("ERC20InsufficientBalance");
        const [permit2Amount, permit2Expiration] = permit2Allowance as readonly [
          bigint,
          number | bigint,
          number | bigint,
        ];
        const steps = planPermit2Sell({
          amount: reviewedInput,
          erc20Allowance: erc20Allowance as bigint,
          permit2: { amount: permit2Amount, expiration: BigInt(permit2Expiration) },
          nowSeconds: currentUnixSeconds(),
          ttlSeconds: DEFAULT_TTL,
        });
        if (steps.length > 1) {
          const tokenWrite = (amount: bigint) => ({
            address: tokenAddress,
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
                args: [tokenAddress, router, reviewedInput, Number(reviewed.deadline!)],
              });
          }
          run.approvalsDone(
            "Allowances are set for this exact sell. Review the swap again to sign.",
          );
          return;
        }
      }
      await run.sign(reviewed, { abi: browserAbis.universalRouter as Abi, requote });
    });
  };
  if (!tokenAddress || !key)
    return (
      <UnavailableState
        title="Pool unavailable"
        description="The token address is invalid for the reviewed V4 pool."
      />
    );
  const ready = Boolean(
    inputUnits && inputUnits > 0n && minimum !== null && trade.walletReady && !trade.unresolved,
  );
  return (
    <div className="graduated-swap">
      <WrongNetworkBanner write={trade} />
      <p className="transaction-risk">
        Reviewed Universal Router only:{" "}
        {routerExplorer ? (
          <a href={routerExplorer} target="_blank" rel="noreferrer" className="mono">
            {router}
          </a>
        ) : (
          <span className="mono">{router}</span>
        )}
        . Pool <span className="mono">{poolId(key)}</span> charges a 1% LP fee. Third-party
        liquidity in this pool dilutes the locked position&apos;s fee share. Sells approve Permit2
        and this router for the exact sell amount only.
      </p>
      <TradeSideTabs
        side={side}
        label="Pool trade side"
        disabled={executionBusy}
        onChange={(next) => {
          trade.changeSide(next);
          setOutput(null);
        }}
      >
        <div className="transaction-form">
          <TradeAmountFields trade={trade} symbol={token.symbol} />
          {output !== null ? (
            <div className="quote-summary">
              <span>
                Pool output <strong className="mono">{formatBaseUnits(output, 18, 6)}</strong>
                {side === "sell" ? <UsdLine wei={output} /> : null}
              </span>
              <span>
                Minimum output{" "}
                <strong className="mono">
                  {minimum === null ? "—" : formatBaseUnits(minimum, 18, 6)}
                </strong>
                {side === "sell" && minimum !== null ? <UsdLine wei={minimum} /> : null}
              </span>
              <small>LP fee 1% of the input, taken by the pool and included in this quote.</small>
            </div>
          ) : null}
        </div>
        {confirming ? (
          <div className="transaction-confirmation">
            <h3>Confirm {side}</h3>
            <ReviewNotice notice={reviewNotice} />
            <dl>
              <dt>Network</dt>
              <dd>{configuration.deployment?.name}</dd>
              <dt>Contract</dt>
              <dd className="mono">{router}</dd>
              <dt>Pool</dt>
              <dd className="mono">{poolId(key)}</dd>
              <dt>Input / minimum</dt>
              <dd className="mono">
                {reviewInput === null ? "—" : formatBaseUnits(reviewInput, 18, 6)} /{" "}
                {reviewIntent?.minimumOutput === undefined
                  ? "—"
                  : formatBaseUnits(reviewIntent.minimumOutput, 18, 6)}
              </dd>
              <dt>Wallet</dt>
              <dd className="mono">{reviewIntent?.account ?? "Unavailable"}</dd>
              <dt>Chain ID</dt>
              <dd className="mono">{reviewIntent?.chainId ?? "Unavailable"}</dd>
              <dt>Function</dt>
              <dd className="mono">execute(V4_SWAP: exact input single, settle all, take all)</dd>
              <dt>Native value</dt>
              <dd className="mono">
                {reviewIntent
                  ? `${eth(reviewIntent.value)} (${reviewIntent.value.toString()} wei)`
                  : "Unavailable"}
              </dd>
              <dt>Slippage</dt>
              <dd className="mono">{slippageBps === null ? "Invalid" : `${slippage}%`}</dd>
              <dt>Fee</dt>
              <dd className="mono">1% pool LP fee, included in the quoted output.</dd>
              <dt>Deadline</dt>
              <dd className="mono">
                {reviewIntent?.deadline?.toString() ?? "Unavailable"} (Unix seconds)
              </dd>
              {side === "sell" ? (
                <>
                  <dt>Allowances</dt>
                  <dd className="mono">
                    approve({addresses.permit2}, {reviewInput?.toString() ?? "—"}) on {tokenAddress}
                    ; Permit2.approve({tokenAddress}, {router}, {reviewInput?.toString() ?? "—"},{" "}
                    {reviewIntent?.deadline?.toString() ?? "—"}). Already exact allowances are
                    skipped.
                  </dd>
                </>
              ) : null}
            </dl>
            <ReviewActions
              write={trade}
              label={`Sign ${side}`}
              canSign={ready}
              onSign={() => void execute()}
            />
          </div>
        ) : (
          <>
            {/* A notice set while the confirmation closes (allowances mined, a failed review)
                must stay visible next to the action it explains. */}
            <ReviewNotice notice={reviewNotice} />
            <Button
              variant="primary"
              size="lg"
              disabled={executionBusy || !ready}
              onClick={() => void prepareSwapReview()}
            >
              {side === "sell" && approvalHash ? "Resume sell" : `Review ${side}`}
            </Button>
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
