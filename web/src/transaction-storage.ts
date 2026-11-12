import type { TransactionStatus } from "./transactions";

// "v2-creator": the engine-2 creator panel (fee recipient changes and the LP fee locker claim).
export type PersistedTransactionAction =
  "launch" | "trade" | "claim" | "refund" | "approval" | "v2-creator";
export type PersistedTransactionStatus = Extract<
  TransactionStatus,
  | "submitted"
  | "rpc-failure"
  | "receipt-reverted"
  | "reverted"
  | "mined"
  | "indexing"
  | "indexed"
  | "safe"
  | "finalized"
>;

export type PersistedTransaction = {
  version: 1;
  hash: `0x${string}`;
  chainId: number;
  account: `0x${string}`;
  tokenAddress: `0x${string}` | null;
  action: PersistedTransactionAction;
  status: PersistedTransactionStatus;
  updatedAt: number;
};

export type TransactionStorageContext = Pick<
  PersistedTransaction,
  "chainId" | "account" | "tokenAddress" | "action"
>;

const STORAGE_KEY = "launchpad.transactions.v1";
const MAX_RECORDS = 100;
const MAX_AGE_MS = 30 * 24 * 60 * 60 * 1000;
const hashPattern = /^0x[0-9a-f]{64}$/i;
const addressPattern = /^0x[0-9a-f]{40}$/i;
const actions = new Set<PersistedTransactionAction>([
  "launch",
  "trade",
  "claim",
  "refund",
  "approval",
  "v2-creator",
]);
const statuses = new Set<PersistedTransactionStatus>([
  "submitted",
  "rpc-failure",
  "receipt-reverted",
  "reverted",
  "mined",
  "indexing",
  "indexed",
  "safe",
  "finalized",
]);

export function readPersistedTransaction(
  context: TransactionStorageContext,
  storage: Pick<Storage, "getItem">,
  now = Date.now(),
): PersistedTransaction | null {
  const records = readRecords(storage, now);
  return records.find((record) => sameContext(record, context)) ?? null;
}

export function writePersistedTransaction(
  context: TransactionStorageContext,
  value: { hash: string; status: TransactionStatus },
  storage: Pick<Storage, "getItem" | "setItem">,
  now = Date.now(),
): void {
  if (
    !isContext(context) ||
    !hashPattern.test(value.hash) ||
    !statuses.has(value.status as PersistedTransactionStatus)
  )
    return;
  const next: PersistedTransaction = {
    version: 1,
    hash: value.hash as `0x${string}`,
    chainId: context.chainId,
    account: context.account.toLowerCase() as `0x${string}`,
    tokenAddress: (context.tokenAddress?.toLowerCase() as `0x${string}` | undefined) ?? null,
    action: context.action,
    status: value.status as PersistedTransactionStatus,
    updatedAt: now,
  };
  const records = readRecords(storage, now).filter((record) => !sameContext(record, context));
  records.unshift(next);
  try {
    storage.setItem(STORAGE_KEY, JSON.stringify(records.slice(0, MAX_RECORDS)));
  } catch {
    // Storage is an optional recovery aid. A quota or privacy-mode error must not block signing.
  }
}

function readRecords(storage: Pick<Storage, "getItem">, now: number): PersistedTransaction[] {
  try {
    const raw = storage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const value: unknown = JSON.parse(raw);
    if (!Array.isArray(value)) return [];
    return value
      .filter(isPersistedTransaction)
      .filter((record) => now - record.updatedAt <= MAX_AGE_MS && record.updatedAt <= now)
      .map((record) => ({
        version: 1,
        hash: record.hash.toLowerCase() as `0x${string}`,
        chainId: record.chainId,
        account: record.account.toLowerCase() as `0x${string}`,
        tokenAddress: record.tokenAddress?.toLowerCase() as `0x${string}` | null,
        action: record.action,
        status: record.status,
        updatedAt: record.updatedAt,
      }));
  } catch {
    return [];
  }
}

function sameContext(left: TransactionStorageContext, right: TransactionStorageContext) {
  return (
    left.chainId === right.chainId &&
    left.account.toLowerCase() === right.account.toLowerCase() &&
    (left.tokenAddress?.toLowerCase() ?? null) === (right.tokenAddress?.toLowerCase() ?? null) &&
    left.action === right.action
  );
}

function isContext(value: TransactionStorageContext): boolean {
  return (
    Number.isSafeInteger(value.chainId) &&
    value.chainId > 0 &&
    addressPattern.test(value.account) &&
    (value.tokenAddress === null || addressPattern.test(value.tokenAddress)) &&
    actions.has(value.action)
  );
}

function isPersistedTransaction(value: unknown): value is PersistedTransaction {
  if (typeof value !== "object" || value === null) return false;
  const record = value as Partial<PersistedTransaction>;
  return (
    record.version === 1 &&
    typeof record.hash === "string" &&
    hashPattern.test(record.hash) &&
    Number.isSafeInteger(record.chainId) &&
    (record.chainId ?? 0) > 0 &&
    typeof record.account === "string" &&
    addressPattern.test(record.account) &&
    (record.tokenAddress === null ||
      (typeof record.tokenAddress === "string" && addressPattern.test(record.tokenAddress))) &&
    actions.has(record.action as PersistedTransactionAction) &&
    statuses.has(record.status as PersistedTransactionStatus) &&
    typeof record.updatedAt === "number" &&
    Number.isSafeInteger(record.updatedAt)
  );
}
