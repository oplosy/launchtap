# Deep reorg soak test — pre-flight 2026-11-14

Closes the first "Indexer and data correctness" Path to 9 item: "a reorg-depth soak test (deep
reorgs on Anvil, indexer output compared with a fresh re-index)" (`docs/quality-scorecard.md`).

## Starting point

`TestAnvilV2LifecycleAndReorg` (`backend/internal/indexer/anvil_v2_e2e_integration_test.go`)
reorgs one graduation away (depth 1) and compares the recovered projections with a fresh index of
the final chain. Recovery itself (`Engine.recoverReorg`, `indexer/reorg.go`) walks back up to
`ReorgSearchDepth` (default 128) headers. Nothing exercises repeated or deep reorgs.

## Decisions

1. **Where it runs.** A new test, `TestAnvilV2DeepReorgSoak`, in the same Anvil v2 gate
   (`scripts/run-anvil-v2-indexer-e2e.ps1`, part of `task verify` and CI). It launches its own
   token on the deployed engine-2 system, so it does not depend on the lifecycle test's state.
   *Alternative:* a separate gate with its own Anvil — a second deployment for no extra coverage.

2. **Shape of a round.** Snapshot the chain, mine an abandoned branch of `depth` blocks (one
   transaction per block), index it, revert, mine a longer replacement branch with different
   transactions, index again. Depths 1, 2, 8, 16, 32, 64 and 100, in that order, so each round
   reorgs further back than the last and the deepest stays under the default search depth (128).
   *Rationale:* depth is what breaks ancestor search and `DeleteCanonicalAbove`; increasing depth
   also leaves earlier rounds' surviving blocks below each new ancestor, so recovery must keep them.

3. **Transactions.** A seeded PRNG (`ANVIL_REORG_SOAK_SEED`, default 20261114, logged) picks curve
   buys, curve sells and token transfers among three dev accounts, with amounts small enough that
   the token never graduates. Graduation reorgs stay covered by the lifecycle test.

4. **What is compared after every round.**
   - `indexer_reorgs` gained exactly one `recovered` row with the exact depth and ancestor.
   - Holder balances equal `balanceOf` on the chain for every holder.
   - A fresh database indexed from scratch over the same chain produces identical rows: every
     `trades` and `transfers` row of the deployment (the canonical ledger, all chain-derived
     columns) and the soak token's projections (`v2ProjectionRows`).
   *Rationale:* the ledger comparison catches a row kept from an abandoned branch or lost from the
   surviving one; the projection comparison catches recovery that rebuilds the wrong state.

## Out of scope

Reorgs below the safe head (refused by design, `ErrSafeViolation`, unit-tested) and depths beyond
128 (`ReorgRecoveryMode`, an operator action).
