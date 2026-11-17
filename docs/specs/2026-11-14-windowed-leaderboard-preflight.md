# Windowed leaderboard precompute — pre-flight 2026-11-14

## Starting point

At production-like volume (`docs/specs/2026-11-14-production-volume-measurement.md`: 50k wallets,
200k realized legs over 30 days) the 7-day and 30-day leaderboards take 260–295 ms per request,
over the 250 ms p95 read target (`2026-11-14-performance-targets.md`). `ListLeaderboardWindow`
sums every `wallet_realized_legs` row inside the window on every request; the window holds most of
the table, so no index helps (a covering index on `(chain_id, block_time)` still reads 200k rows and
aggregates 39k wallets: ~150 ms before the exclusions). The cost grows with trading volume.

The all-time leaderboard was fixed separately in this branch: `tokens.locker` had no index, so the
planner hashed every token and wallet instead of walking `wallet_totals_leaderboard_idx`
(67 ms → 1.9 ms, migration 00031).

## Decisions

1. **Precompute the ranked windows; reads become a page lookup.** A new table holds the top 100
   (the API's page-size ceiling, `checkWalletLimit`) wallets per chain and window, already
   ranked and with every exclusion applied. A second table records when each window was last
   refreshed. The read path joins usernames at read time, since names change independently.
   *Alternatives:* an in-process TTL cache in the API (rejected by the product owner: a miss still
   costs the full aggregation, which keeps growing); a daily rollup table (cannot sum
   `count(DISTINCT token)` across days, and still aggregates per request).

2. **Who refreshes.** A `stats.LeaderboardRefresher` loop in `cmd/indexer`, next to the volume
   window sweeper, on the shared pool. Every 30 s it rebuilds both windows, each in one
   transaction behind a per-chain advisory lock: delete the window's rows, insert the new ranking,
   upsert the refresh time from the database clock. A failed refresh is logged and retried next
   tick; it never stops ingest.
   *Rationale:* legs are written by the position worker and the window moves with wall-clock time
   even without new blocks, so a timer is the only trigger that covers both; reorged pairs are
   recomputed by the position worker and reach the ranking on the next tick.

3. **Freshness and fallback.** A read uses the precomputed ranking only when its refresh is at
   most 5 minutes old; otherwise (fresh deployment, refresher down) it runs the existing live
   query, so a stuck refresher costs latency, never wrong or empty results.
   *Rationale:* the all-time leaderboard already reads `wallet_totals`, which the position worker
   writes asynchronously, so leaderboards were never exact at the response's snapshot stamp. A
   ranking up to ~30 s old (one tick) is within the same contract; the stamp stays the read
   snapshot's.

4. **Single source of the ranking logic.** The refresh inserts the output of the same CTE and
   exclusions as `ListLeaderboardWindow`, which stays as the fallback. An integration test asserts
   the precomputed page equals the live query on a fixture with every exclusion kind.

## Tests

- Integration: refresh then read equals the live query (both windows, page sizes 1, 3 and 100,
  curve, pair, locker, pool-manager and zero-address exclusions); a stale refresh and a missing
  refresh both fall back to the live query; a second refresh replaces the first.
- Unit: the refresher refreshes on start and on every tick, reports errors without stopping, and
  stops on context cancellation.
- Measurement: EXPLAIN and loadprobe at volume for `/leaderboard?window=7d` and `window=30d`.
