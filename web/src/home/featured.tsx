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
import {
  KingOfTheHill,
  TrendingList,
  useHighlights,
  type TrendingItem,
} from "@/discovery/highlights";
import { TokenCard, tokenLabel } from "@/discovery/token-card";
import { communityLabel, tokenHref } from "@/forum/forum-model";
import { relativeTime } from "@/live/live-strip-model";
import { addressHue } from "@/token/address";

const SECTION_SIZE = 5;
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
            <div className="home-featured-grid">
              <div className="home-featured-main">
                <KingOfTheHill king={highlights.data.king_of_the_hill} apiBaseUrl={apiBaseUrl} />
              </div>
              <ul className="home-featured-side" aria-label="Also moving">
                {trending.slice(0, 3).map((item) => (
                  <FeaturedItem key={item.address} item={item} apiBaseUrl={apiBaseUrl} />
                ))}
              </ul>
            </div>
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
                <ForumRow key={post.id} post={post} />
              ))}
            </ul>
          )}
        </Section>
      </Reveal>
    </>
  );
}

function FeaturedItem({ item, apiBaseUrl }: { item: TrendingItem; apiBaseUrl: string }) {
  const label = tokenLabel(item);
  return (
    <li
      className="home-featured-item"
      style={{ "--hue": addressHue(item.address) } as CSSProperties}
    >
      <SafeImage
        src={resolveApiAssetUrl(apiBaseUrl, `/v1/tokens/${encodeURIComponent(item.address)}/image`)}
        alt={`${label} token`}
        fallbackLabel="No artwork"
        className="token-image"
      />
      <div className="token-identity">
        <h3>
          <Link href={`/token/${item.address}` as never}>{label}</Link>
        </h3>
        <p className="token-symbol">
          <span>{item.symbol || "—"}</span>
        </p>
      </div>
    </li>
  );
}

function ForumRow({ post }: { post: ForumPost }) {
  const href = tokenHref(post.token);
  const author = authorLabel(post.author);
  const [now] = useState(Date.now);
  return (
    <li className="home-post">
      <p className="home-post-meta">
        {href ? (
          <Link href={href as never}>{communityLabel(post.symbol)}</Link>
        ) : (
          <span>{communityLabel(post.symbol)}</span>
        )}
        <span>{author.primary}</span>
        <time dateTime={post.created_at}>{relativeTime(post.created_at, now)}</time>
      </p>
      <p className="home-post-body">{post.body}</p>
    </li>
  );
}
