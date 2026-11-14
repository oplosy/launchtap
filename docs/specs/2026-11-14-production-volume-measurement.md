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
