# P0-A Market Data and Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add USD reference pricing, trending / king-of-the-hill highlights, an age filter, a
server-side watchlist, Dexscreener/GeckoTerminal links, and fix the non-decaying `volume_24h`.

**Architecture:** The API process polls CoinGecko for ETH/USD in a stdlib-only `internal/pricefeed`
package and serves it. The web converts ETH base units to USD with exact bigint math. The API
computes highlights at query time from `trades` and `pool_swaps`, with new `block_time` indexes and
a 15 s in-process cache. The indexer gains a one-minute sweep that marks tokens dirty when their
trades leave the 24 h window. The watchlist is a new table behind Privy-authenticated
`/v1/me/watchlist` routes.

**Tech Stack:** Go 1.26 (Huma, pgx/sqlc, goose), PostgreSQL, Next.js 16 / React, vitest,
Playwright (Anvil gate).

**Spec:** `docs/specs/2026-11-05-p0-market-data-discovery-design.md`

## Global Constraints

- No contract change and no new engine version.
- On-chain amounts are `*big.Int` / `NUMERIC(78,0)`, never floats. The USD price is a decimal
  string end-to-end; the web multiplies with bigint.
- `internal/pricefeed` is stdlib-only (new depguard rule beside `internal/curve` / `internal/config`).
- Provider request:
  `GET https://pro-api.coingecko.com/api/v3/simple/price?ids=ethereum&vs_currencies=usd&include_last_updated_at=true&precision=full`
  with header `x-cg-pro-api-key: <COINGECKO_API_KEY>`.
- Polling policy:
  - Poll at most once per 60 s.
  - A sample is fresh only when `last_updated_at` is present, not in the future, and ≤ 120 s old at
    read time.
  - Price is positive with ≤ 18 fractional digits.
  - 429 → exponential backoff with jitter.
  - 5xx, timeout or malformed data → keep the last sample (it ages out to `null`).
- `COINGECKO_API_KEY` is a backend secret: never `NEXT_PUBLIC_*`, never logged, never committed.
- `ETH_USD_SOURCE`: empty = disabled, `coingecko` = enabled, anything else is a startup error.
  `COINGECKO_API_KEY` is required iff the source is `coingecko`.
- Trending:
  - At most 20 tokens; 60-minute window; score = gross ETH volume × distinct buyers.
  - Ties break by most recent trade, then token address.
  - Cached 15 s per API process.
- Age filter: `age=all|24h|7d`, default `all`; anything else → 400 `invalid_age`. The cutoff is
  fixed on the first page and carried in the cursor.
- 24 h decay sweep: every 60 s, mark dirty every token with a canonical trade in
  `[now − 24 h − 2 min, now − 24 h)`.
- Watchlist:
  - At most 200 entries, newest first.
  - 409 `watchlist_full`; 404 for an unknown token.
  - `Cache-Control: private, no-store`; the DID never appears in logs.
- Web copy (exact):
  - attribution "Data provided by CoinGecko";
  - toast "Watchlist full (200)";
  - tabs "Trending", "New", "Market cap", "Volume", "Watchlist";
  - age options "All", "24h", "7d";
  - card title "King of the hill".
- Commits stay local: the repository is archived, so no push. Conventional prefix, imperative,
  subject < 72 chars, no AI attribution.
- Never run `gofmt`/`forge fmt` unscoped. Run generators only through their npm/go commands; never
  hand-edit generated files.

## Review Focus

1. **Concurrent watchlist adds at 199 entries.** Two simultaneous `PUT`s must not produce 201
   entries. Task 7 serializes per user with a transaction-scoped advisory lock and tests it with
   parallel `PUT`s.
2. **Age cursor reused with a different `age`, or replayed a day later.** The cursor must not drift
   to a new cutoff or silently mix filters. Task 4 includes `age` in the cursor filter hash and
   tests that a mismatch returns the existing invalid-cursor problem.
3. **Price in exponent form, `"0"` or a 19-digit fraction from the provider.** The price must
   become `null`, never a wrong USD value. Task 2 has table tests for `"3.1e3"`, `"0"`, `"-1"` and
   a 19-digit fraction.
4. **Reorged trades in the trending window.** A reorged block's trades must not score. Task 5's
   integration test inserts a non-canonical row and asserts it is excluded.
5. **USD for very large amounts (total supply × price).** The display must not lose precision or
   switch to exponent notation. Task 9 tests `formatUsd` on 10^27 base units.

---

### Task 1: Config — validate `ETH_USD_SOURCE`, add `COINGECKO_API_KEY`

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/.env.example`
- Modify: `deploy/compose.yaml`, `deploy/README.md` — pass `COINGECKO_API_KEY` through the existing
  external secret-file pattern.
- Test: `backend/internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `Config.ETHUSDSource string` (now validated);
  - `Config.CoinGeckoAPIKey string` (env `COINGECKO_API_KEY`);
  - exported constant `config.ETHUSDSourceCoinGecko = "coingecko"`.

- [ ] **Step 1: Write failing tests** in `config_test.go`, in the existing table style:
  - `TestETHUSDSourceRejectsUnknownValue`: `ETH_USD_SOURCE=coinbase` →
    `FieldError{Field:"ETH_USD_SOURCE", Err: ErrInvalid}`.
  - `TestCoinGeckoKeyRequiredWhenEnabled`: `ETH_USD_SOURCE=coingecko`, no key →
    `FieldError{Field:"COINGECKO_API_KEY", Err: ErrMissing}`.
  - `TestCoinGeckoKeyRejectedWhenDisabled`: source empty, key set →
    `FieldError{Field:"COINGECKO_API_KEY", Err: ErrInvalid}`.
  - `TestCoinGeckoEnabled`: both set → no error; fields populated.
  - `TestCoinGeckoKeyNotInErrorText`: the error string never contains the key value.
- [ ] **Step 2: Run tests to confirm they fail.**
  - Run: `cd backend && go test ./internal/config -run 'ETHUSD|CoinGecko' -v`
  - Expected: FAIL.
- [ ] **Step 3: Implement** in `config.go` beside the other field validators. Keep the package
  stdlib-only.
- [ ] **Step 4: Run tests to confirm they pass.**
  - Run: `cd backend && go test ./internal/config -v`
  - Expected: PASS.
- [ ] **Step 5: Add `ETH_USD_SOURCE=` and `COINGECKO_API_KEY=` to `backend/.env.example`** (empty,
  with a comment that the key is a secret), and wire the secret into `deploy/`.
  - Run: `cd deploy && powershell -File check.ps1`
  - Expected: pass. If Docker is unavailable, run `docker compose config` validation instead and
    say so.
- [ ] **Step 6: Commit** `feat(config): validate ETH/USD source and CoinGecko key`

### Task 2: `internal/pricefeed` and `GET /v1/reference/eth-usd`

**Files:**
- Create:
  - `backend/internal/pricefeed/feed.go`
  - `backend/internal/pricefeed/price.go`
  - `backend/internal/apiserver/reference_routes.go`
- Modify: `backend/.golangci.yml` (depguard rule), `backend/cmd/api/main.go`
- Test:
  - `backend/internal/pricefeed/feed_test.go`
  - `backend/internal/pricefeed/price_test.go`
  - `backend/internal/apiserver/reference_routes_test.go`

**Interfaces:**
- Consumes: `Config.ETHUSDSource`, `Config.CoinGeckoAPIKey` (Task 1).
- Produces:
  - Price parsing:
    - `pricefeed.ParsePrice(n json.Number) (string, error)` — returns the canonical decimal.
    - It rejects:
      - exponent form;
      - sign;
      - a value of zero or less;
      - more than 18 fractional digits;
      - an empty string.
  - Options:
    ```go
    type Options struct {
        BaseURL  string // default "https://pro-api.coingecko.com/api/v3"
        APIKey   string
        Client   *http.Client // default timeout 10s
        Now      func() time.Time
        Interval time.Duration // default 60s
        MaxAge   time.Duration // default 120s
        Jitter   func(time.Duration) time.Duration
    }
    ```
  - Feed methods:
    - `pricefeed.New(o Options) *Feed`
    - `(*Feed).Refresh(ctx) error` — one provider call. Concurrent callers coalesce: one in-flight
      request, and the others wait for its result.
    - `(*Feed).Run(ctx) error` — the loop until ctx ends; returns `nil` on cancel.
    - `(*Feed).Snapshot() Snapshot`
  - Snapshot:
    ```go
    type Snapshot struct {
        Price             *string    // nil unless fresh at read time
        ProviderUpdatedAt *time.Time
        FetchedAt         *time.Time
    }
    ```
  - Constants: `pricefeed.Source = "coingecko"`,
    `pricefeed.AttributionURL = "https://www.coingecko.com/en/api"` (runbook
    `docs/runbooks/eth-usd-enrichment.md` §attribution).
  - Routes:
    - `apiserver.PriceSource interface{ Snapshot() pricefeed.Snapshot }`
    - `apiserver.ReferenceRoutes{ Prices PriceSource }` — a nil `Prices` means disabled.
    - `(*Server).RegisterReferenceRoutes(r ReferenceRoutes)`
  - Response, operation id `getEthUsdReference`: `{price, provider_updated_at, fetched_at, source,
    attribution_url}`.
    - Enabled: `source` and `attribution_url` are set, even while `price` is null.
    - Disabled: all five fields are `null`.
    - Header: `Cache-Control: public, max-age=15`.

- [ ] **Step 1: Write failing price tests.** In `price_test.go`, `TestParsePrice`, table:

  | Input | Result |
  | --- | --- |
  | `"3456.12"` | `"3456.12"` |
  | `"3456"` | `"3456"` |
  | `"0.000000000000000001"` | ok |
  | `"0.0000000000000000001"` | error |
  | `"0"` | error |
  | `"-1"` | error |
  | `"3.1e3"` | error |
  | `""` | error |

- [ ] **Step 2: Write failing feed tests.** In `feed_test.go`, use `httptest.Server` and a
  controllable `Now`:
  - `TestRefreshSendsKeyHeader`: the server asserts `x-cg-pro-api-key` and the exact query string.
  - `TestSnapshotFresh`: `last_updated_at = now−30s` → `Price` set.
  - `TestSnapshotStaleAfterMaxAge`: advance `Now` to 121 s after `last_updated_at` → `Price == nil`.
  - `TestRejectsFutureTimestamp`: `last_updated_at = now+5s` → sample not accepted.
  - `TestRejectsMissingTimestamp`.
  - `TestMalformedKeepsPreviousSample`: a good sample, then a malformed body. The previous sample
    stays; it is still fresh if ≤ 120 s old.
  - `Test5xxKeepsPreviousSample`.
  - `Test429BacksOff`: three 429s give delays 60s→120s→240s plus jitter (pin it with
    `Jitter: func(d) d`), capped at 15 min; reset to 60 s after a success.
  - `TestRefreshCoalesces`: 10 concurrent `Refresh` calls → the server counts exactly 1 request.
  - `TestErrorsDoNotContainKey`.
- [ ] **Step 3: Run tests to confirm they fail.**
  - Run: `cd backend && go test ./internal/pricefeed -v`
  - Expected: FAIL (package missing).
- [ ] **Step 4: Implement `price.go` and `feed.go`.** Decode with `json.Decoder.UseNumber()` into
  `{"ethereum":{"usd":json.Number,"last_updated_at":int64}}`. Validate fractional digits with a
  regexp, not `big.Float`. In `Run`, compute the next delay from the backoff state; the default
  jitter is ±20 %.
- [ ] **Step 5: Run tests to confirm they pass.**
  - Run: `cd backend && go test ./internal/pricefeed -race -v`
  - Expected: PASS.
- [ ] **Step 6: Add the depguard rule** `pricefeed-stdlib-only` (files `**/internal/pricefeed/**`,
  allow `$gostd`) next to the existing curve/config rules.
  - Run: `cd backend && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./internal/pricefeed/...`
  - Expected: `0 issues.`
- [ ] **Step 7: Write failing route tests** in `reference_routes_test.go`:
  - `TestEthUsdDisabledAllNull`;
  - `TestEthUsdFresh` (stub `PriceSource`, JSON fields and the `Cache-Control` header);
  - `TestEthUsdStaleNullPriceKeepsSource`.
- [ ] **Step 8: Implement the route and wire it.** Implement `reference_routes.go`. In
  `cmd/api/main.go`, when `c.ETHUSDSource == config.ETHUSDSourceCoinGecko`, create the feed, start
  `go feed.Run(ctx)` on the server's lifetime context, and register with `Prices: feed`. Otherwise
  register with `Prices: nil`.
  - Run: `cd backend && go test ./internal/apiserver -run EthUsd -v && go build ./...`
  - Expected: PASS.
- [ ] **Step 9: Commit** `feat(api): serve ETH/USD reference price from CoinGecko`

### Task 3: Migration 00015 and sqlc queries

**Files:**
- Create: `backend/internal/store/postgres/migrations/00015_market_discovery.sql`
- Modify:
  - `backend/internal/store/postgres/queries/read.sql`
  - `backend/internal/store/postgres/queries/projections.sql`
- Create: `backend/internal/store/postgres/queries/watchlist.sql`
- Generated: `backend/internal/store/postgres/sqlc/*` via `sqlc generate`
- Test: the existing migrations up/down/up test in `backend/internal/store/postgres/migrations`.

**Interfaces:**
- Produces (sqlc query names, used by Tasks 4–7):
  - Read queries:
    - `ListTokenCardsNewest|Oldest|MarketCap|Volume` gain `sqlc.narg(launched_after)::timestamptz`:
      `AND (sqlc.narg(launched_after)::timestamptz IS NULL OR t.launch_block_time >= sqlc.narg(launched_after))`.
    - `ListTrendingTokens(chain_id, window_start, limit)` → rows: `token_address`, summary columns
      as in `ListTokenCardsNewest`, `window_volume_eth_wad NUMERIC`, `distinct_buyers BIGINT`,
      `last_trade_time TIMESTAMPTZ`.
    - `GetKingOfTheHill(chain_id)` → `:one`: summary columns, `graduation_progress_bps`,
      `remaining_eth_wad = graduation_eth − real_curve_eth`. Reuse the `real_curve_eth` and
      `graduation_progress_bps` expressions of `GetTokenDetail`. Filter `phase = 'curve'`, order
      by progress DESC, `launch_block_time` DESC, `token_address`; `LIMIT 1`.
  - Projections:
    - `MarkTokensDirtyInWindow(chain_id, window_from, window_to) :execrows` inserts into
      `aggregation_dirty` with the same `nextval` / `ON CONFLICT` form as `MarkTokenDirty`, for
      `DISTINCT token_address` from:
      - canonical `trades` with `block_time >= from AND < to`;
      - canonical `pool_swaps` joined to `tokens.lp_pair` in that range.
  - Watchlist:
    - `CountWatchlist(chain_id, deployment_id, privy_did) :one`
    - `LockWatchlistOwner(lock_key BIGINT) :exec` → `SELECT pg_advisory_xact_lock($1)`
    - `InsertWatchlistEntry(...) :execrows` — `ON CONFLICT DO NOTHING`
    - `DeleteWatchlistEntry(...) :execrows`
    - `ListWatchlist(chain_id, deployment_id, privy_did, limit)` — `LEFT JOIN tokens` and
      `token_stats` with the summary columns; nullable when the token is gone; ordered by
      `created_at DESC, token_address`.
    - `TokenExists(chain_id, token_address) :one bool`

- [ ] **Step 1: Write the migration.**
  - Up:
    - `CREATE INDEX trades_chain_block_time_idx ON trades (chain_id, block_time);`
    - the same for `pool_swaps_chain_block_time_idx`;
    - the `watchlist_entries` table:
      - columns `chain_id BIGINT`, `deployment_id TEXT`, `privy_did TEXT`, `token_address BYTEA`
        (`octet_length = 20` check), `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`;
      - PK `(chain_id, deployment_id, privy_did, token_address)`;
      - index `(chain_id, deployment_id, privy_did, created_at DESC)`;
      - no FK.
  - Down: drop the table, then the indexes, with the comment
    `-- Down deletes every user's watchlist entries.`
- [ ] **Step 2: Write the trending query.** It takes two branches, each joined to `indexed_blocks`
  on `(chain_id, block_number)` exactly as `market_trades` does, and filtered on `block_time >=
  window_start`:
  - Curve: `trades` gives `eth_gross` volume; buyer = `trader` when `is_buy`.
  - DEX: `pool_swaps` joined to `tokens` on `lp_pair`. ETH volume is the WETH-side `in + out`,
    chosen by `token_is_token0` as in the `market_trades` legs, but without the `pool_syncs`
    lateral. A buy is token-side `out > 0`; buyer = `to_address`.

  Combine them:
  - `GROUP BY token_address`;
  - `score = sum(volume) * count(DISTINCT buyer)`;
  - `HAVING sum(volume) > 0`;
  - `ORDER BY score DESC, max(block_time) DESC, token_address`;
  - `LIMIT`.

  Join `tokens` and `token_stats` for the summary columns.
- [ ] **Step 3: Add the remaining queries listed under Interfaces.** Add `launched_after` to the
  four list queries.
- [ ] **Step 4: Regenerate and check.**
  - Run: `cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate && go build ./... && go run github.com/go-task/task/v3/cmd/task@v3.53.1 sqlc-diff`
  - Expected: build OK, no diff. Fix compile errors in `read_adapter.go` by passing a NULL
    `launched_after` for now.
- [ ] **Step 5: Run the migration test.**
  - Run: `cd backend && go test -tags=integration ./internal/store/postgres/migrations/... -race`
  - Expected: PASS (up/down/up including 00015).
- [ ] **Step 6: Commit** `feat(store): add market discovery migration and queries`

### Task 4: Age filter on `GET /v1/tokens`

**Files:**
- Modify:
  - `backend/internal/pagination/cursor.go`
  - `backend/internal/token/token.go` (the file holding `ListQuery`)
  - `backend/internal/store/postgres/read_adapter.go`
  - `backend/internal/apiserver/token_routes.go`
- Test:
  - `backend/internal/pagination/cursor_test.go`
  - `backend/internal/apiserver/token_routes_test.go`
  - `backend/internal/store/postgres/read_integration_test.go` (or the existing token-list
    integration test file)

**Interfaces:**
- Consumes: the `launched_after` param (Task 3).
- Produces:
  - `pagination.Cursor.Since string \`json:"since,omitempty"\`` (RFC 3339, UTC, second precision).
  - `token.ListQuery.Age string` (`"all"|"24h"|"7d"`) and `token.ListQuery.Now time.Time`.
  - `token.Page` is unchanged.

- [ ] **Step 1: Write failing tests.**
  - `cursor_test.go`: `TestCursorSinceRoundTrip`; an old cursor without `since` still decodes.
  - `token_routes_test.go`:
    - `TestTokenListRejectsInvalidAge` (`age=1h` → 400, type `urn:launchpad:problem:invalid_age`);
    - `TestTokenListPassesAge` (stub reader receives `Age:"24h"`).
- [ ] **Step 2: Write the failing integration test.** `TestTokenListAgeCursorStable`:
  - Seed 3 tokens launched 1 h, 2 h and 30 h ago. With `age=24h&limit=1`, page 1 and page 2 return
    the 2 recent ones.
  - Insert a new token launched now between page requests; page 2 does not include it and still
    ends.
  - Reusing the `age=24h` cursor with `age=7d` returns the existing invalid-cursor problem.
- [ ] **Step 3: Run tests to confirm they fail.**
  - Run: `cd backend && go test ./internal/pagination ./internal/apiserver -run 'Age|Since' -v`
  - Expected: FAIL.
- [ ] **Step 4: Implement.**
  - The route validates `age` (default `all`).
  - The adapter adds `age` to `cursorFilter(...)`.
  - First page: `since = Now − 24h|7d`, truncated to the second. Later pages use `cursor.Since`.
  - `all` passes NULL and leaves `Since` empty.
- [ ] **Step 5: Run unit and integration tests.**
  - Run: `cd backend && go test ./... -race && go test -tags=integration ./internal/store/postgres/... -run Age -race`
  - Expected: PASS.
- [ ] **Step 6: Commit** `feat(api): add age filter to token list`

### Task 5: `GET /v1/tokens/highlights`

**Files:**
- Create:
  - `backend/internal/token/highlights.go`
  - `backend/internal/store/postgres/highlights_reader.go`
  - `backend/internal/apiserver/highlights_routes.go`
- Modify: `backend/cmd/api/main.go`
- Test:
  - `backend/internal/apiserver/highlights_routes_test.go`
  - `backend/internal/store/postgres/highlights_integration_test.go`

**Interfaces:**
- Consumes: `ListTrendingTokens`, `GetKingOfTheHill` (Task 3).
- Produces:
  ```go
  type TrendingToken struct { Summary; WindowVolumeETH *big.Int; DistinctBuyers int64 }
  type KingOfTheHill struct { Summary; GraduationProgressBPS int32; RemainingETH *big.Int }
  type Highlights struct {
      Trending []TrendingToken; King *KingOfTheHill
      WindowStart time.Time; Snapshot pagination.Snapshot; Finality string
  }
  type HighlightsReader interface {
      Highlights(ctx context.Context, chainID int64, windowStart time.Time, limit int) (Highlights, error)
  }
  ```
  - `storepostgres.HighlightsReader{Pool, DeploymentID}`.
  - Routes: `apiserver.HighlightsRoutes{Reader token.HighlightsReader; ChainID int64; Now func()
    time.Time; TTL time.Duration /*15s*/}`. Register `GET /v1/tokens/highlights` (operation id
    `getTokenHighlights`) **before** `GET /v1/tokens/{address}` so it is not parsed as an address.
  - Response: `{trending:[tokenDTO + window_volume_eth, distinct_buyers], king_of_the_hill: dto|null,
    window_start, as_of, finality}`.

- [ ] **Step 1: Write the failing integration test.** `TestHighlightsTrending`:
  - Token A: 1 buyer, 10 ETH. Token B: 3 buyers, 4 ETH. Token C: a trade 61 min old only. Token D:
    a trade in a block that is then reorged (remove it from `indexed_blocks` / replace its hash the
    way existing reorg tests do).
  - Expected order: B (12), A (10). C and D are absent.
  - A tie on score orders by the latest trade.
  - Include one DEX buy via `pool_swaps` whose `to_address` adds a distinct buyer.
- [ ] **Step 2: Write the failing KOTH integration test.** `TestHighlightsKingOfTheHill`: the
  curve token with the highest progress wins; graduated tokens are excluded; no curve token →
  `King == nil`.
- [ ] **Step 3: Write failing route tests.**
  - `TestHighlightsCached`: two calls within 15 s → the reader is called once; after advancing
    `Now` 16 s → called twice.
  - `TestHighlightsRouteNotShadowed`: `/v1/tokens/highlights` does not return the invalid-address
    problem.
  - `TestHighlightsReaderErrorMapped`: uses `mapReadError`.
- [ ] **Step 4: Run tests to confirm they fail.**
  - Run: `cd backend && go test ./internal/apiserver -run Highlights -v`
  - Expected: FAIL.
- [ ] **Step 5: Implement the reader, route and cache.** The cache is a mutex-guarded single entry;
  failed reads are not cached. Wire it in `cmd/api/main.go`.
- [ ] **Step 6: Run unit and integration tests.**
  - Run: `cd backend && go test ./internal/apiserver -race && go test -tags=integration ./internal/store/postgres/... -run Highlights -race`
  - Expected: PASS.
- [ ] **Step 7: Commit** `feat(api): add trending and king of the hill highlights`

### Task 6: 24 h volume decay sweep

**Files:**
- Create: `backend/internal/stats/window_sweeper.go`
- Modify:
  - `backend/internal/store/postgres/` (the type implementing `stats.DirtySource`; add the method
    there)
  - `backend/cmd/indexer/main.go`
- Test:
  - `backend/internal/stats/window_sweeper_test.go`
  - `backend/internal/store/postgres/stats_decay_integration_test.go`

**Interfaces:**
- Consumes: `MarkTokensDirtyInWindow` (Task 3).
- Produces:
  ```go
  type ExpiringWindowMarker interface {
      MarkExpiringWindow(ctx context.Context, chainID int64, from, to time.Time) (int64, error)
  }
  type WindowSweeper struct {
      Source ExpiringWindowMarker; ChainID int64
      Interval time.Duration // 60s
      Window time.Duration   // 24h
      Overlap time.Duration  // 2m
      Now func() time.Time; Wake func(); OnError func(error)
  }
  func (s WindowSweeper) Run(ctx context.Context) error
  ```
  - `Wake` is the stats worker's existing wake hook. It is called when `marked > 0`.

- [ ] **Step 1: Write failing unit tests.**
  - `TestSweeperWindowBounds`: with fixed `Now = T`, the fake receives `from = T−24h−2m`,
    `to = T−24h`.
  - `TestSweeperWakesOnlyWhenMarked`.
  - `TestSweeperErrorContinues`: an error calls `OnError` and the next tick still runs.
- [ ] **Step 2: Write the failing integration test.** `TestVolume24hDecaysWithoutNewTrades`:
  - Seed a trade 23 h 59 m ago and run the aggregation worker once; `volume_24h > 0`.
  - Move the clock so the trade is 24 h 1 m old and run sweep + worker; `volume_24h_eth_wad == 0`,
    and `tokens.sort_volume_24h_eth_wad == 0` via the 00014 trigger.
  - `ComputeTokenStats` takes `now`. If it reads wall-clock time, pass it through the existing
    calculator parameter rather than changing semantics.
- [ ] **Step 3: Run tests to confirm they fail.**
  - Run: `cd backend && go test ./internal/stats -run Sweeper -v`
  - Expected: FAIL.
- [ ] **Step 4: Implement** the sweeper and the store method. Start it in `cmd/indexer/main.go`
  next to the stats worker, sharing its context and wake channel.
- [ ] **Step 5: Run unit and integration tests.**
  - Run: `cd backend && go test ./internal/stats ./cmd/indexer -race && go test -tags=integration ./internal/store/postgres/... -run Decay -race`
  - Expected: PASS.
- [ ] **Step 6: Commit** `fix(stats): decay 24h volume for tokens that stop trading`

### Task 7: Watchlist store and `/v1/me/watchlist` routes

**Files:**
- Create:
  - `backend/internal/watchlist/watchlist.go`
  - `backend/internal/store/postgres/watchlist_store.go`
  - `backend/internal/apiserver/watchlist_routes.go`
- Modify: `backend/cmd/api/main.go`
- Test:
  - `backend/internal/apiserver/watchlist_routes_test.go`
  - `backend/internal/store/postgres/watchlist_integration_test.go`

**Interfaces:**
- Consumes: the watchlist queries (Task 3); the `privyauth` verifier (`ParseBearer`, `Verify` →
  `Principal.PrivyDID`).
- Produces:
  ```go
  package watchlist
  const MaxEntries = 200
  var ErrFull = errors.New("watchlist full")
  var ErrUnknownToken = errors.New("unknown token")
  type Owner struct { ChainID int64; DeploymentID, PrivyDID string }
  type Entry struct { Token common.Address; AddedAt time.Time; Summary *token.Summary } // nil = unavailable
  type Store interface {
      Add(ctx context.Context, o Owner, t common.Address) error // idempotent
      Remove(ctx context.Context, o Owner, t common.Address) error // idempotent
      List(ctx context.Context, o Owner) ([]Entry, error)
  }
  ```
  - `storepostgres.WatchlistStore{Pool}`. `Add` runs in one transaction:
    1. `pg_advisory_xact_lock(hashtextextended(chain||deployment||did, 0))`.
    2. Check `TokenExists`; if not, `ErrUnknownToken`.
    3. Insert `ON CONFLICT DO NOTHING`. Zero rows means already present → nil.
    4. Otherwise, if the count is now > `MaxEntries`, roll back with `ErrFull`.
  - `apiserver.WatchlistRoutes{Verifier, Store watchlist.Store, ChainID int64, DeploymentID string}`.
  - Endpoints:
    - `GET /v1/me/watchlist` (`listWatchlist`)
    - `PUT /v1/me/watchlist/{address}` (`addWatchlistEntry`) → 204
    - `DELETE /v1/me/watchlist/{address}` (`removeWatchlistEntry`) → 204
  - Auth headers follow `profileInput`.
  - Errors:
    - 401 `authentication_failed`
    - 400 `invalid_address` (reuse the token-route address parser)
    - 404 `token_not_found`
    - 409 `watchlist_full`
  - Every response, including errors, sets `Cache-Control: private, no-store`.

- [ ] **Step 1: Write failing route tests** with a stub verifier and store:
  - `TestWatchlistRequiresAuth` (missing header → 401);
  - `TestWatchlistPutIdempotent`;
  - `TestWatchlistDeleteIdempotent`;
  - `TestWatchlistFullReturns409`;
  - `TestWatchlistUnknownToken404`;
  - `TestWatchlistNoStoreHeader`;
  - `TestWatchlistScopesByDID` (the store receives the verified DID, never a client-supplied one);
  - `TestWatchlistLogsOmitDID` (capture the server logger and assert the DID string is absent).
- [ ] **Step 2: Write failing integration tests.**
  - `TestWatchlistAddListRemove`;
  - `TestWatchlistLimitConcurrent`: seed 199 entries, then 5 parallel `Add`s of distinct tokens →
    exactly 1 succeeds, 4 return `ErrFull`, the count is 200;
  - `TestWatchlistUnavailableToken`: delete the token row → `List` returns the entry with
    `Summary == nil`;
  - `TestWatchlistIsolatedByDeployment`.
- [ ] **Step 3: Run tests to confirm they fail.**
  - Run: `cd backend && go test ./internal/apiserver -run Watchlist -v`
  - Expected: FAIL.
- [ ] **Step 4: Implement** the package, store and routes, and wire them in `cmd/api/main.go`.
- [ ] **Step 5: Run unit and integration tests.**
  - Run: `cd backend && go test ./internal/apiserver -race && go test -tags=integration ./internal/store/postgres/... -run Watchlist -race`
  - Expected: PASS.
- [ ] **Step 6: Commit** `feat(api): add per-user watchlist`

### Task 8: OpenAPI, web client and manifest `marketLinks`

**Files:**
- Generated:
  - `backend/openapi/v1.json` (via `go run ./cmd/check-openapi --write`)
  - `web/src/api/generated.ts` (via `npm run web-api-sync`)
  - `web/src/contracts/generated*` (via `npm run web-contracts-sync`)
- Modify:
  - `contracts/deployments/deployment.schema.json` and its byte-identical copy
    `backend/deployments/testdata/deployment.schema.json`
  - `web/scripts/generate-contracts.mjs`
  - `web/src/api/client.ts`
  - `web/src/config/public.ts` (fixture deployment gets `marketLinks: null`)
- Test:
  - `web/src/api/client.test.ts`
  - `web/src/config/public.test.ts`
  - the `backend/deployments` tests

**Interfaces:**
- Schema: optional `marketLinks: { dexscreenerChain?: string /*^[a-z0-9-]{1,32}$*/,
  geckoterminalNetwork?: string /*same pattern*/ }`, `additionalProperties:false`.
- Generator: emits `marketLinks: document.marketLinks ?? null` on `ReviewedDeployment`.
- Client methods (all take `signal?: AbortSignal`):
  - `getEthUsd(baseUrl)` → `EthUsdReference`
  - `getHighlights(baseUrl)` → `TokenHighlights`
  - `TokenListQuery.age?: "all"|"24h"|"7d"`
  - `getWatchlist(baseUrl, auth)`
  - `addWatchlist(baseUrl, address, auth)`
  - `removeWatchlist(baseUrl, address, auth)`

- [ ] **Step 1: Write failing tests.**
  - `client.test.ts`:
    - `getTokens` sends `age=24h`;
    - `addWatchlist` sends `PUT` with both auth headers;
    - 409 surfaces problem code `watchlist_full`.
  - `backend/deployments` test: a manifest with an unknown `marketLinks` key is rejected; one
    without `marketLinks` is accepted.
- [ ] **Step 2: Run tests to confirm they fail.**
  - Run: `cd web && npx vitest run src/api/client.test.ts`
  - Expected: FAIL.
- [ ] **Step 3: Regenerate and implement.** Regenerate OpenAPI, then run `web-api-sync`. Edit the
  schema in both places, the generator, `web-contracts-sync` and the client methods.
- [ ] **Step 4: Run the drift checks and tests.**
  - Run: `cd backend && go run ./cmd/check-openapi && go run ./cmd/check-deployments && go test ./deployments/...`
  - Run: `cd web && npm run typecheck && npm test`
  - Expected: all pass, with no drift.
  - On Windows, a CRLF-only drift report is a known false positive: confirm with `git diff --stat`
    before acting.
- [ ] **Step 5: Commit** `feat: expose discovery endpoints to web and add market links schema`

### Task 9: Web USD display

**Files:**
- Create:
  - `web/src/market/eth-usd.ts` (`useEthUsd` hook)
  - `web/src/market/usd-line.tsx` (`<UsdLine wei={…} />`, `<CoinGeckoAttribution />`)
- Modify:
  - `web/src/amounts.ts`
  - `web/src/discovery/token-discovery.tsx` (cards)
  - `web/src/token/token-detail.tsx` (metrics)
  - `web/src/transactions-panel.tsx` (inputs, quote)
  - the trade history rows in `token-detail.tsx`
  - `web/src/profile/profile-view.tsx` (claims)
  - `web/src/components/app-shell.tsx` (footer attribution)
- Test:
  - `web/src/amounts.test.ts`
  - `web/src/market/eth-usd.test.ts`

**Interfaces:**
- Consumes: `getEthUsd` (Task 8).
- Produces:
  - `formatUsd(baseUnits: bigint, decimals: number, priceUsd: string): string`
    - Exact bigint multiply.
    - Display rule:
      - ≥ $1 → 2 decimals with thousands separators;
      - < $1 → 4 significant digits;
      - 0 → `"$0.00"`.
    - Never exponent notation.
  - `useEthUsd(): { price: string | null; attributionUrl: string | null }`
    - Polls every 60 s and pauses on `document.visibilityState === "hidden"`.
    - On error, returns `price: null`.

- [ ] **Step 1: Write failing tests.**
  - `amounts.test.ts`:
    - `formatUsd(10n**18n, 18, "3456.12") === "$3,456.12"`;
    - `formatUsd(1n, 18, "3456.12") === "$0.000000000000003456"`;
    - `formatUsd(10n**27n, 18, "3456.123456789012345678")` gives the exact integer part
      `"$3,456,123,456,789.01"`;
    - a truncation case rounds half-up.
  - `eth-usd.test.ts`:
    - returns null when the API returns `price:null`;
    - does not poll while hidden (fake timers plus a `visibilitychange` event);
    - `UsdLine` renders nothing when the price is null.
- [ ] **Step 2: Run tests to confirm they fail.**
  - Run: `cd web && npx vitest run src/amounts.test.ts src/market`
  - Expected: FAIL.
- [ ] **Step 3: Implement** `formatUsd`, the hook (one shared fetch per page via a module-level
  subscriber set) and `UsdLine`. Place `UsdLine` under each ETH figure on the listed surfaces. Put
  `CoinGeckoAttribution` in the footer and in the trade panel only when the price is non-null.
- [ ] **Step 4: Run tests, lint, typecheck and build.**
  - Run: `cd web && npm test && npm run lint && npm run typecheck && npm run build`
  - Expected: all pass.
- [ ] **Step 5: Check in the browser.** Run the local stack with the source disabled: no `$`
  appears anywhere. Then serve a stub price: USD lines and the attribution appear. Use the built-in
  browser at both widths (mobile 375 px, desktop).
- [ ] **Step 6: Commit** `feat(web): show USD values from the ETH/USD reference`

### Task 10: Explore tabs, king of the hill, age filter, watchlist star

**Files:**
- Create:
  - `web/src/discovery/highlights.tsx` (Trending list plus the King of the hill card)
  - `web/src/watchlist/use-watchlist.ts`
  - `web/src/watchlist/star-button.tsx`
- Modify:
  - `web/src/discovery/query-state.ts`
  - `web/src/discovery/explore-hub.tsx`
  - `web/src/discovery/token-discovery.tsx`
  - `web/src/token/token-detail.tsx` (star in the header)
- Test:
  - `web/src/discovery/query-state.test.ts`
  - `web/src/watchlist/use-watchlist.test.ts`

**Interfaces:**
- Consumes: `getHighlights`, `getTokens({age})`, `getWatchlist` / `addWatchlist` / `removeWatchlist`
  (Task 8); `usePrivy`, `useIdentityToken` as in `profile-view.tsx`.
- Produces:
  - Query state: `ExploreView = "trending"|"new"|"marketcap"|"volume"|"watchlist"` (URL param
    `view`, default `trending`); `TokenListQueryState.age: "all"|"24h"|"7d"` (URL param `age`,
    default omitted).
    - `new` → sort `newest`; `marketcap` / `volume` → the existing sorts.
    - Unknown values decode to the defaults.
  - Watchlist hook: `useWatchlist(): { has(addr): boolean; toggle(addr): Promise<void>; full: boolean;
    status }`.
    - Optimistic update; on failure it rolls back and shows a `Toast`.
    - 409 → toast "Watchlist full (200)".
    - 401, or signed out → Privy `login()`.
- `StarButton` renders on every token card (both Explore sections and the Watchlist tab) and in
  the token detail header.
- Layout: the Graduated section stays as it is. The curve "Explore" section gets the view tabs and
  the age control. Trending shows `highlights.tsx`; on error it shows the existing error state
  with a "Show new launches" button that switches to `new`. King of the hill sits above the tabs
  with progress % and remaining ETH (and USD). The Watchlist tab shows the user's entries, marks
  unavailable ones "Unavailable", and prompts sign-in when signed out.

- [ ] **Step 1: Write failing tests.**
  - `query-state.test.ts`:
    - `?view=volume&age=7d` round-trips;
    - `?view=bogus` → `trending`;
    - `age` is omitted from the URL when `all`.
  - `use-watchlist.test.ts`:
    - optimistic add, then server 500 → rollback plus an error toast;
    - 409 → "Watchlist full (200)" and rollback;
    - signed out → `login` called and no request.
- [ ] **Step 2: Run tests to confirm they fail.**
  - Run: `cd web && npx vitest run src/discovery src/watchlist`
  - Expected: FAIL.
- [ ] **Step 3: Implement** the query state, hook, star, highlights component and Explore wiring.
- [ ] **Step 4: Run tests, lint, typecheck and build.**
  - Run: `cd web && npm test && npm run lint && npm run typecheck && npm run build`
  - Expected: all pass.
- [ ] **Step 5: Check in the browser.** With the local stack, the tabs switch and the URL updates;
  the age filter narrows the list; a failed highlights request shows the fallback. Check at 375 px
  and desktop; the existing `e2e/accessibility.spec.ts` still passes.
- [ ] **Step 6: Commit** `feat(web): add trending, king of the hill, age filter and watchlist`

### Task 11: Dexscreener and GeckoTerminal links

**Files:**
- Create: `web/src/token/market-links.ts`
- Modify: `web/src/token/token-detail.tsx`
- Test: `web/src/token/market-links.test.ts`

**Interfaces:**
- Consumes: `deployment.marketLinks` (Task 8), the token's `lp_pair`.
- Produces: `marketLinks(deployment, pair: string | null, phase: string): { dexscreener?: string;
  geckoterminal?: string }`.
  - Dexscreener URL: `https://dexscreener.com/${dexscreenerChain}/${pair.toLowerCase()}`.
  - GeckoTerminal URL:
    `https://www.geckoterminal.com/${geckoterminalNetwork}/pools/${pair.toLowerCase()}`.
  - Both are empty unless `phase === "graduated"`, the pair is a valid address, and the identifier
    is present.

- [ ] **Step 1: Write failing tests:**
  - both identifiers → both URLs;
  - one missing → only the other;
  - curve phase → none;
  - malformed pair → none.
- [ ] **Step 2: Run tests to confirm they fail.**
  - Run: `cd web && npx vitest run src/token/market-links.test.ts`
  - Expected: FAIL.
- [ ] **Step 3: Implement**, and render the links with the existing `SafeExternalLink` beside the
  explorer link.
- [ ] **Step 4: Run tests.**
  - Run: `cd web && npm test && npm run typecheck`
  - Expected: PASS.
- [ ] **Step 5: Commit** `feat(web): link graduated tokens to Dexscreener and GeckoTerminal`

### Task 12: Anvil browser gate and full verification

**Files:**
- Modify: `web/e2e/task6-anvil.spec.ts`
- Modify: `backlog.md` — add an Active item for the external inputs: the CoinGecko key and the
  verified Dexscreener/GeckoTerminal identifiers.

- [ ] **Step 1: Extend the Anvil spec.** After the existing buy, Explore → Trending lists the traded
  token. Signed in with the fixture account, star it, open the Watchlist tab and see it; unstar it
  and see it disappear.
- [ ] **Step 2: Run the Anvil gate** from PowerShell, not Git Bash (Git Bash background runs kill
  the API/indexer). Prepend `C:\Users\mesut\.foundry\bin` to `PATH`.
  - Run: `cd web; npm run test:anvil`
  - Expected: all tests pass, including the new ones.
- [ ] **Step 3: Run the full gates.**
  - Run: `cd backend && go run github.com/go-task/task/v3/cmd/task@v3.53.1 verify`
  - Run: `cd web && npm run lint && npm run typecheck && npm test && npm run build`
  - Run: `cd contracts && forge test`
  - Expected: all green. Any drift report must be checked against `git diff` for CRLF-only
    differences before treating it as real.
- [ ] **Step 4: Update `backlog.md`** with the external-inputs item.
- [ ] **Step 5: Commit** `test(web): cover trending and watchlist in the Anvil gate`
