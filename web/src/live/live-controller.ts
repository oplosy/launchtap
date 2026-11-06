import { SseInvalidationStream, type EventSourceFactory } from "@/api/sse";
import type { LiveEvent } from "@/api/types";

export type LiveListener = (event: LiveEvent) => void;
export type LiveRecover = (signal?: AbortSignal) => Promise<void>;

export const SLOW_RETRY_MS = 30_000;

export type LiveController = {
  subscribe: (listener: LiveListener) => () => void;
  addRecovery: (recover: LiveRecover) => () => void;
  /** Opens the single stream; returns a disposer. */
  start: () => () => void;
};

/**
 * Framework-free core of the shared live stream: one SseInvalidationStream, a subscriber fan-out
 * and the long-lived recovery policy. After the stream's own retry budget is exhausted it keeps
 * retrying every SLOW_RETRY_MS and immediately when the tab becomes visible again.
 */
export function createLiveController(options: {
  url: string;
  eventSourceFactory?: EventSourceFactory;
  slowRetryMs?: number;
}): LiveController {
  const listeners = new Set<LiveListener>();
  const recoveries = new Set<LiveRecover>();
  const slowRetryMs = options.slowRetryMs ?? SLOW_RETRY_MS;

  const emit = (event: LiveEvent) => {
    for (const listener of [...listeners]) {
      try {
        listener(event);
      } catch (error) {
        // One failing subscriber must not starve the others, but it must not be hidden either.
        console.error("Live event subscriber failed", error);
      }
    }
  };

  // Each subscriber owns its own error state; a failed refill must not block reconnection.
  const runRecoveries = async (signal?: AbortSignal) => {
    const results = await Promise.allSettled([...recoveries].map((recover) => recover(signal)));
    for (const result of results) {
      if (result.status === "rejected") {
        console.warn("Live stream recovery failed for a subscriber", result.reason);
      }
    }
  };

  return {
    subscribe: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    addRecovery: (recover) => {
      recoveries.add(recover);
      return () => {
        recoveries.delete(recover);
      };
    },
    start: () => {
      let disposed = false;
      let exhausted = false;
      let stopStream: (() => void) | null = null;
      let slowTimer: ReturnType<typeof setTimeout> | null = null;
      let restartAbort: AbortController | null = null;

      const clearSlowTimer = () => {
        if (slowTimer) clearTimeout(slowTimer);
        slowTimer = null;
      };

      const scheduleSlowRetry = () => {
        clearSlowTimer();
        slowTimer = setTimeout(() => void restart(), slowRetryMs);
      };

      const open = () => {
        const stream = new SseInvalidationStream({
          url: options.url,
          eventSourceFactory: options.eventSourceFactory,
          queryClient: { invalidateQueries: async () => undefined },
          onEvent: emit,
          onReconnected: () => emit({ event: "reconnected" }),
          refetchSnapshot: runRecoveries,
          onRecoveryExhausted: () => {
            if (disposed) return;
            exhausted = true;
            scheduleSlowRetry();
          },
        });
        stopStream = stream.start();
      };

      const restart = async () => {
        if (disposed || !exhausted || restartAbort) return;
        clearSlowTimer();
        const abort = new AbortController();
        restartAbort = abort;
        try {
          stopStream?.();
          stopStream = null;
          // Events were missed while the stream was down: refill REST state first.
          await runRecoveries(abort.signal);
          if (disposed) return;
          exhausted = false;
          open();
          emit({ event: "reconnected" });
        } finally {
          if (restartAbort === abort) restartAbort = null;
        }
      };

      const onVisibility = () => {
        if (document.visibilityState === "visible") void restart();
      };
      const hasDocument = typeof document !== "undefined";
      if (hasDocument) document.addEventListener("visibilitychange", onVisibility);

      open();
      return () => {
        disposed = true;
        clearSlowTimer();
        restartAbort?.abort();
        if (hasDocument) document.removeEventListener("visibilitychange", onVisibility);
        stopStream?.();
        stopStream = null;
      };
    },
  };
}
