# Indexer dirty-mark de-duplication — pre-flight

Date: 2026-11-13. Status: locked.

## Problem

Every ingested event writes its ledger row, its projections, and one or more dirty marks
(`aggregation_dirty` for the stats worker, `position_dirty` for the positions worker), each a
separate round trip inside the chunk's commit transaction. A curve buy costs about nine sequential
statements, four of them marks: the Transfer marks the token and both transfer sides, the Trade marks
the token and the trader again. Busy tokens repeat the same marks hundreds of times per chunk.

## Locked decisions

1. **A mark already written in the current transaction is skipped.** Marks only bump a row's
   `generation`, and workers read them after the commit, so the second identical mark inside one
   transaction has no observable effect. The first mark still runs at its original position, so the
   statement order is unchanged for every mark that is not skipped.
2. **Scope: the indexer's unit-of-work transaction only.** `IndexerStore.Transaction` turns it on
   (`Adapter.WithDirtyMarkDedupe`); a plain adapter (API, workers, backfills, tests) keeps marking
   every time.
3. **What can change a mark's result is reset.** Marks read `tokens` (curve, pair, locker
   exclusions; pair→token), `graduations` (pool→token) and `v4_pool_managers`. Within ingest those
   change only through launch, graduation (incl. the V4 pool projection) and the reorg rebuild, and
   `ClearLaunchPositionDirty` (launch) is the only ingest statement that deletes dirty rows. Each of
   those functions forgets the remembered marks when it returns; a launch also forgets them first,
   since earlier marks in the transaction resolved without its token. `v4_pool_managers` is registered at
   start-up, outside chunks.
4. **Mark identity is semantic, not per query.** A position is `(chain, wallet, token)`; the trader
   mark of a trade and either side of a transfer share it, because `MarkPositionDirty` and
   `MarkTransferPositionsDirty` apply the same exclusions. A transfer with one side already marked
   marks only the other side through `MarkPositionDirty`.
5. **A pool swap without a known sender skips its position mark.** The query is defined as a no-op
   for a NULL `tx_from` (the V4 path already skips it).
6. **Out of scope:** pipelining the remaining per-event statements (pgx batches). That needs result
   handling per statement and its own decision.

## Implementation pointers

`Adapter.WithDirtyMarkDedupe`, `Adapter.markOnce` and `Adapter.forgetDirtyMarks` in
`backend/internal/store/postgres/dirty_marks.go`; call sites in `adapter.go` and `adapter_v2.go`.
