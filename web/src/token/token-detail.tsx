"use client";

import { type CSSProperties } from "react";
import { resolveApiAssetUrl } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { DEFAULT_E2E_WEB_URL, publicConfiguration } from "@/config/public";
import { reviewedDeployments } from "@/contracts/generated";
import { addressExplorerUrl } from "@/wallet/explorer";
import { WatchlistToaster } from "@/watchlist/star-button";
import { CommentPanel } from "@/comments/comment-panel";
import { marketLinks } from "./market-links";
import { addressHue } from "./address";
import { MetadataEditor } from "./metadata-editor";
import { launchFacts } from "./launch-facts";
import { v2Facts } from "./v2-facts";
import { V2FactsPanel, LaunchFactsPanel } from "./facts-panels";
import { TokenUnavailable, TokenMissing, TokenLoadError, TokenSkeleton } from "./token-states";
import { useTokenPageData } from "./use-token-page-data";
import { useTradeSheet } from "./use-trade-sheet";
import {
  AboutPanel,
  ActivityPanel,
  ChartPanel,
  MarketStats,
  TokenHeader,
  TradeBar,
  TradeSheet,
} from "./token-sections";

type TokenDetailProps = { address: `0x${string}` };
export { intervals, CANDLE_LIMIT } from "./use-token-page-data";

export function TokenDetail({
  address,
  fixture = false,
}: TokenDetailProps & { fixture?: boolean }) {
  const baseConfiguration = publicConfiguration();
  const useFixture = fixture && process.env.NEXT_PUBLIC_E2E_FIXTURE === "1";
  const configuration = useFixture
    ? {
        ...baseConfiguration,
        status: "ready" as const,
        apiBaseUrl: process.env.NEXT_PUBLIC_TASK6_ANVIL_WEB_URL || DEFAULT_E2E_WEB_URL,
        chainId: 4663,
        deploymentId: "robinhood-mainnet",
        deployment: reviewedDeployments[0] ?? null,
      }
    : baseConfiguration;
  const tokenAddress = address.toLowerCase() as `0x${string}`;
  const {
    token,
    tokenError,
    loading,
    interval,
    setInterval,
    chartPoints,
    chartLoading,
    chartError,
    tab,
    selectTab,
    pages,
    collectionLoading,
    collectionError,
    resetNotice,
    liveFeed,
    priceFlash,
    commentSignal,
    loadToken,
    loadCandles,
    loadCollection,
  } = useTokenPageData(tokenAddress, configuration, useFixture);
  const { sheetSide, setSheetSide, asideRef } = useTradeSheet();
  const explorer = addressExplorerUrl(configuration, tokenAddress);
  const market = marketLinks(configuration.deployment, token?.pair ?? null, token?.phase ?? "");
  const currentImageUrl = resolveApiAssetUrl(configuration.apiBaseUrl, token?.image_url);

  if (configuration.status !== "ready") return <TokenUnavailable address={tokenAddress} />;
  if (loading) return <TokenSkeleton />;
  if (tokenError instanceof ApiProblem && tokenError.status === 404)
    return <TokenMissing address={tokenAddress} />;
  if (tokenError || !token) return <TokenLoadError onRetry={() => window.location.reload()} />;

  const facts = v2Facts(token);
  const engineOneFacts = launchFacts(token);
  const liquidityIndexed = facts?.liquidityIndexed ?? true;
  const title = token.name.trim() || token.symbol.trim() || "Unnamed token";
  const snapshot = token.snapshot;
  const showTrade = !useFixture;
  return (
    <div
      className="token-page"
      data-trade-bar={showTrade ? "" : undefined}
      style={{ "--hue": addressHue(token.address) } as CSSProperties}
    >
      <TokenHeader
        token={token}
        title={title}
        currentImageUrl={currentImageUrl}
        priceFlash={priceFlash}
        configuration={configuration}
        useFixture={useFixture}
        tokenAddress={tokenAddress}
        explorer={explorer}
      />
      <WatchlistToaster />

      {snapshot.finality === "stale" || snapshot.finality === "provisional" ? (
        <p className="token-trust-notice" role="status">
          This data is still updating and may change as the chain settles.
        </p>
      ) : null}

      <div className="token-layout" data-single={showTrade ? undefined : ""}>
        <div className="token-main">
          <ChartPanel
            tokenAddress={tokenAddress}
            interval={interval}
            setInterval={setInterval}
            points={chartPoints}
            loading={chartLoading}
            error={chartError}
            onRetry={() => void loadCandles()}
          />
          <MarketStats token={token} liquidityIndexed={liquidityIndexed} />

          {facts ? <V2FactsPanel facts={facts} configuration={configuration} /> : null}
          {engineOneFacts ? (
            <LaunchFactsPanel token={token} facts={engineOneFacts} configuration={configuration} />
          ) : null}
          <ActivityPanel
            token={token}
            tokenAddress={tokenAddress}
            configuration={configuration}
            tab={tab}
            selectTab={selectTab}
            pages={pages}
            liveFeed={liveFeed}
            collectionLoading={collectionLoading}
            collectionError={collectionError}
            resetNotice={resetNotice}
            loadMore={(cursor) => void loadCollection(tab, cursor, true)}
          />

          {!useFixture && configuration.status === "ready" && configuration.apiBaseUrl ? (
            <CommentPanel
              key={tokenAddress}
              tokenAddress={tokenAddress}
              apiBaseUrl={configuration.apiBaseUrl}
              liveSignal={commentSignal}
            />
          ) : null}
          <AboutPanel
            token={token}
            explorer={explorer}
            market={market}
            configuration={configuration}
          />
          {!useFixture ? (
            <MetadataEditor
              token={{
                address: token.address,
                description: token.description,
                x_url: token.x_url,
                telegram_url: token.telegram_url,
                image_url: token.image_url,
              }}
              onConflict={loadToken}
            />
          ) : null}
        </div>

        {showTrade ? (
          <TradeSheet
            token={token}
            sheetSide={sheetSide}
            setSheetSide={setSheetSide}
            asideRef={asideRef}
          />
        ) : null}
      </div>

      {showTrade ? <TradeBar sheetSide={sheetSide} setSheetSide={setSheetSide} /> : null}
    </div>
  );
}
