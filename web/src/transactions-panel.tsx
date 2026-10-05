"use client";

import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useAccount, usePublicClient, useWalletClient } from "wagmi";
import { formatBaseUnits, parseDecimal } from "@/amounts";
import { ApiClient } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { browserAbis, type ReviewedDeployment } from "@/contracts/generated";
import { publicConfiguration } from "@/config/public";
import { addressExplorerUrl } from "@/wallet/explorer";
import { useWalletReadiness } from "@/wallet/readiness";
import { WalletConnectButton } from "@/wallet/connection";
import {
  calculateLaunchValue,
  classifyTransactionFailure,
  decodeTransactionError,
  minimumOutput,
  reconcileCanonicalFetch,
  parseRuntimeQuantity,
  parseSlippageBps,
  parseQuoteQuantity,
  reviewedRouterAddress,
  sameWriteIntent,
  sameReviewedWriteIntent,
  transactionDeadline,
  validateLaunchInput,
  type TransactionState,
  type ReviewedWriteIntent,
  createTransactionState,
  transitionTransaction,
} from "@/transactions";
import {
  readPersistedTransaction,
  writePersistedTransaction,
  type PersistedTransactionAction,
  type TransactionStorageContext,
} from "@/transaction-storage";
import {
  createScopeBoundCommitter,
  transactionStateForScope,
  type ScopedTransactionSnapshot,
} from "@/transaction-scope";
import {
  Badge,
  Button,
  ErrorState,
  Input,
  SafeImage,
  UnavailableState,
} from "@/components/primitives";

const DEFAULT_TTL = 900n;
type Address = `0x${string}`;
const useCommitEffect = typeof window === "undefined" ? useEffect : useLayoutEffect;

function eth(value: bigint) {
  return `${formatBaseUnits(value, 18, 6)} ETH`;
}

function safeAddress(value: string | null | undefined): Address | null {
  return value && /^0x[0-9a-fA-F]{40}$/.test(value) ? (value as Address) : null;
}

function statusLabel(status: TransactionState["status"]) {
  return status === "awaiting-signature"
    ? "Awaiting signature"
    : status.charAt(0).toUpperCase() + status.slice(1);
}

function hasUnresolvedSubmission(state: TransactionState) {
  return Boolean(
    state.hash && ["submitted", "mined", "indexing", "rpc-failure"].includes(state.status),
  );
}

function currentUnixSeconds() {
  return BigInt(Math.floor(Date.now() / 1000));
}

function ErrorCopy({ error }: { error: unknown }) {
  const decoded = decodeTransactionError(error);
  return (
    <p className="transaction-error" role="alert">
      {decoded.message}
    </p>
  );
}

function observeFreshSnapshot(
  baseUrl: string | null,
  tokenAddress: string | null,
  action: import("@/transactions").CanonicalObservationAction,
  state: Pick<TransactionState, "hash" | "status">,
  setState: (next: TransactionState) => void,
  isCurrent: () => boolean,
): () => void {
  if (!baseUrl) return () => undefined;
  const client = new ApiClient({ baseUrl });
  const delays = [500, 1_000, 2_000, 4_000, 8_000, 15_000, 30_000, 60_000];
  const abortController = new AbortController();
  let observedState = state;
  let attempt = 0;
  let inFlight = false;
  let timer: number | undefined;
  let stopped = false;

  const stop = () => {
    if (stopped) return;
    stopped = true;
    abortController.abort();
    if (timer !== undefined) window.clearTimeout(timer);
    window.removeEventListener("launchpad:canonical-reorg", refreshOnSignal);
    window.removeEventListener("focus", refreshOnSignal);
    window.removeEventListener("online", refreshOnSignal);
    window.removeEventListener("pageshow", refreshOnSignal);
    document.removeEventListener("visibilitychange", refreshOnSignal);
  };
  const schedule = () => {
    if (stopped) return;
    if (!isCurrent()) {
      stop();
      return;
    }
    if (observedState.status === "finalized") {
      stop();
      return;
    }
    const delay = delays[Math.min(attempt++, delays.length - 1)] ?? 60_000;
    timer = window.setTimeout(() => {
      timer = undefined;
      void refresh();
    }, delay);
  };
  const refresh = async () => {
    if (stopped || !isCurrent() || inFlight) return;
    inFlight = true;
    try {
      const fresh = await client.getCanonicalTransaction(
        observedState.hash ?? "",
        abortController.signal,
      );
      if (stopped || !isCurrent()) return;
      const records = (fresh.events ?? []).map((event) => ({
        tx_hash: event.tx_hash,
        launch_tx_hash: event.kind === "token_launch" ? event.tx_hash : undefined,
        finality: event.finality,
      }));
      const next = reconcileCanonicalFetch({
        state: observedState,
        submittedHash: observedState.hash ?? "",
        action,
        records,
        snapshotFinality: fresh.finality,
        outcome: "success",
      });
      const wasCanonical = ["indexed", "safe", "finalized"].includes(observedState.status);
      setState(next);
      observedState = next;
      if (next.status === "indexing" && wasCanonical)
        window.dispatchEvent(
          new CustomEvent("launchpad:canonical-reorg", { detail: { tokenAddress } }),
        );
    } catch (cause) {
      if (stopped || !isCurrent()) return;
      const authoritativeAbsence = cause instanceof ApiProblem && cause.status === 404;
      const wasCanonical = ["indexed", "safe", "finalized"].includes(observedState.status);
      const next = reconcileCanonicalFetch({
        state: observedState,
        submittedHash: observedState.hash ?? "",
        action,
        outcome: authoritativeAbsence ? "not-found" : "unavailable",
      });
      setState(next);
      if (next.status === "indexing" && wasCanonical && authoritativeAbsence) {
        window.dispatchEvent(
          new CustomEvent("launchpad:canonical-reorg", { detail: { tokenAddress } }),
        );
      }
      observedState = next;
    } finally {
      inFlight = false;
      schedule();
    }
  };
  const refreshOnSignal = () => {
    if (document.visibilityState === "hidden") return;
    void refresh();
  };
  window.addEventListener("launchpad:canonical-reorg", refreshOnSignal);
  window.addEventListener("focus", refreshOnSignal);
  window.addEventListener("online", refreshOnSignal);
  window.addEventListener("pageshow", refreshOnSignal);
  document.addEventListener("visibilitychange", refreshOnSignal);
  void refresh();
  return stop;
}

function usePersistedTransactionState(
  action: PersistedTransactionAction,
  chainId: number | null,
  accountAddress: string | undefined,
  tokenAddress: string | null,
  apiBaseUrl: string | null,
  enabled = true,
) {
  const [snapshot, setSnapshot] = useState<ScopedTransactionSnapshot>(() => ({
    scope: "",
    state: createTransactionState("disconnected"),
  }));
  const [restoredScope, setRestoredScope] = useState("");
  const scope = `${chainId ?? "unknown"}:${accountAddress?.toLowerCase() ?? "disconnected"}:${tokenAddress?.toLowerCase() ?? "no-token"}:${action}`;
  const activeScopeRef = useRef(scope);
  useCommitEffect(() => {
    activeScopeRef.current = scope;
  }, [scope]);
  const state = transactionStateForScope(snapshot, scope);
  const ready = Boolean(enabled && chainId && accountAddress && restoredScope === scope);

  const setState = useCallback(
    (next: TransactionState) => {
      const commit = createScopeBoundCommitter<TransactionState>(
        scope,
        () => activeScopeRef.current,
        (value) => {
          setSnapshot({ scope, state: value });
          if (!chainId || !accountAddress || !value.hash) return;
          const context: TransactionStorageContext = {
            chainId,
            account: accountAddress as `0x${string}`,
            tokenAddress: tokenAddress as `0x${string}` | null,
            action,
          };
          try {
            writePersistedTransaction(
              context,
              { hash: value.hash, status: value.status },
              window.localStorage,
            );
          } catch {
            // Browser storage can be disabled; transaction execution remains available.
          }
        },
      );
      commit(next);
    },
    [action, accountAddress, chainId, scope, tokenAddress],
  );

  useEffect(() => {
    if (!enabled || !chainId || !accountAddress) return;
    const context: TransactionStorageContext = {
      chainId,
      account: accountAddress as `0x${string}`,
      tokenAddress: tokenAddress as `0x${string}` | null,
      action,
    };
    let restored: ReturnType<typeof readPersistedTransaction> = null;
    try {
      restored = readPersistedTransaction(context, window.localStorage);
    } catch {
      restored = null;
    }
    if (activeScopeRef.current !== scope) return;
    // Restore after mount so server rendering never reads browser storage.
    setSnapshot({
      scope,
      state: restored ? { status: restored.status, hash: restored.hash } : createTransactionState(),
    });
    setRestoredScope(scope);
  }, [action, accountAddress, chainId, enabled, scope, tokenAddress]);

  useEffect(() => {
    if (
      !ready ||
      !state.hash ||
      !apiBaseUrl ||
      action === "approval" ||
      state.status === "finalized" ||
      state.status === "receipt-reverted" ||
      state.status === "reverted"
    )
      return;
    return observeFreshSnapshot(
      apiBaseUrl,
      tokenAddress,
      action,
      { hash: state.hash, status: state.status },
      setState,
      () => activeScopeRef.current === scope,
    );
  }, [action, apiBaseUrl, ready, scope, setState, state.hash, state.status, tokenAddress]);

  return { state, setState, ready };
}

function TradeSideTabs({
  side,
  label,
  disabled,
  onChange,
  children,
}: {
  side: "buy" | "sell";
  label: string;
  disabled: boolean;
  onChange: (side: "buy" | "sell") => void;
  children: React.ReactNode;
}) {
  const baseId = useId();
  const tabRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const tabId = (value: "buy" | "sell") => `${baseId}-${value}-tab`;
  const panelId = `${baseId}-panel`;
  const selectSide = (next: "buy" | "sell") => {
    onChange(next);
    tabRefs.current[next === "buy" ? 0 : 1]?.focus();
  };
  return (
    <>
      <div className="trade-tabs" role="tablist" aria-label={label}>
        {(["buy", "sell"] as const).map((value, index) => (
          <button
            key={value}
            ref={(element) => {
              tabRefs.current[index] = element;
            }}
            id={tabId(value)}
            type="button"
            role="tab"
            disabled={disabled}
            aria-selected={side === value}
            aria-controls={panelId}
            tabIndex={side === value ? 0 : -1}
            className={side === value ? "is-active" : ""}
            onClick={() => selectSide(value)}
            onKeyDown={(event) => {
              const keys = ["ArrowLeft", "ArrowRight", "Home", "End"];
              if (!keys.includes(event.key)) return;
              event.preventDefault();
              const next =
                event.key === "Home"
                  ? "buy"
                  : event.key === "End"
                    ? "sell"
                    : value === "buy"
                      ? "sell"
                      : "buy";
              selectSide(next);
            }}
          >
            {value === "buy" ? "Buy" : "Sell"}
          </button>
        ))}
      </div>
      <div
        id={panelId}
        role="tabpanel"
        aria-labelledby={tabId(side)}
        tabIndex={0}
        className="trade-side-panel"
      >
        {children}
      </div>
    </>
  );
}

function ReadinessGate({ children }: { children: React.ReactNode }) {
  const configuration = publicConfiguration();
  if (configuration.status !== "ready")
    return (
      <UnavailableState
        title="Transactions unavailable"
        description="A reviewed deployment, RPC endpoint, API, and wallet configuration are required. No transaction can be submitted."
      />
    );
  return <>{children}</>;
}

export function TradingPanel({
  token,
}: {
  token: {
    address: string;
    curve: string;
    pair: string;
    phase: string;
    name: string;
    symbol: string;
  };
}) {
  return (
    <ReadinessGate>
      <TradingPanelReady token={token} />
    </ReadinessGate>
  );
}

function TradingPanelReady({
  token,
}: {
  token: {
    address: string;
    curve: string;
    pair: string;
    phase: string;
    name: string;
    symbol: string;
  };
}) {
  const tokenAddress = safeAddress(token.address);
  const curveAddress = safeAddress(token.curve);
  const safeTokenAddress =
    tokenAddress ?? ("0x0000000000000000000000000000000000000000" as Address);
  const safeCurveAddress =
    curveAddress ?? ("0x0000000000000000000000000000000000000000" as Address);
  const graduated = token.phase.toLowerCase() === "graduated";
  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [input, setInput] = useState("");
  const [slippage, setSlippage] = useState("5");
  const [executionBusy, setExecutionBusy] = useState(false);
  const [quote, setQuote] = useState<{
    output: bigint;
    fee: bigint;
    source: "backend" | "contract";
  } | null>(null);
  const [quoteError, setQuoteError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [creatorClaimable, setCreatorClaimable] = useState<bigint | null>(null);
  const [refundClaimable, setRefundClaimable] = useState<bigint | null>(null);
  const [claimBusy, setClaimBusy] = useState(false);
  const [reviewIntent, setReviewIntent] = useState<ReviewedWriteIntent | null>(null);
  const [reviewDetails, setReviewDetails] = useState<{
    side: "buy" | "sell";
    inputUnits: bigint;
    output: bigint;
    fee: bigint;
  } | null>(null);
  const [reviewNotice, setReviewNotice] = useState<string | null>(null);
  const executionLockRef = useRef(false);
  const claimLockRef = useRef(false);
  const configuration = publicConfiguration();
  const curveExplorer = addressExplorerUrl(configuration, safeCurveAddress);
  const readiness = useWalletReadiness();
  const account = useAccount();
  const chainId = readiness.chainId;
  const transaction = usePersistedTransactionState(
    "trade",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
    !graduated,
  );
  const { state, setState } = transaction;
  const approvalTransaction = usePersistedTransactionState(
    "approval",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
    !graduated,
  );
  const setApprovalTransactionState = approvalTransaction.setState;
  const approvalHash = approvalTransaction.ready
    ? (approvalTransaction.state.hash as Address | undefined)
    : undefined;
  const creatorClaimTransaction = usePersistedTransactionState(
    "claim",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
    !graduated,
  );
  const refundClaimTransaction = usePersistedTransactionState(
    "refund",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
    !graduated,
  );
  const creatorClaimState = creatorClaimTransaction.state;
  const setCreatorClaimState = creatorClaimTransaction.setState;
  const refundClaimState = refundClaimTransaction.state;
  const setRefundClaimState = refundClaimTransaction.setState;
  const publicClient = usePublicClient();
  const { data: walletClient } = useWalletClient();
  const reviewedRouter = reviewedRouterAddress(configuration.deployment!, configuration.chainId);
  const reviewedWeth = safeAddress(configuration.deployment?.weth);
  const decimals = 18;
  const inputUnits = useMemo(() => {
    try {
      return input.trim() ? parseDecimal(input, decimals) : null;
    } catch {
      return null;
    }
  }, [input]);
  const slippageBps = useMemo(() => {
    return parseSlippageBps(slippage);
  }, [slippage]);

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
    void client
      .getQuote(token.address, { amount: inputUnits.toString(), side })
      .then((result) => {
        if (!cancelled) {
          if (parseQuoteQuantity(result.input, "quote input") !== inputUnits)
            throw new Error("QuoteChanged");
          const output = parseQuoteQuantity(result.output, "quote output");
          parseQuoteQuantity(result.refund, "quote refund");
          parseQuoteQuantity(result.next_virtual_eth, "next virtual ETH");
          parseQuoteQuantity(result.next_virtual_token, "next virtual token");
          parseQuoteQuantity(result.reserve_source_block.toString(), "quote source block");
          setQuote({
            output,
            fee:
              parseQuoteQuantity(result.protocol_fee, "protocol fee") +
              parseQuoteQuantity(result.creator_fee, "creator fee"),
            source: "backend",
          });
          setQuoteError(null);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setQuote(null);
          setQuoteError("Informational quote unavailable. Refresh before trading.");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [configuration.apiBaseUrl, configuration.status, inputUnits, side, token.address]);

  useEffect(() => {
    let cancelled = false;
    if (!publicClient || !account.address || !curveAddress) return;
    void Promise.all([
      publicClient.readContract({
        address: safeCurveAddress,
        abi: browserAbis.curve,
        functionName: "creator",
      }),
      publicClient.readContract({
        address: safeCurveAddress,
        abi: browserAbis.curve,
        functionName: "unclaimedCreatorFees",
      }),
      publicClient.readContract({
        address: safeCurveAddress,
        abi: browserAbis.curve,
        functionName: "pendingRefund",
        args: [account.address],
      }),
    ])
      .then(([creator, creatorFees, refund]) => {
        if (cancelled) return;
        setCreatorClaimable(
          String(creator).toLowerCase() === account.address!.toLowerCase()
            ? (creatorFees as bigint)
            : 0n,
        );
        setRefundClaimable(refund as bigint);
      })
      .catch(() => {
        if (!cancelled) {
          setCreatorClaimable(null);
          setRefundClaimable(null);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [account.address, curveAddress, publicClient, safeCurveAddress]);

  const activeQuote = inputUnits && inputUnits > 0n ? quote : null;
  const minimum =
    activeQuote && slippageBps !== null ? minimumOutput(activeQuote.output, slippageBps) : null;
  const switchNetwork = () => void readiness.switchNetwork();
  const readCurveIntent = async (
    accountAddress: Address,
    quoteSide: "buy" | "sell",
    quantity: bigint,
    toleranceBps: bigint,
    deadline: bigint,
  ) => {
    const intentChainId = configuration.chainId;
    if (intentChainId === null) throw new Error("ChainUnavailable");
    const contractQuote = await publicClient!.readContract({
      address: safeCurveAddress,
      abi: browserAbis.curve,
      functionName: quoteSide === "buy" ? "quoteBuy" : "quoteSell",
      args: [quantity],
    });
    const output =
      quoteSide === "buy"
        ? parseRuntimeQuantity((contractQuote as readonly unknown[])[1], "contract output")
        : parseRuntimeQuantity((contractQuote as readonly unknown[])[0], "contract output");
    const fee =
      parseRuntimeQuantity((contractQuote as readonly unknown[])[2], "protocol fee") +
      parseRuntimeQuantity((contractQuote as readonly unknown[])[3], "creator fee");
    const exactMinimum = minimumOutput(output, toleranceBps);
    const args =
      quoteSide === "buy"
        ? ([accountAddress, accountAddress, exactMinimum, deadline] as const)
        : ([quantity, accountAddress, exactMinimum, deadline] as const);
    return {
      intent: {
        account: accountAddress,
        target: safeCurveAddress,
        value: quoteSide === "buy" ? quantity : 0n,
        args,
        deadline,
        minimumOutput: exactMinimum,
        chainId: intentChainId,
        functionName: quoteSide,
      } satisfies ReviewedWriteIntent,
      output,
      fee,
    };
  };
  const prepareTradeReview = async () => {
    if (
      !account.address ||
      !publicClient ||
      !walletClient ||
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
      const phase = await publicClient.readContract({
        address: safeCurveAddress,
        abi: browserAbis.curve,
        functionName: "phase",
      });
      if (Number(phase) !== 0) throw new Error("WrongPhase");
      const deadline = transactionDeadline(currentUnixSeconds(), DEFAULT_TTL);
      const fresh = await readCurveIntent(
        account.address as Address,
        side,
        inputUnits,
        slippageBps,
        deadline,
      );
      setQuote({ output: fresh.output, fee: fresh.fee, source: "contract" });
      setReviewIntent(fresh.intent);
      setReviewDetails({ side, inputUnits, output: fresh.output, fee: fresh.fee });
      setReviewNotice(null);
      setQuoteError(null);
      setConfirming(true);
    } catch (cause) {
      setReviewIntent(null);
      setConfirming(false);
      setQuoteError(decodeTransactionError(cause).message);
    }
  };
  const replaceTradeReview = (
    intent: ReviewedWriteIntent,
    details: { side: "buy" | "sell"; inputUnits: bigint; output: bigint; fee: bigint },
  ) => {
    setReviewIntent(intent);
    setReviewDetails(details);
    setQuote({ output: details.output, fee: details.fee, source: "contract" });
    setReviewNotice(
      "The wallet write details changed. Review the updated values, then confirm again.",
    );
  };
  const execute = async () => {
    if (graduated) return;
    if (executionLockRef.current) return;
    const resumingApproval = side === "sell" && approvalHash !== undefined;
    if (
      !account.address ||
      !publicClient ||
      !walletClient ||
      !inputUnits ||
      slippageBps === null ||
      !reviewIntent ||
      !reviewDetails ||
      !transaction.ready ||
      (reviewDetails.side === "sell" && !approvalTransaction.ready) ||
      (!resumingApproval && hasUnresolvedSubmission(state)) ||
      chainId !== configuration.chainId ||
      !readiness.selectedAccountVerified
    ) {
      setReviewNotice("Wallet, network, or review changed. Refresh the review before signing.");
      return;
    }
    executionLockRef.current = true;
    setExecutionBusy(true);
    const reviewed = reviewIntent;
    const reviewedSide = reviewDetails.side;
    const reviewedInput = reviewDetails.inputUnits;
    const stateBeforeReview = state;
    let submittedHash: Address | undefined;
    let approvalWrite = false;
    let failurePhase: "preflight" | "simulation" | "write" | "receipt" = "preflight";
    try {
      const phase = await publicClient.readContract({
        address: safeCurveAddress,
        abi: browserAbis.curve,
        functionName: "phase",
      });
      if (Number(phase) !== 0) throw new Error("WrongPhase");
      const tokenBalance = await publicClient.readContract({
        address: safeTokenAddress,
        abi: browserAbis.token,
        functionName: "balanceOf",
        args: [reviewed.account as Address],
      });
      if (reviewedSide === "sell" && tokenBalance < reviewedInput)
        throw new Error("ERC20InsufficientBalance");
      if (reviewedSide === "buy") {
        const nativeBalance = await publicClient.getBalance({
          address: reviewed.account as Address,
        });
        if (nativeBalance < reviewedInput) throw new Error("InsufficientETHBalance");
      }
      const [walletAccounts, latestChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        latestChainId !== reviewed.chainId ||
        walletAccounts[0]?.toLowerCase() !== reviewed.account.toLowerCase() ||
        account.address?.toLowerCase() !== reviewed.account.toLowerCase()
      ) {
        setReviewNotice("The selected wallet or network changed. Return to review before signing.");
        return;
      }
      const fresh = await readCurveIntent(
        reviewed.account as Address,
        reviewedSide,
        reviewedInput,
        slippageBps,
        reviewed.deadline!,
      );
      if (!sameReviewedWriteIntent(reviewed, fresh.intent)) {
        replaceTradeReview(fresh.intent, {
          side: reviewedSide,
          inputUnits: reviewedInput,
          output: fresh.output,
          fee: fresh.fee,
        });
        return;
      }
      if (reviewedSide === "sell") {
        const allowance = await publicClient.readContract({
          address: safeTokenAddress,
          abi: browserAbis.token,
          functionName: "allowance",
          args: [reviewed.account as Address, safeCurveAddress],
        });
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
          failurePhase = "simulation";
          setApprovalTransactionState({ status: "simulating" });
          const approvalArgs = [safeCurveAddress, reviewedInput] as const;
          await publicClient.simulateContract({
            address: safeTokenAddress,
            abi: browserAbis.token,
            functionName: "approve",
            account: reviewed.account as Address,
            args: approvalArgs,
          } as never);
          setApprovalTransactionState({ status: "awaiting-signature" });
          failurePhase = "write";
          const hash = await walletClient.writeContract({
            address: safeTokenAddress,
            abi: browserAbis.token,
            functionName: "approve",
            account: reviewed.account as Address,
            args: approvalArgs,
          } as never);
          submittedHash = hash as Address;
          setApprovalTransactionState({ status: "submitted", hash: hash as Address });
          failurePhase = "receipt";
          const approvalReceipt = await publicClient.waitForTransactionReceipt({ hash });
          if (approvalReceipt.status !== "success") throw new Error("ReceiptReverted");
          setApprovalTransactionState({ status: "mined", hash: hash as Address });
          setConfirming(false);
          return;
        }
      }
      setState({ status: "simulating" });
      const phaseBeforeSign = await publicClient.readContract({
        address: safeCurveAddress,
        abi: browserAbis.curve,
        functionName: "phase",
      });
      if (Number(phaseBeforeSign) !== 0) throw new Error("WrongPhase");
      failurePhase = "simulation";
      await publicClient.simulateContract({
        address: reviewed.target as Address,
        abi: browserAbis.curve,
        functionName: reviewed.functionName as "buy" | "sell",
        account: reviewed.account as Address,
        args: reviewed.args as never,
        value: reviewed.value,
      } as never);
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
        setState(stateBeforeReview);
        return;
      }
      const finalQuote = await readCurveIntent(
        reviewed.account as Address,
        reviewedSide,
        reviewedInput,
        slippageBps,
        reviewed.deadline!,
      );
      if (!sameReviewedWriteIntent(reviewed, finalQuote.intent)) {
        replaceTradeReview(finalQuote.intent, {
          side: reviewedSide,
          inputUnits: reviewedInput,
          output: finalQuote.output,
          fee: finalQuote.fee,
        });
        setState(stateBeforeReview);
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
        setState(stateBeforeReview);
        return;
      }
      setState({ status: "awaiting-signature" });
      failurePhase = "write";
      const hash = await walletClient.writeContract({
        address: reviewed.target as Address,
        abi: browserAbis.curve,
        functionName: reviewed.functionName as "buy" | "sell",
        account: reviewed.account as Address,
        args: reviewed.args as never,
        value: reviewed.value,
      } as never);
      submittedHash = hash as Address;
      setConfirming(false);
      setReviewIntent(null);
      setState({ status: "submitted", hash: hash as Address });
      failurePhase = "receipt";
      const receipt = await publicClient.waitForTransactionReceipt({ hash });
      if (receipt.status !== "success") throw new Error("ReceiptReverted");
      setState({ status: "mined", hash: hash as Address });
      setState({ status: "indexing", hash: hash as Address });
    } catch (error) {
      if (error instanceof Error && /wallet or network changed/i.test(error.message)) {
        setReviewNotice("The selected wallet or network changed. Return to review before signing.");
        return;
      }
      const message = classifyTransactionFailure(error, failurePhase);
      const failure = {
        status: message,
        hash: submittedHash ?? state.hash,
        error: decodeTransactionError(error).message,
      } satisfies TransactionState;
      if (approvalWrite) setApprovalTransactionState(failure);
      else setState(failure);
    } finally {
      executionLockRef.current = false;
      setExecutionBusy(false);
    }
  };
  const executeClaim = async (kind: "creator" | "refund") => {
    const claimTransaction = kind === "creator" ? creatorClaimTransaction : refundClaimTransaction;
    const claimState = claimTransaction.state;
    if (
      claimLockRef.current ||
      !claimTransaction.ready ||
      hasUnresolvedSubmission(claimState) ||
      !publicClient ||
      !walletClient ||
      !account.address ||
      chainId !== configuration.chainId ||
      !readiness.selectedAccountVerified
    )
      return;
    claimLockRef.current = true;
    setClaimBusy(true);
    const setClaimState = kind === "creator" ? setCreatorClaimState : setRefundClaimState;
    let submittedHash: Address | undefined;
    let failurePhase: "preflight" | "simulation" | "write" | "receipt" = "preflight";
    try {
      setClaimState({ status: "validating" });
      const [walletAccounts, latestChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        latestChainId !== configuration.chainId ||
        walletAccounts[0]?.toLowerCase() !== account.address.toLowerCase()
      )
        throw new Error("Wallet or network changed; review the claim again.");
      if (kind === "creator") {
        const [creator, amount] = await Promise.all([
          publicClient.readContract({
            address: safeCurveAddress,
            abi: browserAbis.curve,
            functionName: "creator",
          }),
          publicClient.readContract({
            address: safeCurveAddress,
            abi: browserAbis.curve,
            functionName: "unclaimedCreatorFees",
          }),
        ]);
        if (
          String(creator).toLowerCase() !== account.address.toLowerCase() ||
          (amount as bigint) === 0n
        )
          throw new Error("NothingToClaim");
      } else {
        const amount = await publicClient.readContract({
          address: safeCurveAddress,
          abi: browserAbis.curve,
          functionName: "pendingRefund",
          args: [account.address],
        });
        if ((amount as bigint) === 0n) throw new Error("NothingToClaim");
      }
      const functionName = kind === "creator" ? "claimCreatorFees" : "claimRefund";
      setClaimState({ status: "simulating" });
      failurePhase = "simulation";
      await publicClient.simulateContract({
        address: safeCurveAddress,
        abi: browserAbis.curve,
        functionName,
        account: account.address,
      } as never);
      setClaimState({ status: "awaiting-signature" });
      failurePhase = "write";
      const hash = await walletClient.writeContract({
        address: safeCurveAddress,
        abi: browserAbis.curve,
        functionName,
        account: account.address,
      } as never);
      submittedHash = hash as Address;
      setClaimState({ status: "submitted", hash: submittedHash });
      failurePhase = "receipt";
      const receipt = await publicClient.waitForTransactionReceipt({ hash });
      if (receipt.status !== "success") throw new Error("ReceiptReverted");
      setClaimState({ status: "indexing", hash: submittedHash });
    } catch (cause) {
      setClaimState({
        status: classifyTransactionFailure(cause, failurePhase),
        hash: submittedHash,
        error: decodeTransactionError(cause).message,
      });
    } finally {
      claimLockRef.current = false;
      setClaimBusy(false);
    }
  };
  const canTrade = Boolean(
    inputUnits &&
    inputUnits > 0n &&
    minimum !== null &&
    readiness.selectedAccountVerified &&
    chainId === configuration.chainId,
  );
  const canSubmit = canTrade && confirming && (side !== "sell" || approvalTransaction.ready);
  if (!tokenAddress || !curveAddress)
    return (
      <UnavailableState
        title="Trading unavailable"
        description="This token contract is not valid for the selected network."
      />
    );
  if (graduated)
    return (
      <section
        className="workspace-panel transaction-panel"
        aria-labelledby="graduated-trade-title"
      >
        <div className="panel-head">
          <div>
            <p className="panel-kicker">Graduated route</p>
            <h2 id="graduated-trade-title">Router handoff</h2>
          </div>
          <Badge tone="success">Graduated</Badge>
        </div>
        {reviewedRouter && reviewedWeth ? (
          <GraduatedSwapPanel token={token} router={reviewedRouter} weth={reviewedWeth} />
        ) : (
          <UnavailableState
            title="Router unavailable"
            description="No enabled reviewed Uniswap v2 router is configured for this chain. Trading is safely disabled."
          />
        )}
      </section>
    );
  return (
    <section className="workspace-panel transaction-panel" aria-labelledby="trade-title">
      <div className="panel-head">
        <div>
          <p className="panel-kicker">Wallet action</p>
          <h2 id="trade-title">Trade {token.symbol}</h2>
        </div>
        <Badge tone={token.phase === "graduated" ? "success" : "accent"}>{token.phase}</Badge>
      </div>
      {chainId !== configuration.chainId ? (
        <div className="transaction-warning" role="alert">
          Wrong network. Switch to {configuration.deployment?.name ?? "the reviewed network"} before
          signing.
          <Button size="sm" onClick={switchNetwork}>
            Switch network
          </Button>
        </div>
      ) : null}
      <TradeSideTabs
        side={side}
        label="Trade side"
        disabled={executionBusy}
        onChange={(next) => {
          setSide(next);
          setInput("");
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
            placeholder="0.00"
            hint="Integer base units are used for signing; decimals are presentation only."
          />
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
            hint="Minimum output is rounded down on-chain. At most 50%."
          />
          {quoteError ? (
            <p className="transaction-error" role="alert">
              {quoteError}
            </p>
          ) : null}
          {quote ? (
            <div className="quote-summary">
              <span>
                Estimated output{" "}
                <strong className="mono">{formatBaseUnits(quote.output, 18, 6)}</strong>
              </span>
              <span>
                Fees <strong className="mono">{eth(quote.fee)}</strong>
              </span>
              <span>
                Minimum output{" "}
                <strong className="mono">
                  {minimum === null ? "—" : formatBaseUnits(minimum, 18, 6)}
                </strong>
              </span>
              <small>
                {quote.source === "backend"
                  ? "Informational API quote; contract quote is authoritative before signing."
                  : "Authoritative contract quote, read immediately before simulation."}
              </small>
            </div>
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
        {confirming ? (
          <div className="transaction-confirmation" role="region" aria-label="Trade confirmation">
            <h3>Confirm {reviewDetails?.side ?? side}</h3>
            {reviewNotice ? (
              <p className="transaction-warning" role="status">
                {reviewNotice}
              </p>
            ) : null}
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
              <dt>Fee</dt>
              <dd className="mono">{reviewDetails ? eth(reviewDetails.fee) : "—"}</dd>
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
              <dd className="mono">
                {reviewIntent?.deadline?.toString() ?? "Unavailable"} (Unix seconds)
              </dd>
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
                disabled={executionBusy || !canSubmit || !reviewIntent || !transaction.ready}
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
            disabled={
              executionBusy ||
              !canTrade ||
              !transaction.ready ||
              (side === "sell" && !approvalTransaction.ready) ||
              hasUnresolvedSubmission(state)
            }
            onClick={() => void prepareTradeReview()}
          >
            {side === "sell" && approvalHash ? "Resume sell" : `Review ${side}`}
          </Button>
        )}
      </TradeSideTabs>
      {state.status !== "disconnected" ? (
        <div className="transaction-progress" role="status">
          <Badge
            tone={
              state.status === "reverted" || state.status === "rejected-signature"
                ? "danger"
                : state.status === "indexing"
                  ? "warning"
                  : "success"
            }
          >
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
      <div className="claim-row">
        <span>
          Claimable creator fees:{" "}
          {creatorClaimable === null ? "Unavailable" : eth(creatorClaimable)}
          <br />
          Claimable refund: {refundClaimable === null ? "Unavailable" : eth(refundClaimable)}
        </span>
        <div className="transaction-actions">
          <Button
            size="sm"
            disabled={
              creatorClaimable === null ||
              creatorClaimable === 0n ||
              claimBusy ||
              !creatorClaimTransaction.ready ||
              hasUnresolvedSubmission(creatorClaimState)
            }
            loading={
              creatorClaimState.status === "simulating" ||
              creatorClaimState.status === "awaiting-signature"
            }
            onClick={() => void executeClaim("creator")}
          >
            Claim creator fees
          </Button>
          <Button
            size="sm"
            disabled={
              refundClaimable === null ||
              refundClaimable === 0n ||
              claimBusy ||
              !refundClaimTransaction.ready ||
              hasUnresolvedSubmission(refundClaimState)
            }
            loading={
              refundClaimState.status === "simulating" ||
              refundClaimState.status === "awaiting-signature"
            }
            onClick={() => void executeClaim("refund")}
          >
            Claim refund
          </Button>
        </div>
      </div>
      {[creatorClaimState, refundClaimState].map((claim) =>
        claim.status !== "disconnected" ? (
          <div className="transaction-progress" role="status" key={claim.hash ?? claim.status}>
            <Badge
              tone={
                claim.status === "reverted" || claim.status === "rejected-signature"
                  ? "danger"
                  : "warning"
              }
            >
              {statusLabel(claim.status)}
            </Badge>
            {claim.hash ? <span className="mono">{claim.hash}</span> : null}
            {claim.error ? <ErrorCopy error={new Error(claim.error)} /> : null}
            {claim.canonicalRefreshUnavailable ? (
              <span className="transaction-error" role="alert">
                Canonical status refresh unavailable. Showing the last known status.
              </span>
            ) : null}
          </div>
        ) : null,
      )}
    </section>
  );
}

function GraduatedSwapPanel({
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
    return () => {
      cancelled = true;
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
        const balance = await publicClient.readContract({
          address: tokenAddress,
          abi: browserAbis.token,
          functionName: "balanceOf",
          args: [reviewed.account as Address],
        });
        if (balance < reviewedInput) throw new Error("ERC20InsufficientBalance");
        const allowance = await publicClient.readContract({
          address: tokenAddress,
          abi: browserAbis.token,
          functionName: "allowance",
          args: [reviewed.account as Address, router],
        });
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
      {chainId !== configuration.chainId ? (
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
          {output !== null ? (
            <div className="quote-summary">
              <span>
                Router output <strong className="mono">{formatBaseUnits(output, 18, 6)}</strong>
              </span>
              <span>
                Minimum output{" "}
                <strong className="mono">
                  {minimum === null ? "—" : formatBaseUnits(minimum, 18, 6)}
                </strong>
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

export function LaunchPanel() {
  return (
    <ReadinessGate>
      <LaunchPanelReady />
    </ReadinessGate>
  );
}

function LaunchPreview({
  imageUrl,
  name,
  symbol,
  launchFee,
  value,
}: {
  imageUrl: string;
  name: string;
  symbol: string;
  launchFee: bigint | null;
  value: bigint | null;
}) {
  return (
    <aside className="launch-preview-card" aria-label="Token preview">
      <div className="launch-preview-image">
        {imageUrl.trim() ? (
          <SafeImage src={imageUrl} alt="Token preview" />
        ) : (
          <span aria-hidden="true">◈</span>
        )}
      </div>
      <h2>{name.trim() || "Your token"}</h2>
      <p className="mono">{symbol.trim() || "ticker"}</p>
      <dl>
        <div>
          <dt>Launch fee</dt>
          <dd>{launchFee === null ? "Unavailable" : eth(launchFee)}</dd>
        </div>
        <div>
          <dt>Paired with</dt>
          <dd>ETH</dd>
        </div>
        <div>
          <dt>Trade fee</dt>
          <dd>1.00%</dd>
        </div>
        <div>
          <dt>Launch value</dt>
          <dd>{value === null ? "Unavailable" : eth(value)}</dd>
        </div>
        <div>
          <dt>Graduation</dt>
          <dd>4.2 ETH</dd>
        </div>
        <div>
          <dt>Liquidity</dt>
          <dd>Initial LP burned</dd>
        </div>
      </dl>
    </aside>
  );
}

function LaunchPanelReady() {
  const configuration = publicConfiguration();
  const readiness = useWalletReadiness();
  const account = useAccount();
  const launchChainId = readiness.chainId;
  const publicClient = usePublicClient();
  const { data: walletClient } = useWalletClient();
  const transaction = usePersistedTransactionState(
    "launch",
    configuration.chainId,
    account.address,
    null,
    configuration.apiBaseUrl,
  );
  const { state, setState } = transaction;
  const [name, setName] = useState("");
  const [symbol, setSymbol] = useState("");
  const [imageUrl, setImageUrl] = useState("");
  const [xUrl, setXUrl] = useState("");
  const [telegramUrl, setTelegramUrl] = useState("");
  const [buy, setBuy] = useState("0");
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [launchFee, setLaunchFee] = useState<bigint | null>(null);
  const [defaultsRead, setDefaultsRead] = useState(false);
  const [launchesPaused, setLaunchesPaused] = useState<boolean | null>(null);
  const [tradingPaused, setTradingPaused] = useState<boolean | null>(null);
  const [engineEnabled, setEngineEnabled] = useState<boolean | null>(null);
  const executionLockRef = useRef(false);
  const deployment = configuration.deployment as ReviewedDeployment;
  const factoryExplorer = deployment.factory
    ? addressExplorerUrl(configuration, deployment.factory as Address)
    : null;
  const switchNetwork = () => void readiness.switchNetwork();
  useEffect(() => {
    if (!publicClient || !deployment.factory) return;
    void Promise.all([
      publicClient.readContract({
        address: deployment.factory as Address,
        abi: browserAbis.factory,
        functionName: "launchFee",
      }),
      publicClient.readContract({
        address: deployment.factory as Address,
        abi: browserAbis.factory,
        functionName: "futureDefaults",
      }),
      publicClient.readContract({
        address: deployment.factory as Address,
        abi: browserAbis.factory,
        functionName: "launchesPaused",
      }),
      publicClient.readContract({
        address: deployment.factory as Address,
        abi: browserAbis.factory,
        functionName: "tradingPaused",
      }),
      publicClient.readContract({
        address: deployment.factory as Address,
        abi: browserAbis.factory,
        functionName: "engineEnabled",
        args: [1],
      }),
    ])
      .then(([fee, , paused, trading, enabled]) => {
        setLaunchFee(fee as bigint);
        setDefaultsRead(true);
        setLaunchesPaused(Boolean(paused));
        setTradingPaused(Boolean(trading));
        setEngineEnabled(Boolean(enabled));
      })
      .catch(() => setError("Factory configuration unavailable; launching is disabled."));
  }, [deployment.factory, publicClient]);
  const buyUnits = useMemo(() => {
    try {
      return parseDecimal(buy || "0");
    } catch {
      return null;
    }
  }, [buy]);
  const value =
    launchFee !== null && buyUnits !== null ? calculateLaunchValue(launchFee, buyUnits) : null;
  const validMetadata = validateLaunchInput({
    name,
    symbol,
    imageUrl,
    xUrl,
    telegramUrl,
    developerBuyGross: buyUnits ?? -1n,
  });
  const submit = async () => {
    if (executionLockRef.current) return;
    if (
      Object.keys(validMetadata).length ||
      !publicClient ||
      !walletClient ||
      !account.address ||
      !deployment.factory ||
      value === null ||
      launchFee === null ||
      !readiness.selectedAccountVerified ||
      !transaction.ready ||
      hasUnresolvedSubmission(state)
    )
      return;
    setConfirming(false);
    executionLockRef.current = true;
    let submittedHash: Address | undefined;
    let failurePhase: "preflight" | "simulation" | "write" | "receipt" = "preflight";
    try {
      setState(
        transitionTransaction(
          createTransactionState("disconnected"),
          { type: "validate" },
          readiness.transactionReadiness,
        ),
      );
      const deadline = transactionDeadline(currentUnixSeconds(), DEFAULT_TTL);
      const args = [
        {
          name: name.trim(),
          symbol: symbol.trim(),
          engineVersion: 1,
          developerBuyGross: buyUnits!,
          minDeveloperTokensOut: 0n,
          deadline,
        },
      ] as const;
      const [latestLaunchFee, latestLaunchesPaused, latestTradingPaused, latestEngineEnabled] =
        await Promise.all([
          publicClient.readContract({
            address: deployment.factory as Address,
            abi: browserAbis.factory,
            functionName: "launchFee",
          }) as Promise<bigint>,
          publicClient.readContract({
            address: deployment.factory as Address,
            abi: browserAbis.factory,
            functionName: "launchesPaused",
          }) as Promise<boolean>,
          publicClient.readContract({
            address: deployment.factory as Address,
            abi: browserAbis.factory,
            functionName: "tradingPaused",
          }) as Promise<boolean>,
          publicClient.readContract({
            address: deployment.factory as Address,
            abi: browserAbis.factory,
            functionName: "engineEnabled",
            args: [1],
          }) as Promise<boolean>,
        ]);
      if (!latestEngineEnabled) throw new Error("EngineDisabled");
      if (latestLaunchesPaused) throw new Error("LaunchesPaused");
      if (buyUnits! > 0n && latestTradingPaused) throw new Error("TradingPaused");
      const exactValue = calculateLaunchValue(latestLaunchFee, buyUnits!);
      const nativeBalance = await publicClient.getBalance({ address: account.address });
      if (nativeBalance < exactValue) throw new Error("InsufficientETHBalance");
      if (latestLaunchFee !== launchFee || exactValue !== value)
        throw new Error("LaunchValueMismatch");
      const capturedIntent = {
        account: account.address,
        target: deployment.factory,
        value: exactValue,
        args,
        deadline,
      };
      const [simulationAccounts, simulationChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        simulationChainId !== configuration.chainId ||
        simulationAccounts[0]?.toLowerCase() !== account.address.toLowerCase()
      )
        throw new Error("Wallet or network changed; review the launch again.");
      failurePhase = "simulation";
      await publicClient.simulateContract({
        address: deployment.factory as Address,
        abi: browserAbis.factory,
        functionName: "launch",
        account: account.address,
        args,
        value: exactValue,
      } as never);
      const [beforeSignLaunchesPaused, beforeSignEngineEnabled] = await Promise.all([
        publicClient.readContract({
          address: deployment.factory as Address,
          abi: browserAbis.factory,
          functionName: "launchesPaused",
        }) as Promise<boolean>,
        publicClient.readContract({
          address: deployment.factory as Address,
          abi: browserAbis.factory,
          functionName: "engineEnabled",
          args: [1],
        }) as Promise<boolean>,
      ]);
      if (beforeSignLaunchesPaused) throw new Error("LaunchesPaused");
      if (!beforeSignEngineEnabled) throw new Error("EngineDisabled");
      const [writeAccounts, writeChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        writeChainId !== configuration.chainId ||
        writeAccounts[0]?.toLowerCase() !== account.address.toLowerCase()
      )
        throw new Error("Wallet or network changed; review the launch again.");
      const writeIntent = {
        account: account.address,
        target: deployment.factory,
        value: exactValue,
        args,
        deadline,
      };
      if (!sameWriteIntent(capturedIntent, writeIntent))
        throw new Error("Write intent changed; retry safely.");
      setState({ status: "awaiting-signature" });
      failurePhase = "write";
      const hash = await walletClient.writeContract({
        address: deployment.factory as Address,
        abi: browserAbis.factory,
        functionName: "launch",
        account: account.address,
        args,
        value: exactValue,
      } as never);
      submittedHash = hash as Address;
      setState({ status: "submitted", hash: hash as Address });
      failurePhase = "receipt";
      const receipt = await publicClient.waitForTransactionReceipt({ hash });
      if (receipt.status !== "success") throw new Error("ReceiptReverted");
      setState({ status: "indexing", hash: hash as Address });
    } catch (cause) {
      setState({
        status: classifyTransactionFailure(cause, failurePhase),
        hash: submittedHash,
        error: decodeTransactionError(cause).message,
      });
    } finally {
      executionLockRef.current = false;
    }
  };
  return (
    <section
      className="workspace-panel transaction-panel launch-panel"
      aria-labelledby="launch-title"
    >
      <div className="launch-panel-head">
        <div>
          <h1 id="launch-title">Launch token</h1>
        </div>
      </div>
      {error ? (
        <ErrorState title="Launch unavailable" description={error} />
      ) : (
        <>
          {!readiness.address ? (
            <div className="transaction-warning" role="status">
              Connect the selected wallet before reviewing this launch.
            </div>
          ) : null}
          {readiness.address && launchChainId !== configuration.chainId ? (
            <div className="transaction-warning" role="alert">
              Wrong network. Switch to {deployment.name} before signing.
              <Button size="sm" onClick={switchNetwork}>
                Switch network
              </Button>
            </div>
          ) : null}
          <div className="launch-layout">
            <div className="launch-main">
              <div className="transaction-form">
                <div className="launch-field-row">
                  <Input
                    label="Name"
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    maxLength={64}
                    placeholder="Token name"
                  />
                  <Input
                    label="Ticker"
                    value={symbol}
                    onChange={(event) => setSymbol(event.target.value.toUpperCase())}
                    maxLength={16}
                    placeholder="Symbol"
                  />
                </div>
                <Input
                  label="Token image"
                  value={imageUrl}
                  onChange={(event) => setImageUrl(event.target.value)}
                  placeholder="https://…"
                />
                <div className="launch-field-row">
                  <Input
                    label="X profile"
                    value={xUrl}
                    onChange={(event) => setXUrl(event.target.value)}
                    placeholder="https://x.com/handle"
                  />
                  <Input
                    label="Telegram"
                    value={telegramUrl}
                    onChange={(event) => setTelegramUrl(event.target.value)}
                    placeholder="https://t.me/community"
                  />
                </div>
                <label className="ui-field">
                  <span>Paired asset</span>
                  <select className="ui-input" defaultValue="ETH" aria-label="Paired asset">
                    <option>ETH</option>
                  </select>
                  <small className="ui-field-hint">Graduates once the curve raises 4.2 ETH.</small>
                </label>
                <Input
                  label="Developer buy (ETH, optional)"
                  value={buy}
                  onChange={(event) => setBuy(event.target.value)}
                  inputMode="decimal"
                  hint="Launch value is exactly launch fee + developer buy gross."
                />
                {imageUrl ? (
                  <SafeImage
                    src={imageUrl}
                    alt="Token image preview"
                    className="launch-image-preview"
                  />
                ) : null}
                <details className="launch-advanced">
                  <summary>Advanced</summary>
                  <p>Engine version and pause state are read from the reviewed factory.</p>
                </details>
              </div>
              <div className="quote-summary">
                <span>
                  Factory launch fee{" "}
                  <strong className="mono">
                    {launchFee === null ? "Unavailable" : eth(launchFee)}
                  </strong>
                </span>
                <span>
                  Factory defaults{" "}
                  <strong className="mono">
                    {defaultsRead ? "Read from chain" : "Unavailable"}
                  </strong>
                </span>
                <span>
                  Launches paused{" "}
                  <strong className="mono">
                    {launchesPaused === null ? "Unavailable" : launchesPaused ? "Yes" : "No"}
                  </strong>
                </span>
                <span>
                  Trading paused{" "}
                  <strong className="mono">
                    {tradingPaused === null ? "Unavailable" : tradingPaused ? "Yes" : "No"}
                  </strong>
                </span>
                <span>
                  Engine v1 enabled{" "}
                  <strong className="mono">
                    {engineEnabled === null ? "Unavailable" : engineEnabled ? "Yes" : "No"}
                  </strong>
                </span>
                <small>
                  Engine v1 is the only generated launch ABI. Pause state is read from the reviewed
                  factory immediately before confirmation and simulation.
                </small>
                <span>
                  Exact launch value{" "}
                  <strong className="mono">{value === null ? "Unavailable" : eth(value)}</strong>
                </span>
              </div>
              <p className="transaction-risk">
                Non-custodial: your selected wallet signs directly. Launch is irreversible and may
                lose value. Metadata links and image preview are validated before this confirmation
                step.
              </p>
              {confirming ? (
                <div className="transaction-confirmation">
                  <h3>Confirm launch</h3>
                  <dl>
                    <dt>Token</dt>
                    <dd>
                      {name || "—"} ({symbol || "—"})
                    </dd>
                    <dt>Launch value</dt>
                    <dd className="mono">{value === null ? "Unavailable" : eth(value)}</dd>
                    <dt>Fee</dt>
                    <dd className="mono">{launchFee === null ? "Unavailable" : eth(launchFee)}</dd>
                    <dt>Developer buy / minimum output</dt>
                    <dd className="mono">{buy || "0"} ETH / 0 tokens</dd>
                    <dt>Network</dt>
                    <dd>{deployment.name}</dd>
                    <dt>Contract</dt>
                    <dd className="mono">
                      {factoryExplorer ? (
                        <a href={factoryExplorer} target="_blank" rel="noreferrer">
                          {deployment.factory}
                        </a>
                      ) : (
                        deployment.factory
                      )}
                    </dd>
                    <dt>Deadline</dt>
                    <dd className="mono">
                      {transactionDeadline(currentUnixSeconds(), DEFAULT_TTL).toString()} (Unix
                      seconds)
                    </dd>
                  </dl>
                  <div className="transaction-actions">
                    <Button variant="quiet" onClick={() => setConfirming(false)}>
                      Back
                    </Button>
                    <Button
                      variant="primary"
                      loading={state.status === "awaiting-signature"}
                      onClick={() => void submit()}
                    >
                      Sign launch
                    </Button>
                  </div>
                </div>
              ) : !readiness.address ? (
                <WalletConnectButton className="launch-wallet-button" variant="primary" size="lg" />
              ) : (
                <Button
                  variant="primary"
                  size="lg"
                  disabled={
                    Object.keys(validMetadata).length > 0 ||
                    value === null ||
                    !defaultsRead ||
                    launchesPaused !== false ||
                    engineEnabled !== true ||
                    !readiness.selectedAccountVerified ||
                    !transaction.ready ||
                    hasUnresolvedSubmission(state)
                  }
                  onClick={() => setConfirming(true)}
                >
                  Review launch
                </Button>
              )}
            </div>
            <LaunchPreview
              imageUrl={imageUrl}
              name={name}
              symbol={symbol}
              launchFee={launchFee}
              value={value}
            />
          </div>
        </>
      )}
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
    </section>
  );
}
