# Graduated pool reconciliation — pre-flight 2026-11-18

Closes the code part of the "Indexer and data correctness" Path to 9.5 item "reconcile every token
over a rotating sample and graduated pool reserves too" (`docs/quality-scorecard.md`). The
rotating sample already shipped in PR #94, so only graduated pool state is left.

## Starting point

`reconcile.Pass` (`docs/specs/2026-11-14-state-reconciliation-preflight.md`) compares:
- the phase;
- curve reserves, while the token is on the curve;
- the holder-balance sum against `totalSupply`;
- the top holders.

For a graduated token it compares only phase and balances. The `token_reserves` row is still
what prices the token everywhere:
- engine 1: source `pair`, built from the Uniswap v2 pair's `Sync` logs;
- engine 2: source `v4`, holding `sqrt_price_x96` from the pool's `Initialize` and `Swap` logs.

A missed or mis-decoded pool log leaves a stale price. The balance checks do not notice it,
because pool swaps move tokens between holders without changing the supply. Graduated tokens are
the ones that trade longest, so this is the largest uncovered surface.

## Decisions

1. **Engine 1: compare the pair reserves.** For a token whose indexed reserve source is `pair`,
   read `getReserves()` on `tokens.lp_pair` at the pinned block. Compare the token side with
   `token_reserve` and the WETH side with `eth_reserve` (checks `pair_token_reserve` and
   `pair_eth_reserve`). The side is chosen by Uniswap v2's address ordering: the token is
   `token0` exactly when its address sorts below `tokens.weth`.
   *Rationale:* both addresses are already indexed, and the ordering rule is part of the pinned
   pair code. Reading `token0()` would add a call and prove nothing new.

2. **Engine 2: compare the pool price.** For a token whose indexed reserve source is `v4`, read
   the pool's `slot0` word through `PoolManager.extsload(keccak256(poolId ‖ uint256(6)))` at the
   pinned block. Compare its low 160 bits (`sqrtPriceX96`) with `sqrt_price_x96` (check
   `v4_sqrt_price`). `poolId` comes from `v4_pools`, the PoolManager from `v4_pool_managers`.
   - The slot layout is v4-core's `StateLibrary` (`POOLS_SLOT = 6`, `slot0` first) at the pinned
     commit.
   - Liquidity changes and donations do not move the price, so the last indexed `Swap` or
     `Initialize` must equal `slot0` exactly.

   *Alternative:* `StateView.getSlot0`. Rejected because StateView does not exist on the
   self-deployed testnet stack (`2026-11-18-v2-testnet-path-preflight.md` decision 3).
   `extsload` lives on the PoolManager on every network.

3. **A graduated token must have pool state.** When both the indexed and the on-chain phase say
   `graduated` but the indexed reserve source is still `curve` (or the pool coordinates are
   missing), report a `pool_reserve_source` mismatch instead of skipping the token. A curve-phase
   token with a pool-sourced row is reported the same way.
   *Rationale:* a graduation the projection missed is exactly the drift this pass exists to find.

4. **Liquidity is not compared.** Pool liquidity includes positions anyone can add, and the
   indexer does not project it. Fees stay out of scope, as in the 2026-11-14 decision.

5. **Cost and signals are unchanged.** Calls per sampled token:
   - curve phase: 1 phase, 2 reserves, 1 supply, ≤ 5 holders;
   - graduated engine 1: 1 phase, 1 `getReserves`, 1 supply, ≤ 5 holders;
   - graduated engine 2: 1 phase, 1 `extsload`, 1 supply, ≤ 5 holders.

   The 452-call bound per pass holds. The existing metrics, `IndexerStateDrift` and the log line
   carry the new check names. There is no new configuration.

## Tests

- Unit tests (fake store and chain):
  - agreement and drift for each new check;
  - the `pool_reserve_source` mismatch;
  - token ordering on both sides of WETH;
  - a chain read error.
- `chain` unit test: the `slot0` storage key for a fixed pool id matches a value computed
  independently with `cast keccak`, and the decoder takes the low 160 bits.
- Anvil engine 1 (`TestAnvilPoolSwapSenderAndReorg`): after the router sell and again after the
  reorg recovery, a pass reports no mismatch. Corrupting the indexed pair `eth_reserve` reports
  exactly `pair_eth_reserve`.
- Anvil engine 2 (`TestAnvilV2LifecycleAndReorg`): the existing pass now covers `v4_sqrt_price`
  after V4 swaps. Corrupting `sqrt_price_x96` reports exactly `v4_sqrt_price`.
