"use client";
/* Data loading is an external synchronization boundary for this route. */
/* eslint-disable react-hooks/set-state-in-effect */

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { ApiClient, type WalletPositionsResponse, type WalletResponse } from "@/api/client";
import type { LiveEvent } from "@/api/types";
import { Button, ErrorState, Skeleton, Tabs, UnavailableState } from "@/components/primitives";
import { publicConfiguration } from "@/config/public";
import { isEventInScope, liveRefreshKind } from "@/live/live-strip-model";
import { useLiveEvents } from "@/live/live-events";
import { UsdLine } from "@/market/usd-line";
import { shortAddress } from "@/token/address";
import { createLiveRefetch, isWalletTrade } from "./wallet-model";
import { eth, Pnl, type Created, type Position, type WalletTrade } from "./wallet-format";
import { usePagedList } from "./use-paged-list";
import { CreatedTable, Holdings, Metric, TradesTable } from "./wallet-tables";
import { WalletShare } from "./wallet-share";
import { CopyAddress } from "@/components/copy-address";

const PAGE_SIZE = 25;

export function WalletView({ address }: { address: string }) {
  const configuration = publicConfiguration();
  const baseUrl = configuration.apiBaseUrl;
  const ready = configuration.status === "ready" && Boolean(baseUrl);
  const chainId = configuration.chainId ?? -1;
  const deploymentId = configuration.deploymentId ?? "";
  const client = useMemo(() => (baseUrl ? new ApiClient({ baseUrl }) : null), [baseUrl]);

  const [version, setVersion] = useState(0);
  const [overview, setOverview] = useState<WalletResponse | null>(null);
  const [open, setOpen] = useState<WalletPositionsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [tab, setTab] = useState("holdings");
  const [showClosed, setShowClosed] = useState(false);

  // One shared stream; bursts of this wallet's trades collapse into a single refetch, followed by
  // one delayed refetch for figures the position worker recomputes after the event.
  const coalescer = useMemo(() => createLiveRefetch(() => setVersion((value) => value + 1)), []);
  useEffect(() => () => coalescer.cancel(), [coalescer]);
  useLiveEvents((event: LiveEvent) => {
    const scope = { chainId, deploymentId };
    if (liveRefreshKind(event, scope) !== null) coalescer.trigger();
    else if (isEventInScope(event, scope) && isWalletTrade(event, address)) coalescer.trigger();
  });

  useEffect(() => {
    if (!ready || !client) {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    setError(null);
    Promise.all([
      client.getWallet(address, controller.signal),
      client.listWalletPositions(address, { closed: false }, controller.signal),
    ])
      .then(([nextOverview, nextOpen]) => {
        if (controller.signal.aborted) return;
        setOverview(nextOverview);
        setOpen(nextOpen);
        setLoading(false);
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setError(cause instanceof Error ? cause : new Error("Wallet unavailable"));
        setLoading(false);
      });
    return () => controller.abort();
  }, [address, client, ready, version]);

  const listsReady = ready && client !== null;
  const closed = usePagedList<Position>(
    listsReady && tab === "holdings" && showClosed,
    version,
    (cursor, signal) =>
      client!.listWalletPositions(address, { closed: true, cursor, limit: PAGE_SIZE }, signal),
    (item) => item.token,
  );
  const trades = usePagedList<WalletTrade>(
    listsReady && tab === "trades",
    version,
    (cursor, signal) => client!.listWalletTrades(address, { cursor, limit: PAGE_SIZE }, signal),
    (item) => `${item.tx_hash}:${item.log_index}`,
  );
  const created = usePagedList<Created>(
    listsReady && tab === "created",
    version,
    (cursor, signal) => client!.listWalletCreated(address, { cursor, limit: PAGE_SIZE }, signal),
    (item) => item.token,
  );

  if (!ready)
    return (
      <div className="page-stack wallet-page">
        <UnavailableState
          title="Wallet unavailable"
          description={`The reviewed deployment is not configured. ${shortAddress(address)} was not loaded.`}
        />
      </div>
    );

  return (
    <div className="page-stack wallet-page">
      <section className="page-hero">
        <div>
          <div className="section-kicker">Wallet</div>
          {overview?.username ? (
            <h1 className="wallet-title" title={address}>
              @{overview.username}
            </h1>
          ) : (
            <h1 className="mono wallet-title" title={address}>
              {shortAddress(address)}
            </h1>
          )}
          <div className="wallet-actions">
            <CopyAddress address={address} />
            <WalletShare address={address} />
            <Link className="ui-button ui-button-quiet ui-button-sm" href="/leaderboard">
              Leaderboard
            </Link>
          </div>
        </div>
      </section>
      {error ? (
        <ErrorState
          title="Wallet data is unavailable"
          description="The indexed wallet snapshot could not be loaded. No estimated values are being substituted."
          action={<Button onClick={() => setVersion((value) => value + 1)}>Retry</Button>}
        />
      ) : null}
      {loading && !overview ? (
        <div className="analytics-grid">
          {[1, 2, 3, 4].map((item) => (
            <div className="workspace-panel metric-panel" key={item}>
              <Skeleton />
              <Skeleton />
            </div>
          ))}
        </div>
      ) : null}
      {overview ? (
        <>
          {!overview.attribution_complete ? (
            <section className="wallet-banner" role="status">
              <strong>PnL may be incomplete</strong>
              <span>
                Some pool trades are still waiting for their sender to be resolved, so figures can
                change.
              </span>
            </section>
          ) : null}
          <section className="analytics-grid" aria-label="Wallet totals">
            <Metric label="Realized PnL" value={<Pnl wei={overview.realized_pnl_wei} />} />
            <Metric label="Unrealized PnL" value={<Pnl wei={overview.unrealized_pnl_wei} />} />
            <Metric
              label="Portfolio value"
              value={
                <>
                  {eth(overview.portfolio_value_wei)}
                  <UsdLine wei={overview.portfolio_value_wei} />
                </>
              }
            />
            <Metric label="Holding" value={overview.held_count.toLocaleString("en-US")} />
            <Metric label="Tokens traded" value={overview.tokens_traded.toLocaleString("en-US")} />
            <Metric label="Created" value={overview.created_count.toLocaleString("en-US")} />
          </section>
          <section className="workspace-panel wallet-panel" aria-label="Wallet activity">
            <Tabs
              ariaLabel="Wallet sections"
              value={tab}
              onChange={setTab}
              tabs={[
                {
                  value: "holdings",
                  label: "Holdings",
                  panel: (
                    <Holdings
                      open={open}
                      closed={closed}
                      showClosed={showClosed}
                      onToggleClosed={setShowClosed}
                    />
                  ),
                },
                { value: "trades", label: "Trades", panel: <TradesTable list={trades} /> },
                { value: "created", label: "Created", panel: <CreatedTable list={created} /> },
              ]}
            />
          </section>
          <p className="ui-field-hint">Pool trades are attributed to the transaction sender.</p>
        </>
      ) : null}
    </div>
  );
}
