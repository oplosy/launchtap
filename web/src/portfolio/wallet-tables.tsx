"use client";

import Link from "next/link";
import type { WalletPositionsResponse } from "@/api/client";
import { shortAddress } from "@/token/address";
import { Badge, Button, EmptyState, ErrorState, Skeleton } from "@/components/primitives";
import { formatSignificantPrice } from "@/amounts";
import { PriceText } from "@/components/price-text";
import { UsdLine } from "@/market/usd-line";
import {
  eth,
  isTransfer,
  Pnl,
  safeDate,
  tokenAmount,
  type Created,
  type Position,
  type WalletTrade,
} from "./wallet-format";
import type { ListState } from "./use-paged-list";

export function Metric({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <section className="workspace-panel metric-panel">
      <dl>
        <dt>{label}</dt>
        <dd className="metric-value wallet-metric">{value}</dd>
      </dl>
    </section>
  );
}

export function TokenLink({
  token,
  symbol,
  name,
}: {
  token: string;
  symbol: string;
  name: string;
}) {
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

export function ListShell<T>({
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

export function TradesTable({ list }: { list: ListState<WalletTrade> }) {
  return (
    <ListShell list={list} label="Trades" empty="This wallet has no recorded trades.">
      <div
        className="history-table wallet-table wallet-trades"
        role="table"
        aria-label="Wallet trades"
      >
        <div className="history-row history-heading" role="row">
          <span role="columnheader">Time</span>
          <span role="columnheader">Token</span>
          <span role="columnheader">Side</span>
          <span role="columnheader">Token volume</span>
          <span role="columnheader">ETH volume</span>
        </div>
        {list.items.map((item) => (
          <div
            className="history-row"
            role="row"
            key={`${item.tx_hash}-${item.log_index}`}
            data-pending={item.finality === "observed" ? "" : undefined}
          >
            <span role="cell" className="mono">
              {safeDate(item.time)}
            </span>
            <span role="cell">
              <TokenLink token={item.token} symbol={item.symbol} name={item.name} />
            </span>
            <span role="cell" className={item.side === "buy" ? "trade-buy" : "trade-sell"}>
              {item.side || "—"}
            </span>
            <span role="cell" className="mono">
              {tokenAmount(item.token_volume)}
            </span>
            <span role="cell" className="mono">
              {eth(item.eth_volume)}
              <UsdLine wei={item.eth_volume} />
            </span>
          </div>
        ))}
      </div>
    </ListShell>
  );
}

export function CreatedTable({ list }: { list: ListState<Created> }) {
  return (
    <ListShell list={list} label="Created tokens" empty="This wallet has not launched a token.">
      <div
        className="history-table wallet-table wallet-created"
        role="table"
        aria-label="Created tokens"
      >
        <div className="history-row history-heading" role="row">
          <span role="columnheader">Token</span>
          <span role="columnheader">Phase</span>
          <span role="columnheader">Market cap</span>
          <span role="columnheader">Launch block</span>
        </div>
        {list.items.map((item) => (
          <div className="history-row" role="row" key={item.token}>
            <span role="cell">
              <TokenLink token={item.token} symbol={item.symbol} name={item.name} />
            </span>
            <span role="cell">
              <Badge tone={item.phase === "graduated" ? "success" : "neutral"}>{item.phase}</Badge>
            </span>
            <span role="cell" className="mono">
              {eth(item.market_cap_wei)}
              <UsdLine wei={item.market_cap_wei} />
            </span>
            <span role="cell" className="mono">
              {item.launch_block.toLocaleString("en-US")}
            </span>
          </div>
        ))}
      </div>
    </ListShell>
  );
}

export function Holdings({
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
            <span role="columnheader">Token</span>
            <span role="columnheader">Balance</span>
            <span role="columnheader">Avg cost</span>
            <span role="columnheader">Value</span>
            <span role="columnheader">Unrealized</span>
            <span role="columnheader">Realized</span>
            <span role="columnheader">Trades</span>
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
                <span role="cell" className="wallet-token-cell">
                  <TokenLink token={item.token} symbol={item.symbol} name={item.name} />
                  {isClosed ? <Badge>Closed</Badge> : null}
                  {isTransfer(item.transferred_in) ? <Badge tone="accent">Transfer</Badge> : null}
                </span>
                <span role="cell" className="mono">
                  {tokenAmount(item.balance)}
                </span>
                <span role="cell" className="mono">
                  {item.average_cost_wad === null ? (
                    "—"
                  ) : (
                    <PriceText
                      value={formatSignificantPrice(item.average_cost_wad) ?? "Unavailable"}
                    />
                  )}
                </span>
                <span role="cell" className="mono">
                  {eth(item.current_value_wei)}
                  <UsdLine wei={item.current_value_wei} />
                </span>
                <span role="cell" className="mono">
                  <Pnl wei={item.unrealized_pnl_wei} />
                </span>
                <span role="cell" className="mono">
                  <Pnl wei={item.realized_pnl_wei} />
                </span>
                <span role="cell" className="mono">
                  {item.trade_count.toLocaleString("en-US")}
                </span>
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
