# External audit package — launch engine v2

This is the document to hand an external smart-contract auditor (or attach to a request for
quote). It states what is in scope, how the system is meant to behave, who can do what, which
risks are already accepted, and how to build and test the code. It does not restate the code:
each section names the function or file that is the source of truth.

Prepared on 2026-11-18 at `dev` commit `feabe07`. The audit runs against a **frozen commit**
chosen when the engagement starts. Record it in the table below and do not change
`contracts/src` on the audited branch until the report and fix review are done.

| Field | Value |
|-------|-------|
| Frozen commit | `<set at engagement>` |
| Auditor | `<set at engagement>` |
| Start / end date | `<set at engagement>` |
| Report location | `docs/audits/<date>-external-audit-<auditor>.md` plus the signed PDF |

## 1. What the protocol does

A fixed-supply token launchpad on Robinhood Chain (an Arbitrum Orbit L2, chain id 4663).
Anyone can launch a token: the factory deploys the token and a bonding-curve clone in one
transaction. Users buy and sell on the curve. The buy that reaches the graduation threshold
opens a Uniswap v4 pool (native ETH / token, 1 % fee), mints a full-range position from the
curve's ETH and the reserved LP tokens, and hands that position to a locker that holds it
forever. The locker's LP fees are split between the token's creator and the protocol.

The system is non-custodial. Users sign every transaction from their own wallet, and no
backend key can move market funds.

Normative design: `docs/specs/2026-11-11-v2-engine-design.md` (v2) and
`docs/specs/2026-09-01-contract-core-design.md` (rules v2 inherits from v1).

## 2. Scope

**In scope: engine 2, which is the mainnet engine.** The approximate counts below are
non-blank, non-comment lines.

| File | Lines | Role |
|------|------:|------|
| `contracts/src/v2/BondingCurveV2.sol` | 607 | Curve trading, anti-snipe tax, fee buckets, graduation into v4 |
| `contracts/src/v2/LaunchFactoryV2.sol` | 400 | Launch, per-launch snapshot, token → curve registry, governance |
| `contracts/src/libraries/CurveMath.sol` | 183 | Constant-product curve quotes, fee split, checked arithmetic |
| `contracts/src/v2/LpFeeLocker.sol` | 124 | Holds the position NFTs, collects and splits LP fees |
| `contracts/src/v2/GraduationGuardHook.sol` | 114 | v4 hook, `beforeInitialize` only |
| `contracts/src/libraries/GraduationMath.sol` | 54 | `sqrtPriceX96`, full-range liquidity, price continuity |
| `contracts/src/v2/LaunchTokenV2.sol` | 47 | Fixed-supply ERC-20 with a pre-graduation transfer guard |
| `contracts/src/libraries/AntiSnipe.sol` | 36 | Linear-decay buy tax, gross-for-net |
| Storage, types: `src/v2/storage/*`, `src/v2/types/LaunchTypesV2.sol`, `src/types/LaunchTypes.sol` | 180 | |
| Interfaces: `src/v2/interfaces/*`, `src/interfaces/{ILaunchErrors,ILaunchPause,IFactoryClaims}.sol` | 306 | |
| **Total** | **~2,050** | about 1,570 of it executable logic |

**Out of scope:**
- Engine 1 (`BondingCurveV1`, `LaunchFactory`, `LaunchToken` and their storage). It runs on
  testnet only and will not be deployed to mainnet (`docs/specs/2026-11-16-owner-decisions-preflight.md`,
  L-1). `CurveMath` is shared with engine 1 and is in scope.
- Vendored dependencies: OpenZeppelin Contracts v5.7.0, Uniswap v4-core `59d3ecf5`, and
  v4-periphery `c6e4754d`. They are trusted, but **how our code uses them is in scope**,
  especially the `PositionManager` action batches.
- Deployment scripts (`contracts/script/`), tests, the backend and the web client.
- The governance contracts themselves (the timelock and the pause multisig). Only the
  privileges they hold over our contracts are in scope (§4).

## 3. Contract map and main flows

- **Launch:** `LaunchFactoryV2.launch` snapshots the current defaults, clones the curve,
  deploys `LaunchTokenV2` (the whole supply is minted to the curve), registers `curveOfToken`,
  initializes the curve, and optionally runs the developer buy through `BondingCurveV2.buyFor`.
  The developer buy is exempt from the anti-snipe tax and capped at 1 % of supply.
- **Trade:** `BondingCurveV2.buy` / `sell`. The anti-snipe tax (`AntiSnipe.taxBps`) is the
  outermost layer on buys. The 1 % trade fee is split 30/70 between protocol and creator.
  Fees, the tax and failed ETH sends land in pull-claim buckets.
  `_assertAccountingInvariant` runs after every state change.
- **Graduation:** `BondingCurveV2._graduate`, inside the final buy. It marks the token graduated,
  initializes the pool (only this curve passes the hook), mints a full-range position to the
  locker (`_mintPosition`), and calls `LpFeeLocker.registerPosition`. Rounding remainders go to
  the protocol. If any step fails, the whole buy reverts.
- **After graduation:** `LpFeeLocker.collect` (permissionless) and `claim` take LP fees with a
  zero-liquidity `DECREASE_LIQUIDITY` + `TAKE_PAIR` and split them by the snapshotted
  `lpCreatorShareBps`.
- **Fee recipient:** two-step `proposeFeeRecipient` / `acceptFeeRecipient` by the current
  recipient only. No admin path exists.

## 4. Roles and trust assumptions

| Actor | Can | Cannot |
|-------|-----|--------|
| Deployer | Call `setDependencies(hook, locker)` once, before the first launch. | Anything else. It holds no residual authority after setup. |
| Pause authority (multisig) | `setLaunchesPaused`; `setTradingPaused`, which blocks curve **buys** on every un-graduated launch. | Pause sells, claims or the v4 pool. Move any funds. |
| Timelock (≥ 48 h delay, off-chain gate) | Change defaults for **future** launches inside the hard caps; set the future treasury; `configureEngine` registers a curve implementation for a future engine version. | Change an existing launch's parameters, recipients, pool or locker position. Raise the caps (`LaunchTypesV2`: anti-snipe ≤ 9000 bps and ≤ 300 s, trade fee ≤ 500 bps, launch fee ≤ 0.05 ETH). |
| Protocol treasury | Claim launch fees, protocol fees and snipe tax, plus its share of LP fees. | — |
| Creator / fee recipient | Claim creator fees and LP fees, and hand over the recipient role in two steps. | — |
| Anyone | Launch, trade, `collect` LP fees, add their own liquidity to a graduated pool. | Initialize a launchpad pool, or supply token-side liquidity before graduation. |

Assumptions the auditor should challenge:
1. **`configureEngine` trusts the timelock with arbitrary future curve code.** A registered
   implementation becomes `curveOfToken` for its launches, and the hook and locker trust that
   registry. The intended blast radius is limited to launches made on that engine version.
   Confirm that existing launches, the shared locker and other tokens' positions cannot be
   reached from a hostile implementation.
2. **The 48 h timelock delay is enforced off-chain, not by the factory.**
   `backend/internal/chain/timelock.go` refuses a production manifest whose timelock's
   `getMinDelay()` is below 48 h. The contracts accept any non-zero timelock address.
3. Robinhood Chain is an Arbitrum Orbit chain. `block.number` there is the L1 block number, so
   all timing uses `block.timestamp`.
4. The live mainnet `PositionManager` is identified by its code hash and wiring, but it was
   **not** reproduced from the pinned v4-periphery build (different source revision). See
   `contracts/deployments/config/robinhood-mainnet-v4.json` → `evidence`. Assume upstream
   behaviour can differ in detail. This is exactly how the `SWEEP` issue in §6 arose.

## 5. Properties that must hold

These are tested as invariants in `contracts/test/v2/V2Invariant.t.sol` and as fuzz tests. The
auditor should try to break them, not only rely on the tests.

1. ETH conservation: the curve's balance always covers real curve ETH + creator fees + protocol
   fees + snipe tax + pending refunds (`_assertAccountingInvariant`,
   `invariant_curveEthIsFullyAccounted`).
2. Supply is fixed. There is no mint, burn, tax, blacklist or owner (`invariant_supplyIsFixed`).
3. The launchpad pool can only be opened by its own curve, at graduation
   (`invariant_poolOpensOnlyThroughCurve`).
4. Locked liquidity never decreases, and no path removes it or moves the NFT
   (`invariant_lockerLiquidityNeverDecreases`, `testFuzz_lockedLiquidityNeverDecreases`).
5. Phases are one-way. Graduation happens exactly once, atomically, with no partial state.
6. Quotes equal execution, including the anti-snipe tax (`testFuzz_quoteMatchesExecution`).
7. Rounding always favours the protocol's solvency, never a trader. The tax rounds down, and
   the final-buy gross rounds up (`testFuzz_grossForNet_isMinimal`).
8. Before graduation, no one but the curve can move tokens into or out of the curve or into the
   `PoolManager` (`LaunchTokenV2._update`).
9. A hostile ETH recipient cannot block trading. Immediate sends use a 50,000 gas probe and
   fall back to a pull credit (`_sendOrCredit`).
10. Direct ETH or token donations to the curve, the `PoolManager` or the `PositionManager`
    cannot change the graduation outcome or underflow accounting.

## 6. Areas we want the most attention on

1. **Graduation against the live v4 contracts.** Check `GraduationMath.sqrtPriceX96` and
   `fullRangePosition`, the remainder accounting, and the `_mintPosition` action batch.
   Internal finding HIGH-01 (`docs/specs/2026-11-16-v2-graduation-sweep-preflight.md`) showed
   that `SWEEP` returns the `PositionManager`'s **whole** native balance. It is fixed, but
   look for siblings.
2. **The pre-graduation guard.** Is there any route that supplies token-side liquidity to
   the pool, or initializes it at a hostile price, before graduation? Candidate routes:
   `PoolManager` flash accounting (`sync` / `settle`), donations, another hook, or a different
   pool key.
3. **MEV around the graduating buy.** Sandwiching, back-running the pool open, and the
   anti-snipe window at launch (the default is 50 % decaying to 0 over 60 s).
4. **Locker fee accounting.** Rounding of the split, `claim` collecting first, a token half
   that may be worthless, and `registerPosition` authorization.
5. **Governance blast radius**, per assumption 1 in §4.

## 7. Already known or accepted (do not report as new)

| Item | Status | Source |
|------|--------|--------|
| `SWEEP` returns the PositionManager's whole balance, so graduation could underflow or misreport | Fixed with regression tests | `docs/specs/2026-11-16-v2-graduation-sweep-preflight.md` |
| Fee and price-continuity caps were missing on engine 2 | Fixed (L-1) | `docs/specs/2026-11-16-owner-decisions-preflight.md` |
| Third-party AMM pairs for the token before graduation | Accepted (L-2): a token cannot tell a pair from a wallet. Such a pair trades holders' own tokens and cannot move the curve. | same |
| A treasury rotation does not move existing launches' fees | Accepted by design (L-4): each launch pins its treasury | same |
| Forced (self-destruct) ETH in the curve stays unbooked | Accepted: the invariant tolerates a surplus, never a deficit | `V2Invariant.t.sol` (`forcedEth`) |
| Anyone may add liquidity to a graduated pool and dilute the locker's fee share | By design, stated in the UI | design spec §5.2 |
| The timelock delay is not enforced on-chain | Accepted, gated off-chain | §4 assumption 2 |
| Slither 0.11.6 high/medium results (17, of which 12 are in engine 2) | Triaged internally as false positives. **Please re-check this triage independently.** | `contracts/slither.db.json` |

## 8. Build and test

Toolchain: Foundry 1.8.1 (`982849d`), solc 0.8.36, optimizer 200 runs, no via-IR, EVM
`cancun`. `contracts/foundry.toml` is authoritative. On Windows, see the long-path note in
`CLAUDE.md` before cloning submodules.

```bash
git clone <repo> && cd launchpad
git checkout <frozen commit>
git submodule update --init --recursive
cd contracts
forge build
forge test                                    # 217 tests at feabe07, all pass
forge test --match-path "test/v2/*"           # engine 2 only
FOUNDRY_PROFILE=deep forge test --match-test "invariant_|testFuzz"   # fixed-seed deep run
FOUNDRY_PROFILE=fork forge test               # needs ROBINHOOD_MAINNET_ARCHIVE_RPC_URL
```

- v4 `PoolManager` and `PositionManager` are built from upstream sources under
  `FOUNDRY_PROFILE=v4` (solc 0.8.26, via-IR) and loaded by `test/v2/V4TestBase.t.sol`.
- The mainnet fork suite (`fork-test/RobinhoodMainnetV2Fork.t.sol`) runs against the live v4
  deployment at the pinned block in `robinhood-mainnet-v4.json`.
- Curve vectors `contracts/vectors/v2/curve-v2.json` are generated from real executions. The
  backend's Go mirror (`backend/internal/curve`) must match them byte for byte.
- Existing internal evidence: `docs/audits/2026-11-14-deep-property-campaign.md`,
  `docs/audits/2026-11-16-security-audit-run-4.md`, `docs/specs/2026-09-11-security-threat-model.md`.

Deployment order (`contracts/script/DeployLaunchpadV2.s.sol`):
1. Implementation.
2. Factory (pins and code-hash-checks the v4 dependencies).
3. Hook, deployed via CREATE2 with a salt mined so that only the `BEFORE_INITIALIZE` flag is set.
4. Locker.
5. `setDependencies`.
6. Post-deploy assertions.

Default parameters:

| Parameter | Value |
|-----------|-------|
| Supply | 1,000,000,000, of which 800M on the curve and 200M for LP |
| Graduation | 4.2 ETH |
| Initial virtual reserves | 1.4 ETH |
| Trade fee | 1 %, split 30 % protocol / 70 % creator |
| Anti-snipe | 50 % over 60 s |
| LP creator share | 70 % |
| Launch fee | 0.0005 ETH |

## 9. Request for quote

Send this file and the repository link to two or three firms and ask for:
- Experience with **Uniswap v4 hooks and `PositionManager` integrations**, with past reports.
- Auditor-days, calendar dates, price, and whether a fix review is included.
- Report format: severity scale, signed PDF, and permission to publish it.
- Whether they also review economic / MEV issues (§6.3) or only code.

## 10. After the report

1. For each finding, either fix it on a `task/<slug>` branch with a regression test, or accept
   it in writing in this repository with a reason. Leave none unanswered.
2. Ask the auditor for a fix review of the fix commits, then record the final commit hash.
3. Add the report to `docs/audits/` and fill the "External audit" row in
   `docs/runbooks/production-readiness.md`. Re-score category 5 in
   `docs/quality-scorecard.md`.
4. Deploy only the audited bytecode. The manifest's `bytecodeHashes` must match a build of the
   frozen-plus-fixes commit.
5. After mainnet launch, open a bug bounty (for example on Immunefi) for the same scope.
