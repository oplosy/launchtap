"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useAccount, usePublicClient, useWalletClient } from "wagmi";
import { formatBaseUnits, parseDecimal } from "@/amounts";
import { browserAbis } from "@/contracts/generated";
import { publicConfiguration } from "@/config/public";
import { addressExplorerUrl } from "@/wallet/explorer";
import { useWalletReadiness } from "@/wallet/readiness";
import {
  classifyTransactionFailure,
  decodeTransactionError,
  minimumOutput,
  parseSlippageBps,
  parseQuoteQuantity,
  sameReviewedWriteIntent,
  transactionDeadline,
  type TransactionState,
  type ReviewedWriteIntent,
} from "@/transactions";
import { TradeUsdHint, UsdLine } from "@/market/usd-line";
import { Button, Input, UnavailableState } from "@/components/primitives";
import {
  DEFAULT_TTL,
  QUOTE_DEBOUNCE_MS,
  type Address,
  eth,
  safeAddress,
  hasUnresolvedSubmission,
  currentUnixSeconds,
  usePersistedTransactionState,
  TradeSideTabs,
  SlippageChips,
  TransactionProgress,
} from "./shared";

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
  const account = useAccount();
  const readiness = useWalletReadiness();
  const chainId = readiness.chainId;
  const publicClient = usePublicClient();
  const { data: walletClient } = useWalletClient();
  const configuration = publicConfiguration();
  const routerExplorer = addressExplorerUrl(configuration, router);
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
  const slippageBps = useMemo(() => {
    return parseSlippageBps(slippage);
  }, [slippage]);
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
      output: outputAmount,
      path,
    };
  };
  const prepareRouterReview = async () => {
    if (
      !tokenAddress ||
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
      const fresh = await readRouterIntent(
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
  const replaceRouterReview = (intent: ReviewedWriteIntent, refreshedOutput: bigint) => {
    setOutput(refreshedOutput);
    setReviewInput(inputUnits);
    setReviewIntent(intent);
    setReviewNotice(
      "The wallet write details changed. Review the updated values, then confirm again.",
    );
  };
  const execute = async () => {
    if (
      lockRef.current ||
      !tokenAddress ||
      !publicClient ||
      !walletClient ||
      !account.address ||
      inputUnits === null ||
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
    let approvalWrite = false;
    let failurePhase: "preflight" | "simulation" | "write" | "receipt" = "preflight";
    try {
      const [walletAccounts, latestChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        latestChainId !== reviewed.chainId ||
        walletAccounts[0]?.toLowerCase() !== reviewed.account.toLowerCase() ||
        account.address.toLowerCase() !== reviewed.account.toLowerCase()
      ) {
        setReviewNotice("The selected wallet or network changed. Return to review before signing.");
        return;
      }
      const fresh = await readRouterIntent(
        reviewed.account as Address,
        reviewedSide,
        reviewedInput,
        slippageBps!,
        reviewed.deadline!,
      );
      if (!sameReviewedWriteIntent(reviewed, fresh.intent)) {
        replaceRouterReview(fresh.intent, fresh.output);
        return;
      }
      if (reviewedSide === "buy") {
        const balance = await publicClient.getBalance({ address: reviewed.account as Address });
        if (balance < reviewedInput) throw new Error("InsufficientETHBalance");
      }
      if (reviewedSide === "sell") {
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
          approvalWrite = true;
          const [approvalAccounts, approvalChainId] = await Promise.all([
            walletClient.getAddresses(),
            publicClient.getChainId(),
          ]);
          if (
            approvalChainId !== reviewed.chainId ||
            approvalAccounts[0]?.toLowerCase() !== reviewed.account.toLowerCase()
          ) {
            setReviewNotice(
              "The selected wallet or network changed. Return to review before signing.",
            );
            return;
          }
          const approvalArgs = [router, reviewedInput] as const;
          failurePhase = "simulation";
          setApprovalTransactionState({ status: "simulating" });
          await publicClient.simulateContract({
            address: tokenAddress,
            abi: browserAbis.token,
            functionName: "approve",
            account: reviewed.account as Address,
            args: approvalArgs,
          } as never);
          const [finalApprovalAccounts, finalApprovalChainId] = await Promise.all([
            walletClient.getAddresses(),
            publicClient.getChainId(),
          ]);
          if (
            finalApprovalChainId !== reviewed.chainId ||
            finalApprovalAccounts[0]?.toLowerCase() !== reviewed.account.toLowerCase()
          ) {
            setReviewNotice(
              "The selected wallet or network changed during simulation. Review again.",
            );
            return;
          }
          failurePhase = "write";
          setApprovalTransactionState({ status: "awaiting-signature" });
          const approvalTxHash = await walletClient.writeContract({
            address: tokenAddress,
            abi: browserAbis.token,
            functionName: "approve",
            account: reviewed.account as Address,
            args: approvalArgs,
          } as never);
          submittedHash = approvalTxHash as Address;
          setApprovalTransactionState({ status: "submitted", hash: submittedHash });
          failurePhase = "receipt";
          const approvalReceipt = await publicClient.waitForTransactionReceipt({
            hash: approvalTxHash,
          });
          if (approvalReceipt.status !== "success") throw new Error("ReceiptReverted");
          setApprovalTransactionState({ status: "mined", hash: submittedHash });
          setConfirming(false);
          return;
        }
      }
      setState({ status: "simulating" });
      failurePhase = "simulation";
      await publicClient.simulateContract({
        address: router,
        abi: browserAbis.router,
        functionName: reviewed.functionName,
        account: reviewed.account as Address,
        args: reviewed.args as never,
        value: reviewed.value,
      } as never);
      const finalQuote = await readRouterIntent(
        reviewed.account as Address,
        reviewedSide,
        reviewedInput,
        slippageBps!,
        reviewed.deadline!,
      );
      const [finalAccounts, finalChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        finalChainId !== reviewed.chainId ||
        finalAccounts[0]?.toLowerCase() !== reviewed.account.toLowerCase() ||
        !readiness.selectedAccountVerified
      ) {
        setReviewNotice("The selected wallet or network changed during simulation. Review again.");
        setState(priorState);
        return;
      }
      if (!sameReviewedWriteIntent(reviewed, finalQuote.intent)) {
        replaceRouterReview(finalQuote.intent, finalQuote.output);
        setState(priorState);
        return;
      }
      const [writeAccounts, writeChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        writeChainId !== reviewed.chainId ||
        writeAccounts[0]?.toLowerCase() !== reviewed.account.toLowerCase() ||
        !readiness.selectedAccountVerified
      ) {
        setReviewNotice("The selected wallet or network changed before signing. Review again.");
        setState(priorState);
        return;
      }
      setState({ status: "awaiting-signature" });
      failurePhase = "write";
      const hash = await walletClient.writeContract({
        address: router,
        abi: browserAbis.router,
        functionName: reviewed.functionName,
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
      const indexing = { status: "indexing" as const, hash: submittedHash };
      setState(indexing);
    } catch (cause) {
      const failure = {
        status: classifyTransactionFailure(cause, failurePhase),
        hash: submittedHash,
        error: decodeTransactionError(cause).message,
      } satisfies TransactionState;
      if (approvalWrite) setApprovalTransactionState(failure);
      else setState(failure);
    } finally {
      lockRef.current = false;
      setExecutionBusy(false);
    }
  };
  if (!tokenAddress)
    return (
      <UnavailableState
        title="Router unavailable"
        description="The token address is invalid for the reviewed router."
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
            {reviewNotice ? (
              <p className="transaction-warning" role="status">
                {reviewNotice}
              </p>
            ) : null}
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
          <Button
            variant="primary"
            size="lg"
            disabled={executionBusy || !ready}
            onClick={() => void prepareRouterReview()}
          >
            {side === "sell" && approvalHash ? "Resume sell" : `Review ${side}`}
          </Button>
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
