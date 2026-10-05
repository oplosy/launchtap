"use client";

import { usePathname } from "next/navigation";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ComponentType,
  type PropsWithChildren,
} from "react";
import { publicConfiguration, type PublicConfiguration } from "@/config/public";
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
  const pathname = usePathname();
  const available = useMemo(() => walletStackAvailable(publicConfiguration()), []);
  const required = available && WALLET_ROUTES.some((route) => route.test(pathname));
  const [Stack, setStack] = useState<WalletStackComponent | null>(null);
  const [openOnMount, setOpenOnMount] = useState(false);
  const [loading, setLoading] = useState(false);

  const requestStack = useCallback((open: boolean) => {
    if (open) setOpenOnMount(true);
    setLoading(true);
    void loadWalletStack()
      .then((module) => setStack(() => module.WalletStack))
      .catch(() => setLoading(false));
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (required && !Stack && !loading) requestStack(false);
  }, [Stack, loading, required, requestStack]);

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
    () => ({ ...fallbackConnection, available, pending: loading, open: () => requestStack(true) }),
    [available, loading, requestStack],
  );
  const status = useMemo(() => ({ required, ready: Stack !== null }), [Stack, required]);

  if (!available) return children;
  if (Stack)
    return (
      <WalletStackStatus.Provider value={status}>
        <Stack openOnMount={openOnMount}>{children}</Stack>
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
