# Launch Engine v2 — Contracts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement
> this plan task-by-task (no subagent-driven development in this repository). Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the v2 launch engine contracts — anti-snipe tax, guarded Uniswap V4
graduation, locked LP fee locker, two-step fee recipient — with unit, fuzz, invariant,
grief and mainnet-fork tests and Solidity-generated v2 curve vectors.

**Architecture:** A new, non-upgradeable `LaunchFactoryV2` deploys `LaunchTokenV2` and an
EIP-1167 `BondingCurveV2` clone per launch. Pure math lives in two libraries
(`AntiSnipe`, `GraduationMath`) so tests and vectors pin it. At the final buy the curve
initializes a `GraduationGuardHook`-guarded V4 pool, mints a full-range position owned by
`LpFeeLocker`, and books rounding remainders to the protocol. v1 sources are not modified.

**Tech Stack:** Solidity 0.8.36, Foundry 1.8.1, OpenZeppelin (existing pin), Uniswap
`v4-core` + `v4-periphery` (new pinned submodules; approved by the product owner on
2026-11-11), Slither 0.11.6.

**Spec:** `docs/specs/2026-11-11-v2-engine-design.md` (normative). The v1 contract spec
`docs/specs/2026-09-01-contract-core-design.md` stays normative wherever v2 is silent.

**Out of this plan:** backend/indexer (V4 decoder, projections, Go mirror of the vectors)
and web. Each gets its own plan after this plan's event schema is locked in review.

## Global Constraints

- Our contracts: `pragma solidity 0.8.36;`, checked arithmetic only (no `unchecked` outside
  the existing `ceilDiv` pattern), `deny = "warnings"`, `forge fmt` scoped to `src test
  script fork-test` only — never run it unscoped (vendored submodules).
- Uniswap code is imported, never copied or edited. Only `PoolManager` (and other v4
  deployables used in tests) may compile with its own pinned solc.
- No upgradeable contract, no owner on token/hook/locker, no admin path to any launch's
  parameters, pool, position, split or recipients (spec §2).
- Hard caps in `LaunchFactoryV2`: `antiSnipeStartBps ≤ 9000`, `antiSnipeDurationSeconds ≤
  300`; `lpCreatorShareBps ≤ 10000`.
- Pool key constants: `currency0 = address(0)` (native ETH), `currency1 = token`,
  `fee = 10000`, `tickSpacing = 200`, `hooks = GraduationGuardHook`; full range
  `tickLower = -887200`, `tickUpper = 887200`.
- Anti-snipe time base is `block.timestamp`; `block.number` is never read by v2 code.
- Default future profile: `startBps = 5000`, `durationSeconds = 60`, `lpCreatorShareBps =
  5000`; curve parameters identical to v1 defaults.
- Every new dependency is pinned in `contracts/dependencies.lock` and enforced by
  `scripts/check-dependencies.ps1`.
- Commits: conventional prefix, imperative, one task per commit, reference `Plan v2-A Task N`.

## Review Focus

1. **Buy in the same second as launch, and exactly at `launchTime + duration`** — expected:
   full `startBps` at `t = launchTime`, zero at `t = launchTime + duration`. Pinned in
   Task 2 and Task 7.
2. **Final buy that crosses graduation inside the tax window** — expected: tax only on the
   ETH actually consumed, refund untaxed, graduation succeeds in the same transaction.
   Pinned in Task 7.
3. **Someone initializes the pool, or sends ETH/tokens to the PoolManager or curve, before
   graduation** — expected: initialize reverts; donations change neither the opening price
   nor the position size. Pinned in Task 9.
4. **Fee recipient is a contract that rejects ETH** — expected: its claim reverts, nobody
   else's claim or the pool's trading is affected. Pinned in Task 6 and Task 7.
5. **A third party adds liquidity after graduation** — expected: locker still collects only
   its own position's fees; split math stays exact. Pinned in Task 6.

---

### Task 1: Pin Uniswap v4 and stand up a local V4 test base

**Files:**
- Modify: `.gitmodules`, `contracts/foundry.toml`, `contracts/dependencies.lock`,
  `contracts/scripts/check-dependencies.ps1`
- Create: `contracts/lib/v4-core` and `contracts/lib/v4-periphery` (submodules)
- Create: `contracts/test/v2/utils/V4TestBase.sol`
- Test: `contracts/test/v2/V4TestBase.t.sol`

**Interfaces:**
- Produces: `abstract contract V4TestBase is Test` with `IPoolManager internal poolManager;
  IPositionManager internal positionManager; IAllowanceTransfer internal permit2;` and
  `function deployV4() internal`.
- Produces remappings: `@uniswap/v4-core/=lib/v4-core/`,
  `@uniswap/v4-periphery/=lib/v4-periphery/`, `permit2/=lib/v4-periphery/lib/permit2/`.

- [ ] **Step 1:** Add both submodules at the latest tagged release that matches the
  mainnet deployment (record the exact commit for each in `dependencies.lock` as
  `UNISWAP_V4_CORE_COMMIT` / `UNISWAP_V4_PERIPHERY_COMMIT`); extend
  `check-dependencies.ps1` so a drifted submodule fails.
- [ ] **Step 2: Write the failing test** `test_deployV4_initializesAndMintsFullRange`:
  deploy V4, initialize a hookless ETH/ERC-20 pool at `sqrtPriceX96 = 2**96`, mint a
  full-range position of 1 ETH + 1e18 tokens through `positionManager.modifyLiquidities`,
  assert `positionManager.ownerOf(tokenId) == address(this)` and
  `positionManager.getPositionLiquidity(tokenId) > 0`.
- [ ] **Step 3:** Run `forge test --match-path test/v2/V4TestBase.t.sol` — expected FAIL
  (missing `V4TestBase`).
- [ ] **Step 4:** Make `PoolManager` and `PositionManager` deployable in tests. First try
  Foundry `compilation_restrictions` pinning `lib/v4-core/src/PoolManager.sol` (and the
  periphery deployables) to their own pragma while `solc_version = "0.8.36"` stays the
  default. If Foundry 1.8.1 cannot build that, fall back to committed artifacts under
  `contracts/test/v2/artifacts/` built once with the upstream compiler settings, loaded
  with `vm.deployCode`, and pinned by runtime bytecode hash in the test.
- [ ] **Step 5:** Run the test — expected PASS; run `./scripts/check.ps1 pins` and
  `./scripts/check.ps1 build` — expected clean.
- [ ] **Step 6: Commit** `chore(contracts): pin uniswap v4 and add local v4 test base`.

### Task 2: `AntiSnipe` library

**Files:**
- Create: `contracts/src/libraries/AntiSnipe.sol`
- Test: `contracts/test/v2/AntiSnipe.t.sol` (via `test/harness/AntiSnipeHarness.sol`)

**Interfaces:**
- Produces:
  - `function taxBps(uint16 startBps, uint32 durationSeconds, uint64 launchTime, uint256 nowTs) internal pure returns (uint16)`
    — `startBps × remaining / durationSeconds`, floor; `remaining = launchTime + duration − now`
    when positive, else 0; `nowTs < launchTime` is treated as `nowTs = launchTime`.
  - `function split(uint256 gross, uint16 taxBps_) internal pure returns (uint256 tax, uint256 net)`
    — `tax = floor(gross × taxBps_ / 10000)`.
  - `function grossForNet(uint256 net, uint16 taxBps_) internal pure returns (uint256 gross)`
    — smallest `gross` with `split(gross).net ≥ net`.

- [ ] **Step 1: Write the failing tests:**
  - `test_taxBps_fullAtLaunch`: `(5000, 60, 1000, 1000) == 5000`.
  - `test_taxBps_zeroAtEnd`: `(5000, 60, 1000, 1060) == 0` and `(…, 5000) == 0`.
  - `test_taxBps_floors`: `(5000, 60, 1000, 1030) == 2500`; `(5000, 60, 1000, 1059) == 83`.
  - `test_taxBps_zeroDuration`: `(5000, 0, 1000, 1000) == 0`.
  - `test_split`: `split(1e18, 2500) == (0.25e18, 0.75e18)`; `split(3, 5000) == (1, 2)`.
  - `testFuzz_grossForNet_isMinimal(uint128 net, uint16 bps)` with `bps ≤ 9000`:
    `split(g).net ≥ net` and, when `g > 0`, `split(g − 1).net < net`.
- [ ] **Step 2:** Run `forge test --match-path test/v2/AntiSnipe.t.sol` — expected FAIL.
- [ ] **Step 3:** Implement the three functions in `AntiSnipe.sol` with the semantics above.
- [ ] **Step 4:** Run — expected PASS.
- [ ] **Step 5: Commit** `feat(contracts): add anti-snipe tax math`.

### Task 3: `GraduationMath` library

**Files:**
- Create: `contracts/src/libraries/GraduationMath.sol`
- Test: `contracts/test/v2/GraduationMath.t.sol`

**Interfaces:**
- Consumes: v4-core `FullMath`, `TickMath`, `SqrtPriceMath`; v4-periphery
  `LiquidityAmounts`; OpenZeppelin `Math.sqrt`.
- Produces:
  - `int24 constant TICK_LOWER = -887200; int24 constant TICK_UPPER = 887200;`
  - `function sqrtPriceX96(uint256 ethAmount, uint256 tokenAmount) internal pure returns (uint160)`
    — `sqrt(FullMath.mulDiv(tokenAmount, 2**192, ethAmount))`; reverts
    `GraduationPriceOutOfRange()` unless within `[TickMath.MIN_SQRT_PRICE, TickMath.MAX_SQRT_PRICE)`.
  - `function fullRangePosition(uint160 sqrtPrice, uint256 ethAmount, uint256 tokenAmount) internal pure returns (uint128 liquidity, uint256 ethUsed, uint256 tokenUsed)`
    — liquidity via `LiquidityAmounts.getLiquidityForAmounts` over the full range; used
    amounts via `SqrtPriceMath.getAmount0Delta/getAmount1Delta(..., roundUp = true)`, i.e.
    what the PoolManager will actually settle.

- [ ] **Step 1: Write the failing tests:**
  - `test_sqrtPrice_v1Defaults`: `sqrtPriceX96(4.2e18, 200_000_000e18) ==
    546726262810727612002490081201916`.
  - `test_sqrtPrice_noOverflowAtCaps`: `sqrtPriceX96(1, 1e30 * 1e18)` does not revert with
    an arithmetic panic (either returns or reverts `GraduationPriceOutOfRange`).
  - `test_fullRange_usesNoMoreThanSupplied` and
    `testFuzz_fullRange_remaindersBounded(uint96 eth, uint128 tok)` (bounded to ratios the
    factory caps allow): `ethUsed ≤ ethAmount`, `tokenUsed ≤ tokenAmount`, `liquidity > 0`,
    and for v1 defaults each remainder is `≤ ethAmount / 1e9` and `≤ tokenAmount / 1e9`.
- [ ] **Step 2:** Run — expected FAIL.
- [ ] **Step 3:** Implement both functions as specified.
- [ ] **Step 4:** Run — expected PASS.
- [ ] **Step 5: Commit** `feat(contracts): add v4 graduation price and liquidity math`.

### Task 4: `LaunchTokenV2`

**Files:**
- Create: `contracts/src/v2/LaunchTokenV2.sol`, `contracts/src/v2/interfaces/ILaunchTokenV2.sol`
- Test: `contracts/test/v2/LaunchTokenV2.t.sol`

**Interfaces:**
- Produces: `constructor(string name_, string symbol_, address curve_, address poolManager_, uint256 totalSupply_)`;
  `curve() view returns (address)`; `graduated() view returns (bool)`;
  `markGraduated()` (curve only, once).

- [ ] **Step 1: Write the failing tests:** pre-graduation a non-curve operator moving
  tokens to/from the curve or **to the pool manager** reverts `TransferRestricted`; plain
  wallet-to-wallet transfers succeed; the curve may do all of these; after
  `markGraduated()` every transfer is unrestricted; `markGraduated` from a non-curve
  reverts and a second call reverts `AlreadyGraduated`; total supply minted once to the curve.
- [ ] **Step 2:** Run — expected FAIL.
- [ ] **Step 3:** Implement, mirroring v1 `LaunchToken._update` with `poolManager` added to
  the guarded set (no `lpPair`).
- [ ] **Step 4:** Run — expected PASS.
- [ ] **Step 5: Commit** `feat(contracts): add v2 launch token with pool manager guard`.

### Task 5: `GraduationGuardHook` and hook address mining

**Files:**
- Create: `contracts/src/v2/GraduationGuardHook.sol`
- Create: `contracts/script/v2/MineGuardHook.s.sol`
- Test: `contracts/test/v2/GraduationGuardHook.t.sol`

**Interfaces:**
- Consumes: `V4TestBase`; v4-periphery `HookMiner` (test/script only); v4-core `Hooks`.
- Produces: `interface ICurveRegistry { function curveOfToken(address token) external view returns (address); }`
  in `contracts/src/v2/interfaces/ICurveRegistry.sol` (implemented by the factory in Task 8;
  tests use `test/v2/mocks/MockCurveRegistry.sol`).
- Produces: `constructor(IPoolManager poolManager_, ICurveRegistry factory_)`;
  `getHookPermissions()` with only `beforeInitialize = true`;
  `beforeInitialize(address sender, PoolKey key, uint160)` returns the selector iff
  `sender != address(0) && sender == factory.curveOfToken(Currency.unwrap(key.currency1))`,
  else reverts `UnauthorizedInitializer(sender)`. Also reverts unless `key.currency0` is
  native ETH, `key.fee == 10000`, `key.tickSpacing == 200`. No storage, no owner.

- [ ] **Step 1: Write the failing tests:** mined address has exactly the
  `BEFORE_INITIALIZE_FLAG` bit set; the registry's curve initializes successfully; any other
  address (including the token creator and the factory) cannot; an unknown token (registry
  returns zero) cannot; wrong fee, tick spacing or non-native currency0 reverts.
- [ ] **Step 2:** Run — expected FAIL.
- [ ] **Step 3:** Implement the hook and the mining script (CREATE2 salt search via
  `HookMiner.find` for `Hooks.BEFORE_INITIALIZE_FLAG`).
- [ ] **Step 4:** Run — expected PASS.
- [ ] **Step 5: Commit** `feat(contracts): add graduation guard hook`.

### Task 6: `LpFeeLocker`

**Files:**
- Create: `contracts/src/v2/LpFeeLocker.sol`, `contracts/src/v2/interfaces/ILpFeeLocker.sol`
- Test: `contracts/test/v2/LpFeeLocker.t.sol`

**Interfaces:**
- Consumes: `V4TestBase`; `ICurveRegistry` (Task 5).
- Produces: `interface IFeeRecipientSource { function feeRecipient() external view returns (address); function protocolTreasury() external view returns (address); function lpCreatorShareBps() external view returns (uint16); }`
  in `contracts/src/v2/interfaces/IFeeRecipientSource.sol` (implemented by the curve in
  Task 7; tests use `test/v2/mocks/MockFeeRecipientSource.sol`).
- Produces: `constructor(IPositionManager positionManager_, ICurveRegistry factory_)`;
  `registerPosition(address token, uint256 tokenId)` — only `factory.curveOfToken(token)`,
  once per token; `onERC721Received` accepts only from `positionManager`;
  `collect(address token) returns (uint256 ethAmount, uint256 tokenAmount)` permissionless;
  `claim(address token) returns (uint256 eth, uint256 tokens)` — collects first, pays the
  caller's balances; `owed(address token, address account) view returns (uint256 eth, uint256 tokens)`.
  Creator share = `floor(amount × lpCreatorShareBps / 10000)`, protocol gets the rest.
  Events per spec §7 (`LpFeesCollected`, `LpFeesClaimed`).
  The contract contains no call that decreases nonzero liquidity, burns, transfers or
  approves a position.

- [ ] **Step 1: Write the failing tests:** after swaps in both directions `collect` books
  ETH and token fees split 50/50 exactly (odd wei goes to protocol); `claim` by the current
  fee recipient and by the treasury pays the right amounts; a stranger's claim pays zero;
  after the recipient changes, already-credited balances stay with the old recipient;
  a recipient contract that rejects ETH makes only its own claim revert; third-party
  full-range liquidity added after graduation does not change the locker position's
  liquidity and the locker only collects its own position's fees; `getPositionLiquidity`
  never decreases across a fuzzed sequence of collect/claim/swaps; ERC-721 from any other
  sender is rejected.
- [ ] **Step 2:** Run — expected FAIL.
- [ ] **Step 3:** Implement. Collect = `modifyLiquidities` with `DECREASE_LIQUIDITY(tokenId, 0, 0, 0, "")`
  then `TAKE_PAIR(currency0, currency1, address(this))`.
- [ ] **Step 4:** Run — expected PASS.
- [ ] **Step 5: Commit** `feat(contracts): add lp fee locker`.

### Task 7: `BondingCurveV2`

**Files:**
- Create: `contracts/src/v2/BondingCurveV2.sol`, `contracts/src/v2/storage/BondingCurveV2Storage.sol`,
  `contracts/src/v2/interfaces/IBondingCurveV2.sol`, `contracts/src/v2/interfaces/ILaunchEventsV2.sol`,
  `contracts/src/v2/types/LaunchTypesV2.sol`
- Test: `contracts/test/v2/BondingCurveV2Trading.t.sol`, `contracts/test/v2/BondingCurveV2Graduation.t.sol`,
  `contracts/test/v2/BondingCurveV2Recipient.t.sol`

**Interfaces:**
- Consumes: Tasks 2–6; v1 `CurveMath` unchanged.
- Produces:
  - `LaunchTypesV2.AntiSnipeProfile { uint16 startBps; uint32 durationSeconds; }`,
    `LaunchTypesV2.CurveInitializationV2 { address factory; address implementation; address token; address creator; address protocolTreasury; IPoolManager poolManager; IPositionManager positionManager; address hook; address locker; LaunchTypes.CurveParameters parameters; AntiSnipeProfile antiSnipe; uint16 lpCreatorShareBps; }`.
  - `ENGINE_VERSION() == 2`; `initialize(CurveInitializationV2)` (stores `launchTime = block.timestamp`).
  - `buy`/`buyFor`/`sell` with v1 signatures; `quoteBuy(uint256 ethGross) view returns (uint256 ethGrossUsed, uint256 tokensOut, uint256 protocolFee, uint256 creatorFee, uint256 snipeTax, uint256 refund, bool graduates)`;
    `quoteSell` as v1; `currentTaxBps() view returns (uint16)`.
  - `feeRecipient()`, `pendingFeeRecipient()`, `proposeFeeRecipient(address)`,
    `cancelFeeRecipientProposal()`, `acceptFeeRecipient()`; creator-fee claims pay
    `feeRecipient`; `claimSnipeTax()` pays the treasury; `IFeeRecipientSource` getters.
  - Events exactly as spec §7 in `ILaunchEventsV2`.

- [ ] **Step 1: Write the failing trading tests:**
  - `test_buyAtLaunchSecond_chargesStartTax`: buy 1 ETH at `launchTime` with profile
    (5000, 60) → `snipeTax == 0.5 ether`, and `tokensOut` equals v1 `CurveMath.quoteBuy`
    for `0.5 ether`.
  - `test_buyAfterWindow_noTax`: `vm.warp(launchTime + 60)` → `snipeTax == 0` and output
    equals v1 for the full amount.
  - `test_sell_neverTaxed` inside the window.
  - `test_quoteMatchesExecution` (fuzzed amount and time).
  - `test_tradeEvent_ethGrossIncludesTax`: `ethGross == snipeTax + protocolFee + creatorFee + curve ETH`.
  - `test_snipeTaxClaim_toTreasuryOnly`.
- [ ] **Step 2: Write the failing graduation tests:**
  - `test_finalBuyInWindow_taxOnlyOnUsedEth`: refund is untaxed; required gross uses
    `AntiSnipe.grossForNet` over v1's `exactGrossForNet`.
  - `test_graduate_initializesGuardedPoolAtExpectedPrice`: pool `sqrtPriceX96 ==
    GraduationMath.sqrtPriceX96(graduationEth, lpTokens)`; position owner is the locker;
    position liquidity equals `GraduationMath.fullRangePosition(...).liquidity`; ETH/token
    actually settled equal the predicted `ethUsed`/`tokenUsed`; remainders credited to the
    treasury; `Graduated` event fields match.
  - `test_graduate_isAtomic`: when the pool is already initialized (simulate with a mock
    registry that briefly admits a third party) the final buy reverts and the curve stays
    in `Curve` phase.
  - `test_afterGraduation_curveTradingReverts` and `test_tradingPause_appliesToCurveOnly`.
- [ ] **Step 3: Write the failing recipient tests:** propose → accept moves claims;
  proposal by non-recipient reverts; accept by non-pending reverts; cancel works; a rejecting
  recipient's claim reverts without affecting trading or other claims.
- [ ] **Step 4:** Run the three files — expected FAIL.
- [ ] **Step 5:** Implement. Graduation order follows spec §6.2: mark graduated →
  `poolManager.initialize(key, sqrtPrice)` → `positionManager.modifyLiquidities` with
  `MINT_POSITION(key, TICK_LOWER, TICK_UPPER, liquidity, ethUsed, tokenUsed, locker, "")`,
  `SETTLE_PAIR`, `SWEEP` → `locker.registerPosition` → credit remainders → emit.
  Token approvals to the position manager go through Permit2 as v4-periphery requires.
- [ ] **Step 6:** Run — expected PASS; `forge test` whole suite — v1 tests still PASS.
- [ ] **Step 7: Commit** `feat(contracts): add v2 bonding curve with anti-snipe and v4 graduation`.

### Task 8: `LaunchFactoryV2`

**Files:**
- Create: `contracts/src/v2/LaunchFactoryV2.sol`, `contracts/src/v2/storage/LaunchFactoryV2Storage.sol`,
  `contracts/src/v2/interfaces/ILaunchFactoryV2.sol`
- Test: `contracts/test/v2/LaunchFactoryV2.t.sol`

**Interfaces:**
- Consumes: Tasks 4–7.
- Produces: `LaunchTypesV2.FactoryDefaultsV2 { LaunchTypes.CurveParameters parameters; AntiSnipeProfile antiSnipe; uint16 lpCreatorShareBps; uint256 launchFee; }`;
  `launch(LaunchTypes.LaunchRequest) payable returns (address token, address curve)` (v1
  request shape, `engineVersion == 2`); `setFutureDefaults(FactoryDefaultsV2)` timelock
  only, enforcing the Global Constraints caps; pause authority, timelock and launch-fee
  claim semantics as v1; implements `ICurveRegistry.curveOfToken`;
  `setDependencies(address hook, address locker)` callable once, by the deployer only,
  and every `launch` reverts `DependenciesNotSet()` until it has run; emits v2
  `TokenLaunched`.

- [ ] **Step 1: Write the failing tests:** launch snapshots current defaults into the
  curve and later `setFutureDefaults` does not change it; caps (9001 bps, 301 s, 10001 bps)
  revert; developer buy in the launch transaction is untaxed and still capped at 1%;
  `curveOfToken(token)` returns the new curve; launch before `setDependencies` reverts and
  a second `setDependencies` or one from a non-deployer reverts; `TokenLaunched` fields match;
  non-timelock `setFutureDefaults` reverts; v1 factory untouched (`forge test` v1 suite passes).
- [ ] **Step 2:** Run — expected FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run — expected PASS.
- [ ] **Step 5: Commit** `feat(contracts): add v2 launch factory`.

### Task 9: System invariants and grief tests

**Files:**
- Create: `contracts/test/v2/V2Invariant.t.sol`, `contracts/test/v2/handlers/V2Handler.sol`,
  `contracts/test/v2/V2Grief.t.sol`

**Interfaces:**
- Consumes: Tasks 1–8.

- [ ] **Step 1: Write the invariants** (handler: buys/sells with `vm.warp` across the
  window, claims, recipient changes, graduation, post-graduation swaps, collect, claim):
  curve ETH balance == real curve ETH + unclaimed creator + protocol + snipe tax + pending
  refunds; locker position liquidity never decreases; at most one initialized pool per
  token and only via its curve; token supply constant.
- [ ] **Step 2: Write the grief tests:** third party `initialize` before graduation
  reverts; ETH and tokens sent directly to the `PoolManager` (via `donate`/`sync` attempts),
  to the curve, and to the locker before graduation leave the opening `sqrtPriceX96` and
  position liquidity identical to an undisturbed run.
- [ ] **Step 3:** Run `forge test --match-path "test/v2/V2*"` — expected PASS (fix
  implementation in the owning task's files if not).
- [ ] **Step 4: Commit** `test(contracts): add v2 invariants and grief tests`.

### Task 10: v2 curve vectors

**Files:**
- Create: `contracts/script/v2/GenerateCurveVectorsV2.s.sol`, `contracts/vectors/v2/curve-v2.json`,
  `contracts/vectors/v2/curve.schema.json`
- Modify: `contracts/scripts/check-vectors.ps1`

**Interfaces:**
- Produces: `vectors/v2/curve-v2.json` — v1 vector fields plus `antiSnipeStartBps`,
  `antiSnipeDurationSeconds`, `secondsSinceLaunch`, `expectedSnipeTax`, and graduation
  cases with `expectedSqrtPriceX96`, `expectedLiquidity`, `expectedEthRemainder`,
  `expectedTokenRemainder`. The Go mirror is the backend plan's first task.

- [ ] **Step 1:** Write the generator by executing the real `BondingCurveV2` (as the v1
  generator does), covering: t = 0, mid-window, last second, after window, final buy in
  window, final buy after window, v1 default graduation.
- [ ] **Step 2:** Extend `check-vectors.ps1` so regenerating produces byte-identical
  output and the schema validates.
- [ ] **Step 3:** Run `./scripts/check.ps1 vectors` — expected PASS.
- [ ] **Step 4: Commit** `feat(contracts): generate v2 curve vectors`.

### Task 11: Mainnet-fork acceptance and deployment script

**Files:**
- Create: `contracts/fork-test/RobinhoodMainnetV2Fork.t.sol`, `contracts/script/DeployLaunchpadV2.s.sol`,
  `contracts/deployments/robinhood-mainnet-v4-dependencies.json` (addresses + runtime code hashes)
- Modify: `contracts/scripts/check.ps1` (fork target runs the v2 fork test too)

- [ ] **Step 1:** Verify `PoolManager`, `PositionManager`, `Permit2` (and Universal Router
  for the web plan) for chain 4663 against Uniswap's official deployments feed and live
  bytecode; record addresses and `extcodehash` in the dependencies JSON. Stop and report if
  any address cannot be verified (spec §10).
- [ ] **Step 2: Write the fork test** asserting each pinned `extcodehash`, then the full
  path on the real contracts: deploy v2 → launch → buy inside and after the window →
  graduate → swap both ways on the pool → `collect` → both claims → recipient change →
  claim again.
- [ ] **Step 3:** Run `./scripts/check.ps1 fork` with the fork RPC — expected PASS.
- [ ] **Step 4:** Write `DeployLaunchpadV2.s.sol` (deploys factory and curve
  implementation, mines and deploys the hook, deploys the locker, calls
  `setDependencies` once; refuses to run on chain IDs
  without a verified dependencies JSON — testnet stays fail-closed, spec §11.1).
- [ ] **Step 5:** Run `./scripts/check.ps1 all` and `./scripts/check.ps1 slither` —
  expected clean or triaged with written rationale.
- [ ] **Step 6: Commit** `feat(contracts): add v2 mainnet fork acceptance and deploy script`.

**After this plan:** independent external audit of the v2 contracts (spec §9) before any
mainnet user funds; then the backend plan (Go vector mirror, V4 decoder, projections,
quote) and the web plan.
