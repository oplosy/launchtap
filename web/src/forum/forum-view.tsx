"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import {
  ApiClient,
  type ForumCommunity,
  type ForumPost,
  type TokenListResponse,
} from "@/api/client";
import { authorLabel } from "@/comments/comment-model";
import { Badge, Button, EmptyState, ErrorState, Skeleton } from "@/components/primitives";
import { publicConfiguration } from "@/config/public";
import { displayAmount } from "@/discovery/token-card";
import { shortAddress } from "@/token/address";
import {
  appendForumPage,
  BIGGEST_LAUNCHES,
  communityLabel,
  FORUM_PAGE_SIZE,
  FORUM_TAB_LABELS,
  FORUM_TABS,
  forumTabHref,
  tokenHref,
  type ForumTab,
} from "./forum-model";

function apiClient(): ApiClient | null {
  const configuration = publicConfiguration();
  return configuration.status === "ready" && configuration.apiBaseUrl
    ? new ApiClient({ baseUrl: configuration.apiBaseUrl })
    : null;
}

function formatTime(iso: string) {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? iso : date.toLocaleString();
}

function Loading({ label }: { label: string }) {
  return (
    <div className="history-state" role="status" aria-label={label}>
      <Skeleton />
      <Skeleton />
      <Skeleton />
    </div>
  );
}

export function ForumTabs({ active }: { active: ForumTab }) {
  return (
    <nav className="explore-tabs forum-tabs" aria-label="Forum view">
      {FORUM_TABS.map((tab) => (
        <Link
          key={tab}
          href={forumTabHref(tab) as never}
          className="explore-tab"
          aria-current={tab === active ? "page" : undefined}
        >
          {FORUM_TAB_LABELS[tab]}
        </Link>
      ))}
    </nav>
  );
}

function CommunityLink({ token, symbol }: { token: string; symbol: string }) {
  const href = tokenHref(token);
  const label = communityLabel(symbol);
  return href ? (
    <Link href={href as never} className="forum-community">
      {label}
    </Link>
  ) : (
    <span className="forum-community">{label}</span>
  );
}

function PostCard({ post }: { post: ForumPost }) {
  const author = authorLabel(post.author);
  const href = tokenHref(post.token);
  return (
    <li className="forum-post">
      <div className="forum-post-meta">
        <CommunityLink token={post.token} symbol={post.symbol} />
        {author.walletHref ? (
          <Link href={author.walletHref as never} title={post.author.wallet ?? undefined}>
            {author.primary}
          </Link>
        ) : (
          <span>{author.primary}</span>
        )}
        {post.author.username && post.author.wallet ? (
          <span className="forum-post-wallet mono">{shortAddress(post.author.wallet)}</span>
        ) : null}
        {post.author.role === "creator" ? <Badge tone="accent">Creator</Badge> : null}
        <time dateTime={post.created_at}>{formatTime(post.created_at)}</time>
      </div>
      <p className="forum-post-body">{post.body}</p>
      {href ? (
        // Stretched over the row: the whole post is one target that opens the token.
        <Link className="forum-post-open" href={href as never}>
          Trade {post.symbol.trim() || "token"}
        </Link>
      ) : null}
    </li>
  );
}

type FeedState =
  | { status: "loading" | "error" }
  | {
      status: "ready";
      items: ForumPost[];
      next: string | null;
      more: "idle" | "loading" | "error";
    };

function ForumFeed() {
  const [state, setState] = useState<FeedState>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const client = apiClient();
    const controller = new AbortController();
    if (!client) {
      void Promise.resolve().then(() => setState({ status: "error" }));
      return;
    }
    client
      .getForumPosts({ limit: FORUM_PAGE_SIZE }, controller.signal)
      .then((page) =>
        setState({
          status: "ready",
          items: page.items ?? [],
          next: page.next_cursor ?? null,
          more: "idle",
        }),
      )
      .catch(() => {
        if (!controller.signal.aborted) setState({ status: "error" });
      });
    return () => controller.abort();
  }, [attempt]);

  const retry = useCallback(() => {
    setState({ status: "loading" });
    setAttempt((value) => value + 1);
  }, []);

  const loadMore = useCallback(() => {
    const client = apiClient();
    if (!client || state.status !== "ready" || !state.next || state.more === "loading") return;
    const cursor = state.next;
    setState({ ...state, more: "loading" });
    client
      .getForumPosts({ cursor, limit: FORUM_PAGE_SIZE })
      .then((page) =>
        setState((current) =>
          current.status === "ready"
            ? {
                ...current,
                items: appendForumPage(current.items, page.items ?? []),
                next: page.next_cursor ?? null,
                more: "idle",
              }
            : current,
        ),
      )
      .catch(() =>
        setState((current) =>
          current.status === "ready" ? { ...current, more: "error" } : current,
        ),
      );
  }, [state]);

  if (state.status === "error")
    return (
      <ErrorState
        title="Forum unavailable"
        description="Posts could not be loaded. Try again shortly."
        action={<Button onClick={retry}>Retry</Button>}
      />
    );
  if (state.status !== "ready") return <Loading label="Loading posts" />;
  if (state.items.length === 0)
    return (
      <EmptyState
        title="No posts yet"
        description="Holders and creators post from each token's page. The newest posts across every launch appear here."
      />
    );
  return (
    <div className="forum-feed">
      <ul className="forum-post-list">
        {state.items.map((post) => (
          <PostCard key={post.id} post={post} />
        ))}
      </ul>
      {state.more === "error" ? (
        <p className="forum-error" role="alert">
          More posts could not be loaded.
        </p>
      ) : null}
      {state.next ? (
        <Button onClick={loadMore} disabled={state.more === "loading"}>
          {state.more === "loading" ? "Loading…" : state.more === "error" ? "Retry" : "Load more"}
        </Button>
      ) : null}
    </div>
  );
}

type CommunitiesState =
  { status: "loading" | "error" } | { status: "ready"; items: ForumCommunity[] };

function ForumCommunities({ sort }: { sort: "hot" | "top" }) {
  const [state, setState] = useState<CommunitiesState>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const client = apiClient();
    const controller = new AbortController();
    if (!client) {
      void Promise.resolve().then(() => setState({ status: "error" }));
      return;
    }
    client
      .getForumCommunities(sort, FORUM_PAGE_SIZE, controller.signal)
      .then((body) => setState({ status: "ready", items: body.items ?? [] }))
      .catch(() => {
        if (!controller.signal.aborted) setState({ status: "error" });
      });
    return () => controller.abort();
  }, [attempt, sort]);

  if (state.status === "error")
    return (
      <ErrorState
        title="Forum unavailable"
        description="Communities could not be loaded. Try again shortly."
        action={
          <Button
            onClick={() => {
              setState({ status: "loading" });
              setAttempt((value) => value + 1);
            }}
          >
            Retry
          </Button>
        }
      />
    );
  if (state.status !== "ready") return <Loading label="Loading communities" />;
  if (state.items.length === 0)
    return (
      <EmptyState
        title={sort === "hot" ? "Quiet right now" : "No communities yet"}
        description={
          sort === "hot"
            ? "No launch has posts from the last 24 hours. Top ranks every community by all its posts."
            : "Communities appear once holders post on a token's page."
        }
      />
    );
  return (
    <ol className="forum-community-list">
      {state.items.map((community, index) => {
        const href = tokenHref(community.token);
        return (
          <li key={community.token} className="forum-community-row">
            <span className="forum-rank mono">{index + 1}</span>
            <div className="forum-community-main">
              <CommunityLink token={community.token} symbol={community.symbol} />
              <span className="forum-community-name">{community.name}</span>
            </div>
            <span className="forum-community-count">
              {community.comment_count.toLocaleString()}{" "}
              {community.comment_count === 1 ? "post" : "posts"}
              {sort === "hot" ? " today" : ""}
            </span>
            <time className="forum-community-time" dateTime={community.last_comment_at}>
              {formatTime(community.last_comment_at)}
            </time>
            {href ? (
              <Link className="ui-button ui-button-primary ui-button-sm" href={href as never}>
                Trade
              </Link>
            ) : null}
          </li>
        );
      })}
    </ol>
  );
}

/** Right column: the top launches by market cap from the existing token list. */
export function BiggestLaunches() {
  const [items, setItems] = useState<TokenListResponse["items"] | "error" | null>(null);

  useEffect(() => {
    const client = apiClient();
    const controller = new AbortController();
    if (!client) {
      void Promise.resolve().then(() => setItems("error"));
      return;
    }
    client
      .getTokens({ sort: "market_cap", limit: BIGGEST_LAUNCHES }, controller.signal)
      .then((body) => setItems(body.items ?? []))
      .catch(() => {
        if (!controller.signal.aborted) setItems("error");
      });
    return () => controller.abort();
  }, []);

  return (
    <aside className="forum-sidebar" aria-labelledby="biggest-launches-title">
      <h2 id="biggest-launches-title">Biggest launches</h2>
      {items === null ? (
        <Loading label="Loading launches" />
      ) : items === "error" ? (
        <p className="forum-error">Launches could not be loaded.</p>
      ) : items.length === 0 ? (
        <p className="forum-muted">No launches yet.</p>
      ) : (
        <ol className="forum-launch-list">
          {items.map((token, index) => {
            const href = tokenHref(token.address);
            return (
              <li key={token.address}>
                <span className="forum-rank mono">{index + 1}</span>
                {href ? (
                  <Link href={href as never} className="forum-community">
                    {communityLabel(token.symbol)}
                  </Link>
                ) : (
                  <span className="forum-community">{communityLabel(token.symbol)}</span>
                )}
                <span className="forum-launch-cap mono">
                  {displayAmount(token.market_cap_eth)} ETH
                </span>
              </li>
            );
          })}
        </ol>
      )}
      <Link className="forum-browse" href="/explore">
        Browse the launchpad
      </Link>
    </aside>
  );
}

export function ForumView({ tab }: { tab: ForumTab }) {
  return tab === "new" ? <ForumFeed /> : <ForumCommunities key={tab} sort={tab} />;
}
