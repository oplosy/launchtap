"use client";

import { useIdentityToken, usePrivy } from "@privy-io/react-auth";
import { useCallback, useContext } from "react";
import { E2ESessionContext } from "@/e2e/session-context";

/**
 * The signed-in account as the account pages see it: Privy's session, or in the fixture build a
 * browser test's fixed session standing in for Privy (src/e2e).
 */
export function useAccountSession() {
  const testSession = useContext(E2ESessionContext);
  const privy = usePrivy();
  const { identityToken: privyIdentityToken } = useIdentityToken();
  const privyGetAccessToken = privy.getAccessToken;
  const getAccessToken = useCallback(
    async () => (testSession ? testSession.accessToken : privyGetAccessToken()),
    [privyGetAccessToken, testSession],
  );
  return {
    testSession,
    privy,
    authenticated: testSession ? true : privy.authenticated,
    ready: testSession ? true : privy.ready,
    getAccessToken,
    identityToken: testSession ? testSession.identityToken : privyIdentityToken,
  };
}
