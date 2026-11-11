"use client";

import Link from "next/link";
import { useEffect, useState, type CSSProperties } from "react";
import {
  ApiClient,
  resolveApiAssetUrl,
  type ForumPost,
  type TokenListResponse,
} from "@/api/client";
import { authorLabel } from "@/comments/comment-model";
import { Section } from "@/components/layout";
import { Button, EmptyState, ErrorState, SafeImage, Skeleton } from "@/components/primitives";
import { Reveal } from "@/components/reveal";
import { publicConfiguration } from "@/config/public";
import { KingOfTheHill, TrendingList, useHighlights } from "@/discovery/highlights";
import { TokenCard } from "@/discovery/token-card";
import { communityLabel, tokenHref } from "@/forum/forum-model";
import { relativeTime } from "@/live/live-strip-model";
import { addressHue } from "@/token/address";

/** From the forum: one row of three post cards. */
const SECTION_SIZE = 3;
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

/** Featured band, Trending, Just graduated and From the forum, read from the public API. */
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
          new ApiClient({ baseUrl: apiBaseUrl }).getForumPosts({ limit: SECTION_SIZE }, signal)
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
        <section className="home-featured" aria-label="Featured">
          {highlights.failed ? (
            <ErrorState
              title="Highlights unavailable"
              description="The leading launch could not be loaded."
              action={<Button onClick={highlights.retry}>Retry</Button>}
            />
          ) : highlights.loading || !highlights.data ? (
            <ListSkeleton />
          ) : highlights.data.king_of_the_hill ? (
            <KingOfTheHill king={highlights.data.king_of_the_hill} apiBaseUrl={apiBaseUrl} />
          ) : (
            <EmptyState
              title="No launch is leading yet"
              description="The launch closest to graduation is featured here."
            />
          )}
        </section>
      </Reveal>

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
            <ul className="home-posts">
              {(forum.state.data.items ?? []).map((post) => (
                <ForumCard key={post.id} post={post} apiBaseUrl={apiBaseUrl} />
              ))}
            </ul>
          )}
        </Section>
      </Reveal>
    </>
  );
}

/** A post as a card: the token's artwork and community, the author, and a clipped body. */
function ForumCard({ post, apiBaseUrl }: { post: ForumPost; apiBaseUrl: string }) {
  const href = tokenHref(post.token);
  const author = authorLabel(post.author);
  const community = communityLabel(post.symbol);
  const [now] = useState(Date.now);
  return (
    <li className="home-post" style={{ "--hue": addressHue(post.token) } as CSSProperties}>
      <div className="home-post-head">
        <SafeImage
          src={resolveApiAssetUrl(apiBaseUrl, `/v1/tokens/${encodeURIComponent(post.token)}/image`)}
          alt=""
          fallbackLabel="No artwork"
          className="token-image"
        />
        <p className="home-post-meta">
          <span className="home-post-community">{community}</span>
          <span>
            {author.primary} ·{" "}
            <time dateTime={post.created_at}>{relativeTime(post.created_at, now)}</time>
          </span>
        </p>
      </div>
      <p className="home-post-body">{post.body}</p>
      {href ? (
        // Laid over the whole card: the post is one target that opens its token.
        <Link className="home-post-open" href={href as never}>
          <span className="sr-only">Open {community}</span>
        </Link>
      ) : null}
    </li>
  );
}
