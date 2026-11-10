"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { ApiClient, type TokenListQuery, type TokenListResponse } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { queryKeys, snapshotIdentity, type Snapshot } from "@/api/types";
import type { LiveEvent } from "@/api/types";
import { useLiveEvents } from "@/live/live-events";
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
import { TokenCard } from "./token-card";
import { loadTokenDiscoveryPage } from "./controller";
import { Button, EmptyState, ErrorState, Input, Skeleton } from "@/components/primitives";

export type TokenListFetcher = (
  query: TokenListQuery,
  signal?: AbortSignal,
) => Promise<TokenListResponse>;

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

const DISCOVERY_REFRESH_INTERVAL_MS = 2_000;

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
  const listActiveRef = useRef(listActive);
  const highlights = useHighlights(view === "trending" ? configuration.apiBaseUrl : null);
  const [layout, setLayout] = useState<Layout>("list");
  const [searchDraft, setSearchDraft] = useState(urlState.q);
  const [pages, setPages] = useState<TokenListResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<ApiProblem | Error | null>(null);
  const [resetNotice, setResetNotice] = useState(false);
  const pagesRef = useRef<TokenListResponse[]>([]);
  const requestId = useRef(0);
  const abortRef = useRef<AbortController | null>(null);
  const latestState = useRef(urlState);
  const searchEditedRef = useRef(false);
  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    latestState.current = urlState;
    listActiveRef.current = listActive;
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

  const loadPage = useCallback(
    async (
      state: TokenListQueryState,
      cursor?: string,
      append = false,
      externalSignal?: AbortSignal,
    ) => {
      if (configuration.status !== "ready" || !configuration.apiBaseUrl) {
        setLoading(false);
        setLoadingMore(false);
        return;
      }
      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;
      const abortFromOutside = () => controller.abort();
      externalSignal?.addEventListener("abort", abortFromOutside, { once: true });
      const currentRequest = ++requestId.current;
      setError(null);
      if (append) setLoadingMore(true);
      else {
        setLoading(true);
        setPages([]);
        pagesRef.current = [];
      }
      const fetchPage = injectedFetcher
        ? injectedFetcher
        : (query: TokenListQuery, signal?: AbortSignal) =>
            new ApiClient({ baseUrl: configuration.apiBaseUrl! }).getTokens(query, signal);
      try {
        const result = await loadTokenDiscoveryPage({
          fetchPage,
          state,
          cursor: append ? cursor : undefined,
          existingPages: pagesRef.current,
          signal: controller.signal,
        });
        if (controller.signal.aborted || currentRequest !== requestId.current) return;
        pagesRef.current = result.pages;
        setPages(result.pages);
        setResetNotice(result.reset);
      } catch (cause) {
        if (controller.signal.aborted || currentRequest !== requestId.current) return;
        setError(cause instanceof Error ? cause : new Error("The token list could not be loaded."));
        if (!append) {
          pagesRef.current = [];
          setPages([]);
        }
      } finally {
        externalSignal?.removeEventListener("abort", abortFromOutside);
        if (currentRequest === requestId.current) {
          setLoading(false);
          setLoadingMore(false);
        }
      }
    },
    [configuration.apiBaseUrl, configuration.status, injectedFetcher],
  );
  useEffect(() => {
    // Trending's ranking and Watchlist are not paged token lists, so they do not fetch /v1/tokens.
    if (!listActive) {
      abortRef.current?.abort();
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setLoading(false);
      return;
    }
    // Fetching is the intended synchronization with the external API when URL state changes.
    void loadPage(urlState);
    return () => abortRef.current?.abort();
  }, [listActive, loadPage, urlState]);

  const onLiveEvent = useCallback(
    (event: LiveEvent) => {
      if (event.event !== "launch" && event.event !== "token" && event.event !== "reorg") return;
      if (
        event.data.chain_id !== configuration.chainId ||
        event.data.deployment_id !== configuration.deploymentId
      )
        return;
      // Hints arrive for every trade on every token. Refresh page one at most once per
      // interval, and never replace pages the reader has already appended.
      if (!listActiveRef.current || refreshTimerRef.current || pagesRef.current.length > 1) return;
      refreshTimerRef.current = setTimeout(() => {
        refreshTimerRef.current = null;
        if (pagesRef.current.length > 1) return;
        void loadPage(latestState.current, undefined, false);
      }, DISCOVERY_REFRESH_INTERVAL_MS);
    },
    [configuration.chainId, configuration.deploymentId, loadPage],
  );
  const recoverLive = useCallback(
    async (signal?: AbortSignal) => {
      await loadPage(latestState.current, undefined, false, signal);
    },
    [loadPage],
  );
  useLiveEvents(onLiveEvent, recoverLive);
  useEffect(
    () => () => {
      if (refreshTimerRef.current) clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
    },
    [],
  );

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
            </div>
          ) : null}
          <div className="ui-segment" role="group" aria-label="Layout">
            {(["list", "grid"] as const).map((option) => (
              <button
                key={option}
                type="button"
                aria-pressed={layout === option}
                onClick={() => setLayout(option)}
              >
                {option === "list" ? "List" : "Grid"}
              </button>
            ))}
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
              {items.map((token) => (
                <TokenCard
                  key={token.address}
                  token={token}
                  apiBaseUrl={configuration.apiBaseUrl}
                />
              ))}
            </div>
            <div className="discovery-pagination">
              <span className="discovery-count">
                {items.length} {items.length === 1 ? "token" : "tokens"} shown
              </span>
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
