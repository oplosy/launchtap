import { useCallback, useEffect, useRef, useState } from "react";
import { ApiClient, type TokenListQuery, type TokenListResponse } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import type { LiveEvent } from "@/api/types";
import { useLiveEvents } from "@/live/live-events";
import { publicConfiguration } from "@/config/public";
import { type TokenListQueryState } from "./query-state";
import { loadTokenDiscoveryPage } from "./controller";

export type TokenListFetcher = (
  query: TokenListQuery,
  signal?: AbortSignal,
) => Promise<TokenListResponse>;

const DISCOVERY_REFRESH_INTERVAL_MS = 2_000;

/**
 * Loads the paged /v1/tokens list for the current URL state and keeps page one fresh from live
 * hints: at most one background refresh per interval, never replacing pages the reader appended.
 */
export function useTokenListPages({
  configuration,
  listActive,
  urlState,
  fetchPage: injectedFetcher,
}: {
  configuration: ReturnType<typeof publicConfiguration>;
  listActive: boolean;
  urlState: TokenListQueryState;
  fetchPage?: TokenListFetcher;
}) {
  const [pages, setPages] = useState<TokenListResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<ApiProblem | Error | null>(null);
  const [resetNotice, setResetNotice] = useState(false);
  const pagesRef = useRef<TokenListResponse[]>([]);
  const requestId = useRef(0);
  const abortRef = useRef<AbortController | null>(null);
  const latestState = useRef(urlState);
  const listActiveRef = useRef(listActive);
  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    latestState.current = urlState;
    listActiveRef.current = listActive;
  }, [listActive, urlState]);

  const loadPage = useCallback(
    async (
      state: TokenListQueryState,
      cursor?: string,
      append = false,
      externalSignal?: AbortSignal,
      // Live refreshes keep the shown cards until the new page arrives, instead of blanking
      // the list to a skeleton (which made every card flash every couple of seconds).
      background = false,
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
      else if (!background) {
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
        // A failed background refresh keeps the list already on screen.
        if (background && pagesRef.current.length > 0) return;
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
        void loadPage(latestState.current, undefined, false, undefined, true);
      }, DISCOVERY_REFRESH_INTERVAL_MS);
    },
    [configuration.chainId, configuration.deploymentId, loadPage],
  );
  const recoverLive = useCallback(
    async (signal?: AbortSignal) => {
      await loadPage(latestState.current, undefined, false, signal, true);
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

  return { pages, loading, loadingMore, error, resetNotice, loadPage };
}
