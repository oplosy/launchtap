"use client";

import Link from "next/link";
import { Button, ErrorState, Skeleton } from "@/components/primitives";
import { shortAddress } from "./address";

export function TokenUnavailable({ address }: { address: string }) {
  return (
    <div className="page-stack token-page">
      <UnavailableBlock
        title="Token unavailable"
        description={`The reviewed deployment is not configured. ${shortAddress(address)} was not loaded.`}
      />
    </div>
  );
}
export function TokenMissing({ address }: { address: string }) {
  return (
    <div className="page-stack token-page">
      <UnavailableBlock
        title="Token not found"
        description={`No token was found for ${shortAddress(address)}.`}
      />
    </div>
  );
}
export function TokenLoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="page-stack token-page">
      <ErrorState
        title="Token unavailable"
        description="This token could not be loaded. Try again."
        action={<Button onClick={onRetry}>Retry</Button>}
      />
    </div>
  );
}
export function UnavailableBlock({ title, description }: { title: string; description: string }) {
  return (
    <section className="workspace-panel token-unavailable">
      <p className="panel-kicker">Token route</p>
      <h1>{title}</h1>
      <p>{description}</p>
      <Link className="ui-button ui-button-secondary ui-button-md" href="/explore">
        Return to Explore
      </Link>
    </section>
  );
}
export function TokenSkeleton() {
  return (
    <div className="token-page" aria-busy="true">
      <section className="token-header token-skeleton">
        <Skeleton className="token-detail-image" />
        <Skeleton className="token-skeleton-title" />
        <Skeleton className="token-skeleton-copy" />
      </section>
      <div className="token-layout">
        <div className="token-main">
          <Skeleton className="chart-skeleton" />
        </div>
        <div className="token-aside">
          <Skeleton className="token-skeleton-trade" />
        </div>
      </div>
    </div>
  );
}
