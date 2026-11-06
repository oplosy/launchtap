import type { components } from "./generated";

export type Snapshot = components["schemas"]["SnapshotDTO"];
export type CursorPage<T> = { items: T[] | null; next_cursor?: string; snapshot: Snapshot };
export type TokenList = components["schemas"]["TokenDTO"];
export type ApiEvent =
  | { event: "launch"; data: components["schemas"]["LaunchEvent"] }
  | { event: "reorg"; data: components["schemas"]["ReorgEvent"] }
  | { event: "token"; data: components["schemas"]["TokenEvent"] }
  | { event: "trade"; data: TradeFeedItem }
  | { event: "graduation"; data: components["schemas"]["GraduationFeedEvent"] }
  | { event: "new_token"; data: components["schemas"]["NewTokenFeedEvent"] }
  | { event: "trade_burst"; data: components["schemas"]["TradeBurstFeedEvent"] }
  | { event: "finality"; data: components["schemas"]["FinalityFeedEvent"] };
export type ApiEventName = ApiEvent["event"];
export type TradeFeedItem = components["schemas"]["TradeFeedItem"];
/** Emitted by the live stream provider after a dropped stream recovered and REST was refetched. */
export type LiveEvent = ApiEvent | { event: "reconnected" };

export function stableSerialize(value: unknown): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value) ?? "null";
  if (Array.isArray(value)) return `[${value.map(stableSerialize).join(",")}]`;
  return `{${Object.keys(value as Record<string, unknown>)
    .sort()
    .map(
      (key) => `${JSON.stringify(key)}:${stableSerialize((value as Record<string, unknown>)[key])}`,
    )
    .join(",")}}`;
}

export function snapshotIdentity(snapshot: Snapshot | null | undefined) {
  return snapshot ? stableSerialize(snapshot) : null;
}

export const queryKeys = {
  all: (chainId: number | null, deploymentId: string | null) =>
    ["launchpad", chainId, deploymentId] as const,
  tokens: (
    chainId: number | null,
    deploymentId: string | null,
    filters: Record<string, unknown> = {},
    snapshot?: Snapshot,
  ) =>
    [
      ...queryKeys.all(chainId, deploymentId),
      "tokens",
      stableSerialize(filters),
      snapshotIdentity(snapshot),
    ] as const,
  token: (
    chainId: number | null,
    deploymentId: string | null,
    address: string,
    snapshot?: Snapshot,
  ) =>
    [
      ...queryKeys.all(chainId, deploymentId),
      "token",
      address.toLowerCase(),
      snapshotIdentity(snapshot),
    ] as const,
  trades: (
    chainId: number | null,
    deploymentId: string | null,
    address: string,
    cursor?: string,
    snapshot?: Snapshot,
  ) =>
    [
      ...queryKeys.token(chainId, deploymentId, address, snapshot),
      "trades",
      cursor ?? null,
    ] as const,
  holders: (
    chainId: number | null,
    deploymentId: string | null,
    address: string,
    cursor?: string,
    snapshot?: Snapshot,
  ) =>
    [
      ...queryKeys.token(chainId, deploymentId, address, snapshot),
      "holders",
      cursor ?? null,
    ] as const,
  candles: (
    chainId: number | null,
    deploymentId: string | null,
    address: string,
    filters: Record<string, unknown> = {},
    snapshot?: Snapshot,
  ) =>
    [
      ...queryKeys.token(chainId, deploymentId, address, snapshot),
      "candles",
      stableSerialize(filters),
    ] as const,
  protocol: (chainId: number | null, deploymentId: string | null, snapshot?: Snapshot) =>
    [...queryKeys.all(chainId, deploymentId), "protocol", snapshotIdentity(snapshot)] as const,
  protocolDaily: (
    chainId: number | null,
    deploymentId: string | null,
    filters: Record<string, unknown> = {},
    snapshot?: Snapshot,
  ) =>
    [
      ...queryKeys.protocol(chainId, deploymentId, snapshot),
      "daily",
      stableSerialize(filters),
    ] as const,
};
