import { describe, expect, it } from "vitest";
import {
  calculateLaunchValue,
  classifyTransactionFailure,
  canonicalObservationState,
  claimIsExecutable,
  decodeTransactionError,
  minimumOutput,
  observeCanonicalTransaction,
  reconcileCanonicalFetch,
  parseSlippageBps,
  parseQuoteQuantity,
  parseRuntimeQuantity,
  reviewedRouterAddress,
  sameWriteIntent,
  sameReviewedWriteIntent,
  transactionDeadline,
  validateLaunchInput,
} from "./transactions";

describe("task 6 transaction math and safety", () => {
  it("requires exact launch fee plus developer buy", () => {
    expect(calculateLaunchValue(100n, 25n)).toBe(125n);
    expect(() => calculateLaunchValue(-1n, 0n)).toThrow();
  });

  it("validates names, symbols, and HTTPS metadata", () => {
    expect(
      validateLaunchInput({
        name: "",
        symbol: "bad symbol",
        imageUrl: "http://x",
        developerBuyGross: 0n,
      }),
    ).toMatchObject({
      name: "Enter a token name.",
      symbol: "Use 1–16 letters or numbers for the symbol.",
      imageUrl: "Use a valid HTTPS URL.",
    });
    expect(
      validateLaunchInput({
        name: "Token",
        symbol: "TOK",
        imageUrl: "https://x.test/image",
        developerBuyGross: 0n,
      }),
    ).toEqual({});
  });

  it("rounds minimum output down at boundaries", () => {
    expect(minimumOutput(101n, 0n)).toBe(101n);
    expect(minimumOutput(101n, 500n)).toBe(95n);
    expect(minimumOutput(1n, 10_000n)).toBe(0n);
    expect(() => minimumOutput(1n, 10_001n)).toThrow();
  });

  it("rejects malformed or negative runtime quote quantities", () => {
    expect(parseQuoteQuantity("0")).toBe(0n);
    expect(parseQuoteQuantity("100")).toBe(100n);
    expect(() => parseQuoteQuantity("01")).toThrow();
    expect(() => parseQuoteQuantity("-1")).toThrow();
    expect(() => parseQuoteQuantity("1.5")).toThrow();
    expect(parseRuntimeQuantity(12n)).toBe(12n);
    expect(() => parseRuntimeQuantity(-1n)).toThrow();
    expect(() => parseRuntimeQuantity({})).toThrow();
  });

  it("bounds slippage before render and never throws for an invalid percentage", () => {
    expect(parseSlippageBps("5")).toBe(500n);
    expect(parseSlippageBps("50")).toBe(5_000n);
    expect(parseSlippageBps("50.01")).toBeNull();
    expect(parseSlippageBps("100")).toBeNull();
    expect(parseSlippageBps("-1")).toBeNull();
    expect(parseSlippageBps("not-a-number")).toBeNull();
  });

  it("rejects invalid deadline and preserves exact simulation intent", () => {
    expect(transactionDeadline(100n, 900n)).toBe(1000n);
    expect(() => transactionDeadline(100n, 0n)).toThrow();
    const intent = {
      account: "0xAb",
      target: "0xCd",
      value: 1n,
      args: ["0x01", 2n],
      deadline: 10n,
      minimumOutput: 3n,
    };
    expect(sameWriteIntent(intent, { ...intent, account: "0xab" })).toBe(true);
    expect(sameWriteIntent(intent, { ...intent, value: 2n })).toBe(false);
  });

  it("requires a fresh explicit review when any wallet write intent field changes", () => {
    const reviewed = {
      account: "0xAb",
      target: "0xCd",
      value: 1n,
      args: [2n, 3n],
      deadline: 10n,
      minimumOutput: 3n,
      chainId: 46630,
      functionName: "buy",
    };
    expect(sameReviewedWriteIntent(reviewed, { ...reviewed, account: "0xab" })).toBe(true);
    for (const changed of [
      { minimumOutput: 2n },
      { deadline: 11n },
      { value: 2n },
      { chainId: 1 },
      { functionName: "sell" },
    ])
      expect(sameReviewedWriteIntent(reviewed, { ...reviewed, ...changed })).toBe(false);
  });

  it("maps known custom errors and redacts unknown provider payloads", () => {
    expect(
      decodeTransactionError(new Error("reverted with custom error DeadlineExpired(1,2)")),
    ).toMatchObject({ code: "DeadlineExpired" });
    const unknown = decodeTransactionError(
      new Error("secret=abc authorization Bearer hidden provider payload"),
    );
    expect(unknown.code).toBe("UnknownTransactionError");
    expect(unknown.message).not.toContain("hidden");
    expect(
      decodeTransactionError({
        cause: { details: { originalError: { data: "0x1f43b80200000000" } } },
      }),
    ).toMatchObject({ code: "DeveloperBuyCapExceeded" });
  });

  it("fails closed for routers and claims without reviewed eligibility", () => {
    expect(
      reviewedRouterAddress(
        {
          enabled: false,
          chainId: 1,
          uniswapV2Router02: "0x0000000000000000000000000000000000000001",
        },
        1,
      ),
    ).toBeNull();
    expect(
      reviewedRouterAddress(
        {
          enabled: true,
          chainId: 2,
          uniswapV2Router02: "0x0000000000000000000000000000000000000001",
        },
        1,
      ),
    ).toBeNull();
    expect(
      claimIsExecutable({ serverEligible: true, onChainEligible: true, selectedWalletReady: true }),
    ).toBe(true);
    expect(
      claimIsExecutable({
        serverEligible: false,
        onChainEligible: true,
        selectedWalletReady: true,
      }),
    ).toBe(false);
  });

  it("keeps receipt success in indexing until a canonical snapshot observes it", () => {
    const state = { status: "indexing" as const, hash: "0x123" as `0x${string}` };
    expect(canonicalObservationState(state, false).status).toBe("indexing");
    expect(canonicalObservationState(state, true).status).toBe("indexed");
    expect(canonicalObservationState({ ...state, status: "safe" }, false).status).toBe("indexing");
  });

  it("requires the exact hash in the relevant canonical record and regresses after reorg", () => {
    const state = { status: "indexing" as const, hash: "0xabc" as `0x${string}` };
    expect(
      observeCanonicalTransaction({
        state,
        submittedHash: "0xabc",
        action: "trade",
        records: [{ tx_hash: "0xdef" }],
      }).status,
    ).toBe("indexing");
    expect(
      observeCanonicalTransaction({
        state,
        submittedHash: "0xabc",
        action: "trade",
        records: [{ tx_hash: "0xABC", finality: "safe" }],
      }).status,
    ).toBe("safe");
    expect(
      observeCanonicalTransaction({
        state: { status: "safe", hash: "0xabc" },
        submittedHash: "0xabc",
        action: "trade",
        records: [],
      }).status,
    ).toBe("indexing");
    expect(
      observeCanonicalTransaction({
        state,
        submittedHash: "0xabc",
        action: "launch",
        records: [{ tx_hash: "0xabc" }],
      }).status,
    ).toBe("indexed");
  });

  it("preserves canonical state on API failure but treats authoritative absence as reorg", () => {
    const safe = { status: "safe" as const, hash: "0xabc" as `0x${string}` };
    const unavailable = reconcileCanonicalFetch({
      state: safe,
      submittedHash: safe.hash,
      action: "trade",
      outcome: "unavailable",
    });
    expect(unavailable).toMatchObject({
      status: "safe",
      hash: safe.hash,
      canonicalRefreshUnavailable: true,
    });

    const missing = reconcileCanonicalFetch({
      state: safe,
      submittedHash: safe.hash,
      action: "trade",
      outcome: "not-found",
    });
    expect(missing.status).toBe("indexing");
    expect(missing.canonicalRefreshUnavailable).toBe(false);

    const restored = reconcileCanonicalFetch({
      state: unavailable,
      submittedHash: safe.hash,
      action: "trade",
      outcome: "success",
      records: [{ tx_hash: safe.hash, finality: "safe" }],
    });
    expect(restored).toMatchObject({ status: "safe", canonicalRefreshUnavailable: false });
  });

  it("classifies wallet rejection, provider failure, and contract/receipt reverts separately", () => {
    expect(classifyTransactionFailure(new Error("User rejected the request"))).toBe(
      "rejected-signature",
    );
    expect(classifyTransactionFailure(new Error("Failed to fetch"))).toBe("rpc-failure");
    expect(classifyTransactionFailure(new Error("execution reverted: TradingPaused"))).toBe(
      "reverted",
    );
    expect(
      classifyTransactionFailure(new Error("execution reverted: UnknownEngine"), "preflight"),
    ).toBe("preflight-failure");
    expect(
      classifyTransactionFailure(new Error("execution reverted: UnknownEngine"), "simulation"),
    ).toBe("simulation-reverted");
    expect(classifyTransactionFailure(new Error("execution reverted"), "receipt")).toBe(
      "receipt-reverted",
    );
  });
});
