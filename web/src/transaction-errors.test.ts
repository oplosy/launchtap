import { describe, expect, it } from "vitest";
import { contractMessages, panicMessages, walletMessages } from "@/errors/catalog";
import { decodeTransactionError } from "./transaction-errors";

const named = (name: string, message = "failed", extra: object = {}) =>
  Object.assign(new Error(message), { name, ...extra });

describe("decodeTransactionError wallet and RPC kinds", () => {
  const cases: [string, unknown][] = [
    ["user-rejected", { code: 4001, message: "x" }],
    ["user-rejected", named("UserRejectedRequestError")],
    ["user-rejected", new Error("User denied transaction signature")],
    ["switch-failed", named("SwitchChainError")],
    ["switch-failed", new Error("Switching chain failed")],
    ["chain-not-added", { code: 4902, message: "x" }],
    ["chain-not-added", new Error("Unrecognized chain ID 46630")],
    ["chain-mismatch", named("ChainMismatchError")],
    ["chain-mismatch", new Error("The current chain does not match the target chain")],
    ["insufficient-funds", named("InsufficientFundsError")],
    ["insufficient-funds", new Error("insufficient funds for gas * price + value")],
    ["nonce-too-low", new Error("nonce too low")],
    ["nonce-too-high", named("NonceMaxValueError")],
    ["nonce-too-high", new Error("nonce too high")],
    ["replacement-underpriced", new Error("replacement transaction underpriced")],
    ["fee-too-high", named("TipAboveFeeCapError")],
    ["fee-too-low", named("FeeCapTooLowError")],
    ["fee-too-low", new Error("max fee per gas less than block base fee")],
    ["gas-too-low", new Error("intrinsic gas too low")],
    ["gas-too-high", new Error("exceeds block gas limit")],
    ["wallet-busy", { code: -32002, message: "x" }],
    ["wallet-busy", new Error("Request already pending")],
    ["wallet-unauthorized", { code: 4100, message: "x" }],
    ["wallet-unsupported", named("MethodNotSupportedRpcError")],
    ["wallet-unsupported", { code: 4200, message: "x" }],
    ["wallet-disconnected", { code: 4900, message: "x" }],
    ["wallet-disconnected", named("ConnectorNotConnectedError")],
    ["rpc-rate-limited", { code: -32005, message: "x" }],
    ["rpc-rate-limited", new Error("429 Too Many Requests")],
    ["rpc-timeout", named("WaitForTransactionReceiptTimeoutError")],
    ["rpc-timeout", new Error("The request timed out")],
    ["rpc-unreachable", named("HttpRequestError")],
    ["rpc-unreachable", new TypeError("Failed to fetch")],
    ["rpc-unavailable", { code: -32603, message: "x" }],
    ["execution-reverted", named("ContractFunctionRevertedError")],
    ["execution-reverted", new Error("execution reverted")],
  ];

  it.each(cases)("recognises %s", (kind, cause) => {
    expect(decodeTransactionError(cause)).toEqual({
      code: kind,
      message: walletMessages[kind as keyof typeof walletMessages],
    });
  });

  it("walks nested causes and ignores cycles", () => {
    const inner: Record<string, unknown> = { code: 4001 };
    inner.self = inner;
    const outer = Object.assign(new Error("Request failed"), { cause: { details: [inner] } });
    expect(decodeTransactionError(outer).code).toBe("user-rejected");
  });

  it("keeps certain causes certain after submission", () => {
    expect(decodeTransactionError(new Error("execution reverted"), { submitted: true }).code).toBe(
      "execution-reverted",
    );
    expect(decodeTransactionError({ code: 4001 }, { submitted: true }).code).toBe("user-rejected");
    expect(decodeTransactionError(new Error("odd"), { submitted: true }).code).toBe(
      "outcome-unknown",
    );
  });

  it("falls back without leaking provider text", () => {
    const decoded = decodeTransactionError(new Error("rpc payload with secret"));
    expect(decoded).toEqual({ code: "UnknownTransactionError", message: walletMessages.unknown });
  });
});

describe("decodeTransactionError contract failures", () => {
  it("decodes viem's errorName and args", () => {
    expect(decodeTransactionError({ errorName: "WrongPhase", args: [] }).code).toBe("WrongPhase");
    expect(decodeTransactionError({ errorName: "Panic", args: [0x11n] })).toEqual({
      code: "Panic(0x11)",
      message: panicMessages[0x11],
    });
    expect(
      decodeTransactionError({ errorName: "Error", args: ["UniswapV2Router: EXPIRED"] }).code,
    ).toBe("UniswapV2Router: EXPIRED");
    expect(decodeTransactionError({ errorName: "Panic", args: [0x99n] }).code).toBe(
      "UnknownTransactionError",
    );
  });

  it("decodes a Panic or Error(string) from text", () => {
    expect(decodeTransactionError(new Error("reverted: panic code 0x12")).message).toBe(
      panicMessages[0x12],
    );
    expect(decodeTransactionError(new Error("Panic(17)")).message).toBe(panicMessages[0x11]);
    expect(
      decodeTransactionError(new Error("reverted: UniswapV2Router: INSUFFICIENT_OUTPUT_AMOUNT"))
        .code,
    ).toBe("INSUFFICIENT_OUTPUT_AMOUNT");
  });

  it("names an error from its selector when the data is truncated", () => {
    expect(decodeTransactionError({ data: "0x1f43b80200" }).code).toBe("DeveloperBuyCapExceeded");
    expect(decodeTransactionError({ data: "0xdeadbeef" }).code).toBe("UnknownTransactionError");
  });

  it("recognises a sentence it wrote before", () => {
    expect(decodeTransactionError(new Error(contractMessages.WrongPhase)).message).toBe(
      contractMessages.WrongPhase,
    );
  });
});
