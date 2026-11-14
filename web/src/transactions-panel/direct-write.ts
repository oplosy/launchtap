import type { Abi } from "viem";
import {
  classifyTransactionFailure,
  decodeTransactionError,
  type TransactionState,
} from "@/transactions";
import type { Address } from "./shared";

/** A single contract call with no review step: claims and creator-role actions. */
export type DirectWrite = {
  address: Address;
  abi: Abi;
  functionName: string;
  args?: readonly unknown[];
};

type DirectWriteClients = {
  publicClient: {
    getChainId: () => Promise<number>;
    simulateContract: (request: never) => Promise<{ result: unknown }>;
    waitForTransactionReceipt: (args: { hash: Address }) => Promise<{ status: string }>;
  };
  walletClient: {
    getAddresses: () => Promise<readonly string[]>;
    writeContract: (request: never) => Promise<string>;
  };
};

/**
 * Runs one unreviewed write through the same lifecycle as the reviewed pipeline
 * (`reviewed-trade.tsx`): re-check wallet and network, optional preflight reads, simulate,
 * sign, wait for the receipt, then settle on `settled`. A failure is classified by the phase it
 * happened in and keeps any submitted hash. Returns whether the write was mined successfully.
 */
export async function runDirectWrite({
  publicClient,
  walletClient,
  chainId,
  account,
  write,
  setState,
  changedMessage,
  settled,
  preflight,
  checkSimulation,
}: DirectWriteClients & {
  /** The configured chain; null never matches, so the write stops at preflight. */
  chainId: number | null;
  account: Address;
  write: DirectWrite;
  setState: (state: TransactionState) => void;
  /** Shown when the wallet account or network changed since the action was offered. */
  changedMessage: string;
  /** "indexing" when an indexer event follows the receipt, "mined" when nothing does. */
  settled: "mined" | "indexing";
  /** Extra preflight reads; throw to stop before simulation. */
  preflight?: () => Promise<void>;
  /** Validates the simulated return value; throw to stop before signing. */
  checkSimulation?: (result: unknown) => void;
}): Promise<boolean> {
  let submittedHash: Address | undefined;
  let phase: "preflight" | "simulation" | "write" | "receipt" = "preflight";
  try {
    setState({ status: "validating" });
    const [walletAccounts, latestChainId] = await Promise.all([
      walletClient.getAddresses(),
      publicClient.getChainId(),
    ]);
    if (latestChainId !== chainId || walletAccounts[0]?.toLowerCase() !== account.toLowerCase())
      throw new Error(changedMessage);
    await preflight?.();
    setState({ status: "simulating" });
    phase = "simulation";
    const request = { ...write, account } as never;
    const { result } = await publicClient.simulateContract(request);
    checkSimulation?.(result);
    setState({ status: "awaiting-signature" });
    phase = "write";
    const hash = (await walletClient.writeContract(request)) as Address;
    submittedHash = hash;
    setState({ status: "submitted", hash });
    phase = "receipt";
    const receipt = await publicClient.waitForTransactionReceipt({ hash });
    if (receipt.status !== "success") throw new Error("ReceiptReverted");
    setState({ status: settled, hash });
    return true;
  } catch (cause) {
    setState({
      status: classifyTransactionFailure(cause, phase),
      hash: submittedHash,
      error: decodeTransactionError(cause).message,
    });
    return false;
  }
}
