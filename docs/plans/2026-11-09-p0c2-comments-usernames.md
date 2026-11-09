# P0-C2 Token Comments, Moderation and Usernames Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every token page has a public comment thread. A Privy-signed-in creator or holder of
the token can post; anyone signed in can report; operators hide comments and ban users from a
CLI; users pick a unique username that shows on their comments and, for wallets they publish, on
wallet pages and the leaderboard.

**Architecture:** One migration adds `social_users`, `username_releases`,
`social_public_wallets`, `token_comments`, `comment_reports`, `moderation_actions` and the
`public_wallet_usernames` view. A new `internal/social` package holds the pure rules (body,
username, display) and the store ports. `postgres.SocialStore` implements posting, reporting,
listing and profiles; rate limits are counted from the written rows under a per-DID advisory
lock. Writes send a hint-only `comment` NOTIFY on `api_refresh`, which the API decodes into the
existing hub and SSE stream. A new `cmd/moderate` CLI writes moderation actions directly to
PostgreSQL. The web adds a comment panel to the token page, a username section to `/profile`,
and usernames on the wallet header and leaderboard.

**Tech Stack:** Go 1.26 (Huma, sqlc, pgx), PostgreSQL 18, Next.js 16, React 19, Vitest,
Playwright.

**Spec:** `docs/specs/2026-11-09-p0c2-comments-usernames-design.md`

## Global Constraints

- No new dependency in `backend/go.mod` or `web/package.json`. No contract change. No new
  service or datastore.
- `internal/social` imports only the standard library and `go-ethereum/common`. It holds no SQL
  and no HTTP types.
- Identity key is `(chain_id, deployment_id, privy_did)` from a verified Privy principal; the DID
  is at most 255 bytes (`maxPrivyDIDBytes`). Never log a DID, a comment body or a username; log
  request ID and error type only, as `WatchlistRoutes.fail` does.
- Policy constants live only in `internal/social/policy.go` and are passed to SQL as parameters;
  no literal limit is repeated in SQL or the web except the web's mirror constants in
  `web/src/comments/comment-rules.ts`, which a test pins to the spec values.
- Time comparisons for limits, holds and change intervals use the database clock (`now()`), never
  the API clock.
- Authenticated routes: `Cache-Control: private, no-store`. `listTokenComments`:
  `Cache-Control: public, max-age=5`.
- Comment text is rendered as React text only. No `dangerouslySetInnerHTML`, no auto-linking.
- The token page must not load the wallet stack eagerly for the comment panel. It reads the
  session through `WatchlistAuthContext`, as `StarButton` does.
- Migrations, sqlc output, OpenAPI and the generated web client are regenerated, never
  hand-edited (`task sqlc`, `go run ./cmd/check-openapi --write`, `npm run web-api-sync`).
- Format only touched files. Never run `gofmt`, `forge fmt` or `prettier` unscoped.

## Rulings made while planning

Binding for the tasks below; each is also in the spec's decision table or contract.

- **R1 — Posting wallet is named by the client.** `POST` carries `wallet`; it must be in
  `principal.Wallets` (403 `wallet_not_linked`) and qualify (403 `not_holder`). Role is `creator`
  when `tokens.creator = wallet`, else `holder` when `holder_balances.balance > 0`.
- **R2 — Read-time author display.** `author.username` = the author's username unless banned;
  `author.wallet` = the posting wallet when the author has no username or that wallet is in
  `social_public_wallets` for the author. Implemented once in SQL (`ListTokenComments`, Task 1)
  and pinned by `social.DisplayAuthor` vectors (Task 2) that the integration test compares to.
- **R3 — Rate limits from rows.** Under `pg_advisory_xact_lock(hashtextextended('social' ||
  0x1f || chain || 0x1f || deployment || 0x1f || did, 0))`:
  - comments: newest `created_at` of the DID's comments (in this scope) must be ≤ `now() − 15 s`
    and the count with `created_at > now() − 1 h` must be < 30;
  - reports: count of the DID's reports with `created_at > now() − 1 h` < 20 (a repeat report
    is answered before the check and costs nothing);
  - username: `username_changed_at ≤ now() − 7 d` for a change.

  `Retry-After` is the seconds until the oldest limiting row leaves its window, rounded up,
  minimum 1.
- **R4 — Ban is user state.** `social_users.banned_at`. Reads exclude comments whose author is
  banned; usernames of banned users are never returned; banned users get 403 `banned` on post and
  report; profile GET shows `banned: true`; profile PUT is 403 `banned`; DELETE is allowed.
- **R5 — Eligibility endpoint.** `getCommentEligibility` exists so the UI can show the wallet
  picker and the reason a user cannot post, without a failed POST.
- **R6 — Shared Privy authentication.** Extract the watchlist's credential parsing and
  verification into `apiserver.authenticatePrivy(ctx, verifier, WatchlistAuthInput)
  (privyauth.Principal, error)` in `privy_auth.go`; `WatchlistRoutes.owner` calls it unchanged in
  behaviour (its tests stay green).
- **R7 — `comment` events do not wake the trade tail.** `listenRefreshHints` calls `tail.Hint`
  only for `launch`, `token` and `reorg`.
- **R8 — Cursor format.** `listTokenComments` cursors are `base64url("c1:" + id)`; anything else,
  or a non-positive id, is 422 `invalid_cursor`.
- **R9 — CLI scope flags.** `cmd/moderate` reads `DATABASE_URL` with `config.LoadDatabase` and
  takes `-chain-id`, `-deployment-id` and `-operator` as required flags; it does not load the
  full service config.
- **R10 — Public wallet takeover.** On profile PUT, each requested wallet must be in
  `principal.Wallets`; a row owned by another DID is moved to the caller; the caller's other rows
  are deleted. On profile PUT and on post, the caller's rows for wallets no longer in
  `principal.Wallets` are deleted.

## Review Focus

1. **Privacy of the display rule.** No API response may pair a username with an unpublished
   wallet, including after a username is set, a wallet is unpublished, or a user is banned and
   unbanned. Test in Task 1 (SQL) and Task 3 (route).
2. **Concurrency of the limits.** Ten concurrent posts from one DID produce exactly one 201 and
   nine 429; the hourly cap holds under concurrency. Test in Task 2.
3. **Username races and holds.** Two users claiming one name at once: one 200, one 409 (unique
   violation mapped, not 500). A user who releases a name and a second user who claims it within
   30 days: 409. Test in Task 6.
4. **Reorged token.** Comments of a token whose `tokens` row is gone are kept, listed as 404
   `token_not_found`, and reappear when the row returns. Test in Task 2.
5. **Moderation and live pages.** A CLI hide reaches an open token page through NOTIFY → hub →
   SSE, and the page drops the comment from page 1 without reload. Test in Tasks 4, 5 and 7.

---

### Task 1: Social schema and queries

**Files:**
- Create:
  - `backend/internal/store/postgres/migrations/00020_social.sql`
  - `backend/internal/store/postgres/queries/social.sql`
  - `backend/internal/store/postgres/postgrestest/social_schema_integration_test.go`
- Modify: `backend/sqlc.yaml` only if query files are enumerated; regenerated `sqlc/*`.

**Interfaces:**
- Migration Up: the tables, constraints, indexes and view exactly as in spec "Data model". Down
  drops the view, then `moderation_actions`, `comment_reports`, `token_comments`,
  `social_public_wallets`, `username_releases`, `social_users`.
- Queries (sqlc names; every query takes `chain_id` and `deployment_id`):
  - `LockSocialSubject(subject_key)` — `pg_advisory_xact_lock(hashtextextended(...))`.
  - `EnsureSocialUser(did)` — `INSERT … ON CONFLICT DO NOTHING`.
  - `GetSocialUser(did)` → username, username_changed_at, banned_at.
  - `QualifyWallets(token, wallets BYTEA[])` → `(wallet, role)` for each wallet that is the
    creator or a positive holder; role `creator` wins.
  - `TokenKnown(token)` → bool (reuse `TokenExists` if its shape fits).
  - `CommentRateState(did, window_start)` → `last_created_at`, `count_in_window`,
    `oldest_in_window`.
  - `InsertComment(token, did, wallet, role, body)` → full row.
  - `ListTokenComments(token, before_id NULL, row_limit)` → id, body, created_at, role,
    `display_username`, `display_wallet` per R2; excludes hidden and banned authors; `id DESC`.
  - `CountVisibleComments(token)` → BIGINT.
  - `GetReportableComment(id)` → comment scope, token, author DID; NULL when hidden or author
    banned.
  - `InsertReport(comment_id, did, reason)` → rows affected (0 on repeat).
  - `ReportRateState(did, window_start)` → `count_in_window`, `oldest_in_window`.
  - `NotifyComment(payload)` — reuse `NotifyAPIRefresh` from `sync.sql` if usable inside a
    transaction; otherwise add it here.

- [ ] **Step 1: Write the failing integration tests** in `social_schema_integration_test.go`:
  - `TestSocialMigrationConstraints`: inserting username `Bad`, `ab`, a 501-code-point body, role
    `admin` and a report for comment id 999 each fail with a constraint error.
  - `TestSocialUsernameUniqueness`: two users with `alice` in one scope → unique violation; the
    same name in another deployment → allowed.
  - `TestListTokenCommentsDisplayRule` (Review Focus 1): four authors (username × published) plus
    a banned author. Assert `display_username` / `display_wallet` per R2 and that the banned
    author's comment is absent.
  - `TestListTokenCommentsHidesHidden`: a hidden comment is absent and not counted by
    `CountVisibleComments`.
  - `TestQualifyWallets`: creator with zero balance → `creator`; holder → `holder`; zero balance
    → absent; creator who also holds → `creator`.
- [ ] **Step 2:** Run `cd backend && go test -tags integration ./internal/store/postgres/postgrestest/ -run 'Social|TokenComments|QualifyWallets'`. Expected: FAIL.
- [ ] **Step 3: Implement** the migration and queries. Run `task sqlc`.
- [ ] **Step 4:** Run `task integration` and `task verify` (migrations up/down/up, sqlc diff).
  Expected: PASS, apart from known Windows CRLF false positives, each named.
- [ ] **Step 5: Commit** `feat: add social schema for comments and usernames (Plan P0-C2 Task 1)`

### Task 2: Social domain rules and comment store

**Files:**
- Create:
  - `backend/internal/social/policy.go`, `comment.go`, `username.go`, `ports.go`
  - `backend/internal/social/comment_test.go`, `username_test.go`
  - `backend/internal/store/postgres/social_store.go`
  - `backend/internal/store/postgres/postgrestest/social_store_integration_test.go`

**Interfaces:**
- Produces:
  ```go
  package social
  const (
      MaxBodyRunes           = 500
      MaxNewlineRun          = 3
      CommentMinInterval     = 15 * time.Second
      CommentsPerHour        = 30
      ReportsPerHour         = 20
      UsernameChangeInterval = 7 * 24 * time.Hour
      UsernameHold           = 30 * 24 * time.Hour
      MaxPublicWallets       = 5
      DefaultPageSize, MaxPageSize = 20, 50
  )
  type Scope struct{ ChainID int64; DeploymentID string }
  type Actor struct{ Scope; PrivyDID string; Wallets []common.Address }
  type Role string // RoleCreator = "creator", RoleHolder = "holder"
  type ReportReason string // "spam" | "scam" | "abuse" | "other"
  type Author struct{ Username *string; Wallet *common.Address; Role Role }
  type Comment struct{ ID int64; Token common.Address; Body string; CreatedAt time.Time; Author Author }
  type CommentPage struct{ Items []Comment; NextCursor string; Count int64 }
  type EligibleWallet struct{ Address common.Address; Role Role }
  type Eligibility struct{ Wallets []EligibleWallet; Banned bool; Username *string }
  type RateLimitedError struct{ RetryAfter time.Duration } // Error(); errors.As target
  var ErrUnknownToken, ErrWalletNotLinked, ErrNotHolder, ErrBanned, ErrCommentNotFound,
      ErrOwnComment, ErrInvalidCursor, ErrInvalidReport error
  type BodyError struct{ Rule string } // "empty" | "too_long" | "control" | "bidi" | "utf8" | "newlines"
  func NormalizeBody(raw string) (string, error)     // returns *BodyError
  func ParseReportReason(s string) (ReportReason, error)
  func EncodeCursor(id int64) string
  func DecodeCursor(s string) (int64, error)         // ErrInvalidCursor
  func DisplayAuthor(username *string, banned bool, wallet common.Address, published bool, role Role) Author // reference for R2
  type CommentStore interface {
      List(ctx context.Context, s Scope, token common.Address, cursor string, limit int) (CommentPage, error)
      Eligibility(ctx context.Context, a Actor, token common.Address) (Eligibility, error)
      Post(ctx context.Context, a Actor, token, wallet common.Address, body string) (Comment, error)
      Report(ctx context.Context, a Actor, commentID int64, reason ReportReason) error
  }
  ```
- `NormalizeBody`, in order (doc comment names the spec section): reject invalid UTF-8 (`utf8`);
  replace CRLF then lone CR with LF; `strings.TrimSpace`; reject empty (`empty`); reject any
  `unicode.IsControl` rune except LF (`control`); reject U+202A–U+202E and U+2066–U+2069 (`bidi`);
  reject more than `MaxNewlineRun` consecutive LF (`newlines`); reject more than `MaxBodyRunes`
  runes (`too_long`).
- `postgres.SocialStore{Pool *pgxpool.Pool}` implements `CommentStore`:
  - `Post` in one transaction: lock subject → `EnsureSocialUser` → banned → `ErrBanned`; token
    unknown → `ErrUnknownToken`; wallet not in `a.Wallets` → `ErrWalletNotLinked`; prune
    unlinked published wallets (R10); `QualifyWallets` → `ErrNotHolder`; rate state →
    `*RateLimitedError`; insert; `NotifyComment`; return the comment with `DisplayAuthor` applied.
  - `Report`: lock → banned → `GetReportableComment` (nil → `ErrCommentNotFound`; token unknown →
    `ErrCommentNotFound`) → author is caller → `ErrOwnComment` → repeat → nil → rate state →
    insert.
  - `List`: token unknown → `ErrUnknownToken`; page of `limit+1` rows; `NextCursor` from the last
    returned id when more exist; `Count` from `CountVisibleComments`.
  - `Eligibility`: token unknown → `ErrUnknownToken`; qualifying linked wallets, banned flag,
    username (nil when banned).

- [ ] **Step 1: Write the failing unit tests:**
  - `TestNormalizeBodyVectors`: `"  hi  "` → `"hi"`; `"a\r\nb"` → `"a\nb"`; `"a\n\n\n\nb"` →
    `newlines`; `"a\n\n\nb"` ok; `"\x00"` → `control`; `"\t"` → `control`; `"a‮b"` → `bidi`;
    `"\xff"` → `utf8`; 500 × `"é"` ok; 501 × `"a"` → `too_long`; 500 × `"😀"` ok (code points);
    `"   "` → `empty`.
  - `TestCursorRoundTrip`, `TestDecodeCursorRejects` (`""` is not an error at the call site; `"x"`,
    `"c1:0"`, `"c1:-1"`, `"c2:5"` → `ErrInvalidCursor`).
  - `TestDisplayAuthorVectors`: the four username × published combinations and banned.
  - `TestParseReportReason`.
- [ ] **Step 2: Write the failing integration tests** in `social_store_integration_test.go`:
  - `TestPostQualifiesAndRecordsWallet`, `TestPostRejectsUnlinkedNonHolderBannedUnknownToken`.
  - `TestPostRateLimitInterval`: second post at once → `RateLimitedError` with `RetryAfter` in
    (0 s, 15 s].
  - `TestPostRateLimitHourly`: seed 30 rows with `created_at` 20 min ago → 31st rejected.
  - `TestPostRateLimitConcurrent` (Review Focus 2): 10 goroutines → exactly 1 success.
  - `TestRateLimitSurvivesNewStore`: a fresh `SocialStore` value on the same database still
    rejects.
  - `TestReportIdempotentOwnHiddenAndLimit`: repeat → nil and one row; own → `ErrOwnComment`;
    hidden → `ErrCommentNotFound`; 21st new report in an hour → `RateLimitedError`.
  - `TestListPagingAndReorgedToken` (Review Focus 4): 45 comments → pages 20/20/5 with no
    duplicates; deleting the `tokens` row → `ErrUnknownToken`; restoring it → comments return.
  - `TestPostSendsCommentNotify`: `ListenAPIRefreshReady` receives one payload with
    `"type":"comment"` and the token after commit, none after a rolled-back post.
- [ ] **Step 3:** Run `go test ./internal/social/` and the integration tests. Expected: FAIL.
- [ ] **Step 4: Implement.**
- [ ] **Step 5:** Run `go test ./internal/social/`, `task integration`, `task lint`. Expected: PASS.
- [ ] **Step 6: Commit** `feat: add comment rules and holder-gated comment store (Plan P0-C2 Task 2)`

### Task 3: Comment API routes

**Files:**
- Create:
  - `backend/internal/apiserver/privy_auth.go`
  - `backend/internal/apiserver/comment_routes.go`, `comment_routes_test.go`
- Modify:
  - `backend/internal/apiserver/watchlist_routes.go` (call `authenticatePrivy`, R6)
  - `backend/internal/apiserver/server.go` (`RegisterCommentRoutes`, defaulting `Logger`)
  - `backend/cmd/api/main.go`
  - OpenAPI snapshot (`go run ./cmd/check-openapi --write`); `web/src/api/generated.ts`
    (`npm run web-api-sync`)

**Interfaces:**
- Consumes: `social.CommentStore` (Task 2), `privyauth.Verifier`.
- Produces:
  ```go
  type CommentRoutes struct {
      Verifier     privyauth.Verifier
      Store        social.CommentStore
      ChainID      int64
      DeploymentID string
      Logger       *slog.Logger
  }
  func authenticatePrivy(ctx context.Context, v privyauth.Verifier, in WatchlistAuthInput) (privyauth.Principal, error)
  ```
- Routes and DTOs per spec "API contract" (tag `comments`): `listTokenComments`,
  `getCommentEligibility`, `postTokenComment` (`DefaultStatus: 201`), `reportComment`
  (`DefaultStatus: 204`). Request bodies: `{body string, wallet string}` (wallet parsed with the
  `address` helper → 422 on malformed), `{reason string}`.
- Error mapping (one `fail` function, table in spec): `*social.BodyError` → 422
  `invalid_comment` with detail `"comment rule: <rule>"`; `*social.RateLimitedError` → 429
  `rate_limited` with `Retry-After` header via `huma.ErrorWithHeaders`; context cancel passed
  through; anything else → logged by type, 500 `internal_error`.
- `limit` outside 1–50 → 422 `invalid_limit`.

- [ ] **Step 1: Write the failing route tests** with a fake verifier and fake store:
  - `TestCommentRoutesStatusTable`: one subtest per row of the spec error table that applies to
    comment routes, asserting status and `code`.
  - `TestListTokenCommentsCacheHeader`: `public, max-age=5`; authenticated routes
    `private, no-store`.
  - `TestPostCommentRetryAfter`: `RateLimitedError{RetryAfter: 1500 * time.Millisecond}` →
    `Retry-After: 2`.
  - `TestCommentDTOShape`: `id` is a string; `author.wallet` null when the store returns nil.
  - `TestWatchlistAuthUnchanged`: existing watchlist route tests still pass (run them).
- [ ] **Step 2:** Run `go test ./internal/apiserver/ -run 'Comment|Watchlist'`. Expected: FAIL.
- [ ] **Step 3: Implement**, wire `storepostgres.SocialStore{Pool: pool}` in `cmd/api/main.go`,
  regenerate OpenAPI and the web client.
- [ ] **Step 4:** Run `task verify` and, in `web/`, `npm run web-api-diff`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: add token comment and report endpoints (Plan P0-C2 Task 3)`

### Task 4: `comment` SSE event

**Files:**
- Modify:
  - `backend/internal/realtime/hub.go` (`Decode` accepts `comment`), `hub_test.go`
  - `backend/internal/apiserver/event_routes.go`, `event_routes_test.go`
  - `backend/cmd/api/main.go` (`listenRefreshHints`, R7) and its test file if one exists
  - OpenAPI snapshot; `web/src/api/generated.ts`; `web/src/api/types.ts`; `web/src/api/sse.ts`

**Interfaces:**
- `Decode`: `comment` requires a token matching `^0x[0-9a-fA-F]{40}$`, else an error.
- SSE map gains `"comment": commentEvent{}` with `ChainID`, `DeploymentID`, `Token`. In
  `stream`, `comment` joins the `launch`/`token` coalescing branch, keyed `comment|token`, and is
  dropped when `?token=` names another token.
- Web: `ApiEvent` gains `{ event: "comment"; data: components["schemas"]["CommentEvent"] }`;
  `API_EVENT_NAMES` gains `"comment"`. `INVALIDATION_EVENT_NAMES` does **not** change: a comment
  must not refetch token, chart or holders.

- [ ] **Step 1: Write the failing tests:**
  - `TestDecodeCommentEvent`: valid → ok; missing or malformed token → error.
  - `TestEventsCommentFilteredAndCoalesced`: three `comment` hints for token A within the window
    on a `?token=A` stream → one event; a hint for token B → none.
  - `TestRefreshListenerSkipsTailForComment`: a fake tail records no `Hint` for `comment`, one for
    `token`.
  - `web/src/api/sse.test.ts` (or the existing SSE test file): a `comment` frame is parsed and
    not treated as invalidation.
- [ ] **Step 2:** Run `go test ./internal/realtime/ ./internal/apiserver/ ./cmd/api/` and
  `npm test -- sse`. Expected: FAIL.
- [ ] **Step 3: Implement**; regenerate OpenAPI and the client.
- [ ] **Step 4:** Run `task verify`, `npm run web-api-diff`, `npm test`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: stream comment hints over SSE (Plan P0-C2 Task 4)`

### Task 5: Moderation CLI

**Files:**
- Create:
  - `backend/cmd/moderate/main.go`, `backend/cmd/moderate/main_test.go`
  - `backend/internal/store/postgres/moderation.go`
  - `backend/internal/store/postgres/queries/moderation.sql`
  - `backend/internal/store/postgres/postgrestest/moderation_integration_test.go`
- Modify: `backend/internal/social/ports.go` (`Moderator`); `docs/runbooks/production-readiness.md`
  (section "Moderating comments")

**Interfaces:**
- Produces:
  ```go
  package social
  type ReportSummary struct {
      CommentID int64; Token common.Address; CreatedAt time.Time; Body string
      AuthorDID string; Wallet common.Address; Reports int64; Reasons []ReportReason
  }
  type Resolution string // "dismissed" | "actioned"
  var ErrNotFound error
  type Moderator interface {
      OpenReports(ctx context.Context, s Scope, limit int) ([]ReportSummary, error)
      Hide(ctx context.Context, s Scope, commentID int64, operator, reason string) error
      Unhide(ctx context.Context, s Scope, commentID int64, operator, reason string) error
      Ban(ctx context.Context, s Scope, did string, operator, reason string) ([]common.Address, error) // affected tokens
      BanCommentAuthor(ctx context.Context, s Scope, commentID int64, operator, reason string) ([]common.Address, error)
      Unban(ctx context.Context, s Scope, did string, operator, reason string) ([]common.Address, error)
      Resolve(ctx context.Context, s Scope, commentID int64, r Resolution, operator, reason string) (int64, error)
  }
  ```
- `postgres.ModerationStore{Pool}` implements it; each mutation is one transaction: target
  lookup (`ErrNotFound`) → change → `moderation_actions` row → open reports resolved where the
  spec says → one `comment` NOTIFY per affected token.
- CLI: `execute(ctx, args []string, getenv func(string) string, out io.Writer) error`, mirroring
  `cmd/backfill-swap-sender`. Subcommands and flags exactly as spec "Moderation CLI"; `-operator`
  1–64 bytes; `-reason` 1–500 bytes; `ban` requires exactly one of `-did` / `-comment`. Errors are
  redacted with `redact.Error(err, DATABASE_URL)`. `reports` escapes control characters in bodies
  with `strconv.Quote`.

- [ ] **Step 1: Write the failing tests:**
  - `main_test.go` `TestParseModerateFlags`: missing `-operator`, both `-did` and `-comment`,
    unknown subcommand and bad `-resolution` each error before any database access.
  - Integration `TestModerateHideResolvesAndNotifies`: hide → comment absent from
    `ListTokenComments`, open reports `actioned`, one action row, one NOTIFY.
  - `TestModerateBanHidesAllAndUnbanRestores`: comments on two tokens; ban by `-comment` → both
    absent, username suppressed in `public_wallet_usernames`, two NOTIFYs; unban → both back.
  - `TestModerateUnknownTargets`: unknown comment and DID → `ErrNotFound`, no action row.
  - `TestModerateReportsListing`: grouping, counts, reasons and newest-first ordering.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3: Implement**; `task sqlc`; write the runbook section (commands, example session,
  that the DB credential is the access control).
- [ ] **Step 4:** Run `task verify`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: add comment moderation CLI (Plan P0-C2 Task 5)`

### Task 6: Usernames and published wallets

**Files:**
- Create:
  - `backend/internal/apiserver/profile_identity_routes.go`, `profile_identity_routes_test.go`
  - `backend/internal/store/postgres/postgrestest/public_profile_integration_test.go`
- Modify:
  - `backend/internal/social/username.go` (+ `username_test.go`), `ports.go`
  - `backend/internal/store/postgres/social_store.go`, `queries/social.sql`
  - `backend/internal/wallet/ports.go` (`Overview.Username`, `Ranked.Username` as `*string`)
  - `backend/internal/store/postgres/queries/read.sql` (`GetWalletTotals` or a new
    `GetWalletUsername`, `ListLeaderboard`) and `wallet_reader.go`
  - `backend/internal/apiserver/wallet_routes.go` (DTO fields `username`)
  - `backend/internal/apiserver/server.go`, `backend/cmd/api/main.go`
  - OpenAPI snapshot; `web/src/api/generated.ts`

**Interfaces:**
- Produces:
  ```go
  package social
  var ErrInvalidUsername, ErrUsernameReserved, ErrUsernameTaken, ErrTooManyPublicWallets error
  func NormalizeUsername(raw string) (string, error)
  type PublicProfile struct {
      Username *string; UsernameChangedAt, NextChangeAt *time.Time
      PublicWallets []common.Address; Banned bool
  }
  type ProfileStore interface {
      Get(ctx context.Context, a Actor) (PublicProfile, error)
      Put(ctx context.Context, a Actor, username *string, wallets []common.Address) (PublicProfile, error)
      Delete(ctx context.Context, a Actor) error
  }
  ```
  `username` nil in `Put` keeps the current name (allows changing only wallets).
- `Put` in one transaction under the subject lock: banned → `ErrBanned`; wallets > 5 →
  `ErrTooManyPublicWallets`; any wallet not in `a.Wallets` → `ErrWalletNotLinked`; if the name
  differs from the current one: interval check → `*RateLimitedError`; hold check and unique
  insert (SQLSTATE 23505 → `ErrUsernameTaken`); release the old name; then replace published
  wallets per R10.
- `Delete`: never rate-limited and allowed for banned users; releases the name, sets
  `username = NULL` and `username_changed_at = now()` (so a new name waits 7 days), deletes
  published wallets.
- Routes (tag `profile`, `private, no-store`): `getPublicProfile`, `putPublicProfile`,
  `deletePublicProfile` on `/me/public-profile`, body `{username?: string, public_wallets:
  string[]}`.
- Wallet reads: `username` from `public_wallet_usernames` only.

- [ ] **Step 1: Write the failing unit tests** `TestNormalizeUsernameVectors`: `"Alice"` →
  `"alice"`; `"ab"`, 21 chars, `"al ice"`, `"al-ice"`, `"ålice"` → invalid; `"0xabc"` →
  invalid; `"admin"`, `"Admin_1"`, `"support2"`, `"mod__"` → reserved; `"admin_fan"` → ok.
- [ ] **Step 2: Write the failing integration tests:**
  - `TestPutProfileClaimAndChangeInterval`: first set ok; change at once → `RateLimitedError`;
    after moving `username_changed_at` back 8 days → ok; wallets-only change → ok.
  - `TestUsernameRaceAndHold` (Review Focus 3): two concurrent claims → one ok, one
    `ErrUsernameTaken`; a released name → taken for another DID, reclaimable by its previous
    owner.
  - `TestPublicWalletTakeoverAndPrune`: A publishes W; B (W linked) publishes W → owned by B;
    A's next Put with W no longer linked prunes it.
  - `TestWalletAndLeaderboardUsername`: shown for a published wallet; null for unpublished, for a
    banned user, and after Delete.
- [ ] **Step 3: Write the failing route tests** `TestPublicProfileStatusTable` (409, 422 ×3,
  403 ×2, 429 with `Retry-After`) and the `username` field on `getWallet` and `getLeaderboard`.
- [ ] **Step 4:** Run the tests. Expected: FAIL.
- [ ] **Step 5: Implement**; `task sqlc`; regenerate OpenAPI and client.
- [ ] **Step 6:** Run `task verify`, `npm run web-api-diff`. Expected: PASS.
- [ ] **Step 7: Commit** `feat: add usernames and published wallets (Plan P0-C2 Task 6)`

### Task 7: Token page comments

**Files:**
- Create:
  - `web/src/comments/comment-rules.ts`, `comment-rules.test.ts`
  - `web/src/comments/comment-model.ts`, `comment-model.test.ts`
  - `web/src/comments/comment-panel.tsx`
  - `web/e2e/token-comments.spec.ts`
- Modify:
  - `web/src/api/client.ts` (`listTokenComments`, `getCommentEligibility`, `postTokenComment`,
    `reportComment`)
  - `web/src/token/token-detail.tsx` (mount the panel; forward `comment` events)
  - `web/app/styles/pages.css`
  - `web/scripts/run-e2e-live-strip.mjs` (add `token-comments`)

**Interfaces:**
- Produces:
  ```ts
  // comment-rules.ts — mirrors social.NormalizeBody; the server stays authoritative
  export const MAX_BODY_CODE_POINTS = 500;
  export const MAX_NEWLINE_RUN = 3;
  export type BodyRule = "empty" | "too_long" | "control" | "bidi" | "newlines";
  export function checkBody(raw: string): { ok: true; body: string } | { ok: false; rule: BodyRule };
  export function codePointCount(text: string): number;
  // comment-model.ts
  export type CommentItem = components["schemas"]["CommentDTO"];
  export function mergeFirstPage(current: readonly CommentItem[], firstPage: readonly CommentItem[]): CommentItem[];
  //   = firstPage ∪ current items with id < min(firstPage ids) (BigInt compare), newest first, deduped
  export function authorLabel(author: CommentItem["author"]): { primary: string; walletHref: string | null };
  //   username → "@name"; else shortAddress(wallet); walletHref only when wallet is present
  export function problemMessage(code: string | undefined): string; // one sentence per spec error code
  ```
- Panel: section "Comments" with count, list (`<p>` text with `white-space: pre-wrap`), "Load
  more", composer. Composer states: signed out → "Sign in to comment" (calls `login`); loading
  eligibility; banned → notice; no eligible wallet → "Hold this token to comment"; eligible → a
  wallet select (when more than one), a textarea with a code-point counter and the rule message,
  Post. Each comment has a Report menu with the four reasons; reported → "Reported". Live:
  a `comment` event for this token triggers one coalesced (500 ms) page-1 refetch with
  `cache: "no-store"`, merged by `mergeFirstPage`.

- [ ] **Step 1: Write the failing unit tests:**
  - `checkBody` mirrors every Go vector from Task 2 that is expressible in a JS string.
  - `codePointCount("😀".repeat(500)) === 500`.
  - `mergeFirstPage` drops a hidden comment that disappeared from page 1, keeps older loaded
    items, and dedupes.
  - `authorLabel` for username only, wallet only, and both.
  - constants equal the spec values.
- [ ] **Step 2:** Run `npm test -- comment-rules comment-model`. Expected: FAIL.
- [ ] **Step 3: Implement** the client methods, model and panel.
- [ ] **Step 4: Write the e2e** `token-comments.spec.ts`, mocked with `page.route` like
  `wallet-page.spec.ts`:
  - signed-out reader sees comments and "Sign in to comment";
  - a mocked eligible session posts; the comment appears at the top;
  - a mocked 403 `not_holder` and a 429 show their messages;
  - a mocked SSE `comment` event triggers exactly one refetch of page 1 and the new comment shows
    in a second page without reload;
  - a hidden comment disappears after the event (Review Focus 5);
  - a body with `<script>` renders as literal text;
  - the token page makes no wallet-stack request until the user clicks sign in.
- [ ] **Step 5:** Run `npm run format:check` (touched files), `npm run lint`, `npm run typecheck`,
  `npm test`, `npm run test:e2e:live`. Expected: PASS.
- [ ] **Step 6: Commit** `feat(web): add token comments panel (Plan P0-C2 Task 7)`

### Task 8: Username settings and display

**Files:**
- Create:
  - `web/src/profile/public-profile-model.ts`, `public-profile-model.test.ts`
  - `web/src/profile/public-profile-form.tsx`
  - `web/e2e/public-profile.spec.ts`
- Modify:
  - `web/src/api/client.ts` (`getPublicProfile`, `putPublicProfile`, `deletePublicProfile`)
  - `web/src/profile/profile-view.tsx` (mount the form)
  - `web/src/portfolio/wallet-view.tsx` (header `@username` when present)
  - `web/src/portfolio/leaderboard-view.tsx` (`@username` with the short address as title)
  - `web/scripts/run-e2e-live-strip.mjs` (add `public-profile`)

**Interfaces:**
- Produces:
  ```ts
  export const USERNAME_PATTERN = /^[a-z0-9_]{3,20}$/;
  export function normalizeUsername(raw: string): string;                 // trim + lowercase
  export function usernameHint(raw: string): string | null;               // format/`0x` hint only; reserved and taken are server answers
  export function nextChangeLabel(iso: string | null, now: Date): string | null; // "You can change it again on 16 Nov 2026"
  export const MAX_PUBLIC_WALLETS = 5;
  ```
- Form: username input with hint, checkbox list of the user's linked wallets (from
  `useWalletReadiness().linkedWallets`) to publish (max 5), Save, Remove username (confirm), the
  next-change date, a banned notice, and the residual-risk sentence from spec "Privacy".

- [ ] **Step 1: Write the failing unit tests** for the four exports, including
  `normalizeUsername(" Alice ") === "alice"` and `nextChangeLabel(null, now) === null`.
- [ ] **Step 2:** Run `npm test -- public-profile-model`. Expected: FAIL.
- [ ] **Step 3: Implement** the client methods, form and display changes.
- [ ] **Step 4: Write the e2e** `public-profile.spec.ts` (mocked): save succeeds and shows the next
  change date; 409 and 429 show their messages; publishing a sixth wallet is prevented; the wallet
  page header and `/leaderboard` render `@alice` when the API returns `username`.
- [ ] **Step 5:** Run the web gates as in Task 7. Expected: PASS.
- [ ] **Step 6: Commit** `feat(web): add username settings and display (Plan P0-C2 Task 8)`

### Task 9: Gates, docs and backlog

**Files:**
- Modify:
  - `docs/runbooks/production-readiness.md` (link the moderation section from the operations
    checklist; note the migration's Down deletes social data)
  - `docs/specs/2026-09-11-security-threat-model.md` (one row: comment/username abuse and
    moderation access, pointing to this spec)
  - `backlog.md` (Done entry with commit range and gate evidence; deferred review minors as one
    Active item; C3 as next)
  - `notes.md`, only if it lists comments or usernames as missing

- [ ] **Step 1: Run the full gates** (spec AC11):
  - `cd backend && task verify`;
  - web `format:check`, `lint`, `typecheck`, `test`, `test:e2e:live`, `test:anvil`;
  - `pwsh -File deploy/check.ps1 -Build` if `pwsh` exists, otherwise CI.

  Expected: PASS, apart from the known Windows CRLF false positives, each named.
- [ ] **Step 2: Docs** as listed above.
- [ ] **Step 3: Commit** `docs: record P0-C2 delivery and moderation runbook (Plan P0-C2 Task 9)`
