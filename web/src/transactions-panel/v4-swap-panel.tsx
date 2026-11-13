"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useAccount, usePublicClient, useWalletClient } from "wagmi";
import { formatBaseUnits, parseDecimal } from "@/amounts";
import type { Abi } from "viem";
import { type V4Addresses } from "@/engine";
import { encodeV4ExactInSingle, planPermit2Sell, poolId, poolKey } from "@/v4-swap";
import { browserAbis } from "@/contracts/generated";
import { publicConfiguration } from "@/config/public";
import { addressExplorerUrl } from "@/wallet/explorer";
import { useWalletReadiness } from "@/wallet/readiness";
import {
  classifyTransactionFailure,
  decodeTransactionError,
  minimumOutput,
  parseRuntimeQuantity,
  parseSlippageBps,
  sameReviewedWriteIntent,
  transactionDeadline,
  type TransactionState,
  type ReviewedWriteIntent,
} from "@/transactions";
import { TradeUsdHint, UsdLine } from "@/market/usd-line";
import { Badge, Button, Input, UnavailableState } from "@/components/primitives";
import {
  DEFAULT_TTL,
  QUOTE_DEBOUNCE_MS,
  type Address,
  eth,
  safeAddress,
  statusLabel,
  hasUnresolvedSubmission,
  currentUnixSeconds,
  ErrorCopy,
  usePersistedTransactionState,
  TradeSideTabs,
  SlippageChips,
} from "./shared";

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
  const account = useAccount();
  const readiness = useWalletReadiness();
  const chainId = readiness.chainId;
  const publicClient = usePublicClient();
  const { data: walletClient } = useWalletClient();
  const configuration = publicConfiguration();
  const router = addresses.universalRouter;
  const routerExplorer = addressExplorerUrl(configuration, router);
  const key = useMemo(
    () => (tokenAddress ? poolKey(tokenAddress, addresses.hook) : null),
    [addresses.hook, tokenAddress],
  );
  const transaction = usePersistedTransactionState(
    "trade",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
  );
  const { state, setState } = transaction;
  const approvalTransaction = usePersistedTransactionState(
    "approval",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
  );
  const setApprovalTransactionState = approvalTransaction.setState;
  const approvalHash = approvalTransaction.ready
    ? (approvalTransaction.state.hash as Address | undefined)
    : undefined;
  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [input, setInput] = useState("");
  const [slippage, setSlippage] = useState("5");
  const [executionBusy, setExecutionBusy] = useState(false);
  const [output, setOutput] = useState<bigint | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [reviewIntent, setReviewIntent] = useState<ReviewedWriteIntent | null>(null);
  const [reviewNotice, setReviewNotice] = useState<string | null>(null);
  const [reviewInput, setReviewInput] = useState<bigint | null>(null);
  const lockRef = useRef(false);
  const inputUnits = useMemo(() => {
    try {
      return input.trim() ? parseDecimal(input) : null;
    } catch {
      return null;
    }
  }, [input]);
  const slippageBps = useMemo(() => parseSlippageBps(slippage), [slippage]);
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
      output: outputAmount,
    };
  };
  const walletStillReviewed = async (reviewed: ReviewedWriteIntent) => {
    const [accounts, latestChainId] = await Promise.all([
      walletClient!.getAddresses(),
      publicClient!.getChainId(),
    ]);
    return (
      latestChainId === reviewed.chainId &&
      accounts[0]?.toLowerCase() === reviewed.account.toLowerCase() &&
      account.address?.toLowerCase() === reviewed.account.toLowerCase() &&
      readiness.selectedAccountVerified
    );
  };
  const prepareSwapReview = async () => {
    if (
      !key ||
      !publicClient ||
      !walletClient ||
      !account.address ||
      !inputUnits ||
      slippageBps === null ||
      chainId !== configuration.chainId ||
      !readiness.selectedAccountVerified ||
      !transaction.ready ||
      (side === "sell" && !approvalTransaction.ready) ||
      hasUnresolvedSubmission(state)
    )
      return;
    try {
      const [walletAccounts, currentChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        currentChainId !== configuration.chainId ||
        walletAccounts[0]?.toLowerCase() !== account.address.toLowerCase()
      )
        throw new Error("Wallet or network changed. Review again after reconnecting.");
      const deadline = transactionDeadline(currentUnixSeconds(), DEFAULT_TTL);
      const fresh = await readSwapIntent(
        account.address as Address,
        side,
        inputUnits,
        slippageBps,
        deadline,
      );
      setOutput(fresh.output);
      setReviewInput(inputUnits);
      setReviewIntent(fresh.intent);
      setReviewNotice(null);
      setConfirming(true);
    } catch (cause) {
      setReviewIntent(null);
      setConfirming(false);
      setReviewNotice(decodeTransactionError(cause).message);
    }
  };
  const replaceSwapReview = (intent: ReviewedWriteIntent, refreshedOutput: bigint) => {
    setOutput(refreshedOutput);
    setReviewInput(inputUnits);
    setReviewIntent(intent);
    setReviewNotice(
      "The wallet write details changed. Review the updated values, then confirm again.",
    );
  };
  /** One exact allowance write: simulate, re-check the wallet, sign, wait for the receipt. */
  const approvalWrite = async (
    reviewed: ReviewedWriteIntent,
    write: { address: Address; abi: Abi; functionName: string; args: readonly unknown[] },
  ) => {
    setApprovalTransactionState({ status: "simulating" });
    await publicClient!.simulateContract({
      ...write,
      account: reviewed.account as Address,
    } as never);
    if (!(await walletStillReviewed(reviewed)))
      throw new Error("The selected wallet or network changed during simulation.");
    setApprovalTransactionState({ status: "awaiting-signature" });
    const hash = (await walletClient!.writeContract({
      ...write,
      account: reviewed.account as Address,
    } as never)) as Address;
    setApprovalTransactionState({ status: "submitted", hash });
    const receipt = await publicClient!.waitForTransactionReceipt({ hash });
    if (receipt.status !== "success") throw new Error("ReceiptReverted");
    setApprovalTransactionState({ status: "mined", hash });
    return hash;
  };
  const execute = async () => {
    if (
      lockRef.current ||
      !tokenAddress ||
      !key ||
      !publicClient ||
      !walletClient ||
      !account.address ||
      inputUnits === null ||
      slippageBps === null ||
      !reviewIntent ||
      reviewInput === null ||
      !transaction.ready ||
      (side === "sell" && !approvalTransaction.ready) ||
      hasUnresolvedSubmission(state) ||
      chainId !== configuration.chainId ||
      !readiness.selectedAccountVerified
    )
      return;
    lockRef.current = true;
    setExecutionBusy(true);
    const reviewed = reviewIntent;
    const reviewedSide = side;
    const reviewedInput = reviewInput;
    const priorState = state;
    let submittedHash: Address | undefined;
    let approving = false;
    let failurePhase: "preflight" | "simulation" | "write" | "receipt" = "preflight";
    try {
      if (!(await walletStillReviewed(reviewed))) {
        setReviewNotice("The selected wallet or network changed. Return to review before signing.");
        return;
      }
      const fresh = await readSwapIntent(
        reviewed.account as Address,
        reviewedSide,
        reviewedInput,
        slippageBps,
        reviewed.deadline!,
      );
      if (!sameReviewedWriteIntent(reviewed, fresh.intent)) {
        replaceSwapReview(fresh.intent, fresh.output);
        return;
      }
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
          approving = true;
          failurePhase = "write";
          const tokenWrite = (amount: bigint) => ({
            address: tokenAddress,
            abi: browserAbis.tokenV2 as Abi,
            functionName: "approve",
            args: [addresses.permit2, amount] as const,
          });
          for (const step of steps) {
            if (step === "reset-erc20")
              submittedHash = await approvalWrite(reviewed, tokenWrite(0n));
            if (step === "approve-erc20")
              submittedHash = await approvalWrite(reviewed, tokenWrite(reviewedInput));
            if (step === "approve-permit2")
              submittedHash = await approvalWrite(reviewed, {
                address: addresses.permit2,
                abi: browserAbis.permit2 as Abi,
                functionName: "approve",
                // Exact amount, expiring with the reviewed swap deadline (≤ the TTL).
                args: [tokenAddress, router, reviewedInput, Number(reviewed.deadline!)],
              });
          }
          setConfirming(false);
          setReviewNotice("Allowances are set for this exact sell. Review the swap again to sign.");
          return;
        }
      }
      setState({ status: "simulating" });
      failurePhase = "simulation";
      await publicClient.simulateContract({
        address: router,
        abi: browserAbis.universalRouter,
        functionName: "execute",
        account: reviewed.account as Address,
        args: reviewed.args as never,
        value: reviewed.value,
      } as never);
      const finalQuote = await readSwapIntent(
        reviewed.account as Address,
        reviewedSide,
        reviewedInput,
        slippageBps,
        reviewed.deadline!,
      );
      if (!(await walletStillReviewed(reviewed))) {
        setReviewNotice("The selected wallet or network changed during simulation. Review again.");
        setState(priorState);
        return;
      }
      if (!sameReviewedWriteIntent(reviewed, finalQuote.intent)) {
        replaceSwapReview(finalQuote.intent, finalQuote.output);
        setState(priorState);
        return;
      }
      setState({ status: "awaiting-signature" });
      failurePhase = "write";
      const hash = await walletClient.writeContract({
        address: router,
        abi: browserAbis.universalRouter,
        functionName: "execute",
        account: reviewed.account as Address,
        args: reviewed.args as never,
        value: reviewed.value,
      } as never);
      submittedHash = hash as Address;
      setConfirming(false);
      setReviewIntent(null);
      setState({ status: "submitted", hash: submittedHash });
      failurePhase = "receipt";
      const receipt = await publicClient.waitForTransactionReceipt({ hash });
      if (receipt.status !== "success") throw new Error("ReceiptReverted");
      setState({ status: "indexing", hash: submittedHash });
    } catch (cause) {
      const failure = {
        status: classifyTransactionFailure(cause, failurePhase),
        hash: submittedHash,
        error: decodeTransactionError(cause).message,
      } satisfies TransactionState;
      if (approving) setApprovalTransactionState(failure);
      else setState(failure);
    } finally {
      lockRef.current = false;
      setExecutionBusy(false);
    }
  };
  if (!tokenAddress || !key)
    return (
      <UnavailableState
        title="Pool unavailable"
        description="The token address is invalid for the reviewed V4 pool."
      />
    );
  const ready = Boolean(
    inputUnits &&
    inputUnits > 0n &&
    minimum !== null &&
    readiness.selectedAccountVerified &&
    chainId === configuration.chainId &&
    transaction.ready &&
    (side !== "sell" || approvalTransaction.ready) &&
    !hasUnresolvedSubmission(state),
  );
  return (
    <div className="graduated-swap">
      {readiness.address && chainId !== configuration.chainId ? (
        <div className="transaction-warning" role="alert">
          Wrong network. Switch to {configuration.deployment?.name} before signing.
          <Button size="sm" onClick={() => void readiness.switchNetwork()}>
            Switch network
          </Button>
        </div>
      ) : null}
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
          setSide(next);
          setInput("");
          setOutput(null);
          setConfirming(false);
          setReviewIntent(null);
          setReviewNotice(null);
        }}
      >
        <div className="transaction-form">
          <Input
            label={side === "buy" ? "ETH input" : `${token.symbol} input`}
            value={input}
            disabled={executionBusy}
            onChange={(event) => {
              setInput(event.target.value);
              setConfirming(false);
              setReviewIntent(null);
              setReviewNotice(null);
            }}
            inputMode="decimal"
          />
          {side === "buy" ? <TradeUsdHint ethInput={input} /> : <TradeUsdHint />}
          <Input
            label="Slippage tolerance (%)"
            value={slippage}
            disabled={executionBusy}
            onChange={(event) => {
              setSlippage(event.target.value);
              setConfirming(false);
              setReviewIntent(null);
            }}
            inputMode="decimal"
            hint="At most 50%."
          />
          <SlippageChips
            value={slippage}
            disabled={executionBusy}
            onPick={(value) => {
              setSlippage(value);
              setConfirming(false);
              setReviewIntent(null);
            }}
          />
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
            {reviewNotice ? (
              <p className="transaction-warning" role="status">
                {reviewNotice}
              </p>
            ) : null}
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
            <div className="transaction-actions">
              <Button
                variant="quiet"
                disabled={executionBusy}
                onClick={() => {
                  setConfirming(false);
                  setReviewIntent(null);
                  setReviewNotice(null);
                }}
              >
                Back
              </Button>
              <Button
                variant="primary"
                loading={state.status === "simulating" || state.status === "awaiting-signature"}
                disabled={executionBusy || !ready || !reviewIntent || !transaction.ready}
                onClick={() => void execute()}
              >
                Sign {side}
              </Button>
            </div>
          </div>
        ) : (
          <>
            {/* A notice set while the confirmation closes (allowances mined, a failed review)
                must stay visible next to the action it explains. */}
            {reviewNotice ? (
              <p className="transaction-warning" role="status">
                {reviewNotice}
              </p>
            ) : null}
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
      {state.status !== "disconnected" ? (
        <div className="transaction-progress" role="status">
          <Badge tone={state.status === "reverted" ? "danger" : "warning"}>
            {statusLabel(state.status)}
          </Badge>
          {state.hash ? <span className="mono">{state.hash}</span> : null}
          {state.error ? <ErrorCopy error={new Error(state.error)} /> : null}
          {state.canonicalRefreshUnavailable ? (
            <span className="transaction-error" role="alert">
              Canonical status refresh unavailable. Showing the last known status.
            </span>
          ) : null}
        </div>
      ) : null}
      {approvalTransaction.state.status !== "disconnected" ? (
        <div className="transaction-progress" role="status">
          <Badge tone="warning">Approval {statusLabel(approvalTransaction.state.status)}</Badge>
          {approvalTransaction.state.hash ? (
            <span className="mono">{approvalTransaction.state.hash}</span>
          ) : null}
          {approvalTransaction.state.error ? (
            <ErrorCopy error={new Error(approvalTransaction.state.error)} />
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
