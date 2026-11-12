# Protocol revenue read

Status: Claude design, 2026-11-12. Closes the backlog item "Engine v2 protocol revenue
projection". The owner asked for the deferred work to be built while away; this picks the
recommended consumer (a public Analytics card) instead of an operator-only endpoint.

## Goal

Show on Analytics how much ETH the protocol has earned, all-time, split by source and engine,
derived only from the indexed event ledger.

## Non-goals

- Claimed-versus-unclaimed balances or treasury wallet balances (never on-chain balance reads).
- Token-denominated revenue (engine 2 `token_remainder` and the LP fees' token side): it has no
  ETH value without a price and is shown nowhere else either.
- USD values, daily series, per-token breakdowns.

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| **Earned basis.** Revenue is what the contracts credit to the protocol when it happens, not what the treasury has claimed. | Claims are batched and late; earned is what the economics produce and is reorg-safe from the same ledger rows. | Claimed basis (lags arbitrarily, says nothing about activity). |
| **Sources** (ETH): launch fees (`token_launches.launch_fee_paid`); curve trade fees, protocol share (`trades.protocol_fee`, both engines); engine 2 snipe tax (`trades.snipe_tax`); engine 2 graduation ETH remainder (`graduations.eth_remainder`, credited to protocol fees by `BondingCurveV2`); engine 2 LP fees, protocol share (`lp_fee_collections.eth_amount` minus the creator share, mirroring `LpFeeLocker._credit`: `floor(eth * lp_creator_share_bps / 10_000)` goes to the creator, the rest including odd wei to the protocol). | These are every ETH flow the contracts credit to the treasury. | Counting `protocol_fee_claims` (double counts with the earned sources). |
| **Read at query time**: one snapshot-consistent aggregate query, no projection table or migration. | Five indexed sums; launch-scale volume is small and the read sits behind the API cache. A projection can replace it later without changing the API. | A projection table now (write path, rebuild and reorg handling for a single card). |
| **API**: `GET /v1/stats/protocol/revenue` (tag `stats`): `snapshot`, `total`, and `sources` = `launch_fees`, `trade_fees`, `snipe_tax`, `graduation_remainder`, `lp_fees`, each `{engine_1, engine_2, total}` as decimal wei strings. | Same shape for every source keeps the card simple; engine split answers "what did v2 add". | Fields on `/stats/protocol` (that read is a projection row; mixing a live aggregate in changes its cost). |
| **Web**: an Analytics "Protocol revenue" card: total ETH, then one row per source with a non-zero total. Same snapshot check as the other Analytics reads. | Neutral, verifiable numbers next to the volume stats. | A separate page. |

## Risks

- **Read cost** grows with trade count (full sums over `trades`). If it gets slow, move it into
  `protocol_stats` as projection columns.
- **Rounding**: the LP protocol share must use the contract's floor for the creator share; a
  unit test pins it.

## Acceptance

- Integration test: launches on both engines, trades with protocol fee and snipe tax, a v2
  graduation with a remainder, an LP fee collection with an odd amount; sums per source and
  engine match hand-computed values.
- API wire test; OpenAPI and web types regenerated; web unit test for the card rows; Analytics
  e2e with a fixture.
