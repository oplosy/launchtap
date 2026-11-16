// Classifies and decodes wallet, RPC and contract failures for the transaction UI. The words come
// from errors/catalog.ts (docs/specs/2026-11-16-user-error-messages-preflight.md).
import { decodeErrorResult, toFunctionSelector, type Abi } from "viem";
import { browserAbis } from "@/contracts/generated";
import {
  contractMessages,
  panicMessages,
  revertReasonMessages,
  walletMessages,
  type WalletFailureKind,
} from "@/errors/catalog";

export type DecodedTransactionError = { code: string; message: string };

export type TransactionFailure =
  | "rejected-signature"
  | "preflight-failure"
  | "simulation-reverted"
  | "receipt-reverted"
  | "rpc-failure"
  | "reverted";

export function classifyTransactionFailure(
  cause: unknown,
  phase: "preflight" | "simulation" | "write" | "receipt" = "write",
): TransactionFailure {
  const text = cause instanceof Error ? cause.message : String(cause ?? "");
  if (/user rejected|user denied|rejected|denied signature|4001/i.test(text))
    return "rejected-signature";
  if (phase === "preflight") return "preflight-failure";
  if (phase === "simulation") return "simulation-reverted";
  // Only a mined receipt with a failed status is a revert. A transport error or timeout while
  // waiting leaves the outcome unknown, so it must not release the submission as reverted.
  if (phase === "receipt") return /ReceiptReverted/.test(text) ? "receipt-reverted" : "rpc-failure";
  if (
    /custom error|revert|execution reverted|slippage|deadline|wrongphase|nothingtoclaim/i.test(text)
  )
    return "reverted";
  return "rpc-failure";
}

/** Every custom error in the reviewed browser ABIs, once, for decoding raw revert data. */
const errorAbi: Abi = (() => {
  const seen = new Set<string>();
  const items: Abi[number][] = [];
  for (const abi of Object.values(browserAbis) as readonly (readonly Abi[number][])[]) {
    for (const item of abi) {
      if (item.type !== "error") continue;
      const key = `${item.name}(${item.inputs.map((input) => input.type).join(",")})`;
      if (seen.has(key)) continue;
      seen.add(key);
      items.push(item);
    }
  }
  return items;
})();

/** 4-byte selector to error name, for revert data too short or malformed to decode fully. */
const selectorNames = new Map(
  errorAbi.flatMap((item) =>
    item.type === "error"
      ? [
          [
            toFunctionSelector(`${item.name}(${item.inputs.map((input) => input.type).join(",")})`),
            item.name,
          ] as const,
        ]
      : [],
  ),
);

/** Contract names, longest first, so `ERC20InsufficientAllowance` wins over `InsufficientAllowance`. */
const contractNames = Object.keys(contractMessages).sort((a, b) => b.length - a.length);

/** A catalog sentence found in the text maps back to its code (a stored state re-rendered). */
const knownSentences = new Map<string, string>([
  ...Object.entries(contractMessages).map(([code, message]) => [message, code] as const),
  ...Object.entries(walletMessages).map(([kind, message]) => [message, kind] as const),
]);

type Evidence = {
  texts: string[];
  names: string[];
  codes: number[];
  errorNames: { name: string; args: readonly unknown[] }[];
  revertData: `0x${string}`[];
};

/** Walks a viem/provider error and its causes, collecting what decoding needs. */
function collectEvidence(cause: unknown): Evidence {
  const evidence: Evidence = { texts: [], names: [], codes: [], errorNames: [], revertData: [] };
  const seen = new Set<unknown>();
  const visit = (value: unknown, depth: number) => {
    if (value === null || value === undefined || depth > 8 || seen.has(value)) return;
    if (typeof value === "string") {
      evidence.texts.push(value);
      if (/^0x[0-9a-fA-F]{8,}$/.test(value)) evidence.revertData.push(value as `0x${string}`);
      return;
    }
    if (typeof value !== "object") return;
    seen.add(value);
    if (Array.isArray(value)) {
      for (const item of value) visit(item, depth + 1);
      return;
    }
    const record = value as Record<string, unknown>;
    if (typeof record.name === "string") evidence.names.push(record.name);
    if (typeof record.code === "number") evidence.codes.push(record.code);
    if (typeof record.errorName === "string")
      evidence.errorNames.push({
        name: record.errorName,
        args: Array.isArray(record.args) ? record.args : [],
      });
    // Own and non-enumerable properties: viem keeps `cause` and details on the instance.
    for (const key of Object.getOwnPropertyNames(record)) {
      if (key === "stack") continue;
      visit(record[key], depth + 1);
    }
  };
  visit(cause, 0);
  return evidence;
}

function contractCode(name: string, args: readonly unknown[]): DecodedTransactionError | null {
  const known = contractMessages[name];
  if (known) return { code: name, message: known };
  if (name === "Panic" && typeof args[0] === "bigint") {
    const message = panicMessages[Number(args[0])];
    if (message) return { code: `Panic(0x${args[0].toString(16)})`, message };
  }
  if (name === "Error" && typeof args[0] === "string") return revertReason(args[0]);
  return null;
}

function revertReason(text: string): DecodedTransactionError | null {
  for (const [reason, message] of revertReasonMessages)
    if (text.includes(reason)) return { code: reason, message };
  return null;
}

function walletKind(evidence: Evidence, text: string): WalletFailureKind | null {
  const { names, codes } = evidence;
  const named = (...candidates: string[]) => candidates.some((name) => names.includes(name));
  if (codes.includes(4001) || named("UserRejectedRequestError") || /user rejected|user denied|denied transaction signature/i.test(text))
    return "user-rejected";
  if (named("SwitchChainError") || /switch(ing)? chain.*(failed|rejected)/i.test(text))
    return "switch-failed";
  if (codes.includes(4902) || /unrecognized chain|chain .*not (been )?added/i.test(text))
    return "chain-not-added";
  if (named("ChainMismatchError", "ConnectorChainMismatchError") || /does not match the target chain|chain mismatch/i.test(text))
    return "chain-mismatch";
  if (named("InsufficientFundsError") || /insufficient funds/i.test(text)) return "insufficient-funds";
  if (named("NonceTooLowError") || /nonce too low/i.test(text)) return "nonce-too-low";
  if (named("NonceTooHighError", "NonceMaxValueError") || /nonce too high/i.test(text))
    return "nonce-too-high";
  if (/replacement (transaction )?underpriced/i.test(text)) return "replacement-underpriced";
  if (named("FeeCapTooHighError", "TipAboveFeeCapError")) return "fee-too-high";
  if (named("FeeCapTooLowError") || /max fee per gas less than block base fee|fee cap.*too low/i.test(text))
    return "fee-too-low";
  if (named("IntrinsicGasTooLowError") || /intrinsic gas too low/i.test(text)) return "gas-too-low";
  if (named("IntrinsicGasTooHighError") || /exceeds block gas limit/i.test(text)) return "gas-too-high";
  if (codes.includes(-32002) || named("ResourceUnavailableRpcError") || /already pending/i.test(text))
    return "wallet-busy";
  if (codes.includes(4100) || named("UnauthorizedProviderError")) return "wallet-unauthorized";
  if (codes.includes(4200) || named("UnsupportedProviderMethodError", "MethodNotSupportedRpcError"))
    return "wallet-unsupported";
  if (
    codes.includes(4900) ||
    codes.includes(4901) ||
    named("ProviderDisconnectedError", "ChainDisconnectedError", "ConnectorNotConnectedError")
  )
    return "wallet-disconnected";
  if (codes.includes(-32005) || codes.includes(429) || named("LimitExceededRpcError") || /too many requests|rate limit/i.test(text))
    return "rpc-rate-limited";
  if (named("TimeoutError", "WaitForTransactionReceiptTimeoutError") || /timed? ?out/i.test(text))
    return "rpc-timeout";
  if (named("HttpRequestError", "WebSocketRequestError") || /failed to fetch|fetch failed|network ?error|ECONNREFUSED|ENOTFOUND/i.test(text))
    return "rpc-unreachable";
  if (named("InternalRpcError") || codes.includes(-32603)) return "rpc-unavailable";
  if (named("ExecutionRevertedError", "ContractFunctionRevertedError") || /execution reverted|reverted/i.test(text))
    return "execution-reverted";
  return null;
}

/**
 * Turns any wallet, RPC or contract failure into one catalog sentence. Provider payloads are
 * never returned to the UI. `submitted` says a transaction hash exists: an unexplained failure
 * then means the outcome is unknown, never that nothing was sent.
 */
export function decodeTransactionError(
  cause: unknown,
  options: { submitted?: boolean } = {},
): DecodedTransactionError {
  const evidence = collectEvidence(cause);
  for (const { name, args } of evidence.errorNames) {
    const decoded = contractCode(name, args);
    if (decoded) return decoded;
  }
  for (const data of evidence.revertData) {
    try {
      const { errorName, args } = decodeErrorResult({ abi: errorAbi, data });
      const decoded = contractCode(errorName, args ?? []);
      if (decoded) return decoded;
    } catch {
      // Not full revert data of a reviewed error; the selector alone may still name it.
      const name = selectorNames.get(data.slice(0, 10).toLowerCase() as `0x${string}`);
      const decoded = name ? contractCode(name, []) : null;
      if (decoded) return decoded;
    }
  }
  const text = evidence.texts.join(" ");
  for (const [sentence, code] of knownSentences)
    if (text.includes(sentence)) return { code, message: sentence };
  const name = contractNames.find((candidate) => new RegExp(`\\b${candidate}\\b`).test(text));
  const named = name ? contractCode(name, []) : null;
  if (named) return named;
  const reason = revertReason(text);
  if (reason) return reason;
  const panic = /panic[^0-9]*(?:0x([0-9a-f]+)|\((\d+)\))/i.exec(text);
  if (panic) {
    const decoded = contractCode("Panic", [BigInt(panic[1] ? `0x${panic[1]}` : (panic[2] ?? "0"))]);
    if (decoded) return decoded;
  }
  const kind = walletKind(evidence, text);
  // After a hash exists, only a known cause or a mined revert is certain; anything else may
  // still confirm.
  if (options.submitted && kind !== "user-rejected" && kind !== "execution-reverted")
    return { code: "outcome-unknown", message: walletMessages["outcome-unknown"] };
  if (kind) return { code: kind, message: walletMessages[kind] };
  return { code: "UnknownTransactionError", message: walletMessages.unknown };
}
