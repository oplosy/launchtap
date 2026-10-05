"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Plus } from "@phosphor-icons/react";
import {
  ApiClient,
  resolveApiAssetUrl,
  type TokenListQuery,
  type TokenListResponse,
} from "@/api/client";
import type { components } from "@/api/generated";
import { ApiProblem } from "@/api/problems";
import { queryKeys, snapshotIdentity, type Snapshot } from "@/api/types";
import { SseInvalidationStream } from "@/api/sse";
import { formatDisplayAmount } from "@/amounts";
import { publicConfiguration } from "@/config/public";
import {
  decodeTokenListQuery,
  commitTokenListFilters,
  defaultTokenListQuery,
  encodeTokenListQuery,
  normalizeTokenListQuery,
  sameTokenListQuery,
  tokenListFilters,
  type TokenListQueryState,
  type TokenPhase,
} from "./query-state";
import { loadTokenDiscoveryPage } from "./controller";
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  Input,
  SafeImage,
  Skeleton,
} from "@/components/primitives";

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
  showSortControls?: boolean;
  sectionLabel?: string;
};
type TokenCardData = components["schemas"]["TokenDTO"];

function displayAmount(raw: string, decimals = 18) {
  try {
    return formatDisplayAmount(BigInt(raw), decimals, 4);
  } catch {
    return "Unavailable";
  }
}

function tokenLabel(token: TokenCardData) {
  return token.name.trim() || token.symbol.trim() || "Unnamed token";
}

const DISCOVERY_REFRESH_INTERVAL_MS = 2_000;

const SORT_VIEWS: ReadonlyArray<{
  value: TokenListQueryState["sort"];
  label: string;
}> = [
  { value: "newest", label: "Newest" },
  { value: "oldest", label: "Oldest" },
  { value: "market_cap", label: "Market cap" },
  { value: "volume_24h", label: "Volume" },
];

export function TokenDiscovery({
  defaultPhase = "curve",
  title,
  summary,
  fetchPage: injectedFetcher,
  fixedPhase,
  showHero = true,
  showSearch = true,
  showSortControls = true,
  sectionLabel = "",
}: TokenDiscoveryProps) {
  const configuration = publicConfiguration();
  const phaseDefault = fixedPhase ?? defaultPhase;
  const pathname = usePathname();
  const router = useRouter();
  const searchParams = useSearchParams();
  const urlState = useMemo(
    () =>
      (() => {
        const next = normalizeTokenListQuery(
          decodeTokenListQuery(searchParams.toString(), phaseDefault),
          phaseDefault,
        );
        return fixedPhase ? { ...next, phase: fixedPhase } : next;
      })(),
    [fixedPhase, phaseDefault, searchParams],
  );
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
    searchEditedRef.current = false;
    // URL navigation is an external synchronization point, including browser back/forward.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSearchDraft(urlState.q);
  }, [urlState]);

  const writeUrlState = useCallback(
    (next: TokenListQueryState, replace = false) => {
      const normalized = normalizeTokenListQuery(next, phaseDefault);
      const query = encodeTokenListQuery(
        fixedPhase ? { ...normalized, phase: fixedPhase } : normalized,
        phaseDefault,
      );
      const target = query ? `${pathname}?${query}` : pathname;
      if (replace) router.replace(target as never, { scroll: false });
      else router.push(target as never, { scroll: false });
    },
    [fixedPhase, pathname, phaseDefault, router],
  );

  useEffect(() => {
    if (fixedPhase || defaultPhase !== "graduated" || searchParams.get("phase") === null) return;
    const query = encodeTokenListQuery(urlState, defaultPhase);
    router.replace((query ? `${pathname}?${query}` : pathname) as never, { scroll: false });
  }, [defaultPhase, fixedPhase, pathname, router, searchParams, urlState]);

  useEffect(() => {
    const timer = setTimeout(() => {
      if (!searchEditedRef.current || searchDraft.trim() === urlState.q) return;
      writeUrlState({ ...urlState, q: searchDraft.trim() });
    }, 300);
    return () => clearTimeout(timer);
  }, [searchDraft, urlState, writeUrlState]);

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
    // Fetching is the intended synchronization with the external API when URL state changes.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadPage(urlState);
    return () => abortRef.current?.abort();
  }, [loadPage, urlState]);

  useEffect(() => {
    if (configuration.status !== "ready" || !configuration.apiBaseUrl || injectedFetcher) return;
    const stream = new SseInvalidationStream({
      url: `${configuration.apiBaseUrl}/v1/events`,
      queryClient: {
        invalidateQueries: async () => {
          // Hints arrive for every trade on every token. Refresh page one at most once per
          // interval, and never replace pages the reader has already appended.
          if (refreshTimerRef.current || pagesRef.current.length > 1) return;
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

  const commitFilters = (changes: Partial<Pick<TokenListQueryState, "phase" | "sort">>) => {
    searchEditedRef.current = false;
    writeUrlState(commitTokenListFilters(urlState, searchDraft, changes));
  };
  const chooseSort = (sort: TokenListQueryState["sort"]) => {
    if (urlState.sort !== sort) commitFilters({ sort });
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

      <section className="workspace-panel discovery-panel" aria-labelledby={listTitleId}>
        <div className="panel-head discovery-controls-head">
          <div>
            {sectionLabel ? <p className="panel-kicker">{sectionLabel}</p> : null}
            <h2 id={listTitleId}>{title}</h2>
            <p className="section-description">{summary}</p>
          </div>
          {showSortControls ? (
            <div className="discovery-filter-groups" aria-label="Token list filters">
              <div className="discovery-filter-group" role="group" aria-label="Sort tokens">
                {SORT_VIEWS.map((view) => (
                  <button
                    className={`discovery-filter ${urlState.sort === view.value ? "is-active" : ""}`}
                    key={view.value}
                    type="button"
                    aria-pressed={urlState.sort === view.value}
                    onClick={() => chooseSort(view.value)}
                  >
                    {view.label}
                  </button>
                ))}
              </div>
            </div>
          ) : null}
        </div>
        {resetNotice ? (
          <p className="discovery-notice" role="status">
            The list was refreshed with the latest available routes.
          </p>
        ) : null}
        {configuration.status !== "ready" ? (
          <div className="discovery-state">
            <UnavailableDiscovery />
          </div>
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
                      writeUrlState({ ...urlState, q: "" });
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

function TokenCard({ token, apiBaseUrl }: { token: TokenCardData; apiBaseUrl: string | null }) {
  const label = tokenLabel(token);
  const imageUrl = resolveApiAssetUrl(
    apiBaseUrl,
    `/v1/tokens/${encodeURIComponent(token.address)}/image`,
  );
  return (
    <article className="token-card">
      <div className="token-card-main">
        <SafeImage
          src={imageUrl}
          alt={`${label} token`}
          fallbackLabel="No artwork"
          className="token-image"
        />
        <div className="token-identity">
          <h3>
            <Link href={`/token/${token.address}` as never}>{label}</Link>
          </h3>
          <p className="mono token-symbol">{token.symbol || "—"}</p>
          <p className="mono token-address">{token.address}</p>
        </div>
        <Badge tone={token.phase === "graduated" ? "success" : "accent"}>{token.phase}</Badge>
      </div>
      <dl className="token-metrics">
        <div>
          <dt>Market cap</dt>
          <dd>{displayAmount(token.market_cap_eth)} ETH</dd>
        </div>
        <div>
          <dt>24h volume</dt>
          <dd>{displayAmount(token.volume_24h_eth)} ETH</dd>
        </div>
        <div>
          <dt>Holders</dt>
          <dd>{token.holder_count.toLocaleString("en-US")}</dd>
        </div>
        <div>
          <dt>Launch block</dt>
          <dd>{token.launch_block.toLocaleString("en-US")}</dd>
        </div>
      </dl>
    </article>
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
