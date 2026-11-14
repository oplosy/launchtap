"use client";

import { useEffect, useId, useRef, useState } from "react";
import { formatBaseUnits } from "@/amounts";
import { chainClockOffset, remainingTaxSeconds } from "@/anti-snipe";
import { publicConfiguration } from "@/config/public";
import { decodeTransactionError, type TransactionState } from "@/transactions";
import { Badge, UnavailableState } from "@/components/primitives";
import { SlidingIndicator } from "@/components/sliding-indicator";

export {
  observeFreshSnapshot,
  useCommitEffect,
  usePersistedTransactionState,
} from "./transaction-persistence";

export const DEFAULT_TTL = 900n;
/**
 * Informational quotes wait for typing to pause this long, so each keystroke does not send a
 * backend or RPC quote. Signing never uses these quotes: review and preflight re-read on-chain.
 */
export const QUOTE_DEBOUNCE_MS = 250;

export type Address = `0x${string}`;

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

export const SLIPPAGE_PRESETS = ["1", "2", "5", "10"] as const;
/** The starting slippage on every trade form; one of the presets so its chip shows as chosen. */
export const DEFAULT_SLIPPAGE = "2";

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
    let ended = false;
    // A hidden tab or a finished window needs no chain-clock RPC; showing the tab resyncs.
    const sync = () => {
      if (ended || document.visibilityState === "hidden") return;
      void client
        .getBlock({ blockTag: "latest" })
        .then((block) => {
          if (!cancelled) offset = chainClockOffset(block.timestamp, Date.now());
        })
        .catch(() => undefined);
    };
    sync();
    const syncTimer = setInterval(sync, 15_000);
    document.addEventListener("visibilitychange", sync);
    const tick = setInterval(() => {
      if (offset === null) return;
      const left = remainingTaxSeconds(endsAt, Date.now() / 1000 + offset);
      setSecondsLeft(left);
      if (left <= 0) ended = true;
    }, 1_000);
    return () => {
      cancelled = true;
      clearInterval(syncTimer);
      clearInterval(tick);
      document.removeEventListener("visibilitychange", sync);
    };
  }, [endsAt, client]);
  return endsAt && client ? secondsLeft : 0;
}
