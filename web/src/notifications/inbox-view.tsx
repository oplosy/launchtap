"use client";

import Link from "next/link";
import { useCallback, useContext, useEffect, useRef, useState } from "react";
import { ApiClient, type AuthHeaders, type InboxItem } from "@/api/client";
import { Button, EmptyState, ErrorState, Skeleton } from "@/components/primitives";
import { publicConfiguration } from "@/config/public";
import { parseTokenAddress } from "@/token/address";
import { useWatchlist, WatchlistAuthContext } from "@/watchlist/use-watchlist";
import { appendInboxPage, describeInboxItem, INBOX_PAGE_SIZE, isUnread } from "./inbox-model";

type InboxState =
  | { status: "idle" | "loading" | "error" }
  | {
      status: "ready";
      items: InboxItem[];
      next: string | null;
      unread: number;
      more: "idle" | "loading" | "error";
    };

/** The signed-in reader's server inbox (spec P0-C4): graduations and large trades of watched tokens. */
export function InboxView() {
  const watchlist = useWatchlist();
  const session = useContext(WatchlistAuthContext);
  const { prepare, ready, authenticated, sessionAvailable, availability } = watchlist;
  const [state, setState] = useState<InboxState>({ status: "idle" });
  const preparedRef = useRef(false);
  const generation = useRef(0);
  const configuration = publicConfiguration();
  const baseUrl = configuration.status === "ready" ? configuration.apiBaseUrl : null;

  useEffect(() => {
    // Read-only routes mount no session; load the wallet stack once, without opening the picker.
    if (availability === "loading" && !preparedRef.current) {
      preparedRef.current = true;
      prepare();
    }
  }, [availability, prepare]);

  const auth = useCallback(async (): Promise<AuthHeaders> => {
    const headers = await session.getAuth();
    if (!headers) throw new Error("not signed in");
    return headers;
  }, [session]);

  const load = useCallback(() => {
    if (!baseUrl) return;
    const run = ++generation.current;
    const current = () => run === generation.current;
    const client = new ApiClient({ baseUrl });
    // Every state change happens in a promise callback, never synchronously in the caller.
    void auth()
      .then(async (headers) => {
        if (!current()) return;
        // A previous account's list never stays on screen while this one loads.
        setState({ status: "loading" });
        const page = await client.getNotifications(headers, { limit: INBOX_PAGE_SIZE });
        if (!current()) return;
        const items = page.items ?? [];
        setState({
          status: "ready",
          items,
          next: page.next_cursor ?? null,
          unread: page.unread_count,
          more: "idle",
        });
        // Showing the newest item marks everything up to it read; a failure keeps them unread.
        if (page.unread_count > 0 && items[0])
          void client.markNotificationsRead(items[0].id, headers).catch(() => undefined);
      })
      .catch(() => {
        if (current()) setState({ status: "error" });
      });
  }, [auth, baseUrl]);

  useEffect(() => {
    // Signing out invalidates any load in flight; the signed-out render ignores the old state.
    if (!ready) return;
    if (authenticated) load();
    else generation.current += 1;
  }, [authenticated, load, ready]);

  const loadMore = useCallback(async () => {
    if (!baseUrl || state.status !== "ready" || !state.next || state.more === "loading") return;
    const run = generation.current;
    const cursor = state.next;
    setState({ ...state, more: "loading" });
    try {
      const page = await new ApiClient({ baseUrl }).getNotifications(await auth(), {
        cursor,
        limit: INBOX_PAGE_SIZE,
      });
      if (run !== generation.current) return;
      setState((current) =>
        current.status === "ready"
          ? {
              ...current,
              items: appendInboxPage(current.items, page.items ?? []),
              next: page.next_cursor ?? null,
              more: "idle",
            }
          : current,
      );
    } catch {
      if (run === generation.current)
        setState((current) =>
          current.status === "ready" ? { ...current, more: "error" } : current,
        );
    }
  }, [auth, baseUrl, state]);

  if (!baseUrl || availability === "unavailable")
    return (
      <EmptyState
        title="Inbox unavailable"
        description="Sign-in is not available right now, so your inbox cannot be shown."
      />
    );
  if (availability === "failed")
    return (
      <ErrorState
        title="Inbox unavailable"
        description="Sign-in could not be loaded. Check your connection and try again."
        action={<Button onClick={prepare}>Retry</Button>}
      />
    );
  if (!sessionAvailable || !ready)
    return (
      <p className="route-state" role="status" aria-live="polite">
        Loading your account…
      </p>
    );
  if (!authenticated)
    return (
      <EmptyState
        title="Sign in to see your inbox"
        description="Graduations and trades of 0.5 ETH or more on tokens you watch collect here."
        action={
          <Button variant="primary" onClick={watchlist.login}>
            Sign in
          </Button>
        }
      />
    );
  if (state.status === "error")
    return (
      <ErrorState
        title="Inbox unavailable"
        description="Your notifications could not be loaded. Try again shortly."
        action={<Button onClick={load}>Retry</Button>}
      />
    );
  if (state.status !== "ready")
    return (
      <div className="history-state" role="status" aria-label="Loading notifications">
        <Skeleton />
        <Skeleton />
        <Skeleton />
      </div>
    );
  if (state.items.length === 0)
    return (
      <EmptyState
        title="No notifications yet"
        description="Star tokens to hear about their graduation and trades of 0.5 ETH or more from the last 30 days."
        action={
          <Link
            className="ui-button ui-button-secondary ui-button-md"
            href={"/explore?view=watchlist" as never}
          >
            Open watchlist
          </Link>
        }
      />
    );
  return (
    <div className="inbox">
      <ul className="inbox-list">
        {state.items.map((item, index) => {
          const address = parseTokenAddress(item.token);
          const text = describeInboxItem(item);
          const unread = isUnread(index, state.unread);
          return (
            <li key={item.id} className={unread ? "inbox-item is-unread" : "inbox-item"}>
              {address ? (
                <Link href={`/token/${address}` as never} className="inbox-item-text">
                  {text}
                </Link>
              ) : (
                <span className="inbox-item-text">{text}</span>
              )}
              {unread ? <span className="inbox-new">New</span> : null}
              <time className="inbox-time mono" dateTime={item.block_time}>
                {new Date(item.block_time).toLocaleString()}
              </time>
            </li>
          );
        })}
      </ul>
      {state.more === "error" ? (
        <p className="inbox-more-error" role="alert">
          More notifications could not be loaded.
        </p>
      ) : null}
      {state.next ? (
        <Button onClick={() => void loadMore()} disabled={state.more === "loading"}>
          {state.more === "loading" ? "Loading…" : state.more === "error" ? "Retry" : "Load more"}
        </Button>
      ) : null}
    </div>
  );
}
