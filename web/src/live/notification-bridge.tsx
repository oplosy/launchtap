"use client";

import { useEffect, useMemo } from "react";
import { useWatchlist } from "@/watchlist/use-watchlist";
import { useWalletConnection } from "@/wallet/connection-context";
import { NO_IDENTITY, useNotificationIdentity } from "./notifications";

/**
 * Rendered inside the wallet stack: pushes the signed-in state, watched tokens and connected
 * wallet into the notification provider that lives above route changes.
 */
export function NotificationIdentityBridge() {
  const setIdentity = useNotificationIdentity();
  const wallet = useWalletConnection();
  const watchlist = useWatchlist();
  const watchedKey = useMemo(
    () =>
      [...watchlist.addresses]
        .map((address) => address.toLowerCase())
        .sort()
        .join(","),
    [watchlist.addresses],
  );
  const signedIn = watchlist.authenticated;
  const address = wallet.connected ? wallet.address : undefined;

  useEffect(() => {
    setIdentity?.({
      signedIn,
      watched: signedIn && watchedKey ? watchedKey.split(",") : [],
      wallet: address,
    });
  }, [address, setIdentity, signedIn, watchedKey]);
  useEffect(() => () => setIdentity?.(NO_IDENTITY), [setIdentity]);
  return null;
}
