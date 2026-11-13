# Indexer token identity cache — pre-flight

Date: 2026-11-13. Status: locked.

## Problem

`Engine.Step` read every token identity (`ListTokenIdentities`) and every graduated V4 pool
(`ListGraduatedV4Pools`) from PostgreSQL on every poll and rebuilt them, including idle polls with
no new block. The cost grows with the number of launched tokens and is paid once per
`INDEXER_POLL_INTERVAL`.

## Locked decisions

1. **The engine caches the identity set in memory** (identities with their attached pool ids) and
   reuses it across steps. `ForeignDeployments` and `ReadState` are still read every step.
   *Rationale:* identities only change through rows this engine itself writes. `tokens` is written
   by the launch/graduation projections inside the engine's commit transaction and by
   `rebuild_token_projections` in reorg recovery; `graduations` only by the commit and the reorg
   delete. One indexer owns a chain (session advisory lock, `OwnershipLost` is terminal).
2. **The cache is dropped (and reloaded inside the next step's state transaction) when:**
   - the engine starts (empty cache);
   - a step returns any error, including a commit whose outcome is unknown (an acknowledgement
     lost after a successful commit would otherwise leave new tokens out of the cache);
   - reorg recovery runs, whatever its outcome;
   - a committed chunk contains `TokenLaunched`, `TokenLaunchedV2`, `Graduated` or `GraduatedV2`.
     Graduation does not change the identity columns today, but it is rare and cheap to reload,
     so the rule stays independent of which projection columns a graduation touches.
3. **The cached slice is read-only.** `attachPools` mutates only a freshly loaded slice before it
   is cached; `LedgerRouter.Apply` and discovery only read it.

## Alternatives rejected

- *Change detector query per step (count/max of launches):* still a query per poll, and
  count/max can match across a reorg that replaces one launch with another.
- *Time-based refresh:* bounds staleness instead of removing it, and a stale set silently drops a
  new token's address-filtered Transfer logs on engine 1.

## Implementation pointers

`Engine.identities` / `Engine.invalidateIdentities` and `changesIdentities` in
`backend/internal/indexer/engine.go`.
