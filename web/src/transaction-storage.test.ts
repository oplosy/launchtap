import { describe, expect, it } from "vitest";
import {
  readPersistedTransaction,
  writePersistedTransaction,
  type TransactionStorageContext,
} from "./transaction-storage";

function memoryStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    raw: () => values.get("launchpad.transactions.v1") ?? "[]",
  };
}

const context: TransactionStorageContext = {
  chainId: 46630,
  account: "0x1111111111111111111111111111111111111111",
  tokenAddress: "0x2222222222222222222222222222222222222222",
  action: "trade",
};

describe("public transaction recovery storage", () => {
  it("persists only a validated public hash and restores it for the exact context", () => {
    const storage = memoryStorage();
    writePersistedTransaction(
      context,
      { hash: `0x${"a".repeat(64)}`, status: "indexing" },
      storage,
      1_000,
    );

    expect(readPersistedTransaction(context, storage, 1_001)).toEqual({
      version: 1,
      hash: `0x${"a".repeat(64)}`,
      chainId: 46630,
      account: context.account,
      tokenAddress: context.tokenAddress,
      action: "trade",
      status: "indexing",
      updatedAt: 1_000,
    });
    expect(storage.raw()).not.toContain("signature");
    expect(readPersistedTransaction({ ...context, chainId: 1 }, storage, 1_001)).toBeNull();
    expect(
      readPersistedTransaction({ ...context, account: `0x${"3".repeat(40)}` }, storage, 1_001),
    ).toBeNull();
    expect(readPersistedTransaction({ ...context, action: "claim" }, storage, 1_001)).toBeNull();
  });

  it("keeps the engine-2 creator panel's write apart from the curve claims of the same token", () => {
    // Plan v2-C Task 6: propose, cancel, accept and the LP fee claim share one status line.
    const storage = memoryStorage();
    const creator = { ...context, action: "v2-creator" } as const;
    writePersistedTransaction(
      creator,
      { hash: `0x${"b".repeat(64)}`, status: "mined" },
      storage,
      1_000,
    );
    writePersistedTransaction(
      { ...context, action: "claim" },
      { hash: `0x${"c".repeat(64)}`, status: "indexing" },
      storage,
      1_000,
    );
    expect(readPersistedTransaction(creator, storage, 1_001)).toMatchObject({
      action: "v2-creator",
      hash: `0x${"b".repeat(64)}`,
      status: "mined",
    });
  });

  it("ignores malformed hashes, unknown statuses, stale records, and unsafe payload fields", () => {
    const storage = memoryStorage();
    writePersistedTransaction(context, { hash: "0x123", status: "indexing" }, storage, 10_000);
    writePersistedTransaction(
      context,
      { hash: `0x${"b".repeat(64)}`, status: "awaiting-signature" },
      storage,
      10_000,
    );
    expect(readPersistedTransaction(context, storage, 10_001)).toBeNull();

    storage.setItem(
      "launchpad.transactions.v1",
      JSON.stringify([
        {
          version: 1,
          hash: `0x${"c".repeat(64)}`,
          chainId: 46630,
          account: context.account,
          tokenAddress: context.tokenAddress,
          action: "trade",
          status: "indexing",
          updatedAt: 1,
          signature: "must never be returned",
        },
      ]),
    );
    expect(readPersistedTransaction(context, storage, 31 * 24 * 60 * 60 * 1000)).toBeNull();
  });
});
