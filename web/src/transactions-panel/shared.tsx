"use client";

import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { formatBaseUnits } from "@/amounts";
import { ApiClient } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { chainClockOffset, remainingTaxSeconds } from "@/anti-snipe";
import { publicConfiguration } from "@/config/public";
import {
  decodeTransactionError,
  reconcileCanonicalFetch,
  type TransactionState,
  createTransactionState,
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
import { Badge, UnavailableState } from "@/components/primitives";
import { SlidingIndicator } from "@/components/sliding-indicator";

export const DEFAULT_TTL = 900n;
/**
 * Informational quotes wait for typing to pause this long, so each keystroke does not send a
 * backend or RPC quote. Signing never uses these quotes: review and preflight re-read on-chain.
 */
export const QUOTE_DEBOUNCE_MS = 250;

export type Address = `0x${string}`;
export const useCommitEffect = typeof window === "undefined" ? useEffect : useLayoutEffect;

export function eth(value: bigint) {
  return `${formatBaseUnits(value, 18, 6)} ETH`;
}

export function safeAddress(value: string | null | undefined): Address | null {
  return value && /^0x[0-9a-fA-F]{40}$/.test(value) ? (value as Address) : null;
}

export function statusLabel(status: TransactionState["status"]) {
  return status === "awaiting-signature"
    ? "Awaiting signature"
    : status.charAt(0).toUpperCase() + status.slice(1);
}

export function hasUnresolvedSubmission(state: TransactionState) {
  return Boolean(
    state.hash && ["submitted", "mined", "indexing", "rpc-failure"].includes(state.status),
  );
}

export function currentUnixSeconds() {
  return BigInt(Math.floor(Date.now() / 1000));
}

export function ErrorCopy({ error }: { error: unknown }) {
  const decoded = decodeTransactionError(error);
  return (
    <p className="transaction-error" role="alert">
      {decoded.message}
    </p>
  );
}

/**
 * The status row under a transaction form: badge, hash, decoded error and, unless turned off,
 * the stale canonical-status notice. Renders nothing while the transaction is disconnected.
 */
export function TransactionProgress({
  state,
  tone,
  prefix,
  refreshNotice = true,
}: {
  state: TransactionState;
  tone: React.ComponentProps<typeof Badge>["tone"];
  prefix?: string;
  refreshNotice?: boolean;
}) {
  if (state.status === "disconnected") return null;
  return (
    <div className="transaction-progress" role="status">
      <Badge tone={tone}>
        {prefix}
        {statusLabel(state.status)}
      </Badge>
      {state.hash ? <span className="mono">{state.hash}</span> : null}
      {state.error ? <ErrorCopy error={new Error(state.error)} /> : null}
      {refreshNotice && state.canonicalRefreshUnavailable ? (
        <span className="transaction-error" role="alert">
          Canonical status refresh unavailable. Showing the last known status.
        </span>
      ) : null}
    </div>
  );
}

export function observeFreshSnapshot(
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

export function usePersistedTransactionState(
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
      // Approvals and the engine-2 creator writes have no canonical event to observe; they
      // settle on the receipt alone.
      action === "approval" ||
      action === "v2-creator" ||
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

export function TradeSideTabs({
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
        <SlidingIndicator />
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

export const SLIPPAGE_PRESETS = ["1", "5", "10"] as const;

/** Preset slippage values; they fill the same input, so its validation still applies. */
export function SlippageChips({
  value,
  disabled,
  onPick,
}: {
  value: string;
  disabled: boolean;
  onPick: (value: string) => void;
}) {
  return (
    <div className="trade-chips" role="group" aria-label="Slippage presets">
      {SLIPPAGE_PRESETS.map((preset) => (
        <button
          key={preset}
          type="button"
          className="ui-chip"
          aria-pressed={value.trim() === preset}
          disabled={disabled}
          onClick={() => onPick(preset)}
        >
          {preset}%
        </button>
      ))}
      <SlidingIndicator />
    </div>
  );
}

export function ReadinessGate({ children }: { children: React.ReactNode }) {
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

/**
 * Seconds left in the anti-snipe window by chain time (Plan v2-C D6): the latest block's
 * timestamp fixes the offset to the client clock every 15 s, and a 1 s tick counts down.
 */
export function useTaxSecondsLeft(
  endsAt: string | null,
  client:
    { getBlock: (args: { blockTag: "latest" }) => Promise<{ timestamp: bigint }> } | undefined,
): number {
  const [secondsLeft, setSecondsLeft] = useState(0);
  useEffect(() => {
    if (!endsAt || !client) return;
    let cancelled = false;
    let offset: number | null = null;
    const sync = () =>
      void client
        .getBlock({ blockTag: "latest" })
        .then((block) => {
          if (!cancelled) offset = chainClockOffset(block.timestamp, Date.now());
        })
        .catch(() => undefined);
    sync();
    const syncTimer = setInterval(sync, 15_000);
    const tick = setInterval(() => {
      if (offset !== null) setSecondsLeft(remainingTaxSeconds(endsAt, Date.now() / 1000 + offset));
    }, 1_000);
    return () => {
      cancelled = true;
      clearInterval(syncTimer);
      clearInterval(tick);
    };
  }, [endsAt, client]);
  return endsAt && client ? secondsLeft : 0;
}
