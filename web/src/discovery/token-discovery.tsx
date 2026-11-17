"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { ApiProblem } from "@/api/problems";
import { queryKeys, snapshotIdentity, type Snapshot } from "@/api/types";
import { publicConfiguration } from "@/config/public";
import { CoinGeckoAttribution } from "@/market/usd-line";
import { WatchlistTab } from "@/watchlist/watchlist-tab";
import {
  AGE_LABELS,
  EXPLORE_VIEWS,
  SORT_LABELS,
  TOKEN_AGES,
  VIEW_LABELS,
  decodeExploreView,
  decodeTokenListQuery,
  defaultTokenListQuery,
  encodeTokenListQuery,
  exploreListQuery,
  mergeOwnedParams,
  normalizeTokenListQuery,
  sameTokenListQuery,
  tokenListFilters,
  type ExploreView,
  type TokenAge,
  type TokenListQueryState,
  type TokenPhase,
  type TokenSort,
} from "./query-state";
import { TrendingList, useHighlights } from "./highlights";
import { TokenCard, TokenListHead } from "./token-card";
import { Button, EmptyState, ErrorState, Input, Skeleton } from "@/components/primitives";
import { SlidingIndicator } from "@/components/sliding-indicator";

import { useTokenListPages, type TokenListFetcher } from "./use-token-list-pages";

export type { TokenListFetcher } from "./use-token-list-pages";

type Layout = "list" | "grid";

/** The Sort choices per view; "" is Trending's own ranking. Views without one hide the control. */
const SORT_OPTIONS: Partial<Record<ExploreView, ReadonlyArray<{ value: string; label: string }>>> =
  {
    trending: [
      { value: "", label: "Trending" },
      { value: "market_cap", label: SORT_LABELS.market_cap },
      { value: "volume_24h", label: SORT_LABELS.volume_24h },
    ],
    graduated: [
      { value: "", label: "Newest" },
      { value: "market_cap", label: SORT_LABELS.market_cap },
      { value: "volume_24h", label: SORT_LABELS.volume_24h },
    ],
  };

/** The /explore list: view tabs, one control row and the token list, all driven by the URL. */
export function TokenDiscovery({ fetchPage: injectedFetcher }: { fetchPage?: TokenListFetcher }) {
  const configuration = publicConfiguration();
  const pathname = usePathname();
  const router = useRouter();
  const searchParams = useSearchParams();
  const searchString = searchParams.toString();
  const view = decodeExploreView(searchString);
  const listQuery = exploreListQuery(view, searchParams);
  const urlQ = decodeTokenListQuery(searchString).q;
  const urlAge: TokenAge = decodeTokenListQuery(searchString).age;
  const listActive = listQuery !== null;
  const nextState: TokenListQueryState = {
    q: urlQ,
    phase: listQuery?.phase ?? "curve",
    sort: listQuery?.sort ?? "newest",
    age: listActive ? urlAge : "all",
  };
  // Keep one object identity until a field the list reads changes, so unrelated URL writes do
  // not re-run the list's effects.
  const [urlState, setUrlState] = useState(nextState);
  if (!sameTokenListQuery(urlState, nextState)) setUrlState(nextState);
  const highlights = useHighlights(view === "trending" ? configuration.apiBaseUrl : null);
  const [layout, setLayout] = useState<Layout>("grid");
  const [searchDraft, setSearchDraft] = useState(urlState.q);
  const searchEditedRef = useRef(false);

  useEffect(() => {
    searchEditedRef.current = false;
    // URL navigation is an external synchronization point, including browser back/forward.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSearchDraft(urlState.q);
  }, [listActive, urlState]);

  // Writes only the params a control owns; every other param is carried over untouched.
  const writeOwnedParams = useCallback(
    (owned: Record<string, string | null>) => {
      const query = mergeOwnedParams(searchParams, owned);
      router.push((query ? `${pathname}?${query}` : pathname) as never, { scroll: false });
    },
    [pathname, router, searchParams],
  );

  useEffect(() => {
    const timer = setTimeout(() => {
      if (!searchEditedRef.current || searchDraft.trim() === urlState.q) return;
      writeOwnedParams({ q: searchDraft.trim() });
    }, 300);
    return () => clearTimeout(timer);
  }, [searchDraft, urlState, writeOwnedParams]);

  const { pages, loading, loadingMore, error, resetNotice, loadPage } = useTokenListPages({
    configuration,
    listActive,
    urlState,
    fetchPage: injectedFetcher,
  });

  const snapshot: Snapshot | undefined = pages[0]?.snapshot;
  const items = pages.flatMap((page) => page.items ?? []);
  const nextCursor = pages.at(-1)?.next_cursor;
  const queryKey = queryKeys.tokens(
    configuration.chainId,
    configuration.deploymentId,
    tokenListFilters(urlState),
    snapshot,
  );

  const viewHref = (next: ExploreView) => {
    const query = mergeOwnedParams(searchParams, {
      view: next === "trending" ? null : next,
      sort: null,
    });
    return query ? `${pathname}?${query}` : pathname;
  };
  const chooseAge = (next: TokenAge) => {
    if (next !== urlAge) writeOwnedParams({ age: next === "all" ? null : next });
  };
  const sortOptions = SORT_OPTIONS[view];
  const sortValue = listQuery && listQuery.sort !== "newest" ? listQuery.sort : "";
  const retry = () => void loadPage(urlState);

  return (
    <div className="explore" data-query-key={JSON.stringify(queryKey)}>
      <nav className="explore-tabs" aria-label="Explore views">
        {EXPLORE_VIEWS.map((option) => (
          <Link
            key={option}
            href={viewHref(option) as never}
            scroll={false}
            className="explore-tab"
            aria-current={view === option ? "page" : undefined}
          >
            {VIEW_LABELS[option]}
          </Link>
        ))}
        <SlidingIndicator />
      </nav>

      {view !== "watchlist" ? (
        <div className="explore-controls">
          <div className="explore-search" role="search">
            <Input
              label="Search tokens"
              placeholder="Search name or symbol"
              value={searchDraft}
              onChange={(event) => {
                searchEditedRef.current = true;
                setSearchDraft(event.target.value);
              }}
              maxLength={120}
              autoComplete="off"
            />
          </div>
          {sortOptions ? (
            <label className="explore-sort">
              <span>Sort</span>
              <select
                className="ui-input"
                value={sortValue}
                onChange={(event) =>
                  writeOwnedParams({ sort: (event.target.value as TokenSort | "") || null })
                }
              >
                {sortOptions.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          {listActive ? (
            <div className="ui-segment" role="group" aria-label="Launch age">
              {TOKEN_AGES.map((option) => (
                <button
                  key={option}
                  type="button"
                  aria-pressed={urlAge === option}
                  onClick={() => chooseAge(option)}
                >
                  {AGE_LABELS[option]}
                </button>
              ))}
              <SlidingIndicator />
            </div>
          ) : null}
          <div className="ui-segment" role="group" aria-label="Layout">
            {(["grid", "list"] as const).map((option) => (
              <button
                key={option}
                type="button"
                aria-pressed={layout === option}
                onClick={() => setLayout(option)}
              >
                {option === "list" ? "List" : "Grid"}
              </button>
            ))}
            <SlidingIndicator />
          </div>
        </div>
      ) : null}

      <section className="explore-results" aria-labelledby="explore-list-title">
        <h2 id="explore-list-title" className="sr-only">
          {VIEW_LABELS[view]} tokens
        </h2>
        {resetNotice ? (
          <p className="discovery-notice" role="status">
            The list was refreshed with the latest available routes.
          </p>
        ) : null}
        {configuration.status !== "ready" ? (
          <div className="discovery-state">
            <UnavailableDiscovery />
          </div>
        ) : view === "watchlist" ? (
          <WatchlistTab apiBaseUrl={configuration.apiBaseUrl} />
        ) : !listActive ? (
          highlights.failed ? (
            <div className="discovery-state">
              <ErrorState
                title="Trending unavailable"
                description="Trending tokens could not be loaded. You can browse new launches instead."
                action={
                  <>
                    <Button onClick={() => router.push(viewHref("new") as never)}>
                      Show new launches
                    </Button>
                    <Button variant="quiet" onClick={highlights.retry}>
                      Retry
                    </Button>
                  </>
                }
              />
            </div>
          ) : highlights.loading || !highlights.data ? (
            <TokenListSkeleton layout={layout} />
          ) : (
            <>
              {highlights.stale ? (
                <p className="discovery-stale" role="status">
                  Trending may be out of date. Retrying automatically.
                </p>
              ) : null}
              <TrendingList
                items={highlights.data.trending ?? []}
                apiBaseUrl={configuration.apiBaseUrl}
                layout={layout}
              />
            </>
          )
        ) : error ? (
          <div className="discovery-state">
            <ErrorState
              title="Token list unavailable"
              description={
                error instanceof ApiProblem
                  ? error.message
                  : "Token routes are temporarily unavailable. Try again shortly."
              }
              action={<Button onClick={retry}>Retry</Button>}
            />
          </div>
        ) : loading ? (
          <TokenListSkeleton layout={layout} />
        ) : items.length === 0 ? (
          <div className="discovery-state">
            <EmptyState
              title={urlState.q ? "No matching tokens" : "No tokens yet"}
              description={
                urlState.q
                  ? "Try a different name or symbol."
                  : "New launches will appear here as they become available."
              }
              action={
                urlState.q ? (
                  <Button
                    variant="quiet"
                    onClick={() => {
                      setSearchDraft("");
                      writeOwnedParams({ q: null });
                    }}
                  >
                    Clear search
                  </Button>
                ) : null
              }
            />
          </div>
        ) : (
          <>
            <div
              className="token-list"
              data-layout={layout}
              aria-live="polite"
              aria-busy={loadingMore}
            >
              <TokenListHead />
              {items.map((token) => (
                <TokenCard
                  key={token.address}
                  token={token}
                  apiBaseUrl={configuration.apiBaseUrl}
                />
              ))}
            </div>
            <div className="discovery-pagination">
              {nextCursor ? (
                <Button
                  variant="secondary"
                  loading={loadingMore}
                  onClick={() => void loadPage(urlState, nextCursor, true)}
                >
                  Load more
                </Button>
              ) : null}
            </div>
          </>
        )}
        <div className="explore-attribution">
          <CoinGeckoAttribution />
        </div>
      </section>
    </div>
  );
}

export function TokenListSkeleton({ layout = "list" }: { layout?: Layout }) {
  return (
    <div
      className="token-list"
      data-layout={layout}
      role="region"
      aria-label="Loading token routes"
      aria-busy="true"
    >
      {Array.from({ length: 4 }, (_, index) => (
        <div className="token-card token-card-skeleton" key={index}>
          <Skeleton className="token-image" />
          <div className="token-skeleton-copy">
            <Skeleton />
            <Skeleton />
          </div>
        </div>
      ))}
    </div>
  );
}

function UnavailableDiscovery() {
  return (
    <section className="ui-state ui-unavailable" aria-live="polite">
      <span className="ui-state-mark" aria-hidden="true">
        /
      </span>
      <div>
        <h2>Token discovery unavailable</h2>
        <p>Token routes are temporarily unavailable. Try again shortly.</p>
      </div>
    </section>
  );
}

export function canonicalDiscoveryQuery(search: string, defaultPhase: TokenPhase = "curve") {
  return encodeTokenListQuery(
    normalizeTokenListQuery(decodeTokenListQuery(search, defaultPhase), defaultPhase),
    defaultPhase,
  );
}

export {
  defaultTokenListQuery,
  decodeTokenListQuery,
  encodeTokenListQuery,
  sameTokenListQuery,
  snapshotIdentity,
};
