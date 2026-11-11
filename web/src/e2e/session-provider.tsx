"use client";

import { useMemo, type PropsWithChildren } from "react";
import { WatchlistAuthContext, type WatchlistAuth } from "@/watchlist/use-watchlist";
import { E2ESessionContext, type E2ESession } from "./session-context";

/**
 * Test-only stand-in for the Privy session, loaded by `Providers` only in the E2E fixture build
 * and only when a test set `window.__e2eSession`. It feeds the same contexts Privy feeds, so the
 * comment composer, inbox and account views run their real code against mocked API routes.
 */
export function E2ESessionProvider({
  session,
  children,
}: PropsWithChildren<{ session: E2ESession }>) {
  const auth = useMemo<WatchlistAuth>(
    () => ({
      available: true,
      ready: true,
      authenticated: true,
      login: () => undefined,
      getAuth: async () => ({
        accessToken: session.accessToken,
        identityToken: session.identityToken,
      }),
    }),
    [session],
  );
  return (
    <E2ESessionContext.Provider value={session}>
      <WatchlistAuthContext.Provider value={auth}>{children}</WatchlistAuthContext.Provider>
    </E2ESessionContext.Provider>
  );
}
