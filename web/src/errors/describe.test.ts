import { describe, expect, it } from "vitest";
import { encodeErrorResult, parseAbi } from "viem";
import { ApiProblem, mapProblem, networkProblem } from "@/api/problems";
import { apiMessages, contextMessages, contractMessages, walletMessages } from "./catalog";
import { describeError, UserFacingError } from "./describe";

const problem = (status: number, code: string, requestId: string | null = null) =>
  mapProblem(status, { type: `urn:launchpad:problem:${code}`, detail: "server text" }, requestId);

describe("describeError", () => {
  it("shows an API code in the catalog's words, never the server's detail", () => {
    const described = describeError(problem(409, "username_taken"));
    expect(described).toEqual({ code: "username_taken", message: apiMessages.username_taken });
    expect(described.message).not.toContain("server text");
  });

  it("prefers the context's words and falls back to its parent context", () => {
    expect(describeError(problem(403, "not_holder"), { context: "vote" }).message).toBe(
      contextMessages.vote.not_holder,
    );
    expect(describeError(problem(422, "invalid_comment"), { context: "vote" }).message).toBe(
      contextMessages.comment.invalid_comment,
    );
  });

  it("gives an unknown code the context fallback plus a short request reference", () => {
    const described = describeError(problem(418, "brand_new_code", "abcdef0123456789xyz"), {
      context: "vote",
    });
    expect(described.message).toBe(`${contextMessages.vote.fallback} Reference: abcdef012345.`);
  });

  it("adds a reference to failures only support can act on", () => {
    expect(describeError(problem(500, "internal_error", "req-1234567890abc")).message).toBe(
      `${apiMessages.internal_error} Reference: req-12345678.`,
    );
    expect(describeError(problem(404, "token_not_found", "req-1")).message).toBe(
      apiMessages.token_not_found,
    );
  });

  it("words a response without a problem body by its status", () => {
    expect(mapProblem(429, null).message).toBe(apiMessages.rate_limited);
    expect(mapProblem(502, "<html>").message).toBe(apiMessages.internal_error);
    expect(mapProblem(422, { type: "about:blank" }).message).toBe(apiMessages.invalid_request);
  });

  it("says when the server could not be reached", () => {
    expect(describeError(networkProblem()).message).toBe(apiMessages.network_unreachable);
    expect(describeError(networkProblem(), { context: "comment" }).message).toBe(
      apiMessages.network_unreachable,
    );
  });

  it("shows app-written messages as written", () => {
    expect(describeError(new UserFacingError("Pick a file first.")).message).toBe(
      "Pick a file first.",
    );
  });

  it("does not leak a raw exception in an API-backed context", () => {
    expect(describeError(new TypeError("x is undefined"), { context: "telegram" }).message).toBe(
      contextMessages.telegram.fallback,
    );
  });

  it("decodes contract reverts by name or revert data", () => {
    expect(describeError(new Error("reverted with SlippageExceeded()")).code).toBe(
      "SlippageExceeded",
    );
    const data = encodeErrorResult({
      abi: parseAbi(["error SlippageExceeded(uint256 actual, uint256 limit)"]),
      errorName: "SlippageExceeded",
      args: [1n, 2n],
    });
    const described = describeError(Object.assign(new Error("call failed"), { data }));
    expect(described.message).toBe(contractMessages.SlippageExceeded);
  });

  it("does not claim nothing was sent once a transaction hash exists", () => {
    const timeout = new Error("Timed out while waiting for transaction");
    expect(describeError(timeout, { submitted: true }).message).toBe(
      walletMessages["outcome-unknown"],
    );
    expect(describeError(new Error("ReceiptReverted"), { submitted: true }).code).toBe(
      "ReceiptReverted",
    );
  });

  it("recognises a wallet rejection", () => {
    const rejected = Object.assign(new Error("User rejected the request."), { code: 4001 });
    expect(describeError(rejected).message).toBe(walletMessages["user-rejected"]);
  });

  it("never returns an empty message", () => {
    for (const cause of [
      undefined,
      null,
      42,
      "text",
      {},
      new Error(""),
      new ApiProblem(0, "unknown"),
    ])
      expect(describeError(cause).message.length).toBeGreaterThan(0);
  });
});
