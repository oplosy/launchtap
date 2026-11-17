# Indexer RPC scale — design (2026-11-16)

Production-readiness audit pass 3, M-3: RPC load grows with the number of tokens and pools. Status
2026-11-17: option 1 (default batch 1000) and option 2 (topic-only near the tip) are implemented
(`2026-11-17-run4-owner-defaults-preflight.md` decision 8), and the RPC failover
(`2026-11-17-rpc-failover-preflight.md`).

## Where the load comes from

Each indexer step (`chain.Discoverer.discoverV2`, `internal/chain/discovery_v2.go`) issues:

| Query | Calls per step | Grows with |
|---|---|---|
| Factory logs | 1 | nothing |
| Curve logs (topic-only, filtered locally; batched fallback) | 1 | nothing (our curve topics are unique) |
| Locker logs | 1 | nothing |
| Known token `Transfer` logs, address batches of `INDEXER_LOG_ADDRESS_BATCH_SIZE` (500) | ceil(tokens / 500) | every launched token |
| Known V4 pool logs, pool-id topic batches of 500 on the shared PoolManager | ceil(pools / 500) | every graduated token |

At the chain tip a step covers about one block and runs every `INDEXER_POLL_INTERVAL` (1 s). At
10,000 tokens and 2,000 pools that is about 27 `eth_getLogs` calls per second, almost all of them
for tokens that have had no activity for weeks.

## Why the curve trick does not carry over

Curve logs are fetched once by topic because only our curves emit those topics. Token `Transfer`
is the generic ERC-20 topic and V4 pool events come from the shared Uniswap PoolManager, so a
topic-only query returns every transfer and every V4 swap on the chain. On a chain built for
tokenized assets that response can be far larger than all our batches together.

## Options

1. **Larger address batches.** `INDEXER_LOG_ADDRESS_BATCH_SIZE` already exists (1–2000, default 500);
   raising it to the provider's limit divides the calls by up to four with no code change. Past
   2000 the config cap moves too. Bounded by the provider's address-list and response limits
   (owner fact, see below).
2. **Adaptive topic-only for transfers.** Measure chain-wide `Transfer` and PoolManager log volume
   per block; when it is below a threshold, fetch by topic and filter locally (the curve pattern,
   with the existing batched fallback on a capacity error). One call per step regardless of token
   count, but the response size follows chain activity, not ours.
3. **Cold-token tier.** Scan tokens with no activity in N days every K steps instead of every step.
   Rejected as stated: a step commits all events of its blocks in one transaction, and holder,
   position, funding-attribution and inbox projections assume block order. Delivering a cold
   token's transfers later breaks that invariant (an early buyer's first funder, a notification
   ordered before the trade it refers to). It would need a second cursor per tier and
   out-of-order-safe projections; the cost is far above the gain.
4. **Coarser tip cadence.** At the tip, step only when K blocks or T seconds have accumulated. Divides
   every call by K, but delays trades in the live feed by up to T.

## Recommendation

Do 1 now (configuration), then 2 behind a measured threshold, and keep 3 rejected. Revisit 4 only
if 1 and 2 are not enough, since it costs live-feed latency.

## RPC failover

A single provider is a single point of failure. Add `RPC_FALLBACK_URL`; the client switches after N
consecutive failures and switches back after a healthy probe. Guards the design must keep:

- the fallback must pass the same `eth_chainId` check at startup and on every switch;
- heads from the two providers can disagree by a few blocks, which the reorg detector would read as
  a reorg: after a switch, never step past the lower of the two providers' heads until the new
  provider has caught up, and keep the reviewed bytecode check;
- finality tags (`safe`, `finalized`) must be supported by both, or the indexer refuses to start.

## Owner facts needed

- the provider's `eth_getLogs` limits: maximum addresses per filter, maximum block range, maximum
  logs per response (already an open run-4 owner item);
- chain-wide `Transfer` and PoolManager log volume per block on Robinhood Chain at expected load;
- the fallback provider and its limits.
