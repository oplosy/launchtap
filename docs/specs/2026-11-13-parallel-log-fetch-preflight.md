# Parallel eth_getLogs in discovery — pre-flight

Date: 2026-11-13. Status: locked.

## Problem

Discovery runs every `eth_getLogs` of a chunk one after another: factory logs, curve logs by
topic, a refetch per launch block, every 500-address batch of known tokens and pairs, the locker
and the V4 pools. Only the factory query must come first (it names the new emitters, and on
engine 2 the curve logs name the new pools); the rest are independent, and their results are
merged by `deduplicateAndSort`, which orders by block, transaction and log index.

## Locked decisions

1. **Bounded in-flight calls.** `INDEXER_LOG_FETCH_CONCURRENCY` (default 4, allowed 1–16) caps
   concurrent `FilterLogs` calls per discoverer; the cap is enforced at the call itself, so nested
   fan-out (phases containing address batches) never exceeds it. `1` restores the sequential path.
   The default is moderate because provider rate limits are not known per deployment; the
   existing RPC retry and backoff still apply to each call.
2. **What runs in parallel.** The address batches of one query (`fetch`, `fetchPools`) and the
   independent queries of a chunk after the factory query. Engine 2 keeps one ordering step: the
   refetch of newly graduated pools waits for the curve logs that name them.
3. **Output unchanged.** Results are merged in job order and still pass through
   `deduplicateAndSort`, so the chunk's logs and every validation (emitter, topic, malformed log,
   conflicting duplicate) are identical to the sequential path.
4. **First failure wins.** The first job to fail cancels the others and its error, with the same
   wrapping as before, is returned; the engine retries the chunk as it does today.
5. **No new dependency.** A small `sync`-based helper in `internal/chain`, not `errgroup`
   (`golang.org/x/sync` is only an indirect dependency today).
6. **Out of scope:** parallel halves of a capacity-error range split, and parallel chunks.
