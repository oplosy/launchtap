# Deep property campaign — 2026-11-14

A reproducible, deeper run of every invariant and fuzz suite in `contracts/test`, beyond the
default CI depth. It complements, and does not replace, an external audit.

## Parameters

Profile `deep` in `contracts/foundry.toml` (inherits every compiler setting from `default`):

| Setting | Default (CI) | Deep |
|---------|--------------|------|
| Fuzz runs per test | 256 | 10,000 |
| Fuzz seed | random | `0x6c61756e6368706164` (fixed) |
| Invariant runs | 256 | 1,000 |
| Invariant depth (calls per run) | 100 | 200 |
| `fail_on_revert` | true | true |

Command, from `contracts/` with Foundry 1.8.1 and solc 0.8.36:

```powershell
$env:FOUNDRY_PROFILE = 'deep'; forge test --match-test "invariant_|testFuzz"
```

Source: the commit that adds this file (branch `task/security-perf-evidence`, from `dev` at
`75c35c5`).

## Result

All passed: 10 suites, 11 fuzz tests at 10,000 runs each, 10 invariants; 125.6 s wall time,
300.9 s CPU.

| Suite | Properties | Runs / calls | Reverts |
|-------|-----------|--------------|---------|
| `BondingCurveV1InvariantTest` | `accountingAndFeeRefundConservation`, `fixedSupplyAndAllTokenBalancesRemainAccounted`, `initialLpIsNeverRecoverable`, `inventoryAndCurveProductStayBounded`, `phaseAndGraduationAreOneWayAndConsistent`, `revertsNeverLeavePartialState` | 1,000 / 200,000 | 0 |
| `V2InvariantTest` | `curveEthIsFullyAccounted`, `lockerLiquidityNeverDecreases`, `poolOpensOnlyThroughCurve`, `supplyIsFixed` | 1,000 / 200,000 | 0 |

Fuzz tests (10,000 runs each): curve buy/sell bounds and bucket accounting, exact gross-for-net
minimality (V1 and V2), V2 quote equals execution, full-range remainders, pool-manager settlement
amounts, locked liquidity never decreasing, fixed token supply across transfers, restricted
transfer endpoints, zero-address rejection.

## What it covers and what it does not

- Covers: curve math, fee and refund conservation, fixed supply, one-way phase and graduation,
  the initial LP being unrecoverable, V2 pool opening only through the curve, and locker liquidity.
- Does not cover: an independent reviewer's reading of the code, economic or MEV analysis, or
  behaviour of the live Robinhood Chain deployment. Those remain with the external audit tracked
  in `backlog.md` ("Production release, governance, and audit inputs").

## Re-running

Repeat with the same command after any change under `contracts/src`; record a new dated file
rather than editing this one. A failure prints a counterexample and a seed; keep both in the new
record.
