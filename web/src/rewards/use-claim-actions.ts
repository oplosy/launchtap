/* Claim state follows authenticated external data and is reset when the session changes. */
/* eslint-disable react-hooks/set-state-in-effect */

import { useEffect, useState } from "react";
import { ApiClient, type ProfileResponse } from "@/api/client";

export type ClaimAction = NonNullable<ProfileResponse["items"]>[number];

/**
 * Creator fees and refunds available to the signed-in account's linked wallets, per token, from
 * the authenticated profile read. Empty while signed out or when the API is not configured.
 */
export function useClaimActions({
  authenticated,
  identityToken,
  apiBaseUrl,
  getAccessToken,
}: {
  authenticated: boolean;
  identityToken: string | null | undefined;
  apiBaseUrl: string | null;
  getAccessToken: () => Promise<string | null>;
}) {
  const [actions, setActions] = useState<ClaimAction[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    if (!authenticated || !identityToken || !apiBaseUrl) {
      setActions([]);
      setError(null);
      return;
    }
    setLoading(true);
    setError(null);
    void getAccessToken()
      .then((accessToken) => {
        if (!accessToken) throw new Error("Access token unavailable");
        return new ApiClient({ baseUrl: apiBaseUrl }).getProfile({ accessToken, identityToken });
      })
      .then((response) => {
        if (!cancelled) setActions(response.items ?? []);
      })
      .catch(() => {
        if (!cancelled) {
          setActions([]);
          setError(
            "Authoritative claim availability is unavailable. Open a token detail page to retry.",
          );
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [authenticated, apiBaseUrl, getAccessToken, identityToken]);
  return { actions, loading, error };
}
