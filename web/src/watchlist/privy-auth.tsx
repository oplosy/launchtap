"use client";

import { useIdentityToken, usePrivy } from "@privy-io/react-auth";
import { useEffect, useMemo, useRef, type PropsWithChildren } from "react";
import { WatchlistAuthContext, type WatchlistAuth } from "./use-watchlist";

/** Exposes the Privy session to watchlist stars. Mount only inside PrivyProvider. */
export function PrivyWatchlistAuth({ children }: PropsWithChildren) {
  const { ready, authenticated, login, getAccessToken } = usePrivy();
  const { identityToken } = useIdentityToken();
  const identityRef = useRef(identityToken);
  useEffect(() => {
    identityRef.current = identityToken;
  }, [identityToken]);
  const value = useMemo<WatchlistAuth>(
    () => ({
      available: true,
      ready,
      authenticated,
      login: () => login(),
      getAuth: async () => {
        const accessToken = await getAccessToken();
        const identity = identityRef.current;
        return accessToken && identity ? { accessToken, identityToken: identity } : null;
      },
    }),
    [authenticated, getAccessToken, login, ready],
  );
  return <WatchlistAuthContext.Provider value={value}>{children}</WatchlistAuthContext.Provider>;
}
