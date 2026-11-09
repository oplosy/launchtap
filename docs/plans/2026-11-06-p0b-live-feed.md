# P0-B Live Feed, Notifications and Share Cards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Explore and token pages show live trades, signed-in users get in-app notifications for watched tokens and their own trades, and token links unfurl into share cards.

**Architecture:** Each API process runs a trade tail (`internal/feed`) that reads canonical
`market_trades`, graduations and sync watermarks after an in-memory cursor and publishes
`trade`, `graduation`, `trade_burst` and `finality` events into the existing realtime hub, which
`/v1/events` streams. The web opens one app-wide SSE connection per tab. The strip, the token
page and the notification rules subscribe to it. Share cards are Next.js `opengraph-image`
routes that read the API server-side.

**Tech Stack:** Go 1.26 (Huma SSE, sqlc, pgx), PostgreSQL 18, Next.js 16 (`next/og`), React 19,
Vitest, Playwright, Anvil gate.

**Spec:** `docs/specs/2026-11-06-p0b-live-feed-notifications-share-design.md`

## Global Constraints

- No new dependency in `backend/go.mod` or `web/package.json`. `next/og` and its bundled
  `Geist-Regular.ttf` font ship with Next.
- Every on-chain amount is a decimal string end to end: `*big.Int` / `NUMERIC(78,0)` in Go and
  SQL, `bigint` in TypeScript. Never a float.
- Prometheus code stays in `internal/metrics` (depguard `prometheus-boundary`). `internal/feed`
  reports through a stdlib-only `feed.Observer` interface.
- No address, hash or token name becomes a metric label.
- Rate limit: at most 20 `trade` events per second per process. The rest go into one
  `trade_burst` per 1-second window.
- `GET /v1/trades/recent`: default 50 items, maximum 100.
- Notification large-trade threshold: 0.5 ETH gross (`500000000000000000` wei), a constant.
  Toasts arriving within 5 seconds collapse. Notifications live for the tab session only;
  `sessionStorage` holds only the last-read marker.
- Share card: 1200×630 PNG, `revalidate = 300`. Text-only rendering. Images come only from our
  own API image route.
- New private web setting `API_INTERNAL_BASE_URL` (Compose value `http://api:8080`). When unset,
  it falls back to `NEXT_PUBLIC_API_BASE_URL`.
- Migrations, sqlc output and the OpenAPI/generated web client are regenerated, never
  hand-edited (`task sqlc`, `npm run web-api-sync`). CI diff checks must stay green.
- Don't run `gofmt`/`prettier` unscoped across the repo; format only touched files.

## Review Focus

1. A pool swap whose `trader` is null (router-originated) must render as "—" in the strip. It
   must never match the "own trade" rule. Test in Task 5 and Task 6.
2. A token name or symbol with emoji, right-to-left text or 64+ characters must truncate with an
   ellipsis in the strip and on the card, never overflow or crash. Test in Task 5 and Task 7.
3. After an SSE reconnect, the strip refetches `/v1/trades/recent` and de-duplicates by
   `(tx_hash, log_index)`, so missed events appear once and nothing doubles. Test in Task 4.
4. A tail starting on an empty database (no trades yet) must publish the first trade that
   appears. Test in Task 2.
5. A card whose token image is missing or fails to load renders without the image and still
   returns 200 PNG. Test in Task 7.

---

### Task 1: Feed read model (migration, queries, reader)

**Files:**
- Create: `backend/internal/store/postgres/migrations/00016_live_feed.sql`
- Modify: `backend/internal/store/postgres/queries/read.sql` (new queries), regenerated `backend/internal/store/postgres/sqlc/*`
- Create: `backend/internal/feed/ports.go`, `backend/internal/store/postgres/feed_reader.go`
- Test: `backend/internal/store/postgres/postgrestest/feed_reader_integration_test.go`

**Interfaces:**
- Produces, in `internal/feed/ports.go`:
  ```go
  type Cursor struct{ Block int64; TxIndex, LogIndex int32 }
  type Trade struct {
      Token common.Address; Name, Symbol, Source string; Trader *common.Address; Buy bool
      ETHVolume, TokenVolume *big.Int; Block int64; TxIndex int32; TxHash common.Hash
      LogIndex int32; Time time.Time; Finality string
  }
  type Graduation struct { Token, Pair common.Address; Name, Symbol string; TxHash common.Hash; Block int64; TxIndex, LogIndex int32 }
  type Launch struct { Token, Creator common.Address; Name, Symbol string; TxHash common.Hash; Block int64; TxIndex, LogIndex int32 }
  type Watermarks struct{ Safe, Finalized int64 } // 0 when unknown
  type Reader interface {
      TradesAfter(ctx context.Context, chainID int64, after Cursor, limit int) ([]Trade, error)        // ascending
      RecentTrades(ctx context.Context, chainID int64, limit int) ([]Trade, error)                     // descending
      GraduationsAfter(ctx context.Context, chainID int64, after Cursor, limit int) ([]Graduation, error)
      LaunchesAfter(ctx context.Context, chainID int64, after Cursor, limit int) ([]Launch, error)
      Newest(ctx context.Context, chainID int64) (trade, graduation, launch Cursor, err error)
      Watermarks(ctx context.Context, chainID int64, deploymentID string) (Watermarks, error)
  }
  ```
  `postgres.FeedReader{Pool *pgxpool.Pool}` implements `feed.Reader`.

- [ ] **Step 1:** Migration `00016_live_feed.sql` (goose up/down like `00015`): indexes
  `trades_chain_order_idx ON trades (chain_id, block_number, transaction_index, log_index)` and
  `pool_swaps_chain_order_idx ON pool_swaps (chain_id, block_number, transaction_index, log_index)`.
  The down migration drops both.
- [ ] **Step 2:** Add queries `ListMarketTradesAfter`, `ListRecentMarketTrades`,
  `ListGraduationsAfter`, `ListLaunchesAfter` (from `token_launches`), `GetNewestFeedCursors`, `GetSyncWatermarks` to `read.sql`.
  - They read `market_trades` / `graduations` joined to `token_launches` for name and symbol.
  - Ordering is `(block_number, transaction_index, log_index)`; the "after" form uses a tuple
    comparison, as `ListMarketTrades` does.
  - Run `task sqlc`.
- [ ] **Step 3: Write failing integration tests** in `feed_reader_integration_test.go`. Use the
  existing postgrestest fixtures, with a curve trade, a pool swap with NULL trader, a graduation
  and sync_state at safe=10/finalized=8:
  - `TestFeedReaderTradesAfterOrdersAscending`: returns rows strictly after the cursor, ascending,
    limit respected; the pool swap has `Trader == nil` and `Source == "pool"`.
  - `TestFeedReaderRecentTradesDescending`: the newest first, at most `limit`.
  - `TestFeedReaderEmptyDatabase`: `Newest` returns zero cursors and no error; `TradesAfter(Cursor{})` returns nothing.
  - `TestFeedReaderWatermarks`: returns `{Safe:10, Finalized:8}`; a missing row returns zeros.
  - `TestFeedReaderGraduationsAfter` and `TestFeedReaderLaunchesAfter`: name and symbol are present, ascending order.
- [ ] **Step 4:** Run `go test -tags=integration ./internal/store/postgres/postgrestest/ -run FeedReader`. Expect FAIL (no reader).
- [ ] **Step 5:** Implement `FeedReader`, converting `NUMERIC` with the existing helpers in `convert.go`.
- [ ] **Step 6:** Run the same command and `task integration` (it includes the migration up/down/up tests). Expect PASS.
- [ ] **Step 7: Commit** `feat(store): add live feed read model and ordering indexes`

### Task 2: Trade tail

**Files:**
- Create: `backend/internal/feed/tail.go`, `backend/internal/feed/tail_test.go`
- Modify: `backend/internal/realtime/hub.go` (Event fields)

**Interfaces:**
- Consumes: `feed.Reader`, `feed.Cursor`, `feed.Trade`, `feed.Graduation` (Task 1).
- Produces:
  - `realtime.Event` gains `Trade *feed.Trade`, `Graduation *feed.Graduation`, `Launch *feed.Launch`,
    `Burst []realtime.BurstCount` (`{Token string; Count int}`), `BurstStart`, `BurstEnd time.Time`,
    `SafeBlock`, `FinalizedBlock int64`. New `Type` values: `trade`, `graduation`, `new_token`,
    `trade_burst`, `finality`. (`launch` stays the existing refresh hint.) `realtime.Decode` still accepts only `launch|token|reorg`, because feed events never
    come from NOTIFY. If importing `feed` into `realtime` creates a cycle, move the Trade/Graduation
    structs into `realtime` and alias them from `feed`.
  - ```go
    type Publisher interface{ Publish(realtime.Event) }
    type Observer interface{ Published(eventType string); Burst(); Lag(time.Duration) }
    type Tail struct { Reader Reader; Hub Publisher; ChainID int64; DeploymentID string;
        Observer Observer; Now func() time.Time; Limit int /*200*/; PerSecond int /*20*/; WatermarkEvery time.Duration /*5s*/ }
    func (t *Tail) Start(ctx context.Context) error   // reads Newest, then serves until ctx ends
    func (t *Tail) Hint(event realtime.Event)        // non-blocking; token/launch → read; reorg → rewind
    ```

- [ ] **Step 1: Write failing tests** with a fake Reader, a recording Publisher and a fake clock:
  - `TestTailStartsAtNewestAndSkipsHistory`: existing rows are not published; a new row after `Hint` is.
  - `TestTailEmptyDatabasePublishesFirstTrade` (Review Focus 4).
  - `TestTailPublishesOldestFirstAndAdvancesCursor`: two hints, no duplicates.
  - `TestTailPublishesNewTokenAndGraduation`: launch and graduation rows become `new_token` and `graduation` events with name and symbol.
  - `TestTailRewindsOnReorg`: hint `reorg{CommonAncestor: 5}` moves all three cursors to `Cursor{Block: 5, TxIndex: max, LogIndex: max}`, so a replacement row at block 6 is published again.
  - `TestTailRateLimitEmitsBurst`: 25 rows in one second publishes 20 `trade` and one `trade_burst` with per-token counts summing to 5; `Observer.Burst` is called once.
  - `TestTailFinalityOnTickerWithoutHints`: watermarks go 0→10 with no hint; one `finality{SafeBlock:10}` is published; an unchanged watermark publishes nothing.
  - `TestTailReaderErrorKeepsCursor`: an error, then success, publishes the row once.
- [ ] **Step 2:** Run `go test ./internal/feed/`. Expect FAIL.
- [ ] **Step 3:** Implement `Tail`.
  - Hints coalesce: a single worker goroutine and a 1-slot signal channel.
  - Per-second window counting uses `Now`.
  - Reads happen only in the worker, so the cursor needs no mutex.
- [ ] **Step 4:** Run `go test -race ./internal/feed/ ./internal/realtime/`. Expect PASS.
- [ ] **Step 5: Commit** `feat(feed): add trade tail with rate limit, reorg rewind and finality`

### Task 3: API surface (SSE events, recent trades, metrics, wiring)

**Files:**
- Modify: `backend/internal/apiserver/event_routes.go`, `backend/internal/apiserver/public_routes.go` (or new `feed_routes.go`), `backend/internal/metrics/api.go`, `backend/cmd/api/main.go`, `docs/` OpenAPI artefact via `go run ./cmd/check-openapi` (`task openapi-diff`)
- Test: `backend/internal/apiserver/event_routes_test.go`, `backend/internal/apiserver/feed_routes_test.go`, `backend/internal/metrics/api_test.go`

**Interfaces:**
- Consumes: `feed.Tail`, `feed.Reader`, `feed.Observer`, `realtime.Event` (Tasks 1–2).
- Produces:
  - SSE event types (huma `sse.Register` map) `trade` (`tradeFeedEvent`), `graduation`,
    `new_token` (`chain_id`, `deployment_id`, `token`, `creator`, `name`, `symbol`, `tx_hash`,
    `block_number`), `trade_burst`, `finality`. The JSON fields follow the spec.
    - `trade` reuses `tradeDTO` field names (`source`, `trader`, `side`, `eth_volume`,
      `token_volume`, `block_number`, `transaction_index`, `tx_hash`, `log_index`, `time`,
      `finality`) plus `chain_id`, `deployment_id`, `token`, `name`, `symbol`.
    - `finality`: `safe_block`, `finalized_block`.
    - `trade_burst`: `window_start`, `window_end`, `items: [{token, count}]`.
  - `GET /v1/trades/recent?limit=` (OperationID `listRecentTrades`) returns
    `{snapshot, items: [tradeFeedItem]}`; `limit` is 1–100, default 50.
  - `metrics.API` methods `Published(eventType string)`, `Burst()`, `Lag(time.Duration)`,
    which satisfy `feed.Observer`. Metric names:
    `launchpad_api_feed_events_published_total{type}`, `launchpad_api_feed_bursts_total`,
    `launchpad_api_feed_tail_lag_seconds` (gauge).

- [ ] **Step 1: Write failing tests:**
  - `TestEventStreamForwardsFeedEventsUncoalesced`: two `trade` events for the same token both arrive, unlike token hints.
  - `TestEventStreamTokenFilterAppliesToFeed`: a `?token=A` stream gets A's `trade`/`graduation`, every `finality` and `trade_burst`, no `new_token`, and not B's trade.
  - `TestEventStreamStalledSubscriberEvicted`: with hub buffer 64, a non-reading subscriber is evicted after 65 events and another subscriber still receives all of them (spec AC3).
  - `TestRecentTradesLimitBounds`: limit 0→50, 101→422; items are newest first.
  - `TestAPIFeedMetricsRegistered`: the three metrics exist; `UnsafeLabels` finds nothing.
- [ ] **Step 2:** Run `go test ./internal/apiserver/ ./internal/metrics/`. Expect FAIL.
- [ ] **Step 3:** Implement. In the stream loop, feed events bypass the `eventBatch` coalescing and are sent immediately. In `cmd/api/main.go`:
  - `realtime.NewHub(1000, 64)`.
  - Construct `feed.Tail{Reader: postgres.FeedReader{Pool: pool}, Hub: hub, Observer: apiMetrics, ...}` and start it with the API context.
  - `listenRefreshHints` also calls `tail.Hint(event)`.
- [ ] **Step 4:** Run `go test -race ./internal/apiserver/ ./internal/metrics/ ./internal/feed/`, `task openapi-diff`, and `task lint`. Expect PASS, with the OpenAPI artefact regenerated and committed.
- [ ] **Step 5: Commit** `feat(api): stream live trades and serve recent trades`

### Task 4: Web live event stream (one connection per tab)

**Files:**
- Modify: `web/src/api/sse.ts`, `web/src/api/types.ts`, `web/src/api/generated*` (via `npm run web-api-sync`), `web/app/providers.tsx`, `web/src/discovery/token-discovery.tsx`, `web/src/token/token-detail.tsx`
- Create: `web/src/live/live-events.tsx` (provider + `useLiveEvents`), `web/src/live/feed-store.ts`
- Test: `web/src/api/sse.test.ts`, `web/src/live/feed-store.test.ts`

**Interfaces:**
- Consumes: SSE event types and `GET /v1/trades/recent` (Task 3).
- Produces:
  - `ApiEvent` union extended with `trade`, `graduation`, `new_token`, `trade_burst`, `finality`.
  - `LiveEventsProvider` opens one `SseInvalidationStream` on `/v1/events` (unfiltered) for the
    app and exposes `useLiveEvents(listener: (event: ApiEvent) => void)`. Discovery and token
    detail stop creating their own streams and subscribe through `useLiveEvents`, keeping their
    current invalidation behaviour (token detail filters by its address client-side).
  - `feed-store.ts`:
    ```ts
    type FeedItem = TradeFeedItem & { kind: "trade" | "graduation" | "launch"; key: string /* `${tx_hash}:${log_index}` */ };
    function applyFeedEvent(items: FeedItem[], event: ApiEvent, max = 50): FeedItem[];
    function mergeRecent(items: FeedItem[], recent: FeedItem[], max = 50): FeedItem[];
    ```
    - New items go first; duplicates by key are dropped.
    - `reorg` removes items with `block_number > common_ancestor`.
    - `finality` upgrades `observed` items at or below `safe_block` to `safe`, and at or below
      `finalized_block` to `finalized`.

- [ ] **Step 1: Write failing tests** in `feed-store.test.ts`:
  - `applies trade events newest first and caps at 50`;
  - `drops duplicate tx_hash:log_index`;
  - `reorg removes items above the common ancestor`;
  - `finality upgrades observed items at or below safe_block`;
  - `mergeRecent after reconnect keeps one copy of each item` (Review Focus 3);
  - in `sse.test.ts`: `dispatches trade, graduation, new_token, trade_burst and finality to listeners` and `opens a single EventSource for multiple subscribers`.
- [ ] **Step 2:** Run `npm test -- src/live src/api`. Expect FAIL.
- [ ] **Step 3:** Implement. After a reconnect (the existing `refetchSnapshot` path), the provider emits a synthetic `reconnected` signal that the strip uses to call `mergeRecent`.
- [ ] **Step 4:** Run `npm run web-api-diff`, `npm run typecheck`, `npm run lint`, `npm test`. Expect PASS. Check that the discovery and token-detail tests still pass unchanged.
- [ ] **Step 5: Commit** `feat(web): share one live event stream per tab`

### Task 5: Live strip on Explore and live Recent trades

**Files:**
- Create: `web/src/live/live-strip.tsx`, `web/app/styles/live.css`
- Modify: `web/src/discovery/token-discovery.tsx` (render strip above lists), `web/src/token/token-detail.tsx` (prepend trades), `web/app/globals.css` (import)
- Test: `web/src/live/live-strip.test.tsx`, `web/src/token/token-detail.test.tsx` (extend)

**Interfaces:**
- Consumes: `useLiveEvents`, `applyFeedEvent`, `mergeRecent`, `GET /v1/trades/recent` (Task 4); the existing USD conversion helper from P0-A.
- Produces: `<LiveStrip />`. No exported API.

- [ ] **Step 1: Write failing tests:**
  - `renders recent trades from the initial fetch`;
  - `prepends a live trade`;
  - `shows a dash for a null trader` (Review Focus 1);
  - `truncates a 64-character emoji name with an ellipsis` (Review Focus 2);
  - `uses a static row under prefers-reduced-motion`;
  - `marks observed items pending until finality`;
  - token detail: `prepends a trade event for this token without refetching` and `ignores trade events for other tokens`.
- [ ] **Step 2:** Run `npm test -- src/live src/token`. Expect FAIL.
- [ ] **Step 3:** Implement.
  - Each item shows: buy/sell arrow, ticker, ETH amount (plus USD when fresh), shortened trader
    or "—", and relative time.
  - Launch and graduation items get a badge. Every item links to `/token/{address}`.
  - Use CSS `text-overflow: ellipsis` with a max width.
  - Under `prefers-reduced-motion: reduce`: no animation, `overflow-x: auto`.
- [ ] **Step 4:** Run `npm test`, `npm run lint`, `npm run typecheck`, `npx prettier --check` on touched files, and `npm run test:e2e -- release-hardening` (budgets with the strip present, spec AC7). Expect PASS.
- [ ] **Step 5: Commit** `feat(web): add live trade strip and live recent trades`

### Task 6: In-app notifications

**Files:**
- Create: `web/src/live/notification-rules.ts`, `web/src/live/notifications.tsx` (provider, toast region, bell menu)
- Modify: `web/src/components/app-shell.tsx` (bell in navbar), `web/app/styles/live.css`
- Test: `web/src/live/notification-rules.test.ts`, `web/src/live/notifications.test.tsx`

**Interfaces:**
- Consumes: `useLiveEvents` (Task 4), `useWatchlist()` (watched token addresses), `useWalletConnection().address`.
- Produces:
  ```ts
  const LARGE_TRADE_WEI = 500_000_000_000_000_000n;
  type NotificationKind = "graduation" | "large_trade" | "own_trade_safe";
  type Notification = { id: string; kind: NotificationKind; token: string; symbol: string; txHash: string; at: number };
  type RuleState = { pendingOwn: Map<string, TradeFeedItem> };
  function evaluate(event: ApiEvent, ctx: { watched: Set<string>; wallet?: string }, state: RuleState): Notification[];
  ```

- [ ] **Step 1: Write failing tests:**
  - `notifies when a watched token graduates`;
  - `notifies a watched-token trade at exactly 0.5 ETH` and `not at 0.499999999999999999 ETH`;
  - `ignores trades on unwatched tokens`;
  - `notifies own trade once when finality covers its block`, and `does not notify own trade before finality`;
  - `null trader never matches own trade` (Review Focus 1);
  - `address comparison is case-insensitive`;
  - in the component tests: `collapses toasts arriving within 5 seconds`, `hides the bell when neither signed in nor connected`, and `keeps last-read in sessionStorage only`.
- [ ] **Step 2:** Run `npm test -- src/live`. Expect FAIL.
- [ ] **Step 3:** Implement. Toasts use `role="status"` and `aria-live="polite"`. The bell menu is keyboard accessible and listens for `Escape`.
- [ ] **Step 4:** Run `npm test`, `npm run lint`, `npm run typecheck`, and `npm run test:e2e -- accessibility`. Expect PASS.
- [ ] **Step 5: Commit** `feat(web): add session in-app notifications`

### Task 7: Share cards and share actions

**Files:**
- Create: `web/app/token/[address]/opengraph-image.tsx`, `web/app/token/[address]/twitter-image.tsx` (re-export), `web/src/share/card-data.ts`, `web/src/share/share-actions.tsx`
- Modify: `web/app/token/[address]/page.tsx` (`generateMetadata`), `web/src/token/token-detail.tsx` (actions), `web/src/config/server.ts` (create if missing: `apiInternalBaseUrl()`), `web/scripts/container-preflight.mjs` (+ test), `deploy/compose.yaml` (web `environment: API_INTERNAL_BASE_URL: http://api:8080`), `web/.env.example`
- Test: `web/src/share/card-data.test.ts`, `web/scripts/container-preflight.test.mjs`, `web/e2e/token-detail.spec.ts` (extend)

**Interfaces:**
- Consumes: `GET /v1/tokens/{token}`, the token image route, and `GET /v1/reference/eth-usd` (existing).
- Produces:
  ```ts
  type CardData = { kind: "token"; name: string; symbol: string; imageUrl: string | null; marketCapEth: string; marketCapUsd: string | null; phase: "bonding" | "graduated"; progressBps: number } | { kind: "generic" };
  async function loadCardData(address: string, fetcher?: typeof fetch): Promise<CardData>;
  function apiInternalBaseUrl(): string; // API_INTERNAL_BASE_URL ?? NEXT_PUBLIC_API_BASE_URL; throws on non-http(s)
  ```

- [ ] **Step 1: Write failing tests:**
  - `loadCardData returns generic for an invalid address`;
  - `returns generic when the API fails`;
  - `imageUrl points only at the API image route`;
  - `truncates names over 32 characters` (Review Focus 2);
  - `imageUrl is null when the image request fails` (Review Focus 5);
  - `apiInternalBaseUrl rejects ftp: and javascript:`;
  - preflight: `rejects a non-http(s) API_INTERNAL_BASE_URL`;
  - e2e: `GET /token/<known>/opengraph-image returns 200 image/png 1200x630` (read the PNG IHDR), `unknown token returns 200 generic card`, and `token page has og:image, twitter:card and a token title`.
- [ ] **Step 2:** Run `npm test -- src/share scripts` and `npm run test:e2e -- token-detail`. Expect FAIL.
- [ ] **Step 3:** Implement.
  - `ImageResponse` with the default bundled font; `export const revalidate = 300`; `size = { width: 1200, height: 630 }`.
  - Server fetches use a 3-second `AbortSignal.timeout`.
  - Failures are logged with `console.error` and render the generic card.
  - "Share on X" is `https://x.com/intent/post?text=<$SYMBOL on Launchpad>&url=<canonical>`, encoded with `URLSearchParams`.
  - "Copy link" uses `navigator.clipboard` with a fallback message.
- [ ] **Step 4:** Run `npm test`, `npm run lint`, `npm run typecheck`, `npm run test:e2e`, `npm run verify:bundle`, and `docker compose -f deploy/compose.yaml config --quiet` (with the dummy env `check.ps1` uses). Expect PASS.
- [ ] **Step 5: Commit** `feat(web): add token share cards and share actions`

### Task 8: End-to-end gate, runbook and backlog

**Files:**
- Modify: `web/e2e/task6-anvil.spec.ts` (or the Anvil gate spec that performs a buy), `docs/runbooks/production-readiness.md` (Monitoring table: feed metrics; web env `API_INTERNAL_BASE_URL`), `deploy/README.md`, `backlog.md`

- [ ] **Step 1: Write the failing Anvil test** `a buy appears in the Explore strip and Recent trades without reload within 3 seconds of commit` (spec AC1). Also `a reorg fixture removes the trade from the strip` if the gate already has a reorg path; otherwise rely on the Task 4 and Task 2 unit tests and say so in the report.
- [ ] **Step 2:** Run `npm run test:anvil` (PowerShell, Foundry on PATH). Expect FAIL before wiring, PASS after.
- [ ] **Step 3:** Docs.
  - Runbook: the three feed metrics, `API_INTERNAL_BASE_URL`, and the hub buffer of 64.
  - Backlog: move "P0-B" to Done with commits and gate evidence; add the wallet-holdings
    notifications to the sub-project C scope note.
- [ ] **Step 4:** Run the full gates:
  - `cd backend && task verify`;
  - web `format:check`, `lint`, `typecheck`, `test`;
  - `pwsh -File deploy/check.ps1 -Build` if `pwsh` exists, otherwise CI.

  Expect PASS apart from the known Windows CRLF false positives.
- [ ] **Step 5: Commit** `test: cover live feed in the Anvil gate and document P0-B`
