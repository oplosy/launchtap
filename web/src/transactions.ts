import { parseDecimal } from "@/amounts";

export type TransactionStatus =
  | "disconnected"
  | "provider-loading"
  | "wrong-chain"
  | "validating"
  | "simulating"
  | "awaiting-signature"
  | "rejected-signature"
  | "preflight-failure"
  | "simulation-reverted"
  | "receipt-reverted"
  | "submitted"
  | "rpc-failure"
  | "reverted"
  | "mined"
  | "indexing"
  | "indexed"
  | "safe"
  | "finalized";
export type TransactionState = {
  status: TransactionStatus;
  hash?: `0x${string}`;
  error?: string;
  canonicalRefreshUnavailable?: boolean;
};

export type CanonicalObservationAction = "launch" | "trade" | "claim" | "refund";
export type CanonicalObservationRecord = {
  tx_hash?: string;
  launch_tx_hash?: string;
  finality?: string;
};

/**
 * A receipt is not canonical history. This pure observer requires the submitted hash to occur in
 * the fresh, relevant API record before it can advance finality. Missing records deliberately
 * regress an earlier observation so a reorg cannot remain invisible.
 */
export function observeCanonicalTransaction(args: {
  state: TransactionState;
  submittedHash: string;
  action: CanonicalObservationAction;
  records: readonly CanonicalObservationRecord[];
  snapshotFinality?: string;
}): TransactionState {
  if (!args.submittedHash || !/^0x[0-9a-fA-F]{2,}$/.test(args.submittedHash)) return args.state;
  const submitted = args.submittedHash.toLowerCase();
  const matching = args.records.find((record) => {
    const hash =
      args.action === "launch" ? (record.launch_tx_hash ?? record.tx_hash) : record.tx_hash;
    return typeof hash === "string" && hash.toLowerCase() === submitted;
  });
  if (!matching) {
    if (["indexed", "safe", "finalized"].includes(args.state.status))
      return {
        ...args.state,
        status: "indexing",
        error: "Canonical record disappeared; refreshing after a reorganization.",
      };
    return args.state;
  }
  const finality = (matching.finality ?? args.snapshotFinality ?? "indexed").toLowerCase();
  const status: TransactionStatus =
    finality === "finalized" ? "finalized" : finality === "safe" ? "safe" : "indexed";
  return { ...args.state, status, error: undefined };
}

/**
 * Tolerances above 50% leave a minimum output so low that the trade is free value for any
 * sandwiching searcher, so the review form refuses them.
 */
export const MAX_SLIPPAGE_BPS = 5_000n;

export function parseSlippageBps(value: string): bigint | null {
  try {
    const bps = parseDecimal(value, 2);
    return bps >= 0n && bps <= MAX_SLIPPAGE_BPS ? bps : null;
  } catch {
    return null;
  }
}

export type LaunchValidation = {
  name: string;
  symbol: string;
  imageUrl?: string;
  xUrl?: string;
  telegramUrl?: string;
  developerBuyGross: bigint;
};

export type LaunchValidationErrors = Partial<
  Record<"name" | "symbol" | "imageUrl" | "xUrl" | "telegramUrl" | "developerBuyGross", string>
>;

/** The payable launch amount is exact: no rounding or caller supplied excess is accepted. */
export function calculateLaunchValue(launchFee: bigint, developerBuyGross: bigint): bigint {
  if (launchFee < 0n || developerBuyGross < 0n) throw new Error("Amounts cannot be negative");
  return launchFee + developerBuyGross;
}

export function parseQuoteQuantity(value: string, field = "quote"): bigint {
  if (!/^(?:0|[1-9]\d*)$/.test(value)) throw new Error(`Invalid ${field}`);
  return BigInt(value);
}

export function parseRuntimeQuantity(value: unknown, field = "quote"): bigint {
  if (typeof value === "bigint") {
    if (value < 0n) throw new Error(`Invalid ${field}`);
    return value;
  }
  if (typeof value === "string") return parseQuoteQuantity(value, field);
  throw new Error(`Invalid ${field}`);
}

export function validateLaunchInput(input: LaunchValidation): LaunchValidationErrors {
  const errors: LaunchValidationErrors = {};
  const name = input.name.trim();
  const symbol = input.symbol.trim();
  if (!name) errors.name = "Enter a token name.";
  else if (name.length > 64) errors.name = "Token name must be 64 characters or fewer.";
  if (!symbol) errors.symbol = "Enter a token symbol.";
  else if (!/^[A-Za-z0-9]{1,16}$/.test(symbol))
    errors.symbol = "Use 1–16 letters or numbers for the symbol.";
  for (const [key, value] of [
    ["imageUrl", input.imageUrl],
    ["xUrl", input.xUrl],
    ["telegramUrl", input.telegramUrl],
  ] as const) {
    if (!value?.trim()) continue;
    try {
      if (new URL(value).protocol !== "https:") throw new Error();
    } catch {
      errors[key] = "Use a valid HTTPS URL.";
    }
  }
  if (input.developerBuyGross < 0n) errors.developerBuyGross = "Amount cannot be negative.";
  return errors;
}

/** Floors minimum output, protecting the user at the integer boundary. */
export function minimumOutput(expectedOutput: bigint, slippageBps: bigint): bigint {
  if (expectedOutput < 0n) throw new Error("Output cannot be negative");
  if (slippageBps < 0n || slippageBps > 10_000n) throw new Error("Invalid slippage");
  return (expectedOutput * (10_000n - slippageBps)) / 10_000n;
}

export function transactionDeadline(nowSeconds: bigint, ttlSeconds: bigint): bigint {
  if (nowSeconds < 0n || ttlSeconds <= 0n) throw new Error("Invalid deadline");
  return nowSeconds + ttlSeconds;
}

export function reviewedRouterAddress(
  deployment: {
    enabled: boolean;
    chainId: number;
    uniswapV2Router02: string | null;
  },
  expectedChainId: number | null,
): `0x${string}` | null {
  if (
    !deployment.enabled ||
    deployment.chainId !== expectedChainId ||
    !deployment.uniswapV2Router02
  )
    return null;
  return /^0x[0-9a-fA-F]{40}$/.test(deployment.uniswapV2Router02)
    ? (deployment.uniswapV2Router02 as `0x${string}`)
    : null;
}

export function claimIsExecutable(args: {
  serverEligible: boolean;
  onChainEligible: boolean;
  selectedWalletReady: boolean;
}): boolean {
  return args.serverEligible && args.onChainEligible && args.selectedWalletReady;
}

export function canonicalObservationState(
  state: TransactionState,
  observed: boolean,
): TransactionState {
  return observeCanonicalTransaction({
    state,
    submittedHash: state.hash ?? "",
    action: "trade",
    records: observed ? [{ tx_hash: state.hash }] : [],
  });
}

export type ExactWrite = {
  account: string;
  target: string;
  value: bigint;
  args: readonly unknown[];
  deadline?: bigint;
  minimumOutput?: bigint;
};

export type ReviewedWriteIntent = ExactWrite & {
  chainId: number;
  functionName: string;
};

/** The wallet may be asked to sign only the intent the user has reviewed. */
export function sameReviewedWriteIntent(
  reviewed: ReviewedWriteIntent,
  candidate: ReviewedWriteIntent,
): boolean {
  return (
    reviewed.chainId === candidate.chainId &&
    reviewed.functionName === candidate.functionName &&
    sameWriteIntent(reviewed, candidate)
  );
}

export function reconcileCanonicalFetch(args: {
  state: TransactionState;
  submittedHash: string;
  action: CanonicalObservationAction;
  records?: readonly CanonicalObservationRecord[];
  snapshotFinality?: string;
  outcome: "success" | "not-found" | "unavailable";
}): TransactionState {
  if (args.outcome === "unavailable") return { ...args.state, canonicalRefreshUnavailable: true };
  return {
    ...observeCanonicalTransaction({
      state: args.state,
      submittedHash: args.submittedHash,
      action: args.action,
      records: args.records ?? [],
      snapshotFinality: args.snapshotFinality,
    }),
    canonicalRefreshUnavailable: false,
  };
}

/** Used by tests and executors to ensure simulation and signing use byte-for-byte equal intent. */
export function sameWriteIntent(left: ExactWrite, right: ExactWrite): boolean {
  if (
    left.account.toLowerCase() !== right.account.toLowerCase() ||
    left.target.toLowerCase() !== right.target.toLowerCase() ||
    left.value !== right.value ||
    left.deadline !== right.deadline ||
    left.minimumOutput !== right.minimumOutput ||
    left.args.length !== right.args.length
  )
    return false;
  return left.args.every((value, index) => equalIntent(value, right.args[index]));
}

function equalIntent(left: unknown, right: unknown): boolean {
  if (typeof left === "bigint" || typeof right === "bigint") return left === right;
  if (Array.isArray(left) && Array.isArray(right))
    return (
      left.length === right.length && left.every((value, index) => equalIntent(value, right[index]))
    );
  return left === right;
}

export type { DecodedTransactionError, TransactionFailure } from "./transaction-errors";
export { classifyTransactionFailure, decodeTransactionError } from "./transaction-errors";

export type TransactionReadiness = {
  providerReady: boolean;
  configurationReady: boolean;
  walletConnected: boolean;
  chainSupported: boolean;
  selectedAccountVerified: boolean;
  linkedWalletState:
    | "configuration-unavailable"
    | "provider-loading"
    | "unlinked"
    | "connected-unlinked"
    | "linked"
    | "linked-mismatch";
};
export type TransactionEvent = {
  type:
    | "provider-loading"
    | "disconnected"
    | "wrong-chain"
    | "validate"
    | "simulate"
    | "await-signature"
    | "rejected-signature"
    | "submitted"
    | "rpc-failure"
    | "reverted"
    | "mined"
    | "indexing"
    | "indexed"
    | "safe"
    | "finalized";
  hash?: `0x${string}`;
  error?: string;
};

const transitions: Record<TransactionStatus, readonly TransactionStatus[]> = {
  "provider-loading": ["disconnected", "wrong-chain"],
  disconnected: ["provider-loading", "validating"],
  "wrong-chain": ["provider-loading", "disconnected", "validating"],
  validating: ["simulating", "disconnected", "wrong-chain", "rpc-failure"],
  simulating: ["awaiting-signature", "rpc-failure", "reverted"],
  "awaiting-signature": ["rejected-signature", "submitted", "rpc-failure"],
  "rejected-signature": ["validating", "disconnected"],
  "preflight-failure": ["validating", "disconnected", "wrong-chain"],
  "simulation-reverted": ["validating", "disconnected"],
  "receipt-reverted": ["validating", "disconnected"],
  submitted: ["mined", "reverted", "rpc-failure"],
  "rpc-failure": ["validating", "disconnected", "wrong-chain"],
  reverted: ["validating", "disconnected"],
  mined: ["indexing"],
  indexing: ["indexed", "rpc-failure"],
  indexed: ["safe"],
  safe: ["finalized"],
  finalized: [],
};

export function createTransactionState(
  status: TransactionStatus = "disconnected",
): TransactionState {
  return { status };
}

export function transitionTransaction(
  state: TransactionState,
  event: TransactionEvent,
  readiness?: TransactionReadiness,
): TransactionState {
  const next: TransactionStatus =
    event.type === "validate"
      ? "validating"
      : event.type === "simulate"
        ? "simulating"
        : event.type === "await-signature"
          ? "awaiting-signature"
          : event.type;
  if (
    (event.type === "validate" ||
      event.type === "simulate" ||
      event.type === "await-signature" ||
      event.type === "submitted") &&
    !isTransactionReady(readiness)
  )
    throw new Error("Transaction requires verified wallet and configuration readiness");
  if (!transitions[state.status].includes(next))
    throw new Error(`Invalid transaction transition: ${state.status} -> ${next}`);
  return { status: next, hash: event.hash ?? state.hash, error: event.error };
}

export function isTransactionReady(readiness: TransactionReadiness | undefined): boolean {
  return Boolean(
    readiness?.providerReady &&
    readiness.configurationReady &&
    readiness.walletConnected &&
    readiness.chainSupported &&
    readiness.selectedAccountVerified &&
    readiness.linkedWalletState === "linked",
  );
}

export function canTransitionTransaction(from: TransactionStatus, to: TransactionStatus) {
  return transitions[from].includes(to);
}
