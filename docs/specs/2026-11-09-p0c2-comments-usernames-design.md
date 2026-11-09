# P0-C2 — Token comments, reports, operator moderation and usernames

Status: Claude pre-flight, 2026-11-09. Product-owner decisions and controller rulings are in
`.superpowers/sdd/2026-11-09-p0-social/c2-preflight.md`; this spec turns them into a contract.
Earlier parts: `docs/specs/2026-11-05-p0-market-data-discovery-design.md` (A),
`docs/specs/2026-11-06-p0b-live-feed-notifications-share-design.md` (B),
`docs/specs/2026-11-06-p0c1-wallet-page-pnl-design.md` (C1). Implementation plan:
`docs/plans/2026-11-09-p0c2-comments-usernames.md`.

## Goal

A token page has a public comment thread. Anyone can read it; a Privy-signed-in user can post
when one of their verified linked wallets created or currently holds the token. Users can report
comments, operators hide comments and ban users from a CLI, and users can pick a unique username
that shows on their comments and, only for wallets they choose to publish, on wallet pages and
the leaderboard.

Success means:

- a holder or the creator posts a comment and every open page of that token shows it without
  reload;
- a non-holder, a banned user, an over-limit user and an invalid body are rejected with a
  distinct, documented error;
- an operator can list reports, hide or unhide a comment and ban or unban a user, and the public
  thread reflects it live;
- a username is unique, rate-limited in how often it changes, and never reveals a link between a
  Privy user and a wallet that the user did not publish.

## Non-goals

- Forum / Memestock page (C5), watchlist counts and 7d/30d leaderboards (C3), server-side
  notification inbox (C4).
- Replies, threads, reactions, mentions, images, markdown or link previews.
- Author edit or delete of a comment. A user who wants a comment removed reports it; the operator
  hides it. (Keeps the moderation state machine to hide/unhide only.)
- Automatic hiding by report count, spam classifiers, word filters.
- ENS, avatars, bios, a `/u/<username>` page. Usernames on holder lists, trade lists or the live
  strip.
- Any HTTP admin endpoint or admin role in the API. Any new service, datastore or dependency.
- Storing e-mail or any Privy profile field other than the DID.

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| Identity is the verified Privy DID, scoped by `(chain_id, deployment_id)`, exactly as the watchlist. | One identity model across user-owned data; the DID is stable across wallets and devices. | Keying by wallet (a user with several wallets would be several people; bans would be trivially evaded). |
| Posting requires that the request's `wallet` is one of the verified linked wallets and is the token's creator or has `balance > 0` in `holder_balances` at post time. The comment row records that wallet and the role (`creator` / `holder`). | Gating on economic exposure is the owner's decision; naming the wallet makes the qualification auditable and lets the UI ask the user which wallet to post as. Latest indexed state is acceptable finality for a speech gate. | Server picks any qualifying wallet silently (the user cannot predict what is shown); requiring a safe/finalized balance (a fresh buyer waits minutes). |
| Comment author display is computed at read time: the username when the author has one; the posting wallet only when the author has no username **or** has published that wallet. At least one is always present. | No response ever pairs a username with a wallet the user did not publish, which is the owner's privacy rule. Read-time computation means later changes (setting a username, publishing a wallet, banning) apply to old comments. | Always showing the posting wallet (reveals DID↔wallet links); snapshotting the username into the row (renames and bans would not apply). |
| Usernames are published to wallets only through an explicit list of up to 5 published wallets, each a currently verified linked wallet. | The owner's "opt-in by linking that wallet" rule; a cap bounds abuse. | Showing the username on every linked wallet. |
| Moderation is the operator CLI `backend/cmd/moderate`, writing PostgreSQL with the operator's credentials; every action is appended to `moderation_actions`. | No new network attack surface (controller ruling); the audit table keeps who did what. | Admin HTTP endpoints; an admin role on Privy DIDs. |
| A ban is per-user state; banned users cannot post or report, their comments are hidden from public reads at read time, and their username is suppressed everywhere. Unban restores all of it. | Read-time filtering makes ban and unban exact inverses without rewriting rows. Suppressing the name stops an offensive username from surviving a ban. | Setting `hidden_at` on each comment at ban time (unban could not tell operator-hidden from ban-hidden). |
| Rate limits are derived from the rows the action writes (comments, reports, the username change timestamp), checked under a per-DID transaction advisory lock with the database clock. Limits: 1 comment / 15 s and 30 / rolling hour; 20 reports / rolling hour; 1 username change / 7 days. | Survives restarts with no extra table or write, and is exact for a single API instance and for several. Hidden comments still count, so a spammer gains nothing from being hidden. | A separate counter table (an extra write per post); the in-memory `SubjectLimiter` (lost on restart). |
| Comments are plain text: 1–500 code points after trim; CR/CRLF normalized to LF; control characters other than LF, bidi embedding/override/isolate characters and invalid UTF-8 rejected; at most 3 consecutive LF. The web renders text, never HTML, and does not auto-link. | Removes the HTML/markdown injection class entirely; bidi controls enable spoofed text (Trojan Source). | Markdown with a sanitizer; collapsing extra newlines silently. |
| Public list is newest first with an opaque keyset cursor on the comment id; `Cache-Control: public, max-age=5`. | Comments are off-chain, so the snapshot-bound `pagination.Cursor` does not apply; ids are monotonic. A short cache absorbs a popular page; live refetches bypass it. | Offset pagination; snapshot cursors. |
| New, hidden and unhidden comments publish a hint-only `comment` SSE event for the token, via `pg_notify('api_refresh', …)` in the same transaction as the write, coalesced per token like `token` hints. | Reuses NOTIFY → hub → SSE with no new datastore; the CLI runs out of process and still reaches open pages; NOTIFY is delivered only on commit; hint-only never ships hidden text to clients. | In-process hub publish (misses CLI actions); carrying the comment body in the event. |
| `comment_count` (visible comments) is computed at read time and returned with the list. No per-token stats table in C2. | Exposes the number the Forum needs without a second copy to keep consistent; C5 adds a projection if it needs cross-token sorting. | A maintained `token_comment_stats` table now. |
| Comments, reports, profiles and bans have no FK to `tokens`. A token removed by a reorg keeps its comments stored, but list, post and eligibility return 404 `token_not_found` for it. | Same choice and reason as the watchlist: user content must not be deleted by chain reorganisation. | FK with cascade (a reorg would delete speech). |

## Data model

Migration `00020_social.sql`. No amounts. All addresses `BYTEA` of 20 bytes; all tables carry
`chain_id BIGINT > 0` and `deployment_id TEXT` (non-empty); `privy_did TEXT` non-empty and at most
255 bytes (the API's existing bound).

- `social_users (chain_id, deployment_id, privy_did, username TEXT NULL, username_changed_at
  TIMESTAMPTZ NULL, banned_at TIMESTAMPTZ NULL, ban_reason TEXT NULL, created_at, updated_at)`
  - PK `(chain_id, deployment_id, privy_did)`.
  - CHECK `username ~ '^[a-z0-9_]{3,20}$'` when set; CHECK `username IS NULL OR
    username_changed_at IS NOT NULL`; CHECK `(banned_at IS NULL) = (ban_reason IS NULL)`.
  - UNIQUE `(chain_id, deployment_id, username)` (NULLs distinct). Usernames are stored lowercase,
    so this is the case-insensitive uniqueness.
  - A row is created on first profile save, first post, first report or a ban.
- `username_releases (chain_id, deployment_id, username, privy_did, released_at)`
  - PK `(chain_id, deployment_id, username, privy_did)`; upserted with `released_at = now()`
    whenever a user changes away from or deletes a name.
  - Claim rule: a name is unavailable to DID `d` while a row exists for it with another DID and
    `released_at > now() − 30 days`. The previous owner may reclaim it (subject to the 7-day
    change rule).
- `social_public_wallets (chain_id, deployment_id, wallet, privy_did, created_at)`
  - PK `(chain_id, deployment_id, wallet)`: a wallet is published by at most one user; index
    `(chain_id, deployment_id, privy_did)`.
- `token_comments (id BIGINT GENERATED ALWAYS AS IDENTITY, chain_id, deployment_id, token_address,
  privy_did, wallet, author_role TEXT, body TEXT, created_at, hidden_at NULL, hidden_reason NULL)`
  - PK `id`. CHECK `author_role IN ('creator','holder')`; CHECK `char_length(body) BETWEEN 1 AND
    500`; CHECK `(hidden_at IS NULL) = (hidden_reason IS NULL)`. No FK to `tokens`.
  - Index `(chain_id, deployment_id, token_address, id DESC) WHERE hidden_at IS NULL` (public
    list); index `(chain_id, deployment_id, privy_did, created_at DESC)` (rate limit, ban fan-out).
- `comment_reports (comment_id, chain_id, deployment_id, reporter_did, reason TEXT, created_at,
  resolved_at NULL, resolution TEXT NULL)`
  - PK `(comment_id, reporter_did)`; FK `comment_id → token_comments(id) ON DELETE CASCADE`.
  - CHECK `reason IN ('spam','scam','abuse','other')`; CHECK `resolution IN
    ('dismissed','actioned')` and `(resolved_at IS NULL) = (resolution IS NULL)`.
  - Index `(chain_id, deployment_id, reporter_did, created_at DESC)` (rate limit); partial index
    `(chain_id, deployment_id, created_at) WHERE resolved_at IS NULL` (open reports).
- `moderation_actions (id BIGINT identity, chain_id, deployment_id, action TEXT, comment_id NULL,
  target_did NULL, operator TEXT, reason TEXT, created_at)`
  - CHECK `action IN ('hide_comment','unhide_comment','ban_user','unban_user','resolve_reports')`;
    CHECK `octet_length(operator) BETWEEN 1 AND 64`. Append-only.
- View `public_wallet_usernames (chain_id, deployment_id, wallet, username)`: published wallets
  joined to users with a username and no ban. It is the single SQL source for "which username may
  be shown for this wallet", used by the wallet and leaderboard reads.

Down drops the view and tables in reverse order and deletes all social data (stated in the
migration comment, as for the watchlist).

## API contract

All new routes are under `/v1`, rate-limited by the existing per-IP limiter. Authenticated
routes take `Authorization: Bearer <access>` and `privy-id-token`, verified exactly as the
watchlist does, and answer `Cache-Control: private, no-store`. A missing or invalid credential is
401 `authentication_failed`. An invalid address path parameter is 422 (existing `address`
helper). Problems use the existing `apiProblem` shape and codes below.

| Operation | Route | Auth | Success |
| --- | --- | --- | --- |
| `listTokenComments` | `GET /tokens/{address}/comments?cursor=&limit=` | public | 200 `{items, next_cursor?, comment_count}`; `Cache-Control: public, max-age=5`; `limit` default 20, 1–50 |
| `getCommentEligibility` | `GET /tokens/{address}/comments/eligibility` | Privy | 200 `{eligible_wallets: [{address, role}], banned, username}` |
| `postTokenComment` | `POST /tokens/{address}/comments` body `{body, wallet}` | Privy | 201 `CommentDTO` |
| `reportComment` | `PUT /comments/{id}/report` body `{reason}` | Privy | 204; idempotent per (comment, reporter) |
| `getPublicProfile` | `GET /me/public-profile` | Privy | 200 `PublicProfileDTO` |
| `putPublicProfile` | `PUT /me/public-profile` body `{username, public_wallets}` | Privy | 200 `PublicProfileDTO` |
| `deletePublicProfile` | `DELETE /me/public-profile` | Privy | 204 |

- `CommentDTO`: `id` (decimal string), `token`, `body`, `created_at` (RFC 3339, microseconds,
  UTC), `author: {username: string|null, wallet: string|null, role: "creator"|"holder"}`.
- `PublicProfileDTO`: `username` (nullable), `username_changed_at` (nullable),
  `next_username_change_at` (nullable; null when a change is allowed now), `public_wallets`
  (checksummed addresses), `banned`.
- Existing reads gain one nullable field each: `GET /wallets/{address}` → `username`;
  `GET /leaderboard` items → `username`. Both come from `public_wallet_usernames`.

Errors:

| Status | Code | When |
| --- | --- | --- |
| 401 | `authentication_failed` | missing/invalid Privy credentials, DID empty or > 255 bytes |
| 403 | `wallet_not_linked` | post `wallet` or a `public_wallets` entry is not a verified linked wallet |
| 403 | `not_holder` | post `wallet` is neither creator nor a current holder |
| 403 | `banned` | a banned user posts, reports or saves a public profile (profile reads and deletes stay allowed) |
| 404 | `token_not_found` | token unknown, including removed by a reorg |
| 404 | `comment_not_found` | report target unknown, hidden, authored by a banned user, or its token is unknown |
| 409 | `username_taken` | name held by another user or within another user's 30-day hold |
| 422 | `invalid_comment` | body rule violated (detail names the rule) |
| 422 | `invalid_cursor` / `invalid_limit` | malformed cursor or limit out of range |
| 422 | `invalid_username` / `username_reserved` | format rule / reserved name |
| 422 | `invalid_report` / `own_comment` | unknown reason / reporting one's own comment |
| 422 | `too_many_public_wallets` | more than 5 published wallets |
| 429 | `rate_limited` | comment or report limit, or username changed within 7 days; `Retry-After` in whole seconds (≥ 1) |

Report idempotency: a repeat report by the same user returns 204 and does not count against the
report limit; reporting re-opens nothing once resolved.

## SSE `comment` event

- Written by the API post transaction and by the CLI (hide, unhide, ban, unban — one per
  affected token) as `pg_notify('api_refresh', '{"type":"comment","chain_id":…,
  "deployment_id":"…","token":"0x…"}')` inside the write transaction.
- `realtime.Decode` accepts `comment` with a valid token address. The listener publishes it to the
  hub but does not wake the trade tail.
- `/v1/events` delivers it as `event: comment`, data `{chain_id, deployment_id, token}`,
  coalesced per token over the existing 500 ms window, and filtered by `?token=` like `token`.
- The web token page, on a `comment` event for its token, refetches page 1 with
  `cache: "no-store"` and merges it with any older pages it already shows.

## Moderation CLI

`go run ./cmd/moderate <command> -chain-id N -deployment-id ID -operator NAME [flags]`, with
`DATABASE_URL` from `config.LoadDatabase`. Every mutating command runs in one transaction,
appends `moderation_actions`, sends the `comment` NOTIFY for affected tokens and prints a
one-line result; unknown targets exit non-zero without writing.

| Command | Flags | Effect |
| --- | --- | --- |
| `reports` | `-limit` (default 50) | open reports grouped by comment: comment id, token, created_at, report count, reasons, author DID, posting wallet, body (control characters escaped) |
| `hide` | `-comment ID -reason TEXT` | sets `hidden_at`; resolves the comment's open reports as `actioned` |
| `unhide` | `-comment ID -reason TEXT` | clears `hidden_at` |
| `ban` | `-did DID` or `-comment ID`, `-reason TEXT` | sets `banned_at` on the author (creating the user row if needed); resolves open reports on all their comments as `actioned` |
| `unban` | `-did DID -reason TEXT` | clears the ban |
| `resolve` | `-comment ID -resolution dismissed\|actioned -reason TEXT` | resolves the comment's open reports |

## Username rules

- Input is trimmed and lowercased; the stored and displayed form is lowercase.
- 3–20 characters of `[a-z0-9_]`; must not start with `0x` (would imitate an address).
- Reserved when the name, with underscores removed and trailing digits trimmed, equals a reserved
  word: `admin administrator launchpad support moderator mod official team staff system root help
  security robinhood pons null undefined anonymous deleted operator`.
- Changing to a different name is allowed once per 7 days after the last change; the first set
  (no previous change) is free. Saving the same name or only changing `public_wallets` is not a
  change. Deleting is always allowed (a user can always withdraw their public identity) but
  counts as a change, so a new name can be set 7 days after the deletion.
- Names released by a user are unavailable to others for 30 days.
- Deleting the profile clears the name and every published wallet; comments remain and display
  by the read-time rule.

## Privacy

- Stored per user: DID, username, published wallets, ban state, and per comment the posting
  wallet. No e-mail, no other Privy field.
- Public: comment body, time, role, author username (if any, not banned), posting wallet only
  under the display rule, usernames of published wallets, comment counts.
- Never public: DIDs, unpublished linked wallets, reporter identities, report contents, ban
  reasons, moderation history. Logs carry request ID and error type only, never DID or body.
- A published wallet claimed by a second user who proves current linkage moves to that user.
  A wallet no longer linked to its publisher is pruned on that user's next save or post.
- Residual risk, shown in the settings UI: anyone who saw a comment before its author set a
  username saw the posting wallet and can link the two.

## Threats and abuse

- **Spam / flooding.** Holder gate, per-DID durable limits, per-IP limiter, NOTIFY bounded by the
  post limit, SSE coalescing.
- **Sybil holders.** Buying dust from many wallets costs gas and fees per wallet but all linked
  wallets of one DID share one limit and one ban. Many DIDs remain possible; the operator bans.
- **Impersonation.** Lowercase-only names, reserved list, `0x` prefix banned, 30-day hold, 7-day
  change limit.
- **Injection / spoofing.** Plain text only, React text rendering, bidi controls rejected.
- **Report abuse.** Fixed reasons, one per reporter per comment, 20 / hour, banned users cannot
  report, reports trigger nothing automatically.
- **Moderation access.** The CLI needs the database credential; it is not reachable over HTTP.
- **Stale holder state.** A holder can post, then sell; accepted — the role is a fact at post
  time. A reorg that removes the qualifying buy leaves the comment in place; documented.

## BLOCKERS

None. Every input is in the repository; no external account or dependency is needed.

## ACCEPTANCE CRITERIA

1. Migration up/down/up passes; constraints reject a bad username, a 501-code-point body, an
   unknown role and an orphan report (integration).
2. `internal/social` vectors cover every body rule (trim, CRLF, control, bidi, invalid UTF-8,
   newline runs, 500/501 code points, emoji counted as code points) and every username rule
   (length, charset, `0x`, reserved variants, case folding).
3. Posting: a holder wallet → 201 with role `holder`; the creator with zero balance → `creator`;
   an unlinked wallet → 403 `wallet_not_linked`; a linked non-holder → 403 `not_holder`; an
   unknown token → 404; a banned user → 403 `banned` (integration through the HTTP routes).
4. Rate limits: a second post within 15 s → 429 with `Retry-After`; the 31st post in an hour →
   429; the 21st new report in an hour → 429; limits survive a new store instance (restart);
   concurrent posts from one DID never exceed the limits.
5. Public list: newest first, cursor round trip with no duplicates or gaps, hidden and banned
   authors' comments absent, `comment_count` equals visible rows, `Cache-Control` set, invalid
   cursor 422.
6. Author display rule: no response pairs a username with an unpublished wallet (table test over
   the four username × published combinations, plus banned author).
7. Usernames: uniqueness ignores case; a change within 7 days → 429; a released name is 409 for
   others for 30 days and reclaimable by its previous owner; reserved and `0x` names 422; the
   wallet page and leaderboard show the username only for published wallets of unbanned users.
8. SSE: a post and a CLI hide each deliver one `comment` event to a `?token=` stream of that
   token and none to another token's stream; the trade tail is not woken.
9. CLI: each command against a migrated database has the stated effect, writes one
   `moderation_actions` row per action and exits non-zero for an unknown comment or DID.
10. Web: a holder posts and a second page shows the comment without reload; a non-holder sees why
    they cannot post; report flow; username settings save, show errors and the next change date;
    the wallet header and leaderboard show usernames (Playwright, mocked API).
11. OpenAPI and the generated web client have no drift; existing gates stay green: `task verify`,
    web gates, the Anvil gate and `deploy/check.ps1`.

## Test strategy

- Pure Go table tests for `internal/social` (body and username rules, reserved list, display
  rule helper).
- PostgreSQL integration tests (`postgrestest`, real database) for constraints, holder/creator
  qualification, rate limits including concurrency and restart, cursor paging, ban filtering,
  username holds, public wallet takeover and pruning, and CLI commands.
- Route tests with a fake verifier for every status code in the error table and for cache
  headers; OpenAPI snapshot via `cmd/check-openapi`.
- SSE route and decode tests for the `comment` event and coalescing; a NOTIFY round trip test.
- Web: Vitest for the comment model (merge, cursor, display), the body pre-check mirroring the
  server rules and the username form; Playwright specs with mocked API and SSE.
- No Anvil change is required: the holder gate reads `holder_balances`, already covered by the
  indexer gates; an Anvil post would add no coverage beyond integration tests.
