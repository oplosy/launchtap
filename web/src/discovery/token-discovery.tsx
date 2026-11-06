"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { Plus } from "@phosphor-icons/react";
import { ApiClient, type TokenListQuery, type TokenListResponse } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { queryKeys, snapshotIdentity, type Snapshot } from "@/api/types";
import { SseInvalidationStream } from "@/api/sse";
import { publicConfiguration } from "@/config/public";
import { CoinGeckoAttribution } from "@/market/usd-line";
import { WatchlistTab } from "@/watchlist/watchlist-tab";
import {
  AGE_LABELS,
  EXPLORE_VIEWS,
  TOKEN_AGES,
  VIEW_LABELS,
  decodeExploreView,
  decodeTokenListQuery,
  defaultTokenListQuery,
  encodeTokenListQuery,
  exploreViewSort,
  mergeOwnedParams,
  normalizeTokenListQuery,
  sameTokenListQuery,
  tokenListFilters,
  viewSupportsAge,
  type ExploreView,
  type TokenAge,
  type TokenListQueryState,
  type TokenPhase,
} from "./query-state";
import { KingOfTheHill, TrendingList, useHighlights } from "./highlights";
import { TokenCard } from "./token-card";
import { loadTokenDiscoveryPage } from "./controller";
import { Button, EmptyState, ErrorState, Input, Skeleton } from "@/components/primitives";

export type TokenListFetcher = (
  query: TokenListQuery,
  signal?: AbortSignal,
) => Promise<TokenListResponse>;

type TokenDiscoveryProps = {
  defaultPhase?: TokenPhase;
  title: string;
  summary: string;
  fetchPage?: TokenListFetcher;
  fixedPhase?: TokenPhase;
  showHero?: boolean;
  showSearch?: boolean;
  /** Show the Trending/New/Market cap/Volume/Watchlist tabs, King of the hill and age filter. */
  showViews?: boolean;
  sectionLabel?: string;
};

const DISCOVERY_REFRESH_INTERVAL_MS = 2_000;

export function TokenDiscovery({
  defaultPhase = "curve",
  title,
  summary,
  fetchPage: injectedFetcher,
  fixedPhase,
  showHero = true,
  showSearch = true,
  showViews = false,
  sectionLabel = "",
}: TokenDiscoveryProps) {
  const configuration = publicConfiguration();
  const phaseDefault = fixedPhase ?? defaultPhase;
  const pathname = usePathname();
  const router = useRouter();
  const searchParams = useSearchParams();
  // Each section reads only the params it owns, as primitives, so the other section's URL
  // changes never re-create this section's state or refetch its list.
  const searchString = searchParams.toString();
  const urlQ = decodeTokenListQuery(searchString, phaseDefault).q;
  const urlAge: TokenAge = decodeTokenListQuery(searchString, phaseDefault).age;
  const urlPhase: TokenPhase =
    fixedPhase ??
    normalizeTokenListQuery(decodeTokenListQuery(searchString, phaseDefault), phaseDefault).phase;
  const view: ExploreView | null = showViews ? decodeExploreView(searchString) : null;
  const viewSort = view ? exploreViewSort(view) : null;
  const listSort = viewSort ?? "newest";
  // The age param stays in the URL for views that do not use it, but only ranked lists apply it.
  const listAge: TokenAge = view && viewSupportsAge(view) ? urlAge : "all";
  const nextState: TokenListQueryState = { q: urlQ, phase: urlPhase, sort: listSort, age: listAge };
  // Keep one object identity until a field this section reads changes, so another section's
  // URL writes do not re-run this section's effects.
  const [urlState, setUrlState] = useState(nextState);
  if (!sameTokenListQuery(urlState, nextState)) setUrlState(nextState);
  const listActive = !showViews || viewSort !== null;
  const listActiveRef = useRef(listActive);
  const highlights = useHighlights(showViews ? configuration.apiBaseUrl : null);
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
  const chainSnapshotRef = useRef<Snapshot | undefined>(undefined);
  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    latestState.current = urlState;
    listActiveRef.current = listActive;
    searchEditedRef.current = false;
    // URL navigation is an external synchronization point, including browser back/forward.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSearchDraft(urlState.q);
  }, [listActive, urlState]);

  // Writes only the params this section owns; every other param is carried over untouched.
  const writeOwnedParams = useCallback(
    (owned: Record<string, string | null>) => {
      const query = mergeOwnedParams(searchParams, owned);
      router.push((query ? `${pathname}?${query}` : pathname) as never, { scroll: false });
    },
    [pathname, router, searchParams],
  );

  useEffect(() => {
    if (fixedPhase || defaultPhase !== "graduated" || searchParams.get("phase") === null) return;
    const query = encodeTokenListQuery(urlState, defaultPhase);
    router.replace((query ? `${pathname}?${query}` : pathname) as never, { scroll: false });
  }, [defaultPhase, fixedPhase, pathname, router, searchParams, urlState]);

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
        chainSnapshotRef.current = undefined;
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
        chainSnapshotRef.current = result.pages[0]?.snapshot;
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
    // Trending and Watchlist are not paged token lists, so they do not fetch /v1/tokens.
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

  useEffect(() => {
    if (configuration.status !== "ready" || !configuration.apiBaseUrl || injectedFetcher) return;
    const stream = new SseInvalidationStream({
      url: `${configuration.apiBaseUrl}/v1/events`,
      queryClient: {
        invalidateQueries: async () => {
          // Hints arrive for every trade on every token. Refresh page one at most once per
          // interval, and never replace pages the reader has already appended.
          if (!listActiveRef.current || refreshTimerRef.current || pagesRef.current.length > 1)
            return;
          refreshTimerRef.current = setTimeout(() => {
            refreshTimerRef.current = null;
            if (pagesRef.current.length > 1) return;
            void loadPage(latestState.current, undefined, false);
          }, DISCOVERY_REFRESH_INTERVAL_MS);
        },
      },
      queryKeyForEvent: (event) => {
        if (
          event.data.chain_id !== configuration.chainId ||
          event.data.deployment_id !== configuration.deploymentId
        )
          return undefined;
        return queryKeys.tokens(
          configuration.chainId,
          configuration.deploymentId,
          tokenListFilters(latestState.current),
          chainSnapshotRef.current,
        );
      },
      refetchSnapshot: async (signal) => {
        void signal;
        await loadPage(latestState.current, undefined, false, signal);
      },
    });
    const stop = stream.start();
    return () => {
      stop();
      if (refreshTimerRef.current) clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
    };
  }, [
    configuration.apiBaseUrl,
    configuration.chainId,
    configuration.deploymentId,
    configuration.status,
    injectedFetcher,
    loadPage,
  ]);

  const firstPage = pages[0];
  const snapshot: Snapshot | undefined = firstPage?.snapshot;
  const items = pages.flatMap((page) => page.items ?? []);
  const nextCursor = pages.at(-1)?.next_cursor;
  const queryKey = queryKeys.tokens(
    configuration.chainId,
    configuration.deploymentId,
    tokenListFilters(urlState),
    snapshot,
  );

  const chooseView = (next: ExploreView) => {
    if (next !== view) writeOwnedParams({ view: next === "trending" ? null : next, sort: null });
  };
  const chooseAge = (next: TokenAge) => {
    if (next !== urlAge) writeOwnedParams({ age: next === "all" ? null : next });
  };
  const retry = () => void loadPage(urlState);
  const listTitleId = fixedPhase ? `${fixedPhase}-list-title` : "list-title";

  return (
    <div
      className={`${showHero ? "page-stack" : "discovery-section"} discovery-page`}
      data-query-key={JSON.stringify(queryKey)}
    >
      {showHero ? (
        <section className="page-hero" aria-labelledby="discovery-title">
          <div>
            <div className="section-kicker">Token ledger</div>
            <h1 id="discovery-title">{title}</h1>
            <p className="hero-summary">{summary}</p>
          </div>
          <div className="hero-aside">
            <span className="hero-rule" />
            <p>Follow each route from curve to graduation. Wallet actions stay in your control.</p>
          </div>
        </section>
      ) : null}

      {showSearch ? (
        <div className="discovery-toolbar">
          <div className="discovery-searchbar" role="search">
            <Input
              label="Search tokens"
              placeholder="Name or symbol"
              value={searchDraft}
              onChange={(event) => {
                searchEditedRef.current = true;
                setSearchDraft(event.target.value);
              }}
              maxLength={120}
              autoComplete="off"
            />
          </div>
          <Link
            className="ui-button ui-button-secondary ui-button-sm discovery-create-button"
            href="/create"
          >
            <Plus aria-hidden="true" size={16} weight="bold" />
            <span>Create</span>
          </Link>
        </div>
      ) : null}

      <section className="discovery-panel" aria-labelledby={listTitleId}>
        <div className="panel-head discovery-controls-head">
          <div>
            {sectionLabel ? <p className="panel-kicker">{sectionLabel}</p> : null}
            <h2 id={listTitleId}>{title}</h2>
            <p className="section-description">{summary}</p>
            <CoinGeckoAttribution />
          </div>
        </div>
        {showViews ? (
          <>
            {highlights.data?.king_of_the_hill ? (
              <KingOfTheHill
                king={highlights.data.king_of_the_hill}
                apiBaseUrl={configuration.apiBaseUrl}
              />
            ) : null}
            <div className="discovery-filter-groups" aria-label="Explore filters">
              <div className="discovery-filter-group" role="group" aria-label="Explore view">
                {EXPLORE_VIEWS.map((option) => (
                  <button
                    className={`discovery-filter ${view === option ? "is-active" : ""}`}
                    key={option}
                    type="button"
                    aria-pressed={view === option}
                    onClick={() => chooseView(option)}
                  >
                    {VIEW_LABELS[option]}
                  </button>
                ))}
              </div>
              {view && viewSupportsAge(view) ? (
                <div className="discovery-filter-group" role="group" aria-label="Launch age">
                  {TOKEN_AGES.map((option) => (
                    <button
                      className={`discovery-filter ${urlAge === option ? "is-active" : ""}`}
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
            </div>
          </>
        ) : null}
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
        ) : view === "trending" ? (
          highlights.failed ? (
            <div className="discovery-state">
              <ErrorState
                title="Trending unavailable"
                description="Trending tokens could not be loaded. You can browse new launches instead."
                action={
                  <>
                    <Button onClick={() => chooseView("new")}>Show new launches</Button>
                    <Button variant="quiet" onClick={highlights.retry}>
                      Retry
                    </Button>
                  </>
                }
              />
            </div>
          ) : highlights.loading || !highlights.data ? (
            <TokenListSkeleton />
          ) : (
            <TrendingList
              items={highlights.data.trending ?? []}
              apiBaseUrl={configuration.apiBaseUrl}
            />
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
          <TokenListSkeleton />
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
            <div className="token-list" aria-live="polite" aria-busy={loadingMore}>
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
                  loading={loadingMore}
                  onClick={() => void loadPage(urlState, nextCursor, true)}
                >
                  Load more
                </Button>
              ) : null}
            </div>
          </>
        )}
      </section>
    </div>
  );
}

function TokenListSkeleton() {
  return (
    <div className="token-list" role="region" aria-label="Loading token routes" aria-busy="true">
      {Array.from({ length: 4 }, (_, index) => (
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
