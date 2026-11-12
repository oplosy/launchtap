# Plan v2-A Task 11 — v2 Slither triage

`check-slither.ps1` (Slither 0.11.6, `--fail-medium`) passes once the twelve v2 results below
are recorded in `slither.db.json`. Each was reviewed against the source; none is a defect.

## Reproducing the ids

Slither ids hash source positions. A Windows checkout with `core.autocrlf=true` (CRLF files)
shifts the reported line numbers, so every result, v1 included, gets a new id and the gate
fails locally even though CI (LF checkout) passes. Run Slither on an LF checkout to reproduce
the recorded ids.

## IR generation fix

Slither could not build IR for `BondingCurveV2.initialize`, `_mintPosition`, `_mintData`, and
`LpFeeLocker._collectData`: OpenZeppelin's and v4-core's libraries are both named `SafeCast`,
and Slither resolved calls against v4-core's, which has no `toUint8`/`toUint48`/`toUint64`.
The v2 contracts now use only v4-core `SafeCast` (`toUint160`, `toUint128`); the two
timestamp narrowings are explicit bound checks that revert with `SafeCast.SafeCastOverflow()`,
and the `Actions` ids (compile-time constants below `0x20`) are cast directly. Behaviour is
unchanged; the v2 curve vectors regenerate byte-identical.

## Triaged results

| Check | Location | Rationale |
| --- | --- | --- |
| arbitrary-send-eth (High) | `LaunchFactoryV2._callDeveloperBuy` | `curve` is the clone the factory created in the same call; the value is the creator's own developer-buy ETH. Same pattern as the triaged v1 `LaunchFactory._callDeveloperBuy`. |
| reentrancy-no-eth | `LpFeeLocker.claim` | `claim` is `nonReentrant`; the only external call before the writes is the immutable, fork-pinned `PositionManager` collect, which must run first so the payout includes the latest fees. Owed balances are zeroed before any transfer; the "cross-function" reader is the `owed` view. |
| incorrect-equality | `BondingCurveV2._creditCreatorFees`, `_takeCreatorFees`, `_takeRefund`, `_sendOrCredit` | `amount == 0` is a no-op early return. Forcing a non-zero amount (e.g. by donation) only reaches the normal path; nothing depends on an exact balance. |
| incorrect-equality | `LpFeeLocker._collect` | `ethAmount == 0 && tokenAmount == 0` skips crediting and the event when nothing was collected; the amounts are balance deltas, not absolute balances. |
| uninitialized-local | `BondingCurveV2._graduate().result` | A memory struct assigned field by field before any read. Same pattern as the triaged v1 `LaunchFactory.launch().eventData`. |
| unused-return | `BondingCurveV2.initialize`, `LaunchFactoryV2._setFutureDefaults` | `GraduationMath.sqrtPriceX96` is called only for its revert: it validates that the graduation price is in range. |
| unused-return | `BondingCurveV2._graduate` | `PoolManager.initialize` returns the initial tick, which the curve does not need; the price is its own input. |
| unused-return | `LpFeeLocker.registerPosition` | Only the pool key from `getPoolAndPositionInfo` is checked (it must pair native ETH with the token); the packed position info is not needed. |
