import { describe, expect, it, vi } from "vitest";
import type { TransactionState } from "@/transactions";
import { runDirectWrite } from "./direct-write";

const account = "0x00000000000000000000000000000000000000a1";
const target = "0x00000000000000000000000000000000000000c1";
const hash = "0x" + "ab".repeat(32);

function setup(overrides: {
  chainId?: number;
  wallet?: string;
  simulate?: () => Promise<{ result: unknown }>;
  write?: () => Promise<string>;
  receipt?: string;
}) {
  const states: TransactionState[] = [];
  const publicClient = {
    getChainId: vi.fn(async () => overrides.chainId ?? 46630),
    simulateContract: vi.fn(overrides.simulate ?? (async () => ({ result: 7n }))),
    waitForTransactionReceipt: vi.fn(async () => ({ status: overrides.receipt ?? "success" })),
  };
  const walletClient = {
    getAddresses: vi.fn(async () => [overrides.wallet ?? account]),
    writeContract: vi.fn(overrides.write ?? (async () => hash)),
  };
  const run = (extra: Partial<Parameters<typeof runDirectWrite>[0]> = {}) =>
    runDirectWrite({
      publicClient,
      walletClient,
      chainId: 46630,
      account,
      write: { address: target, abi: [], functionName: "claimRefund" },
      setState: (state) => states.push(state),
      changedMessage: "changed",
      settled: "indexing",
      ...extra,
    });
  return { states, publicClient, walletClient, run };
}

describe("runDirectWrite", () => {
  it("walks every phase and settles on the requested status", async () => {
    const { states, publicClient, walletClient, run } = setup({});
    const checkSimulation = vi.fn();
    await expect(run({ checkSimulation })).resolves.toBe(true);
    expect(states.map((state) => state.status)).toEqual([
      "validating",
      "simulating",
      "awaiting-signature",
      "submitted",
      "indexing",
    ]);
    expect(states.at(-1)?.hash).toBe(hash);
    expect(checkSimulation).toHaveBeenCalledWith(7n);
    const request = { address: target, abi: [], functionName: "claimRefund", account };
    expect(publicClient.simulateContract).toHaveBeenCalledWith(request);
    expect(walletClient.writeContract).toHaveBeenCalledWith(request);
  });

  it("stops before simulating when the wallet, network or preflight check fails", async () => {
    for (const [overrides, extra] of [
      [{ wallet: "0x00000000000000000000000000000000000000b2" }, {}],
      [{ chainId: 1 }, {}],
      [{}, { chainId: null }],
      [{}, { preflight: async () => Promise.reject(new Error("NothingToClaim")) }],
    ] as const) {
      const { states, publicClient, run } = setup(overrides);
      await expect(run(extra)).resolves.toBe(false);
      expect(states.at(-1)?.status).toBe("preflight-failure");
      expect(publicClient.simulateContract).not.toHaveBeenCalled();
    }
  });

  it("classifies failures by phase and keeps a submitted hash", async () => {
    const simulation = setup({ simulate: () => Promise.reject(new Error("execution reverted")) });
    await simulation.run();
    expect(simulation.states.at(-1)?.status).toBe("simulation-reverted");
    expect(simulation.walletClient.writeContract).not.toHaveBeenCalled();

    const rejected = setup({ write: () => Promise.reject(new Error("User rejected the request")) });
    await rejected.run();
    expect(rejected.states.at(-1)?.status).toBe("rejected-signature");

    const reverted = setup({ receipt: "reverted" });
    await expect(reverted.run({ settled: "mined" })).resolves.toBe(false);
    expect(reverted.states.at(-1)).toMatchObject({ status: "receipt-reverted", hash });
  });

  it("rejects a simulation result the caller does not accept", async () => {
    const { states, walletClient, run } = setup({});
    await run({
      checkSimulation: () => {
        throw new Error("unexpected result");
      },
    });
    expect(states.at(-1)?.status).toBe("simulation-reverted");
    expect(walletClient.writeContract).not.toHaveBeenCalled();
  });
});
