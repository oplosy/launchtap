import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { ApiClient } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import {
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

export const useCommitEffect = typeof window === "undefined" ? useEffect : useLayoutEffect;

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
