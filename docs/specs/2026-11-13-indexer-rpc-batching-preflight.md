# Indexer RPC and block-write batching — pre-flight

Date: 2026-11-13. Status: locked.

## Problem

Every `Engine.Step` paid sequential round trips that do not depend on each other:

- `chain.Client.Heads`: three `eth_getBlockByNumber` calls (latest, safe, finalized), one after
  another, on every poll including idle ones.
- The saved Safe and Observed headers: two more single calls on every poll.
- After discovery: the chunk-tip recheck plus up to two promotion headers, each a single call,
  and one database transaction per promotion block that was not in the chunk.
- The commit wrote `indexed_blocks` with one `UpsertIndexedBlock` round trip per block (100 per
  chunk at the default `INDEXER_CHUNK_SIZE`) while holding the commit transaction open.

## Locked decisions

1. **`Heads` is one JSON-RPC batch.** Per-element results keep the old meaning: a failed or null
   `latest` is "read latest head", an unsupported or missing `safe`/`finalized` tag is still a
   `FinalityTagError`. The retry policy wraps the whole batch, as it wraps `HeadersByNumbers`.
   *Trade-off:* the RPC observer now counts one `eth_getBlockByNumber` per `Heads` instead of three.
2. **Header reads inside `Step` go through one helper** that de-duplicates the numbers and uses
   `BatchHeaderSource` when the source has it, falling back to sequential reads otherwise. The saved
   Safe/Observed headers are one batch; the recheck of the chunk tip and the promotion headers are
   one batch after discovery. All checks run in their previous order on the fetched headers.
   A number requested twice (the chunk tip is often also the safe promotion number) is fetched once,
   so both checks see the same header.
3. **Promotion blocks missing from the chunk are read in one store transaction.**
4. **`indexed_blocks` is written with one multi-row upsert** (`UpsertIndexedBlocks`, `unnest` of
   arrays, same `ON CONFLICT` rule). If it affects fewer rows than given, the adapter replays the
   blocks through the single-row path inside the same transaction to return the exact per-block
   error as before. A unique violation aborts the statement and with it the transaction, so it is
   reported as an invariant conflict for the block range.
5. **Out of scope:** running discovery's address-batched `eth_getLogs` calls in parallel. It
   raises the request rate against provider limits and needs its own decision.

## Implementation pointers

`chain.Client.Heads`; `Engine.fetchHeaders` and the promotion block in `Engine.Step`;
`Adapter.UpsertIndexedBlocks` and the `UpsertIndexedBlocks` query in `queries/blocks.sql`.
