"use client";

import { useSyncExternalStore } from "react";
import { getStatus, type StatusResponse } from "@/api/client";
import { publicConfiguration } from "@/config/public";

/** Stale data notice state (audit pass 3, L-5); `stale` is false while loading or failing. */
export type IndexerStatusState = { stale: boolean; ageSeconds: number | null };

export const INDEXER_STATUS_POLL_INTERVAL_MS = 30_000;

const FRESH: IndexerStatusState = { stale: false, ageSeconds: null };

type VisibilityDocument = Pick<
  Document,
  "visibilityState" | "addEventListener" | "removeEventListener"
>;

/**
 * One shared poller of `GET /v1/status`: the first subscriber starts it, the last one stops it.
 * Only the server's own verdict shows the notice; a failed request clears it, because an
 * unreachable API is not evidence the indexer stopped and the pages show their own errors.
 */
export function createIndexerStatusStore(
  fetcher: (signal: AbortSignal) => Promise<StatusResponse>,
  doc: VisibilityDocument | null,
) {
  const listeners = new Set<() => void>();
  let snapshot = FRESH;
  let timer: ReturnType<typeof setInterval> | null = null;
  let controller: AbortController | null = null;

  const publish = (next: IndexerStatusState) => {
    if (next.stale === snapshot.stale && next.ageSeconds === snapshot.ageSeconds) return;
    snapshot = next;
    listeners.forEach((listener) => listener());
  };

  const refresh = async () => {
    if (doc?.visibilityState === "hidden") return;
    controller?.abort();
    const current = (controller = new AbortController());
    try {
      const result = await fetcher(current.signal);
      if (current.signal.aborted) return;
      publish(
        result.indexer.stale
          ? { stale: true, ageSeconds: result.indexer.heartbeat_age_seconds ?? null }
          : FRESH,
      );
    } catch {
      if (!current.signal.aborted) publish(FRESH);
    }
  };

  const onVisibility = () => {
    if (doc?.visibilityState !== "hidden") void refresh();
  };

  const start = () => {
    void refresh();
    timer = setInterval(() => void refresh(), INDEXER_STATUS_POLL_INTERVAL_MS);
    doc?.addEventListener("visibilitychange", onVisibility);
  };
  const stop = () => {
    if (timer !== null) clearInterval(timer);
    timer = null;
    controller?.abort();
    controller = null;
    doc?.removeEventListener("visibilitychange", onVisibility);
  };

  return {
    subscribe(listener: () => void) {
      listeners.add(listener);
      if (listeners.size === 1) start();
      return () => {
        listeners.delete(listener);
        if (listeners.size === 0) stop();
      };
    },
    getSnapshot: () => snapshot,
  };
}

const sharedStore = createIndexerStatusStore(
  async (signal) => {
    const configuration = publicConfiguration();
    if (configuration.status !== "ready" || !configuration.apiBaseUrl)
      throw new Error("API unavailable");
    return getStatus(configuration.apiBaseUrl, signal);
  },
  typeof document === "undefined" ? null : document,
);

export function useIndexerStatus(): IndexerStatusState {
  return useSyncExternalStore(sharedStore.subscribe, sharedStore.getSnapshot, () => FRESH);
}

/** "3 minutes", "1 hour"; null before the indexer's first step. */
export function describeDelay(ageSeconds: number | null): string | null {
  if (ageSeconds === null) return null;
  const minutes = Math.max(1, Math.floor(ageSeconds / 60));
  if (minutes < 60) return `${minutes} minute${minutes === 1 ? "" : "s"}`;
  const hours = Math.floor(minutes / 60);
  return `${hours} hour${hours === 1 ? "" : "s"}`;
}
