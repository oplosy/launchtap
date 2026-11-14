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

Results are recorded below once the run completes: table sizes, loadprobe per route, and Lighthouse
mobile LCP on the home and token pages.
