// Classifies and decodes wallet, RPC and contract failures for the transaction UI.

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
  if (phase === "receipt") return "receipt-reverted";
  if (
    /custom error|revert|execution reverted|slippage|deadline|wrongphase|nothingtoclaim/i.test(text)
  )
    return "reverted";
  return "rpc-failure";
}

/** Decode only known, versioned contract errors. Provider payloads are never returned to UI. */
export function decodeTransactionError(cause: unknown): DecodedTransactionError {
  const textParts: string[] = [];
  const seen = new Set<unknown>();
  const collect = (value: unknown, depth: number) => {
    if (value === null || value === undefined || depth > 8 || seen.has(value)) return;
    if (typeof value === "string") {
      textParts.push(value);
      return;
    }
    if (Array.isArray(value)) {
      for (const item of value) collect(item, depth + 1);
      return;
    }
    if (typeof value !== "object") return;
    seen.add(value);
    const record = value as Record<string, unknown>;
    for (const key of [
      "message",
      "shortMessage",
      "details",
      "reason",
      "data",
      "errorName",
      "name",
      "metaMessages",
    ]) {
      collect(record[key], depth + 1);
    }
    collect(record.cause, depth + 1);
    // viem/provider errors can put the revert data under an implementation-specific
    // nested key. Traversing the remaining values keeps decoding allowlisted errors
    // without exposing provider messages or payloads to the UI.
    for (const [key, nested] of Object.entries(record)) {
      if (
        key !== "message" &&
        key !== "shortMessage" &&
        key !== "details" &&
        key !== "reason" &&
        key !== "data" &&
        key !== "errorName" &&
        key !== "name" &&
        key !== "metaMessages" &&
        key !== "cause"
      )
        collect(nested, depth + 1);
    }
  };
  collect(cause, 0);
  const text = textParts.join(" ");
  const messages: Record<string, string> = {
    DeadlineExpired: "The deadline expired. Refresh the quote and try again.",
    LaunchValueMismatch: "The launch value changed. Refresh the factory fee and try again.",
    LaunchesPaused: "Launching is paused by the reviewed deployment.",
    TradingPaused: "Buying is paused by the reviewed deployment. Selling remains available.",
    InvalidRecipient: "Tokens cannot be sent to the curve or its liquidity pair.",
    WrongPhase: "This token changed phase. Refresh the token before trading.",
    SlippageExceeded:
      "Price moved beyond your slippage limit. Increase the limit only if you accept the risk.",
    Oversell: "The requested token amount exceeds the curve balance.",
    NothingToClaim: "There is no claimable balance for this wallet.",
    ERC20InsufficientAllowance:
      "Approval is lower than this sell amount. Approve the disclosed amount and resume.",
    ERC20InsufficientBalance: "The selected wallet does not have enough tokens for this action.",
    InsufficientETHBalance: "The selected wallet does not have enough ETH for this action.",
    ReceiptReverted: "The transaction was mined but reverted. No state change was accepted.",
    QuoteChanged:
      "The on-chain quote changed before signing. Refresh and review the new minimum output.",
    UnknownEngine: "This launch engine is not supported by the reviewed factory.",
    EngineDisabled: "This launch engine is disabled by the reviewed factory.",
    EngineVersionMismatch:
      "The selected engine version does not match the reviewed implementation.",
    DeveloperBuyCapExceeded:
      "The developer buy exceeds the contract cap. Reduce the amount and retry.",
    UnauthorizedCreatorClaim: "Only the linked creator wallet can claim these fees.",
  };
  const findErrorName = (
    value: unknown,
    depth: number,
    visited: Set<object>,
  ): string | undefined => {
    if (depth > 8 || value === null || typeof value !== "object" || visited.has(value)) return;
    visited.add(value);
    const record = value as Record<string, unknown>;
    if (typeof record.errorName === "string") return record.errorName;
    for (const key of Object.getOwnPropertyNames(record)) {
      const found = findErrorName(record[key], depth + 1, visited);
      if (found) return found;
    }
    return;
  };
  const nestedErrorName = findErrorName(cause, 0, new Set<object>());
  const knownMessageCode = Object.keys(messages).find((name) =>
    text.includes(messages[name] ?? ""),
  );
  const code =
    nestedErrorName && messages[nestedErrorName]
      ? nestedErrorName
      : (Object.keys(messages).find((name) => text.includes(name)) ??
        knownMessageCode ??
        (text.toLowerCase().includes("0x1f43b802")
          ? "DeveloperBuyCapExceeded"
          : "UnknownTransactionError"));
  return {
    code: messages[code] ? code : "UnknownTransactionError",
    message:
      messages[code] ?? "The wallet or network rejected this transaction. No funds were moved.",
  };
}
