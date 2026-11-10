"use client";
/* Data loading is an external synchronization boundary for this route. */
/* eslint-disable react-hooks/set-state-in-effect */

import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ApiClient,
  type WalletCreatedResponse,
  type WalletPositionsResponse,
  type WalletResponse,
  type WalletTradesResponse,
} from "@/api/client";
import type { LiveEvent } from "@/api/types";
import { formatCanonicalBaseUnits, formatSignificantPrice } from "@/amounts";
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  SafeExternalLink,
  Skeleton,
  Tabs,
  UnavailableState,
} from "@/components/primitives";
import { Check, Copy, ShareNetwork } from "@/components/icons";
import { publicConfiguration } from "@/config/public";
import { isEventInScope, liveRefreshKind } from "@/live/live-strip-model";
import { useLiveEvents } from "@/live/live-events";
import { UsdLine } from "@/market/usd-line";
import { publicWebOrigin } from "@/share/share-links";
import { shortAddress } from "@/token/address";
import {
  createLiveRefetch,
  formatSignedEth,
  isWalletTrade,
  pnlTone,
  pnlUsd,
  reloadLoadedPages,
  walletPath,
} from "./wallet-model";

type Position = NonNullable<WalletPositionsResponse["items"]>[number];
type WalletTrade = NonNullable<WalletTradesResponse["items"]>[number];
type Created = NonNullable<WalletCreatedResponse["items"]>[number];
type Page<T> = { items: T[] | null; next_cursor?: string };

const PAGE_SIZE = 25;

function eth(wei: string) {
  const formatted = formatCanonicalBaseUnits(wei, 4);
  return formatted === null ? "Unavailable" : `${formatted} ETH`;
}
function tokenAmount(raw: string) {
  return formatCanonicalBaseUnits(raw, 3) ?? "Unavailable";
}
function safeDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.valueOf())
    ? "Unavailable"
    : date.toLocaleString("en-US", { dateStyle: "medium", timeStyle: "short" });
}
function isTransfer(transferredIn: string) {
  return /^[1-9]\d*$/.test(transferredIn);
}
function Pnl({ wei }: { wei: string }) {
  const usd = pnlUsd(wei);
  return (
    <span className={`pnl pnl-${pnlTone(wei)}`}>
      <span className="pnl-value">{formatSignedEth(wei)}</span>
      <UsdLine wei={usd.wei} negative={usd.negative} />
    </span>
  );
}

type ListState<T> = {
  items: T[];
  next: string | null;
  loading: boolean;
  error: Error | null;
  more: () => void;
};

/**
 * Cursor-paged list that loads once `enabled`, and whenever `version` changes reloads as many rows
 * as are already on screen (page by page), so a live refetch does not collapse "load more" pages.
 * A reload aborts an in-flight "load more". Rows already shown are not repeated when pages overlap.
 */
function usePagedList<T>(
  enabled: boolean,
  version: number,
  fetchPage: (cursor: string | undefined, signal: AbortSignal) => Promise<Page<T>>,
  identity: (item: T) => string,
): ListState<T> {
  const [state, setState] = useState<Omit<ListState<T>, "more">>({
    items: [],
    next: null,
    loading: false,
    error: null,
  });
  const fetchRef = useRef(fetchPage);
  const identityRef = useRef(identity);
  const loadedRef = useRef(0);
  const moreAbortRef = useRef<AbortController | null>(null);
  useEffect(() => {
    fetchRef.current = fetchPage;
    identityRef.current = identity;
    loadedRef.current = state.items.length;
  });
  useEffect(() => () => moreAbortRef.current?.abort(), []);
  useEffect(() => {
    if (!enabled) return;
    // The reload supersedes a "load more" still in flight; its rows would come from older data.
    moreAbortRef.current?.abort();
    moreAbortRef.current = null;
    const controller = new AbortController();
    setState((current) => ({ ...current, loading: true, error: null }));
    reloadLoadedPages(fetchRef.current, identityRef.current, loadedRef.current, controller.signal)
      .then((page) => {
        if (controller.signal.aborted) return;
        setState({ items: page.items, next: page.next, loading: false, error: null });
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setState((current) => ({
          ...current,
          loading: false,
          error: cause instanceof Error ? cause : new Error("Request failed"),
        }));
      });
    return () => controller.abort();
  }, [enabled, version]);
  const next = state.next;
  const more = useCallback(() => {
    if (!next) return;
    moreAbortRef.current?.abort();
    const controller = new AbortController();
    moreAbortRef.current = controller;
    setState((current) => ({ ...current, loading: true, error: null }));
    fetchRef
      .current(next, controller.signal)
      .then((page) => {
        if (controller.signal.aborted) return;
        setState((current) => {
          const seen = new Set(current.items.map(identityRef.current));
          const fresh = (page.items ?? []).filter((item) => !seen.has(identityRef.current(item)));
          return {
            items: [...current.items, ...fresh],
            next: page.next_cursor ?? null,
            loading: false,
            error: null,
          };
        });
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setState((current) => ({
          ...current,
          loading: false,
          error: cause instanceof Error ? cause : new Error("Request failed"),
        }));
      })
      .finally(() => {
        if (moreAbortRef.current === controller) moreAbortRef.current = null;
      });
  }, [next]);
  return { ...state, more };
}

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

function Metric({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <section className="workspace-panel metric-panel">
      <dl>
        <dt>{label}</dt>
        <dd className="metric-value wallet-metric">{value}</dd>
      </dl>
    </section>
  );
}

function TokenLink({ token, symbol, name }: { token: string; symbol: string; name: string }) {
  return (
    <Link
      className="wallet-token-link"
      href={`/token/${token.toLowerCase()}` as never}
      title={name}
    >
      {symbol.trim() || shortAddress(token, 4)}
    </Link>
  );
}

function ListShell<T>({
  list,
  label,
  empty,
  children,
}: {
  list: ListState<T>;
  label: string;
  empty: string;
  children: React.ReactNode;
}) {
  if (list.loading && !list.items.length)
    return (
      <div className="history-state">
        <Skeleton />
        <Skeleton />
        <Skeleton />
      </div>
    );
  if (list.error && !list.items.length)
    return (
      <ErrorState title={`${label} unavailable`} description={`${label} could not be loaded.`} />
    );
  if (!list.items.length)
    return <EmptyState title={`No ${label.toLowerCase()}`} description={empty} />;
  return (
    <>
      {children}
      {list.next ? (
        <div className="wallet-more">
          <Button variant="secondary" onClick={list.more} loading={list.loading}>
            Load more
          </Button>
        </div>
      ) : null}
    </>
  );
}

function TradesTable({ list }: { list: ListState<WalletTrade> }) {
  return (
    <ListShell list={list} label="Trades" empty="This wallet has no recorded trades.">
      <div
        className="history-table wallet-table wallet-trades"
        role="table"
        aria-label="Wallet trades"
      >
        <div className="history-row history-heading" role="row">
          <span>Time</span>
          <span>Token</span>
          <span>Side</span>
          <span>Token volume</span>
          <span>ETH volume</span>
        </div>
        {list.items.map((item) => (
          <div
            className="history-row"
            role="row"
            key={`${item.tx_hash}-${item.log_index}`}
            data-pending={item.finality === "observed" ? "" : undefined}
          >
            <span className="mono">{safeDate(item.time)}</span>
            <span>
              <TokenLink token={item.token} symbol={item.symbol} name={item.name} />
            </span>
            <span className={item.side === "buy" ? "trade-buy" : "trade-sell"}>
              {item.side || "—"}
            </span>
            <span className="mono">{tokenAmount(item.token_volume)}</span>
            <span className="mono">
              {eth(item.eth_volume)}
              <UsdLine wei={item.eth_volume} />
            </span>
          </div>
        ))}
      </div>
    </ListShell>
  );
}

function CreatedTable({ list }: { list: ListState<Created> }) {
  return (
    <ListShell list={list} label="Created tokens" empty="This wallet has not launched a token.">
      <div
        className="history-table wallet-table wallet-created"
        role="table"
        aria-label="Created tokens"
      >
        <div className="history-row history-heading" role="row">
          <span>Token</span>
          <span>Phase</span>
          <span>Market cap</span>
          <span>Launch block</span>
        </div>
        {list.items.map((item) => (
          <div className="history-row" role="row" key={item.token}>
            <span>
              <TokenLink token={item.token} symbol={item.symbol} name={item.name} />
            </span>
            <span>
              <Badge tone={item.phase === "graduated" ? "success" : "neutral"}>{item.phase}</Badge>
            </span>
            <span className="mono">
              {eth(item.market_cap_wei)}
              <UsdLine wei={item.market_cap_wei} />
            </span>
            <span className="mono">{item.launch_block.toLocaleString("en-US")}</span>
          </div>
        ))}
      </div>
    </ListShell>
  );
}

function Holdings({
  open,
  closed,
  showClosed,
  onToggleClosed,
}: {
  open: WalletPositionsResponse | null;
  closed: ListState<Position>;
  showClosed: boolean;
  onToggleClosed: (value: boolean) => void;
}) {
  const openItems = open?.items ?? [];
  const rows = showClosed ? [...openItems, ...closed.items] : openItems;
  return (
    <div className="wallet-holdings">
      <label className="wallet-toggle">
        <input
          type="checkbox"
          checked={showClosed}
          onChange={(event) => onToggleClosed(event.target.checked)}
        />
        Show closed
      </label>
      {!rows.length && !(showClosed && closed.loading) ? (
        <EmptyState
          title="No holdings"
          description={
            showClosed
              ? "This wallet has no open or closed positions."
              : "This wallet holds no tokens."
          }
        />
      ) : (
        <div
          className="history-table wallet-table wallet-positions"
          role="table"
          aria-label="Holdings"
        >
          <div className="history-row history-heading" role="row">
            <span>Token</span>
            <span>Balance</span>
            <span>Avg cost</span>
            <span>Value</span>
            <span>Unrealized</span>
            <span>Realized</span>
            <span>Trades</span>
          </div>
          {rows.map((item) => {
            const isClosed = item.balance === "0";
            return (
              <div
                className="history-row"
                role="row"
                key={item.token}
                data-closed={isClosed ? "" : undefined}
              >
                <span className="wallet-token-cell">
                  <TokenLink token={item.token} symbol={item.symbol} name={item.name} />
                  {isClosed ? <Badge>Closed</Badge> : null}
                  {isTransfer(item.transferred_in) ? <Badge tone="accent">Transfer</Badge> : null}
                </span>
                <span className="mono">{tokenAmount(item.balance)}</span>
                <span className="mono">
                  {item.average_cost_wad === null
                    ? "—"
                    : (formatSignificantPrice(item.average_cost_wad) ?? "Unavailable")}
                </span>
                <span className="mono">
                  {eth(item.current_value_wei)}
                  <UsdLine wei={item.current_value_wei} />
                </span>
                <span className="mono">
                  <Pnl wei={item.unrealized_pnl_wei} />
                </span>
                <span className="mono">
                  <Pnl wei={item.realized_pnl_wei} />
                </span>
                <span className="mono">{item.trade_count.toLocaleString("en-US")}</span>
              </div>
            );
          })}
        </div>
      )}
      {open?.truncated ? (
        <p className="ui-field-hint">
          Showing the 500 open positions with the highest value; smaller ones are not listed.
        </p>
      ) : null}
      {showClosed && closed.error && !closed.items.length ? (
        <ErrorState
          title="Closed positions unavailable"
          description="Closed positions could not be loaded."
        />
      ) : null}
      {showClosed && closed.next ? (
        <div className="wallet-more">
          <Button variant="secondary" onClick={closed.more} loading={closed.loading}>
            Load more closed
          </Button>
        </div>
      ) : null}
    </div>
  );
}

/** Short address with one-click copy; stays a plain label where the clipboard is unavailable. */
function CopyAddress({ address }: { address: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1600);
    return () => window.clearTimeout(timer);
  }, [copied]);
  return (
    <button
      type="button"
      className="copy-address mono"
      title={address}
      aria-label={copied ? "Address copied" : `Copy address ${address}`}
      onClick={() =>
        void navigator.clipboard
          ?.writeText(address)
          .then(() => setCopied(true))
          .catch(() => undefined)
      }
    >
      {shortAddress(address)}
      {copied ? <Check size={13} aria-hidden="true" /> : <Copy size={13} aria-hidden="true" />}
    </button>
  );
}

/** "Share on X" and "Copy link" for the wallet page; the canonical URL is the public web origin. */
function WalletShare({ address }: { address: string }) {
  const [status, setStatus] = useState<"idle" | "copied" | "failed">("idle");
  useEffect(() => {
    if (status === "idle") return;
    const timer = window.setTimeout(() => setStatus("idle"), 2400);
    return () => window.clearTimeout(timer);
  }, [status]);
  const origin = publicWebOrigin();
  const canonical = origin ? `${origin}${walletPath(address)}` : "";
  const shareUrl = canonical
    ? `https://x.com/intent/post?${new URLSearchParams({ text: "Wallet on Launchpad", url: canonical })}`
    : "";
  return (
    <div className="share-actions">
      {shareUrl ? (
        <SafeExternalLink href={shareUrl} className="ui-button ui-button-secondary ui-button-sm">
          <ShareNetwork size={14} aria-hidden="true" /> Share on X
        </SafeExternalLink>
      ) : null}
      <button
        type="button"
        className="ui-button ui-button-secondary ui-button-sm"
        onClick={() => {
          const write = navigator.clipboard?.writeText(canonical || window.location.href);
          if (!write) {
            setStatus("failed");
            return;
          }
          write.then(
            () => setStatus("copied"),
            () => setStatus("failed"),
          );
        }}
      >
        {status === "copied" ? (
          <Check size={14} aria-hidden="true" />
        ) : (
          <Copy size={14} aria-hidden="true" />
        )}{" "}
        Copy link
      </button>
      <span className="share-status" role="status" aria-live="polite">
        {status === "copied"
          ? "Link copied"
          : status === "failed"
            ? "Copy is unavailable here; copy the address bar instead"
            : ""}
      </span>
    </div>
  );
}
