# V2 graduation sweep accounting — pre-flight (2026-11-16)

Source: `AUDIT_REPORT_TR.md` HIGH-01.

## Problem

`BondingCurveV2._mintPosition` ends its `modifyLiquidities` batch with `SWEEP`, and the v4
`PositionManager._sweep` returns the PositionManager's **whole** native balance, not just the
unsettled part of this call. `modifyLiquidities` is payable, so anyone can leave ETH there.
`ethSpent = ethBefore - address(this).balance` then:

- underflows (`Panic(0x11)`) when the preloaded balance exceeds the ETH the mint used, so the
  graduating buy reverts (griefing; the donor risks losing the ETH to any other sweeper);
- under-reports `ethSpent` (and over-reports `ethRemainder` / protocol fees) when it is smaller.

## Decision

Snapshot `_positionManager.balance` before the call and measure
`ethSpent = ethBefore + positionManagerEthBefore - address(this).balance`. Nothing else in the
batch can move ETH, so this is exact for any preload. The swept-in preload stays in the curve as
unbooked forced ETH. See the comments in `_mintPosition`.

## Rejected alternatives

- **Drop `SWEEP` and treat `ethMax` as spent (the report's suggestion).** Any ETH not settled by
  `SETTLE_PAIR` would stay in the PositionManager for the first caller to sweep, and the event
  would report ETH the pool never received.
- **Book the preload as protocol fees.** It would change `unclaimedProtocolFees` with no event the
  indexer reconciles. The contract already tolerates unbooked forced ETH (see `V2Invariant`
  `forcedEth`), so leaving it unbooked follows the existing rule.

## Impact

Engine 2 has no deployment record yet (`contracts/deployments` holds only the v1 testnet record),
so no reviewed bytecode hash changes. Tests:
`test_graduate_ignoresPositionManagerPreload{Above,Below}EthUsed`. The first one reverts with
`Panic(0x11)` without the fix.
