# P0-C5 — Forum (Memestock)

Status: Claude design, 2026-11-10. The product owner delegated the rulings while away ("pick the
best option yourself"). Builds on P0-C2 token comments
(`docs/specs/2026-11-09-p0c2-comments-usernames-design.md`); reference screen: `notes.md` §8.

## Goal

A `/forum` page where every launch is a community: the newest posts across all tokens and the
communities ranked by what is moving, with a way to trade from each post.

Success means a reader can see, in one place, what people are saying across the platform, jump
to the token to read or post more, and never sees a hidden comment, a banned author or a token
removed by a reorg.

## Non-goals

- A second kind of post. Posts are the C2 token comments; posting stays on the token page with
  C2's holder gate, limits and moderation. No titles, images, markdown or replies.
- Up/down votes and vote-ranked "Hot". Votes add a new write path, a sybil problem (anyone can
  sign in with many Privy accounts) and moderation surface; deferred until there is a reputation
  rule.
- The stock ticker tape. It needs a third-party equities data feed (a new external service,
  licence and secret). Deferred; listed in the backlog as an owner input.
- Inline swap on the forum. The "Trade" action links to the token page's trade panel.

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| Forum reads are cross-token views over `token_comments` with the same visibility rules as C2 (`hidden_at IS NULL`, author not banned, author display computed at read time) and the same token-existence rule as the token comment list (the `tokens` row must exist; name and symbol come from `token_launches`), so a reorged token's comments disappear. | One copy of speech, one moderation path (the C2 CLI applies everywhere at once); no new write path. | A separate `forum_posts` table (two moderation paths, two rate-limit models). |
| `GET /v1/forum/posts?cursor&limit` (limit 1-50, default 20): newest visible comments first, keyset on the comment id with C2's opaque cursor; each item carries the token's name and symbol. `Cache-Control: public, max-age=5`. Invalid cursor → 422 `invalid_cursor` (C2's error). | Same cursor and cache rules as the token comment list. | Offset pagination. |
| `GET /v1/forum/communities?sort=hot|top&limit` (limit 1-50, default 20). `hot`: tokens by visible comments in the last 24 hours (only tokens with at least one), ties by newest comment. `top`: by all-time visible comments, same tie-break. Each item: token, name, symbol, `comment_count` for the window, `last_comment_at`. `public, max-age=15`. Unknown `sort` → 400 `invalid_sort` (like `invalid_window`); limit outside 1-50 → 422 `invalid_limit` (like C2). | "Ranked by what is moving" without votes: comment activity is already moderated and rate-limited per author. | Ranking by trading volume (already on Explore); votes (non-goal). |
| Migration 00023 adds partial indexes on visible comments: `(chain_id, deployment_id, id DESC)` for the feed and `(chain_id, deployment_id, created_at)` for the 24-hour window. `top` aggregates all visible comments of the deployment on each (cached) read. | Bounded feed and hot reads; `top` is a single grouped scan, acceptable at launch scale behind a 15 s cache. | A maintained per-token comment-count table now (a second copy to keep consistent; add it when `top` gets slow). |
| Web `/forum`: tabs New / Hot / Top in the URL (`?tab=`); New lists posts as `s/SYMBOL` cards with author, time, body and a Trade link; Hot and Top list communities. A "Biggest launches" sidebar shows the top 10 tokens by market cap from the existing token list with a "Browse the launchpad" link. The navbar gains a Forum link. Body text and symbols are rendered as text. | Matches the reference screen within the non-goals; reuses existing reads. | |

## RISKS

- `top` cost grows with total comments; the runbook names the projection to add.
- The feed shows comments on tokens whose metadata the operator has not reviewed, exactly as the
  token page does; comment moderation is unchanged.

## ACCEPTANCE CRITERIA

1. The feed lists visible comments across tokens newest first, paginates without gaps or
   duplicates, excludes hidden comments, banned authors, other deployments and tokens no longer
   indexed, and shows a username with a wallet only when C2 would.
2. `hot` counts only the last 24 hours and omits tokens with none; `top` counts all visible
   comments; both apply the same exclusions as the feed.
3. 422 `invalid_cursor` for a malformed cursor, 422 `invalid_limit` for a limit outside 1-50 (as
   C2), 400 `invalid_sort` for an unknown sort; public cache headers as above.
4. Web: `/forum` renders the three tabs from the URL, links each post and community to its token
   page, shows the biggest-launches sidebar, and renders hostile text as text; the navbar links to
   `/forum`.
5. Gates: backend unit/race, integration, lint, sqlc/openapi/migration checks; web vitest, lint,
   typecheck, live e2e; the page checked in a browser at desktop and 375 px.
