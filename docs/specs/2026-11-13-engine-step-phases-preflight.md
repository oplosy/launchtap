# Engine.Step phase split — pre-flight

Date: 2026-11-13. Status: locked.

## Problem

`Engine.Step` (`backend/internal/indexer/engine.go`) was one ~290-line function that ran every
phase of a poll inline: state and identity load, head reads, saved-header verification, chunk
header validation, discovery and decoding, promotion resolution and the commit. Each phase shared
local variables with the others, so a reader had to hold the whole function to know which values
a check depended on.

## Locked decisions

1. **Pure refactor, no behaviour change.** `Step` becomes an orchestrator over named phase methods
   in `backend/internal/indexer/step.go`. The order of every RPC call, store transaction, check and
   returned error stays as it was. Existing engine, reorg and identity-cache tests are the
   regression suite; no test changes are expected.
2. **Phases** (one method each): load state and identities; read and order heads; verify the
   saved Safe/Observed headers (may recover a reorg); read and link the chunk headers (may recover
   a reorg); discover and decode the chunk's logs; resolve safe/finalized promotions with the tip
   recheck; commit. A phase that recovers a reorg reports it with a `recovered` flag so `Step`
   returns `(true, nil)` exactly where it did before.
3. **State flows explicitly.** Phases take what they read as arguments and return what they
   produce (`stepHeads`, `stepChunk`); only the promotion phase mutates `State`, through a pointer,
   as the inline code did. The previous safe/finalized watermarks are captured in `Step` before
   promotion, as before.

## Implementation pointers

`Engine.Step` and the phase methods in `backend/internal/indexer/step.go`.
