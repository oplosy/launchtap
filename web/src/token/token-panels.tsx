"use client";

import type { components } from "@/api/generated";
import type { TokenDetailResponse } from "@/api/client";
import type { publicConfiguration } from "@/config/public";
import type { FeedItem } from "@/live/feed-store";
import { liveTradeRows } from "@/live/live-strip-model";
import { CoinGeckoAttribution } from "@/market/usd-line";
import { Badge, Button, SafeExternalLink, Tabs } from "@/components/primitives";
import { shortAddress } from "./address";
import type { TokenCollection } from "./pagination";
import type { marketLinks } from "./market-links";
import { finalityTone, finalityLabel } from "./token-format";
import { Trades, Holders, CreatorLine, EarlyBuyers } from "./token-activity";
import type { Tab } from "./use-token-page-data";

type Configuration = ReturnType<typeof publicConfiguration>;

export function ActivityPanel({
  token,
  tokenAddress,
  configuration,
  tab,
  selectTab,
  pages,
  liveFeed,
  collectionLoading,
  collectionError,
  resetNotice,
  loadMore,
}: {
  token: TokenDetailResponse;
  tokenAddress: `0x${string}`;
  configuration: Configuration;
  tab: Tab;
  selectTab: (tab: Tab) => void;
  pages: TokenCollection[];
  liveFeed: { address: string; items: FeedItem[] };
  collectionLoading: boolean;
  collectionError: Error | null;
  resetNotice: boolean;
  loadMore: (cursor: string) => void;
}) {
  const collectionSnapshot = pages[0]?.snapshot;
  const pagesItems: unknown[] = [];
  for (const page of pages) {
    for (const item of page.items ?? []) pagesItems.push(item);
  }
  // Both tab panels stay mounted, but `pages` holds only the active tab's rows.
  const restTrades = (tab === "trades"
    ? pagesItems
    : []) as unknown as components["schemas"]["TradeDTO"][];
  const tradeItems =
    tab === "trades"
      ? [
          ...liveTradeRows(liveFeed.address === tokenAddress ? liveFeed.items : [], restTrades),
          ...restTrades,
        ]
      : [];
  const holderItems = (tab === "holders"
    ? pagesItems
    : []) as unknown as components["schemas"]["HolderDTO"][];
  const nextCursor = pages.at(-1)?.next_cursor;
  return (
    <section className="token-history-panel" aria-labelledby="history-title">
      <div className="token-section-head">
        <h2 id="history-title">Recent activity</h2>
        <CoinGeckoAttribution />
        {collectionSnapshot ? (
          <Badge tone={finalityTone(collectionSnapshot.finality)}>
            {finalityLabel(collectionSnapshot.finality)}
          </Badge>
        ) : null}
      </div>
      {resetNotice ? (
        <p className="discovery-notice" role="status">
          The activity list was refreshed with the latest available data.
        </p>
      ) : null}
      <Tabs
        ariaLabel="Token history"
        value={tab}
        onChange={(value) => selectTab(value as Tab)}
        tabs={[
          {
            value: "trades",
            label: "Recent trades",
            panel: (
              <Trades items={tradeItems} loading={collectionLoading} error={collectionError} />
            ),
          },
          {
            value: "holders",
            label: "Holders",
            panel: (
              <>
                <Holders
                  items={holderItems}
                  totalSupply={token.total_supply}
                  launchBlock={token.launch_block}
                  hasMore={Boolean(nextCursor)}
                  loading={collectionLoading}
                  error={collectionError}
                />
                <EarlyBuyers
                  key={token.address}
                  tokenAddress={token.address}
                  totalSupply={token.total_supply}
                  apiBaseUrl={configuration.status === "ready" ? configuration.apiBaseUrl : null}
                />
              </>
            ),
          },
        ]}
      />
      {nextCursor ? (
        <div className="history-pagination">
          <span>{pagesItems.length} rows</span>
          <Button
            variant="secondary"
            loading={collectionLoading}
            onClick={() => loadMore(nextCursor)}
          >
            Load more
          </Button>
        </div>
      ) : null}
    </section>
  );
}

export function AboutPanel({
  token,
  explorer,
  market,
  configuration,
}: {
  token: TokenDetailResponse;
  explorer: string | null;
  market: ReturnType<typeof marketLinks>;
  configuration: Configuration;
}) {
  return (
    <section className="token-metadata" aria-labelledby="metadata-title">
      <div className="token-section-head">
        <div>
          <h2 id="metadata-title">About this token</h2>
          <p className="section-description">Supplied by the creator, not verified.</p>
        </div>
      </div>
      <p className="token-description">{token.description.trim() || "No description supplied."}</p>
      <div className="token-links">
        <SafeExternalLink href={token.x_url}>X profile</SafeExternalLink>
        <SafeExternalLink href={token.telegram_url}>Telegram</SafeExternalLink>
        {explorer ? (
          <SafeExternalLink href={explorer}>
            Contract {shortAddress(token.address)}
          </SafeExternalLink>
        ) : null}
        {market.dexscreener ? (
          <SafeExternalLink href={market.dexscreener}>Dexscreener</SafeExternalLink>
        ) : null}
        {market.geckoterminal ? (
          <SafeExternalLink href={market.geckoterminal}>GeckoTerminal</SafeExternalLink>
        ) : null}
      </div>
      <CreatorLine
        key={`${token.creator}:${token.address}`}
        creator={token.creator}
        tokenAddress={token.address}
        apiBaseUrl={configuration.status === "ready" ? configuration.apiBaseUrl : null}
      />
    </section>
  );
}

/**
 * Publishes the card's height as `--aside-height`, so a card taller than the viewport sticks by
 * its bottom edge (token.css) instead of scrolling inside itself.
 */
