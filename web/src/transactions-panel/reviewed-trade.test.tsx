// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { useState } from "react";
import type { Abi } from "viem";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReviewedWriteIntent, TransactionState } from "@/transactions";

const CHAIN_ID = 31337;
const ACCOUNT = "0x00000000000000000000000000000000000000a1";
const OTHER_ACCOUNT = "0x00000000000000000000000000000000000000b2";
const TOKEN = "0x00000000000000000000000000000000000000c3";
const TARGET = "0x00000000000000000000000000000000000000d4";
const TRADE_HASH = `0x${"11".repeat(32)}`;
const APPROVAL_HASH = `0x${"22".repeat(32)}`;
const ABI = [] as unknown as Abi;

/** The mocked wallet and chain. Each test resets it in `beforeEach` and adjusts what it needs. */
const wallet = {
  connectedAddress: ACCOUNT as string | undefined,
  walletAccounts: [ACCOUNT] as string[],
  chainId: CHAIN_ID,
  verified: true,
  persistedReady: { trade: true, approval: true } as Record<string, boolean>,
  publicClient: {
    getChainId: vi.fn(),
    simulateContract: vi.fn(),
    waitForTransactionReceipt: vi.fn(),
  },
  walletClient: {
    getAddresses: vi.fn(),
    writeContract: vi.fn(),
  },
};

vi.mock("wagmi", () => ({
  useAccount: () => ({ address: wallet.connectedAddress }),
  usePublicClient: () => wallet.publicClient,
  useWalletClient: () => ({ data: wallet.walletClient }),
}));

vi.mock("@/wallet/readiness", () => ({
  useWalletReadiness: () => ({
    address: wallet.connectedAddress,
    chainId: wallet.chainId,
    selectedAccountVerified: wallet.verified,
    switchNetwork: vi.fn(),
  }),
}));

vi.mock("@/wallet/connection-context", () => ({ WalletConnectButton: () => null }));
vi.mock("@/market/usd-line", () => ({ TradeUsdHint: () => null }));

vi.mock("@/config/public", () => ({
  publicConfiguration: () => ({ chainId: CHAIN_ID, apiBaseUrl: null, deployment: null }),
}));

vi.mock("./shared", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./shared")>();
  return {
    ...actual,
    /** In-memory stand-in: no API polling and no browser storage, readiness set per test. */
    usePersistedTransactionState: (action: string) => {
      const [state, setState] = useState<TransactionState>({ status: "validating" });
      return { state, setState, ready: wallet.persistedReady[action] ?? true };
    },
  };
});

const { useReviewedTrade, useReviewedWrite } = await import("./reviewed-trade");

function intent(overrides: Partial<ReviewedWriteIntent> = {}): ReviewedWriteIntent {
  return {
    account: ACCOUNT,
    target: TARGET,
    value: 10n,
    args: [1n, 2n],
    chainId: CHAIN_ID,
    functionName: "buy",
    ...overrides,
  };
}

beforeEach(() => {
  wallet.connectedAddress = ACCOUNT;
  wallet.walletAccounts = [ACCOUNT];
  wallet.chainId = CHAIN_ID;
  wallet.verified = true;
  wallet.persistedReady = { trade: true, approval: true };
  wallet.publicClient.getChainId.mockImplementation(async () => wallet.chainId);
  wallet.publicClient.simulateContract.mockResolvedValue({});
  wallet.publicClient.waitForTransactionReceipt.mockResolvedValue({ status: "success" });
  wallet.walletClient.getAddresses.mockImplementation(async () => wallet.walletAccounts);
  wallet.walletClient.writeContract.mockResolvedValue(TRADE_HASH);
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("useReviewedWrite", () => {
  it("is wallet-ready only on the configured chain with a verified account", () => {
    const { result, rerender } = renderHook(() => useReviewedWrite("trade", TOKEN));
    expect(result.current.walletReady).toBe(true);

    wallet.chainId = 1;
    rerender();
    expect(result.current.walletReady).toBe(false);

    wallet.chainId = CHAIN_ID;
    wallet.verified = false;
    rerender();
    expect(result.current.walletReady).toBe(false);
  });

  it("signs exactly the reviewed write and waits for its receipt", async () => {
    const { result } = renderHook(() => useReviewedWrite("trade", TOKEN));
    const reviewed = intent();
    act(() => result.current.openReview(reviewed));
    expect(result.current.confirming).toBe(true);

    await act(() =>
      result.current.runExecution(async (execution) => {
        const receipt = await execution.sign(reviewed, {
          abi: ABI,
          requote: async () => ({ intent: intent() }),
        });
        expect(receipt).toEqual({ status: "success" });
      }),
    );

    const expectedRequest = {
      address: TARGET,
      abi: ABI,
      functionName: "buy",
      account: ACCOUNT,
      args: [1n, 2n],
      value: 10n,
    };
    expect(wallet.publicClient.simulateContract).toHaveBeenCalledWith(expectedRequest);
    expect(wallet.walletClient.writeContract).toHaveBeenCalledTimes(1);
    expect(wallet.walletClient.writeContract).toHaveBeenCalledWith(expectedRequest);
    expect(result.current.state).toEqual({ status: "indexing", hash: TRADE_HASH });
    expect(result.current.confirming).toBe(false);
    expect(result.current.reviewIntent).toBeNull();
    expect(result.current.executionBusy).toBe(false);
  });

  it("does not sign when the fresh read differs from the review", async () => {
    const { result } = renderHook(() => useReviewedWrite("trade", TOKEN));
    const reviewed = intent();
    const fresh = intent({ value: 11n });
    const replace = vi.fn();
    act(() => result.current.openReview(reviewed));

    await act(() =>
      result.current.runExecution(async (execution) => {
        const receipt = await execution.sign(reviewed, {
          abi: ABI,
          requote: async () => ({ intent: fresh, replace }),
        });
        expect(receipt).toBeUndefined();
      }),
    );

    expect(wallet.walletClient.writeContract).not.toHaveBeenCalled();
    expect(replace).toHaveBeenCalledTimes(1);
    expect(result.current.reviewIntent).toEqual(fresh);
    expect(result.current.reviewNotice).toMatch(/write details changed/);
    expect(result.current.state).toEqual({ status: "validating" });
  });

  it("stops before signing when the wallet account changes during simulation", async () => {
    const { result } = renderHook(() => useReviewedWrite("trade", TOKEN));
    const reviewed = intent();
    wallet.publicClient.simulateContract.mockImplementation(async () => {
      wallet.walletAccounts = [OTHER_ACCOUNT];
      return {};
    });

    await act(() =>
      result.current.runExecution(async (execution) => {
        await execution.sign(reviewed, { abi: ABI, requote: async () => ({ intent: intent() }) });
      }),
    );

    expect(wallet.walletClient.writeContract).not.toHaveBeenCalled();
    expect(result.current.reviewNotice).toMatch(/changed during simulation/);
    expect(result.current.state).toEqual({ status: "validating" });
  });

  it("reports a rejected signature as a failure without a hash", async () => {
    const { result } = renderHook(() => useReviewedWrite("trade", TOKEN));
    wallet.walletClient.writeContract.mockRejectedValue(new Error("User rejected the request."));

    await act(() =>
      result.current.runExecution(async (execution) => {
        await execution.sign(intent(), { abi: ABI, requote: async () => ({ intent: intent() }) });
      }),
    );

    expect(result.current.state.status).toBe("rejected-signature");
    expect(result.current.state.hash).toBeUndefined();
    expect(result.current.executionBusy).toBe(false);
  });

  it("reports a reverted receipt against the submitted hash", async () => {
    const { result } = renderHook(() => useReviewedWrite("trade", TOKEN));
    wallet.publicClient.waitForTransactionReceipt.mockResolvedValue({ status: "reverted" });

    await act(() =>
      result.current.runExecution(async (execution) => {
        await execution.sign(intent(), { abi: ABI, requote: async () => ({ intent: intent() }) });
      }),
    );

    expect(result.current.state.status).toBe("receipt-reverted");
    expect(result.current.state.hash).toBe(TRADE_HASH);
  });

  it("runs one execution at a time", async () => {
    const { result } = renderHook(() => useReviewedWrite("trade", TOKEN));
    let release!: () => void;
    const first = vi.fn(() => new Promise<void>((resolve) => (release = resolve)));
    const second = vi.fn(async () => {});

    let firstRun!: Promise<void>;
    act(() => {
      firstRun = result.current.runExecution(first);
    });
    await waitFor(() => expect(result.current.executionBusy).toBe(true));
    await act(() => result.current.runExecution(second));
    expect(second).not.toHaveBeenCalled();

    await act(async () => {
      release();
      await firstRun;
    });
    expect(first).toHaveBeenCalledTimes(1);
    expect(result.current.executionBusy).toBe(false);
  });
});

describe("useReviewedTrade", () => {
  it("parses the amount and slippage and closes the review on input changes", () => {
    const { result } = renderHook(() => useReviewedTrade(TOKEN));
    act(() => result.current.openReview(intent()));
    act(() => result.current.changeInput("1.5"));

    expect(result.current.inputUnits).toBe(1_500_000_000_000_000_000n);
    expect(result.current.confirming).toBe(false);
    expect(result.current.reviewIntent).toBeNull();

    act(() => result.current.changeInput("not a number"));
    expect(result.current.inputUnits).toBeNull();

    act(() => result.current.changeSlippage("1"));
    expect(result.current.slippageBps).toBe(100n);
  });

  it("clears the amount when the side changes", () => {
    const { result } = renderHook(() => useReviewedTrade(TOKEN));
    act(() => result.current.changeInput("2"));
    act(() => result.current.changeSide("sell"));

    expect(result.current.side).toBe("sell");
    expect(result.current.input).toBe("");
    expect(result.current.inputUnits).toBeNull();
  });

  it("needs the persisted approval state before a sell is wallet-ready", () => {
    wallet.persistedReady = { trade: true, approval: false };
    const { result } = renderHook(() => useReviewedTrade(TOKEN));
    expect(result.current.walletReady).toBe(true);

    act(() => result.current.changeSide("sell"));
    expect(result.current.walletReady).toBe(false);
  });

  it("mines an exact approval, then closes the review for a fresh trade signature", async () => {
    const { result } = renderHook(() => useReviewedTrade(TOKEN));
    const reviewed = intent({ functionName: "sell", value: 0n });
    const approvalWrite = {
      address: TOKEN as `0x${string}`,
      abi: ABI,
      functionName: "approve",
      args: [TARGET, 5n] as const,
    };
    wallet.walletClient.writeContract.mockResolvedValue(APPROVAL_HASH);
    act(() => result.current.openReview(reviewed));

    await act(() =>
      result.current.runExecution(async (execution) => {
        const hash = await execution.approve(reviewed, approvalWrite);
        expect(hash).toBe(APPROVAL_HASH);
        execution.approvalsDone("Approval mined. Review the sell again to sign it.");
      }),
    );

    expect(wallet.walletClient.writeContract).toHaveBeenCalledWith({
      ...approvalWrite,
      account: ACCOUNT,
    });
    expect(result.current.approvalTransaction.state).toEqual({
      status: "mined",
      hash: APPROVAL_HASH,
    });
    expect(result.current.approvalHash).toBe(APPROVAL_HASH);
    expect(result.current.state).toEqual({ status: "validating" });
    expect(result.current.confirming).toBe(false);
    expect(result.current.reviewNotice).toMatch(/Approval mined/);
  });

  it("records an approval failure on the approval, not on the trade", async () => {
    const { result } = renderHook(() => useReviewedTrade(TOKEN));
    wallet.publicClient.simulateContract.mockRejectedValue(new Error("execution reverted"));

    await act(() =>
      result.current.runExecution(async (execution) => {
        await execution.approve(intent(), {
          address: TOKEN as `0x${string}`,
          abi: ABI,
          functionName: "approve",
          args: [TARGET, 5n],
        });
      }),
    );

    expect(result.current.approvalTransaction.state.status).toBe("simulation-reverted");
    expect(result.current.state).toEqual({ status: "validating" });
    expect(wallet.walletClient.writeContract).not.toHaveBeenCalled();
  });
});
