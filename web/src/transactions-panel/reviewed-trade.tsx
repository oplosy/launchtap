"use client";

import { useMemo, useRef, useState, type ReactNode } from "react";
import type { Abi, TransactionReceipt } from "viem";
import { useAccount, usePublicClient, useWalletClient } from "wagmi";
import { parseDecimal } from "@/amounts";
import { publicConfiguration } from "@/config/public";
import { TradeUsdHint } from "@/market/usd-line";
import { Button, Input } from "@/components/primitives";
import { useWalletReadiness } from "@/wallet/readiness";
import { WalletConnectButton } from "@/wallet/connection-context";
import type { PersistedTransactionAction } from "@/transaction-storage";
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
  DEFAULT_SLIPPAGE,
  hasUnresolvedSubmission,
  SlippageChips,
  usePersistedTransactionState,
} from "./shared";

type FailurePhase = "preflight" | "simulation" | "write" | "receipt";
type PersistedTransaction = ReturnType<typeof usePersistedTransactionState>;

/** A wallet or network switch since review: shown as a review notice, never as a failure. */
class ReviewChangedError extends Error {}

const WALLET_CHANGED = "The selected wallet or network changed. Return to review before signing.";
const WALLET_CHANGED_DURING_SIMULATION =
  "The selected wallet or network changed during simulation. Review again.";
const WRITE_CHANGED =
  "The wallet write details changed. Review the updated values, then confirm again.";

/** A fresh on-chain read of the reviewed write, and how the panel shows it if it changed. */
export type RequotedIntent = { intent: ReviewedWriteIntent; replace?: () => void };

export type ExactContractWrite = {
  address: Address;
  abi: Abi;
  functionName: string;
  args: readonly unknown[];
};

/**
 * The steps every reviewed write shares. Each one re-checks the selected wallet and network
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
  /**
   * Simulates the reviewed write, re-reads it (`requote` may throw to stop), re-checks the wallet,
   * signs exactly the reviewed target, function, arguments and value, and waits for the receipt.
   * Resolves to the successful receipt, or `undefined` when the fresh read changed the review.
   */
  sign: (
    reviewed: ReviewedWriteIntent,
    options: {
      abi: Abi;
      requote: () => Promise<RequotedIntent>;
      beforeSimulate?: () => Promise<void>;
    },
  ) => Promise<TransactionReceipt | undefined>;
  /** The hash a failure is reported against when nothing was submitted in this run. */
  setFallbackHash: (hash: Address | undefined) => void;
};

/**
 * The review state and signing pipeline every transaction panel shares: the selected wallet, the
 * persisted transaction state, the execution lock and the review/confirm/sign steps. `approval`
 * is the persisted allowance transaction for panels that write one before the main write.
 */
export function useReviewedWrite(
  action: PersistedTransactionAction,
  tokenAddress: Address | null,
  enabled = true,
  approval?: PersistedTransaction,
) {
  const account = useAccount();
  const readiness = useWalletReadiness();
  const publicClient = usePublicClient();
  const { data: walletClient } = useWalletClient();
  const configuration = publicConfiguration();
  const chainId = readiness.chainId;
  const transaction = usePersistedTransactionState(
    action,
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
    enabled,
  );
  const { state, setState } = transaction;
  const [executionBusy, setExecutionBusy] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [reviewIntent, setReviewIntent] = useState<ReviewedWriteIntent | null>(null);
  const [reviewNotice, setReviewNotice] = useState<string | null>(null);
  const lockRef = useRef(false);

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

  /** Everything a review or a signature needs from the wallet and the persisted state. */
  const walletReady = Boolean(
    publicClient &&
    walletClient &&
    account.address &&
    chainId === configuration.chainId &&
    readiness.selectedAccountVerified &&
    transaction.ready,
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

  /** Runs one locked execution; a thrown error becomes the write's (or approval's) failure. */
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
    const execution: ReviewedExecution = {
      ensureWallet: async (reviewed, notice = WALLET_CHANGED) => {
        if (!(await walletStillReviewed(reviewed))) {
          setState(priorState);
          throw new ReviewChangedError(notice);
        }
      },
      setFallbackHash: (hash) => {
        fallbackHash = hash;
      },
      unchanged: (reviewed, fresh) => {
        if (sameReviewedWriteIntent(reviewed, fresh.intent)) return true;
        fresh.replace?.();
        setReviewIntent(fresh.intent);
        setReviewNotice(WRITE_CHANGED);
        setState(priorState);
        return false;
      },
      approve: async (reviewed, write) => {
        if (!approval) throw new Error("This write has no approval step");
        approving = true;
        const priorApproval = approval.state;
        const request = { ...write, account: reviewed.account as Address } as never;
        phase = "simulation";
        approval.setState({ status: "simulating" });
        await client.simulateContract(request);
        if (!(await walletStillReviewed(reviewed))) {
          approval.setState(priorApproval);
          throw new ReviewChangedError(WALLET_CHANGED_DURING_SIMULATION);
        }
        phase = "write";
        approval.setState({ status: "awaiting-signature" });
        const hash = (await wallet.writeContract(request)) as Address;
        submittedHash = hash;
        approval.setState({ status: "submitted", hash });
        phase = "receipt";
        const receipt = await client.waitForTransactionReceipt({ hash });
        if (receipt.status !== "success") throw new Error("ReceiptReverted");
        approval.setState({ status: "mined", hash });
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
        if (!execution.unchanged(reviewed, fresh)) return undefined;
        await execution.ensureWallet(reviewed, WALLET_CHANGED_DURING_SIMULATION);
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
        return receipt;
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
      if (approving && approval) approval.setState(failure);
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
    state,
    executionBusy,
    confirming,
    reviewIntent,
    reviewNotice,
    setReviewNotice,
    walletReady,
    unresolved: hasUnresolvedSubmission(state),
    openReview,
    closeReview,
    assertWalletForReview,
    runExecution,
  };
}

export type ReviewedWrite = ReturnType<typeof useReviewedWrite>;

/**
 * Shared state of the curve, V2 router and V4 router trade panels on top of `useReviewedWrite`:
 * side, amount, slippage and the persisted allowance transaction. The panels keep what differs:
 * the quote source, the intent they build, balance and allowance preflight, and the details.
 */
export function useReviewedTrade(tokenAddress: Address | null, enabled = true) {
  const configuration = publicConfiguration();
  const account = useAccount();
  const approvalTransaction = usePersistedTransactionState(
    "approval",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
    enabled,
  );
  const write = useReviewedWrite("trade", tokenAddress, enabled, approvalTransaction);
  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [input, setInput] = useState("");
  const [slippage, setSlippage] = useState(DEFAULT_SLIPPAGE);
  const inputUnits = useMemo(() => {
    try {
      return input.trim() ? parseDecimal(input) : null;
    } catch {
      return null;
    }
  }, [input]);
  const slippageBps = useMemo(() => parseSlippageBps(slippage), [slippage]);
  const approvalHash = approvalTransaction.ready
    ? (approvalTransaction.state.hash as Address | undefined)
    : undefined;
  return {
    ...write,
    approvalTransaction,
    approvalHash,
    side,
    input,
    slippage,
    inputUnits,
    slippageBps,
    /** A sell also needs the persisted approval state before it can review or sign. */
    walletReady: write.walletReady && (side !== "sell" || approvalTransaction.ready),
    changeSide: (next: "buy" | "sell") => {
      setSide(next);
      setInput("");
      write.closeReview();
    },
    changeInput: (value: string) => {
      setInput(value);
      write.closeReview();
    },
    changeSlippage: (value: string) => {
      setSlippage(value);
      write.closeReview(false);
    },
  };
}

export type ReviewedTrade = ReturnType<typeof useReviewedTrade>;

export function WrongNetworkBanner({ write }: { write: ReviewedWrite }) {
  const { readiness, chainId, configuration } = write;
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

/**
 * With no wallet selected the primary action is connecting one: a disabled "Review" button would
 * not say why it cannot be pressed.
 */
export function ConnectOr({ write, children }: { write: ReviewedWrite; children: ReactNode }) {
  if (write.readiness.address) return <>{children}</>;
  return <WalletConnectButton className="trade-wallet-button" variant="primary" size="lg" />;
}

/** Back and sign buttons under a confirmation. */
export function ReviewActions({
  write,
  label,
  canSign,
  onSign,
}: {
  write: ReviewedWrite;
  label: string;
  canSign: boolean;
  onSign: () => void;
}) {
  const { executionBusy, state, reviewIntent, transaction } = write;
  return (
    <div className="transaction-actions">
      <Button variant="quiet" disabled={executionBusy} onClick={() => write.closeReview()}>
        Back
      </Button>
      <Button
        variant="primary"
        loading={state.status === "simulating" || state.status === "awaiting-signature"}
        disabled={executionBusy || !canSign || !reviewIntent || !transaction.ready}
        onClick={onSign}
      >
        {label}
      </Button>
    </div>
  );
}
