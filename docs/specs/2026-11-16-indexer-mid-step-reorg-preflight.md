# Pre-flight: retry a reorg that lands mid-step (AUDIT_REPORT_TR HIGH-05)

Date: 2026-11-16. Scope: `backend/internal/indexer` (`engine.go`, `step.go`, `reorg.go`).

## Confirmed defect

A step reads headers, logs and recheck headers in several RPC round trips before it opens the
write transaction. A reorg between those reads produced an error that `Engine.Run` did not
retry, so the indexer process exited:

1. `decodeChunk`, `resolvePromotions` and `commitChunk` return `ErrCanonicalMismatch`, which
   `Run` treated as fatal.
2. A broken parent link inside the chunk (not at the saved Observed block) called
   `recoverReorg`, whose common ancestor is then the Observed tip itself: `depth == 0` and
   "reorg ancestor is not below observed tip".
3. `recoverReorg` failed fatally when the provider's view moved during candidate discovery
   (tip changed, discontinuous candidates, tip drift before the first write).

## Locked decisions

- **`ErrCanonicalMismatch` is retryable in `Run`** and shares the RPC failure budget and
  backoff (`maxConsecutiveRPCFailures`, `maxRPCRetryDelay`). Rationale: every source of it
  fires before the step's write transaction commits (or inside it, rolling it back), so a retry
  starts from the saved state; `verifySavedBlocks` then detects and recovers a real reorg. A
  provider that never settles still exits after the budget, so the orchestrator restarts and
  alerts. Alternative rejected: a separate unbounded retry loop, which would hide a broken
  provider.
- **A broken link inside the chunk is a canonical mismatch, not a reorg recovery.** No saved
  block is involved, so there is nothing to roll back; the next step re-reads the chunk from
  the saved Observed block. Rationale: recovery needs a replaced saved block by definition.
- **Recovery's provider-view checks wrap `ErrCanonicalMismatch`.** They fire before any write
  (the tip recheck sits inside the first transaction, before `RecordReorg`), so they are safe
  to retry. Structural failures (safe violation, no candidates, ancestor not below the tip)
  stay fatal.
- **`ErrSafeViolation` stays fatal.** A replaced safe block needs an operator.

## Not changed

An `indexer_reorgs` row stays `open` when the second recovery transaction fails; the next
step records a new incident. Nothing reads open rows, so this is audit noise, not a defect.

## Tests

`TestRunRetriesCanonicalMismatchFromMidStepReorg`,
`TestStepTreatsBrokenLinkInsideChunkAsCanonicalMismatch`, and the three
`TestReorgRecoveryRejects*` tests now assert the retryable error. All fail without the fix.
