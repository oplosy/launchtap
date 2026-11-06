"use client";

import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type PropsWithChildren,
} from "react";
import type { EventSourceFactory } from "@/api/sse";
import { publicConfiguration } from "@/config/public";
import {
  createLiveController,
  type LiveController,
  type LiveListener,
  type LiveRecover,
} from "./live-controller";

export type { LiveListener, LiveRecover } from "./live-controller";

type LiveEventsBus = Pick<LiveController, "subscribe" | "addRecovery">;

const LiveEventsContext = createContext<LiveEventsBus | null>(null);

/**
 * Opens ONE unfiltered `/v1/events` stream for the whole tab and fans events out to subscribers.
 * Without a ready configuration (or outside the provider) subscriptions are inert.
 */
export function LiveEventsProvider({
  children,
  eventSourceFactory,
}: PropsWithChildren<{ eventSourceFactory?: EventSourceFactory }>) {
  const [controller] = useState(() =>
    createLiveController({
      url: `${publicConfiguration().apiBaseUrl ?? ""}/v1/events`,
      eventSourceFactory,
    }),
  );

  useEffect(() => {
    const configuration = publicConfiguration();
    if (configuration.status !== "ready" || !configuration.apiBaseUrl) return;
    return controller.start();
  }, [controller]);

  return <LiveEventsContext.Provider value={controller}>{children}</LiveEventsContext.Provider>;
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
