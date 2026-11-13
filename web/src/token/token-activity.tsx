"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import type { components } from "@/api/generated";
import { ApiClient } from "@/api/client";
import { UsdLine } from "@/market/usd-line";
import { Badge, EmptyState, ErrorState, Skeleton } from "@/components/primitives";
import { walletPath } from "@/portfolio/wallet-model";
import { parseTokenAddress, shortAddress } from "./address";
import { creatorHistoryLine } from "./creator-history";
import { earlyBuyersView, type EarlyBuyersView } from "./early-buyers";
import { launchBlockHolders, topHolderShare } from "./holder-concentration";
import {
  amount,
  finalityTone,
  finalityLabel,
  safeTokenAmount,
  safeMarketValue,
} from "./token-format";

export function Trades({
  items,
  loading,
  error,
}: {
  items: components["schemas"]["TradeDTO"][];
  loading: boolean;
  error: Error | null;
}) {
  if (loading && !items.length)
    return (
      <div className="history-state">
        <Skeleton />
        <Skeleton />
        <Skeleton />
      </div>
    );
  if (error)
    return (
      <ErrorState title="Trades unavailable" description="Trade history could not be loaded." />
    );
  if (!items.length)
    return (
      <EmptyState title="No trades yet" description="This token has no recorded trades yet." />
    );
  return (
    <div className="history-table" role="table" aria-label="Recent trades">
      <div className="history-row history-heading" role="row">
        <span>Time</span>
        <span>Trader</span>
        <span>Side</span>
        <span>Token volume</span>
        <span>ETH volume</span>
        <span>Execution / spot</span>
        <span>Finality</span>
      </div>
      {items.map((item) => (
        <div
          className="history-row"
          role="row"
          key={`${item.tx_hash}-${item.log_index}`}
          data-pending={item.finality === "observed" ? "" : undefined}
        >
          <span className="mono">{safeDate(item.time)}</span>
          <span className="mono">
            <WalletLink address={item.trader} />
          </span>
          <span className={item.side === "buy" ? "trade-buy" : "trade-sell"}>
            {item.side || "—"}
          </span>
          <span className="mono">{safeTokenAmount(item.token_volume)}</span>
          <span className="mono">
            {amount(item.eth_volume)}
            <UsdLine wei={item.eth_volume} />
          </span>
          <span className="mono trade-prices">
            {item.execution_price === "" ? (
              "—"
            ) : (
              <>
                {safeMarketValue(item.execution_price)} /{" "}
                {/* A live row has no spot price until the next REST refresh. */}
                {item.spot_price === "" ? "—" : safeMarketValue(item.spot_price)}
                <UsdLine wei={item.execution_price} />
              </>
            )}
          </span>
          <span>
            <Badge tone={finalityTone(item.finality)}>{finalityLabel(item.finality)}</Badge>
          </span>
        </div>
      ))}
    </div>
  );
}
export function Holders({
  items,
  totalSupply,
  launchBlock,
  hasMore,
  loading,
  error,
}: {
  items: components["schemas"]["HolderDTO"][];
  totalSupply: string;
  launchBlock: number | undefined;
  hasMore: boolean;
  loading: boolean;
  error: Error | null;
}) {
  if (loading && !items.length)
    return (
      <div className="history-state">
        <Skeleton />
        <Skeleton />
        <Skeleton />
      </div>
    );
  if (error)
    return (
      <ErrorState title="Holders unavailable" description="Holder data could not be loaded." />
    );
  if (!items.length)
    return (
      <EmptyState title="No holders yet" description="No holder balances are available yet." />
    );
  // The list arrives largest first with the curve, pool, locker and burn addresses excluded.
  const concentration = topHolderShare(
    items.map((item) => item.balance),
    totalSupply,
  );
  const launchLine = launchBlockHolders(items, launchBlock, totalSupply, hasMore);
  return (
    <>
      {concentration ? (
        <p className="holder-concentration">
          Top {concentration.holders} {concentration.holders === 1 ? "holder owns" : "holders own"}{" "}
          <strong>{concentration.percent}</strong> of the supply (curve, pool and burned tokens
          excluded from the list).
        </p>
      ) : null}
      {launchLine ? <p className="holder-concentration">{launchLine}</p> : null}
      <div className="history-table" role="table" aria-label="Token holders">
        <div className="history-row history-heading" role="row">
          <span>Address</span>
          <span>Balance</span>
          <span>First acquired block</span>
        </div>
        {items.map((item) => (
          <div className="history-row holder-row" role="row" key={item.address}>
            <span className="mono">
              <WalletLink address={item.address} />
            </span>
            <span className="mono">{safeTokenAmount(item.balance)}</span>
            <span className="mono">
              {Number.isSafeInteger(item.first_acquired_block)
                ? item.first_acquired_block.toLocaleString("en-US")
                : "Unavailable"}
            </span>
          </div>
        ))}
      </div>
    </>
  );
}
/**
 * Who launched the token and what else that wallet launched here, from the public created-tokens
 * list. Keyed by creator and token, so a new token starts without the previous history.
 */
export function CreatorLine({
  creator,
  tokenAddress,
  apiBaseUrl,
}: {
  creator: string;
  tokenAddress: string;
  apiBaseUrl: string | null;
}) {
  const parsed = parseTokenAddress(creator);
  const [history, setHistory] = useState<string | null>(null);
  useEffect(() => {
    if (!parsed || !apiBaseUrl) return;
    const controller = new AbortController();
    void new ApiClient({ baseUrl: apiBaseUrl })
      .listWalletCreated(parsed, { limit: 100 }, controller.signal)
      .then((page) => {
        if (!controller.signal.aborted)
          setHistory(creatorHistoryLine(page.items ?? [], tokenAddress, Boolean(page.next_cursor)));
      })
      // The history is optional context; the creator link stays without it.
      .catch(() => undefined);
    return () => controller.abort();
  }, [apiBaseUrl, parsed, tokenAddress]);
  if (!parsed) return null;
  return (
    <p className="token-creator">
      Created by <WalletLink address={parsed} />.{history ? ` ${history}` : null}
    </p>
  );
}
/**
 * Early wallets linked by same-transaction deliveries, token transfers or a shared funding
 * source (early-buyer clusters specs, Levels 1 and 2). Keyed by token, so a new token starts empty; an unavailable read shows nothing.
 */
export function EarlyBuyers({
  tokenAddress,
  totalSupply,
  apiBaseUrl,
}: {
  tokenAddress: string;
  totalSupply: string;
  apiBaseUrl: string | null;
}) {
  const [view, setView] = useState<EarlyBuyersView | null>(null);
  useEffect(() => {
    if (!apiBaseUrl) return;
    const controller = new AbortController();
    void new ApiClient({ baseUrl: apiBaseUrl })
      .getEarlyBuyers(tokenAddress, controller.signal)
      .then((body) => {
        if (!controller.signal.aborted) setView(earlyBuyersView(body, totalSupply));
      })
      // Optional context under the holders list; the list stays without it.
      .catch(() => undefined);
    return () => controller.abort();
  }, [apiBaseUrl, tokenAddress, totalSupply]);
  if (!view) return null;
  return (
    <section className="early-buyers" aria-labelledby="early-buyers-title">
      <h3 id="early-buyers-title">Linked early wallets</h3>
      <p>{view.summary}</p>
      {view.groups.length ? (
        <ul className="early-buyers-groups">
          {view.groups.map((group, index) => (
            <li key={group.wallets[0]?.address ?? index}>
              <details>
                <summary>
                  <strong>{group.title}</strong>
                  {group.links ? <span>: {group.links}</span> : null}
                  {group.includesCreator ? (
                    <>
                      {" "}
                      <span className="early-buyers-creator">Creator</span>
                    </>
                  ) : null}
                </summary>
                <ul className="early-buyers-wallets">
                  {group.wallets.map((wallet) => (
                    <li key={wallet.address}>
                      <WalletLink address={wallet.address} />
                      <span className="mono">{safeTokenAmount(wallet.balance)}</span>
                    </li>
                  ))}
                </ul>
                {group.unlisted ? <p>and {group.unlisted} more</p> : null}
              </details>
            </li>
          ))}
        </ul>
      ) : null}
      {view.emptyGroups ? <p>{view.emptyGroups}</p> : null}
      {view.moreGroups ? <p>and {view.moreGroups} more linked groups</p> : null}
      {view.funding ? <p>{view.funding}</p> : null}
      <p className="early-buyers-note">
        Links are on-chain facts: tokens delivered by the curve to several wallets in one
        transaction, tokens moved between those wallets
        {view.funding ? ", or first ETH sent to them by the same wallet" : ""}. A link does not
        prove one owner.
      </p>
    </section>
  );
}
/** Links an address to its wallet page; a missing or malformed address stays a dash. */
export function WalletLink({ address }: { address: string | null | undefined }) {
  const parsed = parseTokenAddress(address);
  if (!parsed) return <>—</>;
  return (
    <Link href={walletPath(parsed) as never} title={parsed}>
      {shortAddress(parsed)}
    </Link>
  );
}
export function safeDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.valueOf())
    ? "Unavailable"
    : date.toLocaleString("en-US", { dateStyle: "medium", timeStyle: "short" });
}
