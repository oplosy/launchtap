# Pre-flight: protocol revenue rollup and audit performance fixes (AUDIT_REPORT_TR HIGH-13/14)

Date: 2026-11-16. Validated with `EXPLAIN (ANALYZE)` on a seeded Postgres 18.6 (see `backlog.md`).

## Locked decisions

- **Revenue is read from a daily rollup.** `GET /v1/stats/protocol/revenue` summed every trade,
  graduation and LP fee collection of the chain on each public request. `protocol_daily_revenue`
  (`chain_id, day, engine_version`) now holds the same sums per UTC day; the stats worker rebuilds
  it in `refreshProtocolAggregates` next to `protocol_daily`, from the same `from_day`, under the
  same advisory lock and watermark. Reorg recovery's full rebuild covers it. Migration 00036
  backfills the whole history, so the endpoint is complete before the first refresh.
  - Rationale: it reuses the incremental pattern of `protocol_daily` (one rebuild point, one
    watermark) instead of adding a cache with its own invalidation.
  - Trade-off: revenue now lags the indexed head by the stats worker's refresh interval, like
    the all-time protocol stats. The response's snapshot block is the read snapshot, not the
    rollup watermark.
  - Alternatives rejected: an HTTP `Cache-Control` header alone (every cache miss still scans
    all trades); an in-process TTL cache (per replica, still scans on expiry).
- **ATH uses a partial index** `candles_1m_ath_idx (chain_id, token_address, high_price_wad DESC,
  bucket_start_time) WHERE interval = '1m'` (migration 00037). Measured: 59 ms → 0.07 ms per token
  at 50,000 candles.
- **Realized legs are written in one statement per position** (`InsertWalletRealizedLegs`,
  aligned arrays joined `WITH ORDINALITY`, the `blocks.sql` pattern), checked against the row
  count. The leg list is still recomputed from the full history per position; making that
  incremental is a separate change.

## Not changed (low priority, recorded in `backlog.md`)

Reorg-by-block reads on `transfers` (Postgres 18 skip scan, linear in token count) and the small
engine-2 fee tables without secondary indexes.

## Tests

`TestProtocolRevenueSumsEachSourcePerEngine` now checks the incremental refresh, the full rebuild
and the migration backfill give the same sums. `TestRealizedLegsMatchLedger` covers the batched
insert. Migration chain tests cover 00036 and 00037 up, down and up.
