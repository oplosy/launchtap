"use client";

import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  type PropsWithChildren,
} from "react";
import { SseInvalidationStream } from "@/api/sse";
import type { LiveEvent } from "@/api/types";
import { publicConfiguration } from "@/config/public";

export type LiveListener = (event: LiveEvent) => void;
export type LiveRecover = (signal?: AbortSignal) => Promise<void>;

type LiveEventsBus = {
  subscribe: (listener: LiveListener) => () => void;
  addRecovery: (recover: LiveRecover) => () => void;
};

const LiveEventsContext = createContext<LiveEventsBus | null>(null);

/**
 * Opens ONE unfiltered `/v1/events` stream for the whole tab and fans events out to subscribers.
 * Without a ready configuration (or outside the provider) subscriptions are inert.
 */
export function LiveEventsProvider({ children }: PropsWithChildren) {
  const listeners = useRef(new Set<LiveListener>());
  const recoveries = useRef(new Set<LiveRecover>());
  const bus = useMemo<LiveEventsBus>(
    () => ({
      subscribe: (listener) => {
        listeners.current.add(listener);
        return () => {
          listeners.current.delete(listener);
        };
      },
      addRecovery: (recover) => {
        recoveries.current.add(recover);
        return () => {
          recoveries.current.delete(recover);
        };
      },
    }),
    [],
  );

  useEffect(() => {
    const configuration = publicConfiguration();
    if (configuration.status !== "ready" || !configuration.apiBaseUrl) return;
    const emit = (event: LiveEvent) => {
      for (const listener of [...listeners.current]) {
        try {
          listener(event);
        } catch {
          // One failing subscriber must not starve the others.
        }
      }
    };
    const stream = new SseInvalidationStream({
      url: `${configuration.apiBaseUrl}/v1/events`,
      queryClient: { invalidateQueries: async () => undefined },
      onEvent: emit,
      onReconnected: () => emit({ event: "reconnected" }),
      refetchSnapshot: async (signal) => {
        // Every subscriber refills its canonical REST state before the stream reopens.
        await Promise.all([...recoveries.current].map((recover) => recover(signal)));
      },
    });
    return stream.start();
  }, []);

  return <LiveEventsContext.Provider value={bus}>{children}</LiveEventsContext.Provider>;
}

/**
 * Subscribes to the shared stream. The latest `listener` / `recover` closures are always used,
 * so callers need not memoise them. `recover` runs during stream recovery, before it reopens;
 * a synthetic `{ event: "reconnected" }` follows once the new connection is opened.
 */
export function useLiveEvents(listener: LiveListener, recover?: LiveRecover) {
  const bus = useContext(LiveEventsContext);
  const listenerRef = useRef(listener);
  const recoverRef = useRef(recover);
  useEffect(() => {
    listenerRef.current = listener;
    recoverRef.current = recover;
  });
  useEffect(() => {
    if (!bus) return;
    const unsubscribe = bus.subscribe((event) => listenerRef.current(event));
    const removeRecovery = bus.addRecovery(async (signal) => {
      await recoverRef.current?.(signal);
    });
    return () => {
      unsubscribe();
      removeRecovery();
    };
  }, [bus]);
}
