"use client";

import { useSyncExternalStore } from "react";
import { getEthUsd, type EthUsdReference } from "@/api/client";
import { publicConfiguration } from "@/config/public";

export type EthUsdState = { price: string | null; attributionUrl: string | null };

export const ETH_USD_POLL_INTERVAL_MS = 60_000;
/** A price older than this is dropped rather than shown stale. */
export const ETH_USD_MAX_AGE_MS = 120_000;

const EMPTY: EthUsdState = { price: null, attributionUrl: null };

type VisibilityDocument = Pick<
  Document,
  "visibilityState" | "addEventListener" | "removeEventListener"
>;

/** One shared poller: the first subscriber starts it, the last one stops it. */
export function createEthUsdStore(
  fetcher: (signal: AbortSignal) => Promise<EthUsdReference>,
  doc: VisibilityDocument | null,
) {
  const listeners = new Set<() => void>();
  let snapshot = EMPTY;
  let timer: ReturnType<typeof setInterval> | null = null;
  let controller: AbortController | null = null;
  let inFlight = false;
  let lastSuccess = 0;

  const publish = (next: EthUsdState) => {
    if (next.price === snapshot.price && next.attributionUrl === snapshot.attributionUrl) return;
    snapshot = next;
    listeners.forEach((listener) => listener());
  };

  const refresh = async () => {
    // A request still pending at the next tick, or an old price, must not keep a stale value on screen.
    if (inFlight || Date.now() - lastSuccess > ETH_USD_MAX_AGE_MS) publish(EMPTY);
    if (doc?.visibilityState === "hidden") return;
    controller?.abort();
    const current = (controller = new AbortController());
    inFlight = true;
    try {
      const result = await fetcher(current.signal);
      if (current.signal.aborted) return;
      lastSuccess = Date.now();
      publish(
        result.price ? { price: result.price, attributionUrl: result.attribution_url } : EMPTY,
      );
    } catch {
      if (!current.signal.aborted) publish(EMPTY);
    } finally {
      if (controller === current) inFlight = false;
    }
  };

  const onVisibility = () => {
    if (doc?.visibilityState !== "hidden") void refresh();
  };

  const start = () => {
    void refresh();
    timer = setInterval(() => void refresh(), ETH_USD_POLL_INTERVAL_MS);
    doc?.addEventListener("visibilitychange", onVisibility);
  };
  const stop = () => {
    if (timer !== null) clearInterval(timer);
    timer = null;
    controller?.abort();
    controller = null;
    inFlight = false;
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

const sharedStore = createEthUsdStore(
  async (signal) => {
    const configuration = publicConfiguration();
    if (configuration.status !== "ready" || !configuration.apiBaseUrl)
      throw new Error("API unavailable");
    return getEthUsd(configuration.apiBaseUrl, signal);
  },
  typeof document === "undefined" ? null : document,
);

/** ETH/USD reference price; `price` is null while loading, disabled, stale or failing. */
export function useEthUsd(): EthUsdState {
  return useSyncExternalStore(sharedStore.subscribe, sharedStore.getSnapshot, () => EMPTY);
}
