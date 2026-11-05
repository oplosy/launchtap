"use client";

import { createContext, useContext, useEffect, useMemo, useSyncExternalStore } from "react";
import {
  addWatchlist,
  getWatchlist,
  removeWatchlist,
  type AuthHeaders,
  type WatchlistResponse,
} from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { publicConfiguration } from "@/config/public";
import { useWalletConnection } from "@/wallet/connection-context";

/** Server-side cap on one user's watchlist (spec: 200 entries). */
export const WATCHLIST_LIMIT = 200;

type GeneratedEntry = NonNullable<WatchlistResponse["items"]>[number];
/** The API sends `token: null` for entries it can no longer resolve; the generated type omits it. */
export type WatchlistEntry = Omit<GeneratedEntry, "token"> & { token: GeneratedEntry["token"] | null };
export type WatchlistList = { items: WatchlistEntry[] | null };

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
  const pending = new Set<string>();
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
      fullByServer = false;
      pending.clear();
      snapshot = { ...EMPTY_SNAPSHOT };
      listeners.forEach((listener) => listener());
    },
    async load(deps: WatchlistDeps) {
      if (!deps.auth.authenticated) return;
      const generation = ++loadGeneration;
      set({ status: "loading" });
      try {
        const auth = await deps.auth.getAuth();
        if (!auth) throw new Error("session unavailable");
        const response = await deps.api.list(auth);
        if (generation !== loadGeneration) return;
        const entries = response.items ?? [];
        fullByServer = false;
        set({
          entries,
          addresses: new Set(entries.map((entry) => key(entry.address))),
          status: "ready",
        });
      } catch {
        if (generation !== loadGeneration) return;
        set({ status: "error" });
      }
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
      pending.add(address);
      set({ addresses: withAddress(address, adding) });
      try {
        const headers = await auth.getAuth();
        if (!headers) {
          auth.login();
          throw new Error("session unavailable");
        }
        if (adding) await api.add(address, headers);
        else await api.remove(address, headers);
      } catch (cause) {
        set({ addresses: withAddress(address, !adding) });
        if (cause instanceof ApiProblem && (cause.code === "watchlist_full" || cause.status === 409)) {
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
    list: (auth) => getWatchlist(baseUrl, auth) as Promise<WatchlistList>,
    add: (address, auth) => addWatchlist(baseUrl, address, auth),
    remove: (address, auth) => removeWatchlist(baseUrl, address, auth),
  };
}

export function useWatchlist() {
  const session = useContext(WatchlistAuthContext);
  const wallet = useWalletConnection();
  const snapshot = useSyncExternalStore(store.subscribe, store.getSnapshot, () => EMPTY_SNAPSHOT);
  const configuration = publicConfiguration();
  const baseUrl = configuration.status === "ready" ? configuration.apiBaseUrl : null;
  // Where Privy is not mounted yet (read-only routes), signing in loads the wallet stack.
  const auth = useMemo<WatchlistAuth>(
    () => (session.available ? session : { ...INERT_AUTH, ready: true, login: wallet.open }),
    [session, wallet.open],
  );
  const deps = useMemo<WatchlistDeps | null>(
    () => (baseUrl ? { api: apiFor(baseUrl), auth } : null),
    [auth, baseUrl],
  );
  const { ready, authenticated } = auth;

  useEffect(() => {
    if (!deps || !ready) return;
    if (!authenticated) {
      if (store.getSnapshot().status !== "idle") store.reset();
      return;
    }
    if (store.getSnapshot().status === "idle") void store.load(deps);
  }, [authenticated, deps, ready]);

  return {
    /** The star can act: an API exists and the reader can sign in somehow. */
    available: deps !== null && (session.available || wallet.available),
    ready,
    authenticated,
    login: auth.login,
    status: snapshot.status,
    full: snapshot.full,
    entries: snapshot.entries,
    toast: snapshot.toast,
    has: (address: string) => snapshot.addresses.has(key(address)),
    toggle: async (address: string) => {
      if (deps) await store.toggle(address, deps);
    },
    reload: async () => {
      if (deps) await store.load(deps);
    },
    dismissToast: store.dismissToast,
  };
}
