import { SseInvalidationStream, type EventSourceFactory } from "@/api/sse";
import type { LiveEvent } from "@/api/types";

export type LiveListener = (event: LiveEvent) => void;
export type LiveRecover = (signal?: AbortSignal) => Promise<void>;

export const SLOW_RETRY_MS = 30_000;
/**
 * A tab hidden this long closes its stream, so background tabs do not hold one of the client's
 * `API_SSE_MAX_PER_CLIENT` connections; it reopens through recovery when the tab is visible.
 */
export const HIDDEN_CLOSE_MS = 60_000;

export type LiveController = {
  subscribe: (listener: LiveListener) => () => void;
  addRecovery: (recover: LiveRecover) => () => void;
  /** Opens the single stream; returns a disposer. */
  start: () => () => void;
};

/**
 * Framework-free core of the shared live stream: one SseInvalidationStream, a subscriber fan-out
 * and the long-lived recovery policy. After the stream's own retry budget is exhausted it keeps
 * retrying every SLOW_RETRY_MS and immediately when the tab becomes visible again. A tab hidden
 * for HIDDEN_CLOSE_MS closes ("parks") its stream; becoming visible reopens it through the same
 * recovery path (REST refill, reopen, `reconnected` once the new source opens).
 */
export function createLiveController(options: {
  url: string;
  eventSourceFactory?: EventSourceFactory;
  slowRetryMs?: number;
  hiddenCloseMs?: number;
}): LiveController {
  const listeners = new Set<LiveListener>();
  const recoveries = new Set<LiveRecover>();
  const slowRetryMs = options.slowRetryMs ?? SLOW_RETRY_MS;
  const hiddenCloseMs = options.hiddenCloseMs ?? HIDDEN_CLOSE_MS;

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
      // Closed because the tab stayed hidden; reopened (via restart) when it is visible again.
      let parked = false;
      let hiddenTimer: ReturnType<typeof setTimeout> | null = null;
      let stopStream: (() => void) | null = null;
      let slowTimer: ReturnType<typeof setTimeout> | null = null;
      let restartAbort: AbortController | null = null;

      const clearSlowTimer = () => {
        if (slowTimer) clearTimeout(slowTimer);
        slowTimer = null;
      };

      const hasDocument = typeof document !== "undefined";
      const isHidden = () => hasDocument && document.visibilityState === "hidden";

      const clearHiddenTimer = () => {
        if (hiddenTimer) clearTimeout(hiddenTimer);
        hiddenTimer = null;
      };

      const park = () => {
        clearHiddenTimer();
        if (disposed) return;
        parked = true;
        clearSlowTimer();
        restartAbort?.abort();
        stopStream?.();
        stopStream = null;
      };

      const scheduleSlowRetry = () => {
        clearSlowTimer();
        slowTimer = setTimeout(() => void restart(), slowRetryMs);
      };

      const open = (reconnected = false) => {
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
        stopStream = stream.start({ reconnected });
      };

      const restart = async () => {
        if (disposed || !(exhausted || parked) || restartAbort) return;
        clearSlowTimer();
        // A hidden tab does not retry in the background; becoming visible restarts it.
        if (isHidden()) {
          park();
          return;
        }
        const abort = new AbortController();
        restartAbort = abort;
        try {
          stopStream?.();
          stopStream = null;
          // Events were missed while the stream was down: refill REST state first.
          await runRecoveries(abort.signal);
          if (disposed || abort.signal.aborted) return;
          exhausted = false;
          parked = false;
          // `reconnected` is emitted when the new source opens (SseInvalidationStream).
          open(true);
        } finally {
          if (restartAbort === abort) restartAbort = null;
        }
      };

      const onVisibility = () => {
        if (document.visibilityState === "hidden") {
          if (!hiddenTimer && !parked) hiddenTimer = setTimeout(park, hiddenCloseMs);
          return;
        }
        clearHiddenTimer();
        if (document.visibilityState === "visible") void restart();
      };
      if (hasDocument) document.addEventListener("visibilitychange", onVisibility);

      open();
      if (isHidden()) hiddenTimer = setTimeout(park, hiddenCloseMs);
      return () => {
        disposed = true;
        clearSlowTimer();
        clearHiddenTimer();
        restartAbort?.abort();
        if (hasDocument) document.removeEventListener("visibilitychange", onVisibility);
        stopStream?.();
        stopStream = null;
      };
    },
  };
}
