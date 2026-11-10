"use client";

import { usePathname } from "next/navigation";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentType,
  type PropsWithChildren,
} from "react";
import { publicConfiguration, type PublicConfiguration } from "@/config/public";
import {
  E2E_SESSION_KEY,
  parseE2ESession,
  type E2ESession,
} from "@/e2e/session-context";
import { LiveEventsProvider } from "@/live/live-events";
import { NotificationIdentityBridge } from "@/live/notification-bridge";
import { NotificationsProvider } from "@/live/notifications";
import {
  WalletConnectionContext,
  fallbackConnection,
  type WalletConnection,
} from "@/wallet/connection-context";

type WalletStackComponent = ComponentType<PropsWithChildren<{ openOnMount?: boolean }>>;

const loadWalletStack = () => import("@/wallet/wallet-stack");

/** Routes whose content signs or reads wallet state and therefore needs the wallet stack. */
const WALLET_ROUTES = [/^\/token\//, /^\/create(?:\/|$)/, /^\/profile(?:\/|$)/];

const WalletStackStatus = createContext({ required: false, ready: true });

/** Mirrors the conditions under which `WalletStack` mounts any wallet provider. */
function walletStackAvailable(configuration: PublicConfiguration) {
  return (
    configuration.status === "ready" &&
    Boolean(configuration.chainId && configuration.rpcUrl && configuration.deployment) &&
    (Boolean(configuration.privyAppId) || configuration.deploymentId === "task6-anvil")
  );
}

/**
 * App-wide providers. The Privy/wagmi stack is a large download, so it loads only on routes
 * that need a wallet, when the reader asks to connect, or in idle time; until then the app shell
 * receives a lightweight connection whose `open` loads the stack and opens the wallet picker.
 */
export function Providers({ children }: PropsWithChildren) {
  return (
    <E2ESessionSlot>
      <LiveEventsProvider>
        <NotificationsProvider>
          <WalletProviders>{children}</WalletProviders>
        </NotificationsProvider>
      </LiveEventsProvider>
    </E2ESessionSlot>
  );
}

type SessionProvider = ComponentType<PropsWithChildren<{ session: E2ESession }>>;

/**
 * Fixture build only: when a browser test set `window.__e2eSession`, load the test session
 * provider and mount it above the app. In any other build the inlined fixture flag folds to
 * `false`, so the import below is dead code and the provider never reaches a bundle.
 */
function E2ESessionSlot({ children }: PropsWithChildren) {
  const [mounted, setMounted] = useState<{
    Provider: SessionProvider;
    session: E2ESession;
  } | null>(null);
  useEffect(() => {
    // Inlined (not the shared constant) so the bundler folds it and drops the import.
    if (process.env.NEXT_PUBLIC_E2E_FIXTURE === "1") {
      const session = parseE2ESession((window as unknown as Record<string, unknown>)[E2E_SESSION_KEY]);
      if (!session) return;
      // Aliased in next.config.ts: the real provider in the fixture build, a stub elsewhere.
      void import("launchpad-e2e-session").then((module) =>
        setMounted({ Provider: module.E2ESessionProvider, session }),
      );
    }
  }, []);
  if (!mounted) return children;
  const { Provider, session } = mounted;
  return <Provider session={session}>{children}</Provider>;
}

function WalletProviders({ children }: PropsWithChildren) {
  const pathname = usePathname();
  const available = useMemo(() => walletStackAvailable(publicConfiguration()), []);
  const required = available && WALLET_ROUTES.some((route) => route.test(pathname));
  const [Stack, setStack] = useState<WalletStackComponent | null>(null);
  const [openOnMount, setOpenOnMount] = useState(false);
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  const startedRef = useRef(false);

  const requestStack = useCallback((open: boolean) => {
    if (open) setOpenOnMount(true);
    startedRef.current = true;
    setLoading(true);
    setFailed(false);
    void loadWalletStack()
      .then((module) => setStack(() => module.WalletStack))
      .catch(() => {
        startedRef.current = false;
        setLoading(false);
        setFailed(true);
      });
  }, []);
  // Stable identity: callers may depend on it without re-firing when loading state changes.
  const prepareStack = useCallback(() => {
    if (!startedRef.current) requestStack(false);
  }, [requestStack]);

  useEffect(() => {
    // A failed load is retried by an explicit user action, not by this effect.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (required && !Stack && !loading && !failed) requestStack(false);
  }, [Stack, failed, loading, required, requestStack]);

  useEffect(() => {
    if (!available) return;
    // Warm the module cache after first paint without mounting it.
    const prefetch = () => void loadWalletStack().catch(() => undefined);
    if ("requestIdleCallback" in window) {
      const handle = window.requestIdleCallback(prefetch, { timeout: 5_000 });
      return () => window.cancelIdleCallback(handle);
    }
    const timer = setTimeout(prefetch, 3_000);
    return () => clearTimeout(timer);
  }, [available]);

  const lazyConnection = useMemo<WalletConnection>(
    () => ({
      ...fallbackConnection,
      available,
      pending: loading,
      open: () => requestStack(true),
      prepare: prepareStack,
      stack: failed ? "failed" : loading ? "loading" : "idle",
    }),
    [available, failed, loading, prepareStack, requestStack],
  );
  const status = useMemo(() => ({ required, ready: Stack !== null }), [Stack, required]);

  if (!available) return children;
  if (Stack)
    return (
      <WalletStackStatus.Provider value={status}>
        <Stack openOnMount={openOnMount}>
          <NotificationIdentityBridge />
          {children}
        </Stack>
      </WalletStackStatus.Provider>
    );
  return (
    <WalletStackStatus.Provider value={status}>
      <WalletConnectionContext.Provider value={lazyConnection}>
        {children}
      </WalletConnectionContext.Provider>
    </WalletStackStatus.Provider>
  );
}

/** Holds wallet-route content back until the wallet stack is mounted above it. */
export function WalletRouteGate({ children }: PropsWithChildren) {
  const { required, ready } = useContext(WalletStackStatus);
  if (required && !ready)
    return (
      <p className="route-state" role="status" aria-live="polite">
        Loading wallet…
      </p>
    );
  return children;
}
