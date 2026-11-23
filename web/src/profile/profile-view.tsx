"use client";

import Link from "next/link";
import { useCallback } from "react";
import { useAccountSession } from "@/account/use-account-session";
import { publicConfiguration } from "@/config/public";
import { Button, Badge, UnavailableState } from "@/components/primitives";
import { useWalletReadiness } from "@/wallet/readiness";
import { WalletConnectButton } from "@/wallet/connection-context";
import { shortAddress } from "@/token/address";
import { PublicProfileForm } from "./public-profile-form";

export function ProfileView() {
  const configuration = publicConfiguration();
  const { testSession, privy, authenticated, ready, getAccessToken, identityToken } =
    useAccountSession();
  const { login, logout } = privy;
  const readiness = useWalletReadiness();
  const userId = testSession ? testSession.userId : privy.user?.id;
  const email = testSession
    ? { address: testSession.email ?? undefined }
    : (privy.user?.linkedAccounts?.find((item) => item.type === "email") as
        { address?: string } | undefined);
  const linked = testSession
    ? testSession.linkedWallets.map((address) => ({ address, kind: "wallet" as const }))
    : readiness.linkedWallets;
  const getAuth = useCallback(async () => {
    const accessToken = await getAccessToken();
    return accessToken && identityToken ? { accessToken, identityToken } : null;
  }, [getAccessToken, identityToken]);
  // A fixture build without Privy renders this view only for a test session.
  if (!testSession && !configuration.privyAppId)
    return (
      <UnavailableState
        title="Account unavailable"
        description="Account access is not configured for this environment. Public launch data remains available."
      />
    );
  if (!ready)
    return (
      <section className="workspace-panel profile-auth" aria-live="polite">
        <h2>Loading your account</h2>
        <p>Checking the current sign-in session and linked wallets.</p>
      </section>
    );
  if (!authenticated) {
    return (
      <section className="workspace-panel profile-auth">
        <h2>Sign in to view your account</h2>
        <p>
          Choose a wallet or another available sign-in method. Connecting does not give Launchpad
          custody of your assets.
        </p>
        <Button variant="primary" onClick={() => login()}>
          Sign in
        </Button>
      </section>
    );
  }
  return (
    <div className="profile-layout">
      <section
        className="workspace-panel profile-identity"
        aria-labelledby="profile-identity-title"
      >
        <div className="panel-head">
          <div>
            <p className="panel-kicker">Account</p>
            <h2 id="profile-identity-title">Your account</h2>
          </div>
          <Badge tone="success">Signed in</Badge>
        </div>
        <dl className="profile-details">
          <div>
            <dt>Account ID</dt>
            <dd className="mono">{userId ?? "Unavailable"}</dd>
          </div>
          <div>
            <dt>Email</dt>
            <dd>{email?.address ?? "Not linked"}</dd>
          </div>
          <div>
            <dt>Selected network</dt>
            <dd>
              {readiness.status === "wrong-chain"
                ? "Wrong chain"
                : readiness.chainId
                  ? `Chain ${readiness.chainId}`
                  : "Disconnected"}
            </dd>
          </div>
        </dl>
        <Button variant="quiet" onClick={() => void logout()}>
          Sign out
        </Button>
      </section>
      {configuration.status === "ready" && configuration.apiBaseUrl && identityToken ? (
        <PublicProfileForm
          apiBaseUrl={configuration.apiBaseUrl}
          linkedWallets={linked.map((wallet) => wallet.address)}
          getAuth={getAuth}
        />
      ) : null}
      <section className="workspace-panel" aria-labelledby="profile-wallets-title">
        <div className="panel-head">
          <div>
            <p className="panel-kicker">Wallets</p>
            <h2 id="profile-wallets-title">Linked wallets</h2>
          </div>
          <Badge tone={readiness.linkedWalletState === "linked" ? "success" : "warning"}>
            {readiness.linkedWalletState}
          </Badge>
        </div>
        {linked.length ? (
          <ul className="wallet-list">
            {linked.map((wallet) => (
              <li key={wallet.address}>
                <span className="mono">{shortAddress(wallet.address)}</span>
                <span>
                  {wallet.kind === "smart_wallet" ? "Smart wallet" : "Wallet"}
                  {wallet.address === readiness.address ? " · selected" : ""}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <div className="profile-wallet-empty">
            <p>No EVM wallet is linked to this account.</p>
            <WalletConnectButton className="profile-wallet-button" variant="secondary" />
          </div>
        )}
        <p className="ui-field-hint">Only a linked wallet can update tokens it created.</p>
      </section>
      <section className="workspace-panel profile-claims" aria-labelledby="profile-claims-title">
        <div className="panel-head">
          <div>
            <p className="panel-kicker">Rewards</p>
            <h2 id="profile-claims-title">Creator fees and refunds</h2>
          </div>
        </div>
        <p>
          What your linked wallets can claim, across every token, is on the{" "}
          <Link href="/rewards">Rewards page</Link>.
        </p>
      </section>
    </div>
  );
}
