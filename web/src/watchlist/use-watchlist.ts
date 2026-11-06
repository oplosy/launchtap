"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useSyncExternalStore,
} from "react";
import {
  addWatchlist,
  getWatchlist,
  removeWatchlist,
  type AuthHeaders,
  type WatchlistResponse,
} from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { publicConfiguration } from "@/config/public";
import { useWalletConnection, type WalletStackState } from "@/wallet/connection-context";

/** Server-side cap on one user's watchlist (spec: 200 entries). */
export const WATCHLIST_LIMIT = 200;

/** `token` is null for entries the API can no longer resolve. */
export type WatchlistEntry = NonNullable<WatchlistResponse["items"]>[number];
export type WatchlistList = WatchlistResponse;

/** Session facts the store needs. Supplied by Privy where it is mounted, inert elsewhere. */
export type WatchlistAuth = {
  /** True when this context can sign the reader in (Privy is mounted). */
  available: boolean;
  ready: boolean;
  authenticated: boolean;
  getAuth: () => Promise<AuthHeaders | null>;
  login: () => void;
};

export type WatchlistApi = {
  list: (auth: AuthHeaders) => Promise<WatchlistList>;
  add: (address: string, auth: AuthHeaders) => Promise<void>;
  remove: (address: string, auth: AuthHeaders) => Promise<void>;
};

export type WatchlistDeps = { api: WatchlistApi; auth: WatchlistAuth };

export type WatchlistToastState = {
  id: number;
  title: string;
  description?: string;
  tone: "danger" | "warning";
};

export type WatchlistStatus = "idle" | "loading" | "ready" | "error";

export type WatchlistSnapshot = {
  addresses: ReadonlySet<string>;
  entries: readonly WatchlistEntry[];
  full: boolean;
  status: WatchlistStatus;
  toast: WatchlistToastState | null;
};

const EMPTY_SNAPSHOT: WatchlistSnapshot = {
  addresses: new Set(),
  entries: [],
  full: false,
  status: "idle",
  toast: null,
};

const key = (address: string) => address.toLowerCase();

/**
 * Shared optimistic watchlist state. Pure of React so the failure paths are unit-testable;
 * `useWatchlist` binds it to the session and API client.
 */
export function createWatchlistStore() {
  let snapshot = EMPTY_SNAPSHOT;
  let fullByServer = false;
  let loadGeneration = 0;
  let toastId = 0;
  // Addresses with a toggle in flight, mapped to the optimistic presence they are heading to.
  const pending = new Map<string, boolean>();
  let loading: Promise<void> | null = null;
  const listeners = new Set<() => void>();

  const set = (patch: Partial<WatchlistSnapshot>) => {
    const next = { ...snapshot, ...patch };
    next.full = fullByServer || next.addresses.size >= WATCHLIST_LIMIT;
    snapshot = next;
    listeners.forEach((listener) => listener());
  };
  const toast = (title: string, tone: WatchlistToastState["tone"], description?: string) =>
    set({ toast: { id: ++toastId, title, description, tone } });
  const withAddress = (address: string, present: boolean) => {
    const addresses = new Set(snapshot.addresses);
    if (present) addresses.add(address);
    else addresses.delete(address);
    return addresses;
  };
  const toastFull = () =>
    toast(`Watchlist full (${WATCHLIST_LIMIT})`, "warning", "Remove a token to add another.");

  return {
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
    getSnapshot: () => snapshot,
    dismissToast: () => set({ toast: null }),
    reset() {
      loadGeneration++;
      loading = null;
      fullByServer = false;
      pending.clear();
      snapshot = { ...EMPTY_SNAPSHOT };
      listeners.forEach((listener) => listener());
    },
    /** Loads the list. A load already in flight is shared, never restarted. */
    load(deps: WatchlistDeps): Promise<void> {
      if (!deps.auth.authenticated) return Promise.resolve();
      if (loading) return loading;
      const generation = ++loadGeneration;
      set({ status: "loading" });
      const run: Promise<void> = (async () => {
        try {
          const auth = await deps.auth.getAuth();
          if (!auth) throw new Error("session unavailable");
          const response = await deps.api.list(auth);
          if (generation !== loadGeneration) return;
          const entries = response.items ?? [];
          fullByServer = false;
          const addresses = new Set(entries.map((entry) => key(entry.address)));
          // A toggle still in flight owns its address: keep its optimistic state.
          for (const [address, present] of pending) {
            if (present) addresses.add(address);
            else addresses.delete(address);
          }
          set({ entries, addresses, status: "ready" });
        } catch {
          if (generation !== loadGeneration) return;
          set({ status: "error" });
        }
      })().finally(() => {
        if (loading === run) loading = null;
      });
      loading = run;
      return run;
    },
    /** Sets presence to `want`; does nothing (and sends nothing) if it already holds. */
    async apply(rawAddress: string, want: boolean, deps: WatchlistDeps) {
      if (snapshot.addresses.has(key(rawAddress)) === want) return;
      await this.toggle(rawAddress, deps);
    },
    async toggle(rawAddress: string, deps: WatchlistDeps) {
      const { auth, api } = deps;
      if (!auth.ready) return;
      if (!auth.authenticated) {
        auth.login();
        return;
      }
      const address = key(rawAddress);
      if (pending.has(address)) return;
      const adding = !snapshot.addresses.has(address);
      if (adding && snapshot.full) {
        toastFull();
        return;
      }
      pending.set(address, adding);
      set({ addresses: withAddress(address, adding) });
      try {
        const headers = await auth.getAuth();
        if (!headers) {
          auth.login();
          throw new Error("session unavailable");
        }
        if (adding) await api.add(address, headers);
        else {
          await api.remove(address, headers);
          // A removal frees a slot, so a stale server-side "full" no longer holds.
          fullByServer = false;
        }
      } catch (cause) {
        set({ addresses: withAddress(address, !adding) });
        if (
          cause instanceof ApiProblem &&
          (cause.code === "watchlist_full" || cause.status === 409)
        ) {
          fullByServer = true;
          toastFull();
        } else if (cause instanceof ApiProblem && cause.status === 401) {
          auth.login();
        } else {
          toast(
            adding ? "Could not add to watchlist" : "Could not remove from watchlist",
            "danger",
            "Check your connection and try again.",
          );
        }
      } finally {
        pending.delete(address);
        set({});
      }
    },
  };
}

const store = createWatchlistStore();

const INERT_AUTH: WatchlistAuth = {
  available: false,
  ready: false,
  authenticated: false,
  getAuth: async () => null,
  login: () => undefined,
};

/** Provided by the Privy wallet stack. Without it the star falls back to the wallet picker. */
export const WatchlistAuthContext = createContext<WatchlistAuth>(INERT_AUTH);

function apiFor(baseUrl: string): WatchlistApi {
  return {
    list: (auth) => getWatchlist(baseUrl, auth),
    add: (address, auth) => addWatchlist(baseUrl, address, auth),
    remove: (address, auth) => removeWatchlist(baseUrl, address, auth),
  };
}

/** A star pressed before the wallet stack mounted, recorded with the state it asked for. */
export type WatchlistIntent = { address: string; want: boolean; at: number };
/** An intent older than this is dropped rather than applied on a later, unrelated page. */
export const INTENT_TTL_MS = 30_000;
let pendingIntent: WatchlistIntent | null = null;

export function rememberIntent(address: string, want: boolean, now = Date.now()) {
  pendingIntent = { address, want, at: now };
}
export function clearIntent() {
  pendingIntent = null;
}
/** Returns and clears the pending intent, or null if there is none or it has gone stale. */
export function takeIntent(now = Date.now()): WatchlistIntent | null {
  const intent = pendingIntent;
  pendingIntent = null;
  return intent && now - intent.at <= INTENT_TTL_MS ? intent : null;
}

export type WatchlistAvailability = "unavailable" | "loading" | "failed" | "ready";

/**
 * Whether the watchlist can work here. Without Privy there is never a watchlist session, so a
 * mounted wallet stack that provides none means unavailable, not "still loading".
 */
export function watchlistAvailability(input: {
  apiReady: boolean;
  sessionAvailable: boolean;
  walletAvailable: boolean;
  stack: WalletStackState;
}): WatchlistAvailability {
  if (!input.apiReady) return "unavailable";
  if (input.sessionAvailable) return "ready";
  if (input.stack === "mounted" || !input.walletAvailable) return "unavailable";
  return input.stack === "failed" ? "failed" : "loading";
}

export function useWatchlist() {
  const session = useContext(WatchlistAuthContext);
  const wallet = useWalletConnection();
  const snapshot = useSyncExternalStore(store.subscribe, store.getSnapshot, () => EMPTY_SNAPSHOT);
  const configuration = publicConfiguration();
  const baseUrl = configuration.status === "ready" ? configuration.apiBaseUrl : null;
  const sessionAvailable = session.available;
  const deps = useMemo<WatchlistDeps | null>(
    () => (baseUrl && sessionAvailable ? { api: apiFor(baseUrl), auth: session } : null),
    [baseUrl, session, sessionAvailable],
  );
  const { ready, authenticated } = session;
  const { prepare } = wallet;
  const availability = watchlistAvailability({
    apiReady: baseUrl !== null,
    sessionAvailable,
    walletAvailable: wallet.available,
    stack: wallet.stack,
  });
  const stackState = wallet.stack;

  useEffect(() => {
    // A failed stack load will not deliver the press; do not apply it on some later page.
    if (stackState === "failed") clearIntent();
  }, [stackState]);

  useEffect(() => {
    if (!deps || !ready) return;
    if (!authenticated) {
      if (store.getSnapshot().status !== "idle") store.reset();
      const intent = takeIntent();
      // Only now do we know the reader is signed out, so only now open sign-in.
      if (intent) deps.auth.login();
      return;
    }
    const intent = takeIntent();
    void (async () => {
      if (store.getSnapshot().status === "idle" || intent) await store.load(deps);
      // Set to the state the star asked for; a press on an already-listed token is a no-op.
      if (intent) await store.apply(intent.address, intent.want, deps);
    })();
  }, [authenticated, deps, ready]);

  const toggle = useCallback(
    async (address: string) => {
      if (availability === "unavailable") return;
      if (!deps) {
        // Privy is not mounted here (read-only route): mount it without opening the picker,
        // then apply this press once the session is known.
        rememberIntent(address, !snapshot.addresses.has(key(address)));
        prepare();
        return;
      }
      await store.toggle(address, deps);
    },
    [availability, deps, prepare, snapshot.addresses],
  );
  const reload = useCallback(async () => {
    if (deps) await store.load(deps);
  }, [deps]);
  const login = useCallback(() => session.login(), [session]);
  const has = useCallback(
    (address: string) => snapshot.addresses.has(key(address)),
    [snapshot.addresses],
  );

  return useMemo(
    () => ({
      /** The star can act: an API exists and the reader can sign in somehow. */
      available: availability !== "unavailable",
      availability,
      /** Privy is mounted, so the session is known. */
      sessionAvailable,
      ready,
      authenticated,
      login,
      prepare,
      status: snapshot.status,
      full: snapshot.full,
      entries: snapshot.entries,
      /** Live lowercase set of watched addresses, including optimistic toggles. */
      addresses: snapshot.addresses,
      toast: snapshot.toast,
      has,
      toggle,
      reload,
      dismissToast: store.dismissToast,
    }),
    [
      authenticated,
      availability,
      has,
      login,
      prepare,
      ready,
      reload,
      sessionAvailable,
      snapshot.addresses,
      snapshot.entries,
      snapshot.full,
      snapshot.status,
      snapshot.toast,
      toggle,
    ],
  );
}
