"use client";

import { useMemo, useRef, useState } from "react";
import type { Abi } from "viem";
import { useAccount, usePublicClient, useWalletClient } from "wagmi";
import { parseDecimal } from "@/amounts";
import { publicConfiguration } from "@/config/public";
import { TradeUsdHint } from "@/market/usd-line";
import { Button, Input } from "@/components/primitives";
import { useWalletReadiness } from "@/wallet/readiness";
import {
  classifyTransactionFailure,
  decodeTransactionError,
  parseSlippageBps,
  sameReviewedWriteIntent,
  type ReviewedWriteIntent,
  type TransactionState,
} from "@/transactions";
import {
  type Address,
  hasUnresolvedSubmission,
  SlippageChips,
  usePersistedTransactionState,
} from "./shared";

type FailurePhase = "preflight" | "simulation" | "write" | "receipt";

/** A wallet or network switch since review: shown as a review notice, never as a failure. */
class ReviewChangedError extends Error {}

const WALLET_CHANGED = "The selected wallet or network changed. Return to review before signing.";
const WALLET_CHANGED_DURING_SIMULATION =
  "The selected wallet or network changed during simulation. Review again.";
const WRITE_CHANGED =
  "The wallet write details changed. Review the updated values, then confirm again.";

/** A fresh on-chain read of the reviewed write, and how the panel shows it if it changed. */
export type RequotedIntent = { intent: ReviewedWriteIntent; replace: () => void };

export type ExactContractWrite = {
  address: Address;
  abi: Abi;
  functionName: string;
  args: readonly unknown[];
};

/**
 * The steps every reviewed trade shares. Each one re-checks the selected wallet and network
 * immediately before a signature, and stops on a changed write instead of signing it.
 */
export type ReviewedExecution = {
  /** Throws a review notice when the wallet, account or chain no longer match the review. */
  ensureWallet: (reviewed: ReviewedWriteIntent, notice?: string) => Promise<void>;
  /** False (after showing the new values) when a fresh read no longer matches the review. */
  unchanged: (reviewed: ReviewedWriteIntent, fresh: RequotedIntent) => boolean;
  /** One exact allowance write: simulate, re-check the wallet, sign, wait for the receipt. */
  approve: (reviewed: ReviewedWriteIntent, write: ExactContractWrite) => Promise<Address>;
  /** Closes the review after allowances mined; the user signs the trade on a fresh review. */
  approvalsDone: (notice: string) => void;
  /** Simulates, re-quotes, re-checks the wallet, signs the reviewed write and waits for it. */
  sign: (
    reviewed: ReviewedWriteIntent,
    options: {
      abi: Abi;
      requote: () => Promise<RequotedIntent>;
      beforeSimulate?: () => Promise<void>;
    },
  ) => Promise<void>;
  /** The hash a failure is reported against when nothing was submitted in this run. */
  setFallbackHash: (hash: Address | undefined) => void;
};

/**
 * Shared state and the signing pipeline of the curve, V2 router and V4 router trade panels.
 * The panels keep what differs: the quote source, the intent they build, balance and
 * allowance preflight, and the confirmation details.
 */
export function useReviewedTrade(tokenAddress: Address | null, enabled = true) {
  const account = useAccount();
  const readiness = useWalletReadiness();
  const publicClient = usePublicClient();
  const { data: walletClient } = useWalletClient();
  const configuration = publicConfiguration();
  const chainId = readiness.chainId;
  const transaction = usePersistedTransactionState(
    "trade",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
    enabled,
  );
  const approvalTransaction = usePersistedTransactionState(
    "approval",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
    enabled,
  );
  const { state, setState } = transaction;
  const setApprovalState = approvalTransaction.setState;
  const approvalHash = approvalTransaction.ready
    ? (approvalTransaction.state.hash as Address | undefined)
    : undefined;
  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [input, setInput] = useState("");
  const [slippage, setSlippage] = useState("5");
  const [executionBusy, setExecutionBusy] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [reviewIntent, setReviewIntent] = useState<ReviewedWriteIntent | null>(null);
  const [reviewNotice, setReviewNotice] = useState<string | null>(null);
  const lockRef = useRef(false);
  const inputUnits = useMemo(() => {
    try {
      return input.trim() ? parseDecimal(input) : null;
    } catch {
      return null;
    }
  }, [input]);
  const slippageBps = useMemo(() => parseSlippageBps(slippage), [slippage]);

  const closeReview = (clearNotice = true) => {
    setConfirming(false);
    setReviewIntent(null);
    if (clearNotice) setReviewNotice(null);
  };
  const openReview = (intent: ReviewedWriteIntent) => {
    setReviewIntent(intent);
    setReviewNotice(null);
    setConfirming(true);
  };
  const changeSide = (next: "buy" | "sell") => {
    setSide(next);
    setInput("");
    closeReview();
  };
  const changeInput = (value: string) => {
    setInput(value);
    closeReview();
  };
  const changeSlippage = (value: string) => {
    setSlippage(value);
    closeReview(false);
  };

  /** Everything a review or a signature needs from the wallet and the persisted state. */
  const walletReady = Boolean(
    publicClient &&
    walletClient &&
    account.address &&
    chainId === configuration.chainId &&
    readiness.selectedAccountVerified &&
    transaction.ready &&
    (side !== "sell" || approvalTransaction.ready),
  );

  /** Throws when the wallet's selected account or chain moved since the panel rendered. */
  const assertWalletForReview = async () => {
    const [walletAccounts, currentChainId] = await Promise.all([
      walletClient!.getAddresses(),
      publicClient!.getChainId(),
    ]);
    if (
      currentChainId !== configuration.chainId ||
      walletAccounts[0]?.toLowerCase() !== account.address?.toLowerCase()
    )
      throw new Error("Wallet or network changed. Review again after reconnecting.");
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

  /** Runs one locked execution; a thrown error becomes the trade's (or approval's) failure. */
  const runExecution = async (body: (execution: ReviewedExecution) => Promise<void>) => {
    if (lockRef.current || !publicClient || !walletClient) return;
    lockRef.current = true;
    setExecutionBusy(true);
    const client = publicClient;
    const wallet = walletClient;
    const priorState = state;
    let phase: FailurePhase = "preflight";
    let submittedHash: Address | undefined;
    let fallbackHash: Address | undefined;
    let approving = false;
    const ensureWallet = async (reviewed: ReviewedWriteIntent, notice = WALLET_CHANGED) => {
      if (!(await walletStillReviewed(reviewed))) throw new ReviewChangedError(notice);
    };
    const execution: ReviewedExecution = {
      ensureWallet,
      setFallbackHash: (hash) => {
        fallbackHash = hash;
      },
      unchanged: (reviewed, fresh) => {
        if (sameReviewedWriteIntent(reviewed, fresh.intent)) return true;
        fresh.replace();
        setReviewIntent(fresh.intent);
        setReviewNotice(WRITE_CHANGED);
        setState(priorState);
        return false;
      },
      approve: async (reviewed, write) => {
        approving = true;
        const priorApproval = approvalTransaction.state;
        const request = { ...write, account: reviewed.account as Address } as never;
        phase = "simulation";
        setApprovalState({ status: "simulating" });
        await client.simulateContract(request);
        if (!(await walletStillReviewed(reviewed))) {
          setApprovalState(priorApproval);
          throw new ReviewChangedError(WALLET_CHANGED_DURING_SIMULATION);
        }
        phase = "write";
        setApprovalState({ status: "awaiting-signature" });
        const hash = (await wallet.writeContract(request)) as Address;
        submittedHash = hash;
        setApprovalState({ status: "submitted", hash });
        phase = "receipt";
        const receipt = await client.waitForTransactionReceipt({ hash });
        if (receipt.status !== "success") throw new Error("ReceiptReverted");
        setApprovalState({ status: "mined", hash });
        return hash;
      },
      approvalsDone: (notice) => {
        setConfirming(false);
        setReviewNotice(notice);
      },
      sign: async (reviewed, { abi, requote, beforeSimulate }) => {
        approving = false;
        setState({ status: "simulating" });
        await beforeSimulate?.();
        const request = {
          address: reviewed.target as Address,
          abi,
          functionName: reviewed.functionName,
          account: reviewed.account as Address,
          args: reviewed.args,
          value: reviewed.value,
        } as never;
        phase = "simulation";
        await client.simulateContract(request);
        const fresh = await requote();
        if (!execution.unchanged(reviewed, fresh)) return;
        if (!(await walletStillReviewed(reviewed))) {
          setState(priorState);
          throw new ReviewChangedError(WALLET_CHANGED_DURING_SIMULATION);
        }
        setState({ status: "awaiting-signature" });
        phase = "write";
        const hash = (await wallet.writeContract(request)) as Address;
        submittedHash = hash;
        closeReview(false);
        setState({ status: "submitted", hash });
        phase = "receipt";
        const receipt = await client.waitForTransactionReceipt({ hash });
        if (receipt.status !== "success") throw new Error("ReceiptReverted");
        setState({ status: "indexing", hash });
      },
    };
    try {
      await body(execution);
    } catch (cause) {
      if (cause instanceof ReviewChangedError) {
        setReviewNotice(cause.message);
        return;
      }
      const failure = {
        status: classifyTransactionFailure(cause, phase),
        hash: submittedHash ?? fallbackHash,
        error: decodeTransactionError(cause).message,
      } satisfies TransactionState;
      if (approving) setApprovalState(failure);
      else setState(failure);
    } finally {
      lockRef.current = false;
      setExecutionBusy(false);
    }
  };

  return {
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
    input,
    slippage,
    inputUnits,
    slippageBps,
    executionBusy,
    confirming,
    reviewIntent,
    reviewNotice,
    setReviewNotice,
    setReviewIntent,
    walletReady,
    unresolved: hasUnresolvedSubmission(state),
    changeSide,
    changeInput,
    changeSlippage,
    openReview,
    closeReview,
    assertWalletForReview,
    runExecution,
  };
}

export type ReviewedTrade = ReturnType<typeof useReviewedTrade>;

export function WrongNetworkBanner({ trade }: { trade: ReviewedTrade }) {
  const { readiness, chainId, configuration } = trade;
  if (!readiness.address || chainId === configuration.chainId) return null;
  return (
    <div className="transaction-warning" role="alert">
      Wrong network. Switch to {configuration.deployment?.name ?? "the reviewed network"} before
      signing.
      <Button size="sm" onClick={() => void readiness.switchNetwork()}>
        Switch network
      </Button>
    </div>
  );
}

/** The amount, USD hint, slippage field and presets every trade form starts with. */
export function TradeAmountFields({
  trade,
  symbol,
  inputHint,
  slippageHint = "At most 50%.",
}: {
  trade: ReviewedTrade;
  symbol: string;
  inputHint?: string;
  slippageHint?: string;
}) {
  const { side, input, slippage, executionBusy } = trade;
  return (
    <>
      <Input
        label={side === "buy" ? "ETH input" : `${symbol} input`}
        value={input}
        disabled={executionBusy}
        onChange={(event) => trade.changeInput(event.target.value)}
        inputMode="decimal"
        placeholder={inputHint ? "0.00" : undefined}
        hint={inputHint}
      />
      {side === "buy" ? <TradeUsdHint ethInput={input} /> : <TradeUsdHint />}
      <Input
        label="Slippage tolerance (%)"
        value={slippage}
        disabled={executionBusy}
        onChange={(event) => trade.changeSlippage(event.target.value)}
        inputMode="decimal"
        hint={slippageHint}
      />
      <SlippageChips
        value={slippage}
        disabled={executionBusy}
        onPick={(value) => trade.changeSlippage(value)}
      />
    </>
  );
}

export function ReviewNotice({ notice }: { notice: string | null }) {
  return notice ? (
    <p className="transaction-warning" role="status">
      {notice}
    </p>
  ) : null;
}

/** Back and sign buttons under a confirmation. */
export function ReviewActions({
  trade,
  canSign,
  onSign,
}: {
  trade: ReviewedTrade;
  canSign: boolean;
  onSign: () => void;
}) {
  const { executionBusy, state, reviewIntent, transaction, side } = trade;
  return (
    <div className="transaction-actions">
      <Button variant="quiet" disabled={executionBusy} onClick={() => trade.closeReview()}>
        Back
      </Button>
      <Button
        variant="primary"
        loading={state.status === "simulating" || state.status === "awaiting-signature"}
        disabled={executionBusy || !canSign || !reviewIntent || !transaction.ready}
        onClick={onSign}
      >
        Sign {side}
      </Button>
    </div>
  );
}
