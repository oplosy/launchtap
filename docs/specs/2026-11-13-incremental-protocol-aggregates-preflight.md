# Incremental protocol aggregates — pre-flight

Date: 2026-11-13. Status: locked.

## Problem

Every stats-worker batch (`AggregationSource.ComputeBatch`) called
`RecomputeProtocolAggregates`, which deletes every `protocol_daily` row of the chain, re-aggregates
the whole `market_trades`/`token_launches`/`graduations` history, and computes the all-time
`protocol_stats` columns with full-history scans. Cost grows with total history and is paid on
every batch, including the periodic 24h-window re-marks when nothing new was indexed.

## Locked decisions

1. **Block watermark on `protocol_stats`.** New nullable column `daily_through_block`: the highest
   `indexed_blocks.block_number` whose rows `protocol_daily` already reflects. `NULL` means
   "unknown" and forces a full rebuild (first run after the migration, fresh chains).
   *Rationale:* the indexer commits a block and its events in one transaction and commits blocks
   in ascending order, so "blocks above the watermark" is exactly the set of rows that can be new
   outside a reorg. A time-based bound (`updated_at`) would race with transactions that started
   before the refresh and commit after it.
2. **Refresh only the affected UTC days.** The incremental path rebuilds `protocol_daily` for days
   `>= (min(block_time) of blocks above the watermark)` and leaves earlier days untouched. Each
   rebuilt day is recomputed from the source rows, so the step is idempotent. Using the minimum
   block day (not "today") keeps a catching-up indexer, whose new blocks lie on old days, correct.
3. **Reorg recovery stays a full rebuild** (`RecomputeProtocolAggregates`) and resets the
   watermark to the head visible inside the recovery transaction. Reorgs are rare; a full rebuild
   keeps the rollback logic trivially correct.
4. **All-time columns derive from `protocol_daily`.** `*_all_time` = sum of the daily rows, which
   is exactly the full-history sum once the daily rows are correct. The 24h columns keep reading
   the sources with a `block_time` range.
5. **Serialize refreshes per chain** with `pg_advisory_xact_lock` on
   `'protocol_aggregates:<chain_id>'`, taken before the watermark read. Without it, a worker that
   read the watermark before a reorg committed could write a watermark above the reorg point and
   skip the replacement blocks' days. The repository already uses transaction-scoped advisory locks
   (wallet totals, watchlist, social).
6. **Indexes** `(chain_id, block_time)` on `token_launches`, `graduations`, `v4_swaps` so the
   from-day and 24h scans are range scans on every `market_trades` branch (`trades` and
   `pool_swaps` have them since migration 00015).

## Alternatives rejected

- *Per-event deltas (add/subtract on insert/delete):* duplicates the view's volume rules in the
  router and makes reorg rollback a second code path.
- *Recompute "today and yesterday" only:* wrong while the indexer catches up on history.
- *Materialized view refresh:* still a full-history scan per refresh.

## Implementation pointers

`Adapter.RefreshProtocolAggregates` (incremental, used by the stats worker) and
`Adapter.RecomputeProtocolAggregates` (full, used by reorg recovery) in
`backend/internal/store/postgres/adapter.go`; queries in `queries/projections.sql`.
