"use client";

import Link from "next/link";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type CSSProperties,
} from "react";
import { ApiClient, resolveApiAssetUrl, type ForumPost } from "@/api/client";
import { authorLabel } from "@/comments/comment-model";
import { SafeImage } from "@/components/primitives";
import { communityLabel, tokenHref } from "@/forum/forum-model";
import { useLiveEvents } from "@/live/live-events";
import { relativeTime } from "@/live/live-strip-model";
import { addressHue } from "@/token/address";
import {
  FIELD_LIFETIME_MS,
  FIELD_POOL,
  FIELD_TARGET_NARROW,
  FIELD_TARGET_WIDE,
  nextPost,
  pickSpot,
  type Spot,
} from "./forum-field-model";

/** Below this field width only a few cards fit, stacked loosely. */
const NARROW_FIELD = 640;
/** A card's box in the field. The body is clipped to three lines, so a card is never taller than
 *  CARD_HEIGHT; placement reserves that much and short posts simply leave more air. */
const CARD_WIDTH = 280;
const CARD_HEIGHT = 176;
const TICK_MS = 500;

type Bubble = { key: number; post: ForumPost; spot: Spot; area: Spot; age: number };

const reducedMotionQuery = "(prefers-reduced-motion: reduce)";
function subscribeReducedMotion(onChange: () => void) {
  const query = window.matchMedia(reducedMotionQuery);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}
function useReducedMotion() {
  return useSyncExternalStore(
    subscribeReducedMotion,
    () => window.matchMedia(reducedMotionQuery).matches,
    () => false,
  );
}

/** A post as a card: the token's artwork and community, the author, and a clipped body. */
function ForumPostCard({
  post,
  apiBaseUrl,
  className = "home-post",
  style,
  onAnimationEnd,
}: {
  post: ForumPost;
  apiBaseUrl: string;
  className?: string;
  style?: CSSProperties;
  onAnimationEnd?: (event: React.AnimationEvent<HTMLLIElement>) => void;
}) {
  const href = tokenHref(post.token);
  const author = authorLabel(post.author);
  const community = communityLabel(post.symbol);
  const [now] = useState(Date.now);
  return (
    <li
      className={className}
      style={{ "--hue": addressHue(post.token), ...style } as CSSProperties}
      onAnimationEnd={onAnimationEnd}
    >
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

/**
 * From the forum as a field of drifting posts: each card fogs in at a random free spot, stays,
 * and fogs out after FIELD_LIFETIME_MS; another post then appears somewhere else. New comments
 * (live `comment` events) re-read the latest posts and are shown before older ones. Reduced
 * motion shows the newest posts as a still row of cards instead.
 */
export function ForumField({ initial, apiBaseUrl }: { initial: ForumPost[]; apiBaseUrl: string }) {
  const reducedMotion = useReducedMotion();
  const [posts, setPosts] = useState(initial);
  const [bubbles, setBubbles] = useState<Bubble[]>([]);
  const fieldRef = useRef<HTMLUListElement>(null);
  const live = useRef<Bubble[]>([]);
  const postsRef = useRef(posts);
  const lastShown = useRef(new Map<string, number>());
  const counter = useRef(0);
  const vacated = useRef<Spot | null>(null);

  useEffect(() => {
    postsRef.current = posts;
  }, [posts]);

  const commit = useCallback((next: Bubble[]) => {
    live.current = next;
    setBubbles(next);
  }, []);

  /** Adds one card (aged `age` ms, so a first fill is already staggered); false if none fit. */
  const spawn = useCallback((age: number) => {
    const field = fieldRef.current;
    if (!field) return false;
    const area = { width: field.clientWidth, height: field.clientHeight };
    const size = { w: Math.min(CARD_WIDTH, area.width), h: CARD_HEIGHT };
    const taken = live.current.map((bubble) => bubble.spot);
    // The card that just left keeps its spot for a moment: the next one appears elsewhere.
    if (vacated.current) taken.push(vacated.current);
    const spot = pickSpot(taken, area, size, Math.random);
    const onScreen = new Set(live.current.map((bubble) => bubble.post.id));
    const post = nextPost(postsRef.current, onScreen, lastShown.current);
    if (!spot || !post) return false;
    vacated.current = null;
    lastShown.current.set(post.id, Date.now() - age);
    counter.current += 1;
    live.current = [
      ...live.current,
      {
        key: counter.current,
        post,
        spot,
        area: { x: 0, y: 0, w: area.width, h: area.height },
        age,
      },
    ];
    return true;
  }, []);

  useEffect(() => {
    if (reducedMotion) return;
    const fill = () => {
      const field = fieldRef.current;
      if (!field) return;
      const target = Math.min(
        field.clientWidth < NARROW_FIELD ? FIELD_TARGET_NARROW : FIELD_TARGET_WIDE,
        postsRef.current.length,
      );
      const first = live.current.length === 0;
      let added = false;
      // The first fill places every card at once, each at a different point of its life, so
      // they leave one by one; later ticks add one card at a time.
      for (let index = live.current.length; index < target; index++) {
        const age = first ? Math.round((index * FIELD_LIFETIME_MS) / target) : 0;
        if (!spawn(age)) break;
        added = true;
        if (!first) break;
      }
      if (added) commit(live.current);
    };
    fill();
    const timer = setInterval(fill, TICK_MS);
    return () => clearInterval(timer);
  }, [commit, reducedMotion, spawn]);

  const leave = useCallback(
    (key: number) => (event: React.AnimationEvent<HTMLLIElement>) => {
      if (event.target !== event.currentTarget) return;
      const gone = live.current.find((bubble) => bubble.key === key);
      vacated.current = gone?.spot ?? null;
      live.current = live.current.filter((bubble) => bubble.key !== key);
      spawn(0);
      commit(live.current);
    },
    [commit, spawn],
  );

  // A new comment anywhere: read the latest posts again; unseen ones are shown next.
  const refresh = useRef<ReturnType<typeof setTimeout> | null>(null);
  useLiveEvents(
    useCallback(
      (event) => {
        if (event.event !== "comment" || refresh.current) return;
        refresh.current = setTimeout(() => {
          refresh.current = null;
          new ApiClient({ baseUrl: apiBaseUrl })
            .getForumPosts({ limit: FIELD_POOL })
            .then((page) => {
              if (page.items?.length) setPosts(page.items);
            })
            .catch(() => undefined);
        }, 1_000);
      },
      [apiBaseUrl],
    ),
  );
  useEffect(
    () => () => {
      if (refresh.current) clearTimeout(refresh.current);
    },
    [],
  );

  if (reducedMotion)
    return (
      <ul className="home-posts">
        {posts.slice(0, 3).map((post) => (
          <ForumPostCard key={post.id} post={post} apiBaseUrl={apiBaseUrl} />
        ))}
      </ul>
    );

  return (
    <ul className="forum-field" ref={fieldRef} aria-label="Recent posts">
      {bubbles.map((bubble) => (
        <ForumPostCard
          key={bubble.key}
          post={bubble.post}
          apiBaseUrl={apiBaseUrl}
          className="home-post forum-bubble"
          style={{
            // Positions are kept as shares of the field, so a resize moves cards with it.
            left: `${(bubble.spot.x / bubble.area.w) * 100}%`,
            top: `${(bubble.spot.y / bubble.area.h) * 100}%`,
            width: bubble.spot.w,
            animationDuration: `${FIELD_LIFETIME_MS}ms`,
            animationDelay: `${-bubble.age}ms`,
          }}
          onAnimationEnd={leave(bubble.key)}
        />
      ))}
    </ul>
  );
}
