"use client";

import Link from "next/link";
import { formatBaseUnits } from "@/amounts";
import { useAccountSession } from "@/account/use-account-session";
import { Button, UnavailableState } from "@/components/primitives";
import { publicConfiguration } from "@/config/public";
import { CoinGeckoAttribution, UsdLine } from "@/market/usd-line";
import { useWalletReadiness } from "@/wallet/readiness";
import { useClaimActions, type ClaimAction } from "./use-claim-actions";

function formatEth(value: string | bigint) {
  try {
    return `${formatBaseUnits(BigInt(value), 18, 6)} ETH`;
  } catch {
    return "Unavailable";
  }
}

/** Sums one wei field across the actions; null when any value is not a whole number. */
function total(actions: readonly ClaimAction[], field: "creator_fees" | "refund") {
  try {
    return actions.reduce((sum, action) => sum + BigInt(action[field]), 0n);
  } catch {
    return null;
  }
}

/**
 * The signed-in creator's rewards: what their linked wallets can claim across every token, with
 * each token's claim on its own page (the curve's claim transaction lives in the trading panel).
 */
export function RewardsView() {
  const configuration = publicConfiguration();
  const session = useAccountSession();
  const readiness = useWalletReadiness();
  const apiBaseUrl =
    configuration.status === "ready" && configuration.apiBaseUrl ? configuration.apiBaseUrl : null;
  const { actions, loading, error } = useClaimActions({
    authenticated: session.authenticated,
    identityToken: session.identityToken,
    apiBaseUrl,
    getAccessToken: session.getAccessToken,
  });
  if (!session.testSession && !configuration.privyAppId)
    return (
      <UnavailableState
        title="Rewards unavailable"
        description="Account access is not configured for this environment. Public launch data remains available."
      />
    );
  if (!session.ready)
    return (
      <section className="workspace-panel rewards-panel" aria-live="polite">
        <h2>Loading your rewards</h2>
        <p>Checking the current sign-in session and linked wallets.</p>
      </section>
    );
  if (!session.authenticated)
    return (
      <section className="workspace-panel rewards-panel">
        <h2>Sign in to see your rewards</h2>
        <p>
          Creator fees and refunds are read for the wallets linked to your account. Signing in does
          not give Launchpad custody of your assets.
        </p>
        <Button variant="primary" onClick={() => session.privy.login()}>
          Sign in
        </Button>
      </section>
    );
  const fees = total(actions, "creator_fees");
  const refunds = total(actions, "refund");
  return (
    <section className="workspace-panel rewards-panel" aria-labelledby="rewards-yours-title">
      <h2 id="rewards-yours-title">Your rewards</h2>
      <dl className="rewards-totals">
        <div>
          <dt>Creator fees to claim</dt>
          <dd>
            {fees === null ? "Unavailable" : formatEth(fees)}
            {fees === null ? null : <UsdLine wei={fees.toString()} />}
          </dd>
        </div>
        <div>
          <dt>Refunds to claim</dt>
          <dd>
            {refunds === null ? "Unavailable" : formatEth(refunds)}
            {refunds === null ? null : <UsdLine wei={refunds.toString()} />}
          </dd>
        </div>
      </dl>
      {loading ? <p role="status">Loading authoritative availability…</p> : null}
      {error ? <p className="discovery-notice">{error}</p> : null}
      {!loading && !error && actions.length === 0 ? (
        <p>
          Nothing to claim yet. Fees collect as people trade the tokens you launched.{" "}
          <Link href="/create">Launch a token</Link>
        </p>
      ) : null}
      {actions.length ? (
        <ul className="profile-action-list rewards-list">
          {actions.map((action) => (
            <li key={action.token}>
              <div>
                <Link href={`/token/${action.token}`}>
                  {action.name || action.symbol || action.token}
                </Link>
                <span className="ui-field-hint">{action.phase} · snapshot-bound</span>
              </div>
              <div className="mono profile-action-values">
                <span>
                  Creator fees: {formatEth(action.creator_fees)}{" "}
                  <UsdLine wei={action.creator_fees} />
                </span>
                <span>
                  Refund: {formatEth(action.refund)} <UsdLine wei={action.refund} />
                </span>
              </div>
              <Link
                className="ui-button ui-button-secondary ui-button-sm"
                href={`/token/${action.token}`}
              >
                Claim
              </Link>
            </li>
          ))}
        </ul>
      ) : null}
      <CoinGeckoAttribution />
      {configuration.status !== "ready" || readiness.status !== "ready" ? (
        <p className="discovery-notice">
          Claims unavailable until a reviewed deployment and supported wallet are ready.
        </p>
      ) : null}
    </section>
  );
}
