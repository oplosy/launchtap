# v2 engine backend — pre-flight review

**Scope:** the backend half of `docs/specs/2026-11-11-v2-engine-design.md` §7–§8 (ABI
registry, V4 decoder, projections, quote, Go curve mirror). Reviewed against `dev` +
`task/v2-engine-contracts` (PR #25) on 2026-11-11. No implementation plan exists yet; this
review is its input.

## Current backend shape that v2 collides with

- **One engine per process.** `chain.NewDecoder` accepts only `engineVersion == 1` and
  rejects any `TokenLaunched`/`EngineConfigured` of another version. `deployments`
  `supportedEngines = {1}`. Discovery verifies every launch as a Uniswap V2 pair
  (`VerifyLaunchPair`).
- **Chain-scoped ledger.** `sync_state` is keyed `(chain_id, deployment_id)`, but every
  ledger table (`token_launches`, `trades`, …) and every reorg delete is keyed by `chain_id`
  only (`queries/reorg.sql`: `DELETE … WHERE chain_id=$1 AND block_number>$2`).
- **V2-pair market model.** `tokens.lp_pair` and `tokens.weth` are `NOT NULL` 20-byte
  columns, `token_is_token0` is generated from `token_address < weth`, and 35 query sites
  join on `lp_pair`/`pool_swaps`/`pool_syncs`: price, candles, stats, holder exclusion
  (`holder_address <> t.lp_pair`), positions and PnL. Pool-phase price comes from V2
  reserves (`Sync`).
- **v1-only artefacts.** `contracts/abi/v1`, `contracts/fixtures/v1`, `vectors/v1` feed
  `sync-event-abis` / `sync-curve-vectors`. The deployment manifest schema describes a V2
  pair deployment (`uniswapV2Factory`, `pairInitCodeHash`, `lpBurnAddress`).
- **No v2 environment.** Testnet has no V4; mainnet v2 waits for the external audit;
  `DeployLaunchpadV2` refuses every chain but 4663. There is no local v2 deploy path, so
  the Anvil e2e harness (`task anvil-indexer-e2e`) cannot run a v2 deployment.

## BLOCKERS

1. **v2 event ABI is not frozen or exported.** Spec §7: "final field order is fixed in the
   pre-flight review". `ILaunchEventsV2` + `ILpFeeLocker` events exist in source only; there
   is no `contracts/abi/v2`, no v2 event fixtures, and no golden that fails on drift.
   **Decision needed:** lock the implemented order (`TokenLaunched` = `TokenLaunchedData`'s
   18 fields + 3 indexed; `Trade` = v1 + trailing `snipeTax`; the rest as in
   `ILaunchEventsV2.sol`). *Recommended:* lock as implemented; add a contracts task that
   exports `abi/v2`, generates `fixtures/v2` from the real system, and extends
   `check-event-fixtures.ps1` and the release review allowlist.
2. **Deployment topology.** Because the ledger and reorg are chain-scoped, two deployments
   on one chain in one database would delete each other's rows on a reorg.
   **Decision needed:**
   - (a) *Recommended:* "one engine deployment per chain per database". v1 stays testnet,
     v2 is mainnet-only. Enforce it at startup: refuse a `sync_state` row for another
     deployment on the same chain.
   - (b) Deployment-scope every ledger table and query. This is a large migration, needed
     only if v1 and v2 ever share a chain.
3. **Pool-phase market model for V4.** There are no reserves and no `Sync` event, and the
   `PoolManager` is a singleton. **Decision needed:**
   - (a) *Recommended:*
     - Add V4 ledger tables (`v4_swaps`, `v4_liquidity_changes`) and a pool registry
       `(chain_id, pool_id) → token, position_token_id, hook`.
     - For v2 rows, make `tokens.lp_pair`/`weth` nullable under a `CHECK` on
       `engine_version`, with `token_is_token0` false (native ETH is always `currency0`).
     - Pool-phase price comes from the post-swap `sqrtPriceX96`.
     - Projections become engine-aware.
   - (b) Synthesize V2-shaped `pool_swaps`/`pool_syncs` rows from V4 events. *Rejected:*
     V4 has no reserves to put in `Sync`, and the fake rows would leak into every query.

   Also decide what the API shows as "liquidity" for a graduated v2 token. See R5.
4. **No runnable v2 deployment for e2e.** Needed:
   - a local v2 deploy script (local V4 stack plus the real factory/hook/locker, as in
     `V2SystemBase`) that writes a generated manifest;
   - a manifest schema for engine 2: `poolManager`, `positionManager`, `permit2`, `hook`,
     `locker` and their code hashes, no V2 pair fields.

   This is contracts-side work that must land before backend acceptance can be tested.
5. **Quote clock for a time-dependent tax.** The tax decays with `block.timestamp`.
   **Decision needed** on the "as of" time.
   - *Recommended:* the latest indexed head block time. It is deterministic, and because it
     lags execution it slightly overstates the tax. That is conservative for
     `minTokensOut`, because a later execution only pays less tax.
   - The quote returns `snipeTax`, `taxBps` and `taxEndsAt`.

## RISKS

1. **Singleton log scoping.** `PoolManager` `Swap`/`ModifyLiquidity` are emitted for every
   pool on the chain and their signatures are not protocol-unique. Fetch by
   `address = PoolManager` and `topic1 ∈ known poolIds`, batched like the curve address
   batches. Drop everything else. Pools on the same token with another hook or fee have
   different poolIds and must stay invisible.
2. **Reorg and same-block registration.** A pool becomes known at `Graduated`. Swaps in the
   same block after graduation must be fetched in the same step, as `newByBlock` does for v1
   pairs. A rollback across the graduation block must remove the pool registration and the
   V4 rows.
3. **Swap sign convention and volume.** V4 `Swap.amount0/amount1` are signed `int128`
   swapper deltas, and the input side includes the LP fee (`fee` is in pips). Pin direction,
   volume (ETH side, gross of LP fee, like v1 `ethGross`) and price with fixtures from the
   real `PoolManager`. Do not infer them from documentation.
4. **Trader attribution.** `Swap.sender` is the router, so reuse the `TxFrom` lookup. The
   v1 limitation for aggregator and multi-hop transactions carries over; document it rather
   than guess.
5. **Third-party liquidity.**
   - Outside `ModifyLiquidity` changes pool liquidity; the locker's position never
     decreases.
   - Pool TVL derived from `liquidity` is wrong for concentrated third-party positions.
   - Show the locker position or omit TVL. Do not present full-range math as pool
     reserves.
6. **Holder exclusions.** The `PoolManager` holds every pool's tokens and the locker holds
   uncollected token fees.
   - Replace the `lp_pair` exclusion with `PoolManager` + locker for v2.
   - The `0x…dEaD` burn exclusion no longer applies, because the v2 LP NFT is locked, not
     burned.
7. **Creator fee ledger has no credit event.**
   - Curve creator fees and the locker creator share are credited to the `feeRecipient`
     in force at that log. The locker split is `floor(amount × lpCreatorShareBps / 10000)`;
     the protocol gets the rest.
   - The projection must replay `Trade.creatorFee` and `LpFeesCollected` against the
     recipient ordered by `FeeRecipientAccepted` (block, log index, across curve and locker
     emitters).
   - Drift risk: the e2e reconciles against on-chain `owed()` and the curve's
     `creatorFeesOwed(account)`.
8. **Revenue metrics.** `ethGross` now includes `snipeTax`. Protocol revenue gains the tax
   and the graduation ETH remainder. `SnipeTaxClaimed` is a separate claim stream.
9. **Launch verification.** Replace `VerifyLaunchPair` for v2. Each `TokenLaunched` must
   carry the manifest's `hook` and `locker`, and the pool key derived from (ETH, token, fee
   10000, spacing 200, hook) must hash to the `poolId` that `Graduated` later reports.
10. **Go mirror scope.** `internal/curve` must add the anti-snipe tax with the exact integer
    rounding of `AntiSnipe.sol`, stdlib-only. Graduation math (`sqrtPriceX96`, liquidity)
    is optional for the backend: the indexer reads it from `Graduated`. Mirror it only if a
    pre-graduation preview needs it. `sync-curve-vectors` must copy `vectors/v2`
    byte-identical.

## ACCEPTANCE CRITERIA CHANGES

- **Before the backend plan,** add contracts prerequisites:
  - `abi/v2` export, `fixtures/v2` and the event-fixture gate (B1);
  - the local v2 deploy and the engine-2 manifest schema (B4).
- `task verify` includes:
  - the `vectors/v2` byte-identical copy and the Go tax mirror passing every v2 case;
  - the v2 ABI and fixture copies byte-identical, with the decoder decoding every v2
    fixture.
- Startup fails closed on a second deployment for the same chain (B2a), or every ledger
  query is deployment-scoped (B2b).
- Anvil v2 e2e, asserted through the API, covers this lifecycle:
  1. launch with the dev buy;
  2. a taxed buy, then an untaxed buy;
  3. graduation;
  4. V4 swaps both ways;
  5. `collect`/`claim`;
  6. a recipient change;
  7. claims after the change.

  Projections must equal on-chain views (`owed`, `creatorFeesOwed`, the pool `slot0`
  price).
- A reorg test crosses a v2 graduation block: the pool registry and V4 rows roll back, and
  re-indexing is identical.
- The quote endpoint returns `snipeTax`, `taxBps` and `taxEndsAt` at the agreed clock (B5),
  and matches `curve-v2.json` cases.
- Holder count and top holders exclude the `PoolManager`, the locker and the curve for v2
  tokens.

## Out of scope here

Web (Universal Router V4 swap path, Permit2 sells, locker claims UI, tax countdown), and the
external audit. Both are separate plans or gates.
