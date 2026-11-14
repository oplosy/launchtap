# Production-volume read measurement — 2026-11-14

Follow-up to `2026-11-14-performance-targets.md`, whose baseline ran on a database with a handful
of launches. This change measures the same API read target, p95 < 250 ms, at production-like
table sizes, and records a mobile LCP on a production web build.

## Decisions

**Synthetic history, not a replayed chain.** A real chain at this volume (hundreds of thousands
of trades) would take hours to produce on Anvil and would not be reproducible.
`backend/cmd/volumeseed` (pre-flight below) writes the event ledger directly with COPY and then
derives every projection the way the indexer does:

- `rebuild_token_projections`, the SQL function a re-index uses, builds tokens, reserves, holders
  and candles.
- The indexer's own `stats.Worker` drains `aggregation_dirty` (token stats, protocol aggregates)
  and `position_dirty` (wallet positions, realized legs).

So the read path sees the same table shapes, indexes and row counts it would after indexing that
history. The ledger is engine-1 only. Engine-2 (V4) reads use the same projection tables, apart
from V4 swap legs, which are not exercised.

**Volume.** The default volume is sized as a busy launchpad's first months, not its first day:

- 10,000 launches;
- 50,000 trading wallets;
- 500,000 buys, about 40 % of them later sold, which comes to about 700,000 curve trades and pool
  swaps over 30 days.

Activity is skewed on purpose. Token choice follows `u³` and wallet choice follows `u²`, so a few
tokens and wallets carry most of the trades, which is the case that stresses per-token and
per-wallet routes. About 3 % of tokens cross the graduation threshold and keep trading on their
pair; those dex trades go through the `market_trades` view's `pool_swaps` half. Every number is a
flag, and the run is reproducible from `-seed`.

**Ledger consistency.** The generated ledger must be one a chain could produce, or the
projections would measure impossible data. Unit tests replay it and fail on any of these:

- a negative holder balance;
- a curve trade after its token's graduation;
- a pool swap before graduation;
- a curve trade that breaks the constant product;
- a token whose holder balances do not sum to its total supply;
- a gap in the block sequence, or a block whose time goes backwards.

**Scratch databases only.** The tool refuses unless `-confirm-synthetic` is passed and the
target already has migrations applied but holds no `sync_state`, `indexed_blocks` or
`token_launches` row. It cannot overwrite or mix into indexed chain data.

**Where it runs.** On the local stack's components (Postgres 18.6 image pinned in
`deploy/compose.yaml`, release builds of `cmd/api`, a `next build` / `next start` web build), not
on a hosted test deployment. This answers how queries scale with data volume. It does not answer
network or multi-host latency; that part of the Performance "Path to 9" item stays open.

## Measurement

Run on 2026-11-14 on one Windows 11 workstation: Postgres 18.6 (pinned image), the API and the
load client shared the same CPU. `volumeseed` ran with its default flags; `ANALYZE` ran before
measuring.

### Dataset

| Table                |                   Rows |
| -------------------- | ---------------------: |
| blocks               |                710,529 |
| token launches       | 10,000 (150 graduated) |
| curve trades         |                561,517 |
| pool swaps           |                138,862 |
| holder_balances      |                499,059 |
| candles              |              1,960,436 |
| wallet_positions     |                488,909 |
| wallet_realized_legs |                200,379 |

Database size 2,061 MB. Seeding took about 20 minutes (ledger 2m5s, projections 1m19s, stats and
positions 16m30s). Probe targets: the hottest token (32k pool swaps), a quiet curve token and the
busiest wallet, all reported by `volumeseed`.

### Finding: token trade pages read the whole history

The first run put the hottest token's `/trades` at 283 ms for a single request, over the 250 ms
p95 target. `market_trades` is a `UNION ALL` of curve trades, V2 swaps and V4 swaps, and Postgres
cannot push the page's `ORDER BY ... LIMIT` into the branches, so every page appended and sorted
all 32k swaps (203 ms of the plan).

Fix (`Adapter.listMarketTrades`): a first query takes the page-size newest keys from each base
table's own index and returns the page-size-th newest key as a floor; the view read is then bounded
by `(block, tx, log) >= floor`, which each branch uses as an index condition. When the base tables
hold fewer rows than a page, or the bounded page comes back short because the view dropped a base
row (a swap without its Sync), the read falls back to the unbounded query, so pages stay exactly
the view's order. `TestMarketTradePagesMatchTheView` pages a mixed curve and pair history at eleven
page sizes, including unsynced swaps and two swaps in one transaction, against the full view.
After the fix the same request takes about 8 ms, page 2 as well.

### API latency (loadprobe, 16 workers, 30 s, after the fix)

| Route                        | Requests | Errors |      p50 |       p95 |       p99 |
| ---------------------------- | -------: | -----: | -------: | --------: | --------: |
| /tokens                      |     1155 |      0 | 19.35 ms |  39.44 ms |  52.94 ms |
| /tokens?sort=volume_24h      |     1155 |      0 | 19.52 ms |  40.19 ms |  51.74 ms |
| /tokens/highlights           |     1157 |      0 |  0.52 ms |   0.58 ms |   1.12 ms |
| /stats/protocol              |     1155 |      0 | 18.01 ms |  34.53 ms |  47.13 ms |
| /trades/recent               |     1152 |      0 | 21.62 ms |  39.48 ms |  52.64 ms |
| /leaderboard                 |     1152 |      0 | 95.04 ms | 121.46 ms | 157.71 ms |
| /tokens/{hot}                |     1151 |      0 | 18.74 ms |  38.30 ms |  51.53 ms |
| /tokens/{hot}/trades         |     1152 |      0 | 20.67 ms |  40.01 ms |  52.79 ms |
| /tokens/{hot}/candles        |     1153 |      0 | 19.04 ms |  38.97 ms |  49.62 ms |
| /tokens/{hot}/holders        |     1154 |      0 | 19.09 ms |  35.31 ms |  45.84 ms |
| /tokens/{quiet}/trades       |     1155 |      0 | 20.96 ms |  39.76 ms |  54.08 ms |
| /wallets/{busiest}           |     1154 |      0 | 29.97 ms |  49.20 ms |  62.32 ms |
| /wallets/{busiest}/positions |     1154 |      0 | 36.49 ms |  56.17 ms |  72.99 ms |
| /wallets/{busiest}/trades    |     1154 |      0 | 51.82 ms |  74.47 ms |  93.08 ms |

Every route is under the 250 ms p95 target with no errors at about 540 requests/s in total. The
slowest is `/leaderboard` at 121 ms p95, the first candidate for a cache if traffic grows.

### Web (mobile, cold cache)

Production build (`next build` + `next start`) against the API above, traced in Chrome with a
412×915 mobile viewport, 4× CPU slowdown and Slow 4G.

| Page                       |      LCP |   TTFB | Render delay |  CLS |
| -------------------------- | -------: | -----: | -----------: | ---: |
| Home                       | 1,859 ms | 564 ms |     1,295 ms | 0.06 |
| Token page (hottest token) | 1,878 ms | 564 ms |     1,315 ms | 0.00 |

Both are under the 2.5 s LCP target. Render-blocking CSS is the largest remaining LCP item
(about 330–440 ms estimated savings).

### Limitations

- Local, not hosted: one machine, no network between API, database and client. A hosted test
  deployment measurement is still open.
- The web build used the E2E fixture env (no Privy), so LCP is likely optimistic for the real
  wallet bundle.
- History is engine-1 (curve + V2) only; V4 swap volume is not represented.

## Second run: every metric against its target

Same dataset and machine, after the token trade page fix (#76) and the leaderboard work on
`task/leaderboard-query` (`2026-11-14-windowed-leaderboard-preflight.md`). Postgres ran with
`pg_stat_statements`; the 7d/30d leaderboards were re-ranked every 30 s during the run, as the
indexer's refresher does. Redis cache hit rate is not measured: the stack has no Redis.

| Metric                                | Target (good) | Critical | Measured                                                                                           | Result   |
| ------------------------------------- | ------------- | -------- | -------------------------------------------------------------------------------------------------- | -------- |
| LCP (mobile, Slow 4G, 4× CPU)         | ≤ 2.5 s       | > 4.0 s  | home 1,859 ms; token 1,878 ms; leaderboard 1,791 ms                                                | good     |
| INP (same profile)                    | ≤ 200 ms      | > 500 ms | leaderboard 114 ms; token page 20 ms (was 206 ms)                                                  | good     |
| CLS                                   | ≤ 0.10        | > 0.25   | home 0.06; token 0.00; leaderboard load and window switches 0.00 (was 0.90)                        | good     |
| TTFB, server only (median of 5, warm) | ≤ 800 ms      | > 1.8 s  | home 9 ms; token 12 ms (116 ms cold)                                                               | good     |
| TTFB under Slow 4G                    | ≤ 800 ms      | > 1.8 s  | 564 ms (almost all emulated latency)                                                               | good     |
| API p95 / p99                         | < 200 ms      | > 500 ms | worst route `/wallets/{w}/trades` 62.5 / 78.8 ms; `/leaderboard` 24.8 / 31.2 ms (was 121 / 158 ms) | good     |
| 5xx error rate                        | < 0.1 %       | > 1 %    | 0 errors in 53,966 requests                                                                        | good     |
| DB query time (mean per statement)    | ≤ 15 ms       | > 50 ms  | weighted mean 0.89 ms over 243k statements; one statement over target (below)                      | one over |

### API latency (loadprobe, 16 workers, 60 s)

| Route                        | Requests |      p50 |      p95 |      p99 |
| ---------------------------- | -------: | -------: | -------: | -------: |
| /tokens                      |     3372 | 13.30 ms | 21.49 ms | 26.02 ms |
| /tokens?sort=volume_24h      |     3372 | 13.21 ms | 21.74 ms | 26.85 ms |
| /tokens/highlights           |     3373 |  0.52 ms |  0.66 ms |  1.11 ms |
| /stats/protocol              |     3374 | 12.18 ms | 20.27 ms | 25.88 ms |
| /trades/recent               |     3373 | 15.92 ms | 25.34 ms | 31.65 ms |
| /leaderboard                 |     3373 | 15.81 ms | 24.82 ms | 31.17 ms |
| /leaderboard?window=7d       |     3374 | 13.88 ms | 22.68 ms | 28.76 ms |
| /leaderboard?window=30d      |     3374 | 13.83 ms | 22.99 ms | 29.72 ms |
| /tokens/{hot}                |     3373 | 12.90 ms | 21.58 ms | 28.81 ms |
| /tokens/{hot}/trades         |     3374 | 15.27 ms | 24.57 ms | 31.03 ms |
| /tokens/{hot}/candles        |     3375 | 13.59 ms | 22.74 ms | 28.33 ms |
| /tokens/{hot}/holders        |     3376 | 13.23 ms | 22.24 ms | 27.59 ms |
| /tokens/{quiet}/trades       |     3374 | 14.64 ms | 23.34 ms | 29.38 ms |
| /wallets/{busiest}           |     3375 | 24.28 ms | 33.92 ms | 41.21 ms |
| /wallets/{busiest}/positions |     3373 | 30.55 ms | 41.60 ms | 49.70 ms |
| /wallets/{busiest}/trades    |     3371 | 47.76 ms | 62.54 ms | 78.79 ms |

About 900 requests/s in total, 0 errors. The client, API and Postgres shared one CPU, so latency
includes CPU contention.

### Database statements over 1 ms mean (`pg_stat_statements`)

| Statement                           | Calls |     Mean |       Max |
| ----------------------------------- | ----: | -------: | --------: |
| `ListWalletTrades` (busiest wallet) |  3372 | 35.83 ms | 255.92 ms |
| `ListWalletOpenPositions`           |  3373 | 14.56 ms |  54.49 ms |
| `GetWalletValuation`                |  3375 |  9.17 ms |  45.98 ms |

Every other statement averages under 1 ms; the precomputed leaderboard read averages 0.13 ms and
the all-time leaderboard 0.84 ms. The 7d/30d refresh takes about 250 ms per window in the
indexer, off the request path. `ListWalletTrades` is the one statement over the 15 ms target and
the next item to fix.

### Fixes found by this run

- **Leaderboard, all time:** `tokens.locker` had no index, so the planner hashed every token and
  wallet instead of walking `wallet_totals_leaderboard_idx` (67 ms → 1.9 ms; migration 00031).
- **Leaderboard, 7d/30d:** 260–295 ms per request, now precomputed by the indexer (0.17 ms read).
- **Leaderboard window switch, CLS 0.90:** the `Suspense` boundary was keyed by window, so the
  table collapsed to the skeleton on every switch; the per-window heading wrapped to two lines on
  mobile; and rows keyed by wallet were moved to their new rank. Unkeyed boundary, one heading and
  rank-keyed rows bring every switch to 0.00.
- **Token chart type switch, INP 206 ms:** the switch rebuilt the chart before the click's next
  paint; as a transition it runs after it (20 ms).
