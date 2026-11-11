"use client";

import Link from "next/link";
import { useEffect, useRef } from "react";
import { Button, EmptyState, ErrorState, Skeleton } from "@/components/primitives";
import { TokenCard, TokenListHead } from "@/discovery/token-card";
import { shortAddress } from "@/token/address";
import { StarButton } from "./star-button";
import { useWatchlist } from "./use-watchlist";

/** The Explore "Watchlist" view: the signed-in reader's saved tokens. */
export function WatchlistTab({ apiBaseUrl }: { apiBaseUrl: string | null }) {
  const watchlist = useWatchlist();
  const { reload, prepare, ready, authenticated, sessionAvailable, availability } = watchlist;
  const preparedRef = useRef(false);

  useEffect(() => {
    // Read-only routes mount no session; load the wallet stack (without opening the picker).
    // Once per mount: a failed load is retried only by the Retry button, never in a loop.
    if (availability === "loading" && !preparedRef.current) {
      preparedRef.current = true;
      prepare();
    }
  }, [availability, prepare]);

  useEffect(() => {
    // Refresh on entering the tab so tokens starred elsewhere appear. A load already in
    // flight is shared by the store, so repeated runs never restart the request.
    if (ready && authenticated) void reload();
  }, [authenticated, ready, reload]);

  if (availability === "failed")
    return (
      <div className="discovery-state">
        <ErrorState
          title="Watchlist unavailable"
          description="Sign-in could not be loaded. Check your connection and try again."
          action={<Button onClick={prepare}>Retry</Button>}
        />
      </div>
    );
  if (!watchlist.available)
    return (
      <div className="discovery-state">
        <EmptyState
          title="Watchlist unavailable"
          description="Sign-in is not available right now, so your watchlist cannot be shown."
        />
      </div>
    );
  if (!sessionAvailable || !ready)
    return (
      <p className="route-state" role="status" aria-live="polite">
        Loading your account…
      </p>
    );
  if (!authenticated)
    return (
      <div className="discovery-state">
        <EmptyState
          title="Sign in to see your watchlist"
          description="Star tokens to follow them here. Signing in does not give Launchpad custody of your assets."
          action={
            <Button variant="primary" onClick={watchlist.login}>
              Sign in
            </Button>
          }
        />
      </div>
    );
  if (watchlist.status === "error")
    return (
      <div className="discovery-state">
        <ErrorState
          title="Watchlist unavailable"
          description="Your watchlist could not be loaded. Try again shortly."
          action={<Button onClick={() => void reload()}>Retry</Button>}
        />
      </div>
    );
  if (watchlist.status !== "ready")
    return (
      <div className="token-list" role="region" aria-label="Loading watchlist" aria-busy="true">
        {Array.from({ length: 3 }, (_, index) => (
          <div className="token-card token-card-skeleton" key={index}>
            <Skeleton className="token-image" />
            <div className="token-skeleton-copy">
              <Skeleton />
              <Skeleton />
              <Skeleton />
            </div>
          </div>
        ))}
      </div>
    );
  const entries = watchlist.entries.filter((entry) => watchlist.has(entry.address));
  if (entries.length === 0)
    return (
      <div className="discovery-state">
        <EmptyState
          title="Your watchlist is empty"
          description="Tap the star on any token to add it here."
        />
      </div>
    );
  return (
    <div className="token-list" aria-live="polite">
      <TokenListHead />
      {entries.map((entry) =>
        // The API returns no token summary for entries it can no longer resolve.
        entry.token ? (
          <TokenCard key={entry.address} token={entry.token} apiBaseUrl={apiBaseUrl} />
        ) : (
          <article className="token-card token-card-unavailable" key={entry.address}>
            <div className="token-card-main">
              <div className="token-identity">
                <h3>
                  <Link href={`/token/${entry.address}` as never}>
                    {shortAddress(entry.address, 4)}
                  </Link>
                </h3>
                <p className="token-symbol">
                  <span>Unavailable</span>
                </p>
              </div>
              <StarButton address={entry.address} label={shortAddress(entry.address, 4)} />
            </div>
          </article>
        ),
      )}
    </div>
  );
}
