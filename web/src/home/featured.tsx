"use client";

import { useEffect, useState } from "react";
import { ApiClient, type TokenListResponse } from "@/api/client";
import { Section } from "@/components/layout";
import { Button, EmptyState, ErrorState, Skeleton } from "@/components/primitives";
import { Reveal } from "@/components/reveal";
import { publicConfiguration } from "@/config/public";
import { TrendingList, useHighlights } from "@/discovery/highlights";
import { TokenCard } from "@/discovery/token-card";
import { ForumField } from "./forum-field";
import { FIELD_POOL } from "./forum-field-model";

/** Trending and Just graduated: three rows of five artwork cards. */
const CARD_SECTION_SIZE = 15;

type Load<T> = { status: "loading" } | { status: "error" } | { status: "ready"; data: T };

/** One read on mount (and on retry); aborted when the page goes away. */
function useRead<T>(read: ((signal: AbortSignal) => Promise<T>) | null) {
  const [state, setState] = useState<Load<T>>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!read) return;
    const controller = new AbortController();
    read(controller.signal).then(
      (data) => {
        if (!controller.signal.aborted) setState({ status: "ready", data });
      },
      () => {
        if (!controller.signal.aborted) setState({ status: "error" });
      },
    );
    return () => controller.abort();
    // `read` is a module-level reader chosen once per configuration; attempt re-runs it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [attempt, read === null]);
  return {
    state,
    retry: () => {
      setState({ status: "loading" });
      setAttempt((value) => value + 1);
    },
  };
}

function ListSkeleton() {
  return (
    <div className="home-skeleton" role="status" aria-label="Loading">
      <Skeleton />
      <Skeleton />
      <Skeleton />
    </div>
  );
}

/** Trending, Just graduated and From the forum, read from the public API. */
export function HomeFeed() {
  const configuration = publicConfiguration();
  const apiBaseUrl =
    configuration.status === "ready" && configuration.apiBaseUrl ? configuration.apiBaseUrl : null;
  const highlights = useHighlights(apiBaseUrl);
  const graduated = useRead<TokenListResponse>(
    apiBaseUrl
      ? (signal) =>
          new ApiClient({ baseUrl: apiBaseUrl }).getTokens(
            { phase: "graduated", sort: "newest", limit: CARD_SECTION_SIZE },
            signal,
          )
      : null,
  );
  const forum = useRead(
    apiBaseUrl
      ? (signal) =>
          new ApiClient({ baseUrl: apiBaseUrl }).getForumPosts({ limit: FIELD_POOL }, signal)
      : null,
  );

  if (!apiBaseUrl)
    return (
      <div className="home-unavailable">
        <EmptyState
          title="Live data unavailable"
          description="Launches, trades and posts appear here once the reviewed API is connected."
        />
      </div>
    );

  const trending = highlights.data?.trending ?? [];
  return (
    <>
      <Reveal>
        <Section id="home-trending" title="Trending" viewAllHref="/explore?view=trending">
          {highlights.failed ? (
            <ErrorState
              title="Trending unavailable"
              description="Trending tokens could not be loaded."
              action={<Button onClick={highlights.retry}>Retry</Button>}
            />
          ) : highlights.loading || !highlights.data ? (
            <ListSkeleton />
          ) : (
            <TrendingList
              items={trending.slice(0, CARD_SECTION_SIZE)}
              apiBaseUrl={apiBaseUrl}
              layout="grid"
              className="home-card-grid"
            />
          )}
        </Section>
      </Reveal>

      <Reveal>
        <Section id="home-graduated" title="Just graduated" viewAllHref="/explore?view=graduated">
          {graduated.state.status === "error" ? (
            <ErrorState
              title="Graduated tokens unavailable"
              description="The newest graduations could not be loaded."
              action={<Button onClick={graduated.retry}>Retry</Button>}
            />
          ) : graduated.state.status === "loading" ? (
            <ListSkeleton />
          ) : (graduated.state.data.items ?? []).length === 0 ? (
            <EmptyState
              title="No graduated tokens yet"
              description="Tokens that complete their curve and move to Uniswap appear here."
            />
          ) : (
            <div className="token-list home-card-grid" data-layout="grid">
              {(graduated.state.data.items ?? []).map((token) => (
                <TokenCard key={token.address} token={token} apiBaseUrl={apiBaseUrl} />
              ))}
            </div>
          )}
        </Section>
      </Reveal>

      <Reveal>
        <Section id="home-forum" title="From the forum" viewAllHref="/forum">
          {forum.state.status === "error" ? (
            <ErrorState
              title="Forum unavailable"
              description="Posts could not be loaded."
              action={<Button onClick={forum.retry}>Retry</Button>}
            />
          ) : forum.state.status === "loading" ? (
            <ListSkeleton />
          ) : (forum.state.data.items ?? []).length === 0 ? (
            <EmptyState
              title="No forum posts yet"
              description="Posts from holders and creators appear here."
            />
          ) : (
            <ForumField initial={forum.state.data.items ?? []} apiBaseUrl={apiBaseUrl} />
          )}
        </Section>
      </Reveal>
    </>
  );
}
