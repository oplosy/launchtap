"use client";
/* Data loading is an external synchronization boundary for this route. */
/* eslint-disable react-hooks/set-state-in-effect */

import { useCallback, useEffect, useRef, useState } from "react";
import { reloadLoadedPages } from "./wallet-model";
import type { Page } from "./wallet-format";

export type ListState<T> = {
  items: T[];
  next: string | null;
  loading: boolean;
  error: Error | null;
  more: () => void;
};

/**
 * Cursor-paged list that loads once `enabled`, and whenever `version` changes reloads as many rows
 * as are already on screen (page by page), so a live refetch does not collapse "load more" pages.
 * A reload aborts an in-flight "load more". Rows already shown are not repeated when pages overlap.
 */
export function usePagedList<T>(
  enabled: boolean,
  version: number,
  fetchPage: (cursor: string | undefined, signal: AbortSignal) => Promise<Page<T>>,
  identity: (item: T) => string,
): ListState<T> {
  const [state, setState] = useState<Omit<ListState<T>, "more">>({
    items: [],
    next: null,
    loading: false,
    error: null,
  });
  const fetchRef = useRef(fetchPage);
  const identityRef = useRef(identity);
  const loadedRef = useRef(0);
  const moreAbortRef = useRef<AbortController | null>(null);
  useEffect(() => {
    fetchRef.current = fetchPage;
    identityRef.current = identity;
    loadedRef.current = state.items.length;
  });
  useEffect(() => () => moreAbortRef.current?.abort(), []);
  useEffect(() => {
    if (!enabled) return;
    // The reload supersedes a "load more" still in flight; its rows would come from older data.
    moreAbortRef.current?.abort();
    moreAbortRef.current = null;
    const controller = new AbortController();
    setState((current) => ({ ...current, loading: true, error: null }));
    reloadLoadedPages(fetchRef.current, identityRef.current, loadedRef.current, controller.signal)
      .then((page) => {
        if (controller.signal.aborted) return;
        setState({ items: page.items, next: page.next, loading: false, error: null });
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setState((current) => ({
          ...current,
          loading: false,
          error: cause instanceof Error ? cause : new Error("Request failed"),
        }));
      });
    return () => controller.abort();
  }, [enabled, version]);
  const next = state.next;
  const more = useCallback(() => {
    if (!next) return;
    moreAbortRef.current?.abort();
    const controller = new AbortController();
    moreAbortRef.current = controller;
    setState((current) => ({ ...current, loading: true, error: null }));
    fetchRef
      .current(next, controller.signal)
      .then((page) => {
        if (controller.signal.aborted) return;
        setState((current) => {
          const seen = new Set(current.items.map(identityRef.current));
          const fresh = (page.items ?? []).filter((item) => !seen.has(identityRef.current(item)));
          return {
            items: [...current.items, ...fresh],
            next: page.next_cursor ?? null,
            loading: false,
            error: null,
          };
        });
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setState((current) => ({
          ...current,
          loading: false,
          error: cause instanceof Error ? cause : new Error("Request failed"),
        }));
      })
      .finally(() => {
        if (moreAbortRef.current === controller) moreAbortRef.current = null;
      });
  }, [next]);
  return { ...state, more };
}
