"use client";

import { describeDelay, useIndexerStatus, type IndexerStatusState } from "./indexer-status";

export function StaleDataBannerView({ state }: { state: IndexerStatusState }) {
  if (!state.stale) return null;
  const delay = describeDelay(state.ageSeconds);
  return (
    <div className="stale-data-banner" role="status">
      {delay
        ? `Live data is delayed: the last update was ${delay} ago. Prices, trades and balances shown here may be out of date.`
        : "Live data is not available yet. Prices, trades and balances shown here may be out of date."}
    </div>
  );
}

/** A site-wide notice while the indexer is behind (`GET /v1/status`). */
export function StaleDataBanner() {
  return <StaleDataBannerView state={useIndexerStatus()} />;
}
