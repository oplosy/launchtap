"use client";

import { useEffect, useState } from "react";
import type { Abi } from "viem";
import { formatBaseUnits } from "@/amounts";
import { browserAbis } from "@/contracts/generated";
import { addressExplorerUrl } from "@/wallet/explorer";
import {
  decodeTransactionError,
  minimumOutput,
  parseQuoteQuantity,
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

export function GraduatedSwapPanel({
  token,
  router,
  weth,
}: {
  token: { address: string; name: string; symbol: string };
  router: Address;
  weth: Address;
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
  const routerExplorer = addressExplorerUrl(configuration, router);
  const [output, setOutput] = useState<bigint | null>(null);
  const [reviewInput, setReviewInput] = useState<bigint | null>(null);
  useEffect(() => {
    let cancelled = false;
    if (!tokenAddress || !inputUnits || inputUnits <= 0n || !publicClient) return;
    const timer = setTimeout(() => {
      void publicClient
        .readContract({
          address: router,
          abi: browserAbis.router,
          functionName: "getAmountsOut",
          args: [inputUnits, side === "buy" ? [weth, tokenAddress] : [tokenAddress, weth]],
        } as never)
        .then((amounts) => {
          if (!cancelled)
            setOutput(
              parseQuoteQuantity(String((amounts as readonly unknown[]).at(-1)), "router output"),
            );
        })
        .catch(() => {
          if (!cancelled) setOutput(null);
        });
    }, QUOTE_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [inputUnits, publicClient, router, side, tokenAddress, weth]);
  const minimum =
    output !== null && slippageBps !== null ? minimumOutput(output, slippageBps) : null;
  const readRouterIntent = async (
    accountAddress: Address,
    quoteSide: "buy" | "sell",
    quantity: bigint,
    toleranceBps: bigint,
    deadline: bigint,
  ) => {
    const intentChainId = configuration.chainId;
    if (intentChainId === null) throw new Error("ChainUnavailable");
    const path =
      quoteSide === "buy" ? ([weth, tokenAddress!] as const) : ([tokenAddress!, weth] as const);
    const amounts = await publicClient!.readContract({
      address: router,
      abi: browserAbis.router,
      functionName: "getAmountsOut",
      args: [quantity, path],
    } as never);
    const outputAmount = parseQuoteQuantity(
      String((amounts as readonly unknown[]).at(-1)),
      "router output",
    );
    const minimumAmount = minimumOutput(outputAmount, toleranceBps);
    const functionName = quoteSide === "buy" ? "swapExactETHForTokens" : "swapExactTokensForETH";
    const args =
      quoteSide === "buy"
        ? ([minimumAmount, path, accountAddress, deadline] as const)
        : ([quantity, minimumAmount, path, accountAddress, deadline] as const);
    return {
      intent: {
        account: accountAddress,
        target: router,
        value: quoteSide === "buy" ? quantity : 0n,
        args,
        deadline,
        minimumOutput: minimumAmount,
        chainId: intentChainId,
        functionName,
      } satisfies ReviewedWriteIntent,
      replace: () => {
        setOutput(outputAmount);
        setReviewInput(quantity);
      },
    };
  };
  const prepareRouterReview = async () => {
    if (
      !tokenAddress ||
      !trade.walletReady ||
      !inputUnits ||
      slippageBps === null ||
      trade.unresolved
    )
      return;
    try {
      await trade.assertWalletForReview();
      const deadline = transactionDeadline(currentUnixSeconds(), DEFAULT_TTL);
      const fresh = await readRouterIntent(
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
      readRouterIntent(
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
        const [balance, allowance] = await Promise.all([
          publicClient.readContract({
            address: tokenAddress,
            abi: browserAbis.token,
            functionName: "balanceOf",
            args: [reviewed.account as Address],
          }),
          publicClient.readContract({
            address: tokenAddress,
            abi: browserAbis.token,
            functionName: "allowance",
            args: [reviewed.account as Address, router],
          }),
        ]);
        if (balance < reviewedInput) throw new Error("ERC20InsufficientBalance");
        if (allowance < reviewedInput) {
          await run.approve(reviewed, {
            address: tokenAddress,
            abi: browserAbis.token as Abi,
            functionName: "approve",
            args: [router, reviewedInput],
          });
          run.approvalsDone(
            "The router allowance is set for this exact sell. Review again to sign.",
          );
          return;
        }
      }
      await run.sign(reviewed, { abi: browserAbis.router as Abi, requote });
    });
  };
  if (!tokenAddress)
    return (
      <UnavailableState
        title="Router unavailable"
        description="The token address is invalid for the reviewed router."
      />
    );
  const ready = Boolean(
    inputUnits && inputUnits > 0n && minimum !== null && trade.walletReady && !trade.unresolved,
  );
  return (
    <div className="graduated-swap">
      <WrongNetworkBanner write={trade} />
      <p className="transaction-risk">
        Reviewed router only:{" "}
        {routerExplorer ? (
          <a href={routerExplorer} target="_blank" rel="noreferrer" className="mono">
            {router}
          </a>
        ) : (
          <span className="mono">{router}</span>
        )}
        . Approval, if needed, is limited to this router and this sell amount.
      </p>
      <TradeSideTabs
        side={side}
        label="Graduated trade side"
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
                Router output <strong className="mono">{formatBaseUnits(output, 18, 6)}</strong>
                {side === "sell" ? <UsdLine wei={output} /> : null}
              </span>
              <span>
                Minimum output{" "}
                <strong className="mono">
                  {minimum === null ? "—" : formatBaseUnits(minimum, 18, 6)}
                </strong>
                {side === "sell" && minimum !== null ? <UsdLine wei={minimum} /> : null}
              </span>
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
              <dd className="mono">
                {routerExplorer ? (
                  <a href={routerExplorer} target="_blank" rel="noreferrer">
                    {router}
                  </a>
                ) : (
                  router
                )}
              </dd>
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
              <dt>Fee</dt>
              <dd className="mono">
                0 ETH router fee (Uniswap V2 has no protocol fee). Pool price impact is reflected in
                the quoted output.
              </dd>
              <dt>Deadline</dt>
              <dd className="mono">
                {reviewIntent?.deadline?.toString() ?? "Unavailable"} (Unix seconds)
              </dd>
              {side === "sell" ? (
                <>
                  <dt>Approval spender</dt>
                  <dd className="mono">
                    {routerExplorer ? (
                      <a href={routerExplorer} target="_blank" rel="noreferrer">
                        {router}
                      </a>
                    ) : (
                      router
                    )}{" "}
                    (exact sell amount only)
                  </dd>
                  <dt>Approval call</dt>
                  <dd className="mono">
                    approve({router}, {reviewInput?.toString() ?? "Unavailable"}); target{" "}
                    {tokenAddress}; 0 ETH
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
            <ReviewNotice notice={reviewNotice} />
            <Button
              variant="primary"
              size="lg"
              disabled={executionBusy || !ready}
              onClick={() => void prepareRouterReview()}
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
