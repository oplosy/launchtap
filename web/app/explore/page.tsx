import type { Metadata } from "next";
import { Suspense } from "react";
import { PageHeader } from "@/components/layout";
import { TokenDiscovery, TokenListSkeleton } from "@/discovery/token-discovery";
import { WatchlistToaster } from "@/watchlist/star-button";

export const metadata: Metadata = {
  title: "Explore",
  description: "Every launch on the curve and every token that graduated to Uniswap.",
};

export default function ExplorePage() {
  return (
    <div className="page-stack explore-page">
      <PageHeader
        title="Explore"
        summary="Every launch on the curve and every token that graduated to Uniswap."
      />
      {/* The list reads the URL on the client; the skeleton holds its place until it does. */}
      <Suspense fallback={<TokenListSkeleton />}>
        <TokenDiscovery />
      </Suspense>
      <WatchlistToaster />
    </div>
  );
}
