# Indexer RPC failover — pre-flight (2026-11-17)

Implements the "RPC failover" section of `docs/specs/2026-11-16-rpc-scale-design.md`
(production-readiness audit pass 3, M-3). The batch-size and topic-only options there still need
the provider facts and are not part of this change.

## Locked decisions

1. **Opt-in, indexer only.** `RPC_FALLBACK_URL` (optional, secret, http/https, must differ from
   `RPC_URL`). Unset means the indexer behaves exactly as today. The API and one-off commands keep
   a single provider: they do not advance the ledger, so a failed call there is a retryable
   request, not a stalled projection.
   Tunables: `RPC_FALLBACK_AFTER_FAILURES` (default 3, 1–100) and `RPC_FALLBACK_PROBE_INTERVAL`
   (default 30s, 1s–1h).
2. **Same startup checks for both providers.** The fallback passes the chain-id check, the
   reviewed bytecode check, the governance timelock check and a `latest`/`safe`/`finalized` head
   read before the indexer opens the database; any failure refuses to start.
3. **Switch on consecutive provider failures, counted per method.** Inside `chain.Client`, after
   the configured number of consecutive calls of one JSON-RPC method fail with a provider error
   (timeouts, network errors, HTTP 429/5xx, JSON-RPC -32000..-32099) the client switches to the
   other provider, after checking that provider's `eth_chainId` again. A failed check keeps the
   current provider. Counting per method means an `eth_getLogs` outage is not reset by head reads
   that still succeed. Capacity errors, caller cancellations and -32000-range answers about the
   request itself (a revert, a missing block or trie node) do not count.
4. **No step mixes providers or runs ahead of the old head.** A switch records the highest
   `latest` head seen so far. Until a `Heads` call on the new provider reports at least that
   height, `Heads` returns a catching-up error and every other call returns a switching error, so
   the step in flight aborts without committing and the next steps wait. This keeps the reorg
   detector from reading a lagging provider as a reorg and keeps a lagging provider from returning
   empty logs for blocks it does not have yet. Independently of failover, a step whose `latest`
   head is below the indexed Observed block waits (a retryable `ErrRPCUnhealthy` failure) instead
   of reading the missing saved header as a fault. This covers a provider started on after a
   restart, which has no in-memory gate, and a lagging single provider; a reorg to a shorter
   chain is still detected once the new chain passes the saved height.
5. **Switch back only at a step boundary.** While on the fallback, `Heads` probes the primary at
   most once per probe interval, counted from the switch (chain id plus heads, bounded together
   by one RPC timeout). The primary is taken back only when it is healthy and at or above the
   highest head seen; the step then runs entirely on the primary. Only the step loop calls
   `Heads` and it is sequential, so a switch back never lands while a step's discovery workers are
   in flight. The background readers sharing the client (sender backfill, state reconciliation)
   count toward a switch like any caller; while a switch is gated they get the switching error and
   retry on their next interval.
6. **Start on the fallback when the primary is unreachable.** If the primary's startup checks
   fail with a provider outage, the indexer starts on the fallback and probes the primary as in
   decision 5. A primary that answers with the wrong chain id or bytecode still refuses to start.
7. **Discovery outages retry the step.** A provider outage or a switch during log discovery is
   now an `ErrRPCUnhealthy` step failure, retried with the indexer's existing backoff (as a head
   read already was) instead of stopping the process. Other discovery errors still stop it. This
   also applies without a fallback.
8. **Observability.** Each switch logs at warn level with the provider role (`primary`/
   `fallback`), never the URL. Both URLs are redacted from error text.

## Rejected

- Round-robin or per-call load balancing: heads from two providers disagree by a few blocks, and
  per-call mixing would need every caller to reconcile them.
- A shared cursor across providers inside one step: the gate in decision 4 is simpler and makes
  a mid-step switch abort the step, which the indexer already retries.
