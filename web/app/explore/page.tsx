import type { Metadata } from "next";
import { Suspense } from "react";
import { TokenDiscovery, TokenListSkeleton } from "@/discovery/token-discovery";
import { WatchlistToaster } from "@/watchlist/star-button";

export const metadata: Metadata = {
  title: "Explore",
  description: "Every launch on the curve and every token that graduated to Uniswap.",
};

export default function ExplorePage() {
  return (
    <div className="page-stack explore-page">
      {/* The nav already says where the reader is; the heading stays for screen readers only. */}
      <h1 className="sr-only">Explore</h1>
      {/* The list reads the URL on the client; the skeleton holds its place until it does. */}
      <Suspense fallback={<TokenListSkeleton />}>
        <TokenDiscovery />
      </Suspense>
      <WatchlistToaster />
    </div>
  );
}
