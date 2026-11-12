# Launch Engine v2 — Design Spec

**Date:** 2026-11-11
**Status:** Design agreed with the product owner; implementation requires the normal
high-risk pre-flight review (smart contracts, fee math, graduation, indexer).
**Scope:** A second, separately deployed launch engine: anti-snipe tax on curve buys,
graduation into a Uniswap V4 pool with a permanently locked liquidity position whose LP
fees are shared between creator and protocol, and a two-step creator fee recipient change.
Includes the event contract the backend depends on and the backend/web impact.
**Out of scope:** Changes to v1 contracts or v1 tokens, community takeover (CTO), team fee
splits, buyback/vesting, holder rewards, custom quote assets, a custom swap hook, and
limit orders.
**Prerequisite:** Gate 0 (see `backlog.md` production item and `notes.md` roadmap
candidates) closes before any v2 contract reaches mainnet.

This document is normative for v2 behavior. The v1 contract spec
(`2026-09-01-contract-core-design.md`) stays normative for v1 and for every rule this
document does not override.

## 1. Why v2 exists

v1 creators earn only during the curve phase: after graduation the token trades on a plain
Uniswap V2 pair and LaunchTap takes no fee, so creator revenue stops exactly when a token
succeeds. Competing launchpads on the same chain continue creator revenue after graduation.
v2 closes that gap without a custom swap hook, and adds the two launch protections most
requested by the market: an anti-snipe window and a creator-controlled fee recipient.

### Decisions taken during design (with the rejected options)

| Decision | Chosen | Rejected and why |
|---|---|---|
| Scope | One v2 engine with all three features | Splitting into several engines means several audits and migrations |
| Post-graduation revenue | Locked V4 LP position; LP fees collected and split | Custom V4 swap hook: heavy audit surface, some aggregators skip hooked pools. Token transfer tax: breaks the "plain ERC-20" trust signal and trips honeypot scanners. Router-side fee: misses trades outside our UI |
| Token half of LP fees | Distributed as-is, pull-claimed | Burning it: unpredictable creator income. Swapping to ETH: sell pressure, MEV, slippage handling |
| Anti-snipe tax recipient | Protocol treasury | Creator: a creator could snipe their own launch and recover the tax. Curve reserve: changes curve accounting and graduation math |
| Pool initialization guard | Minimal `beforeInitialize`-only hook | Hookless pool: the deterministic pool key lets anyone initialize the pool at a hostile price before graduation (no tokens needed) |

## 2. System and trust boundaries

- v1 factory, curve implementation, tokens and pairs are untouched. v2 is a new factory
  deployment with its own addresses; the backend tells engines apart by
  `(factory deployment, engineVersion)`.
- Users sign every launch, buy, sell, claim, collect and recipient change from their own
  wallet. No backend key signs user transactions or holds market funds.
- No contract in v2 is upgradeable. Factory governance (pause authority + timelock, same
  roles as v1) can pause curve trading and change defaults for future launches only. It
  cannot change an existing launch's parameters, pool, locker position, fee split or
  recipients, and cannot move curve reserves or locked liquidity.
- After graduation nothing in LaunchTap can pause or tax trading in the pool.

## 3. Components

### 3.1 `LaunchFactoryV2`
- Deploys the token and an EIP-1167 curve clone in one transaction, as v1 does, but creates
  no V2 pair.
- Snapshots every launch parameter into the curve at launch: v1 curve parameters, the
  anti-snipe profile (§4), the LP fee split (§5.2), the pool key constants (§6.1), the
  protocol treasury, the guard hook and the locker.
- Validates the anti-snipe profile against hard caps that even the timelock cannot raise
  without a new factory: `startBps ≤ 9000`, `durationSeconds ≤ 300`.
- Keeps v1's launch fee, developer buy (max 1% of supply, in the launch transaction) and
  pause semantics. The 1% cap is checked on the executed amount: `quoteBuy` includes the
  live anti-snipe tax while the exempt developer buy does not, so a pre-quote would
  understate the tokens received.
- Holds the V4 `PoolManager`, `PositionManager` and `Permit2` as immutables (the
  constructor checks the `PositionManager` is wired to that `PoolManager`). The guard hook
  and locker are set once, by the deployer, after the hook address is mined
  (`setDependencies`, which validates their wiring and the hook's flag bits). The v1 engine
  registry is kept for a future v2.x curve.

### 3.2 `LaunchTokenV2`
- Fixed supply minted once to the curve; no owner, mint, tax, blacklist or pause.
- Pre-graduation transfer guard, extending v1's: until graduation only the curve may move
  tokens into or out of the curve **or into the V4 `PoolManager`**. This stops anyone from
  supplying token-side liquidity before graduation. The guard ends permanently at
  graduation.

### 3.3 `BondingCurveV2`
- v1 curve arithmetic, fees, refunds and pull-claims, with the anti-snipe layer of §4.
- Graduation (§6) initializes the pool, mints the full-range position and hands it to the
  locker in the same transaction as the final buy.
- Holds the token's single `feeRecipient` (§5.3), read by the locker.

### 3.4 `GraduationGuardHook`
- A Uniswap V4 hook whose only permission bit is `BEFORE_INITIALIZE`.
- `beforeInitialize` succeeds only when the caller is the curve the factory records for
  that pool's token (`factory.curveOfToken`). Every other initializer reverts.
- No swap, liquidity or donate callbacks, no owner, no upgrade path, no storage. Its
  address is mined (CREATE2) so that only the `BEFORE_INITIALIZE` flag bit is set. The
  factory learns the hook and locker addresses through a one-time, deployer-only
  `setDependencies` call made before the first launch.

### 3.5 `LpFeeLocker`
- Receives each graduated token's position NFT from the curve and holds it forever.
- Exposes no function that removes liquidity, transfers the NFT or approves it. The only
  `PositionManager` interaction is fee collection: a zero-liquidity decrease followed by
  taking the owed currencies.
- `collect(token)` is permissionless. It splits the collected ETH and tokens by the
  snapshotted creator/protocol split into pull-claim balances.
- `claim(token)` pays the caller's balances, collecting first so a claim never misses
  accrued fees. A recipient that cannot receive ETH does not block anyone else (pull
  model, as in v1).

## 4. Anti-snipe tax

- Applies to **curve buys only**. Sells are never taxed; exit is never made harder.
- Time base: `block.timestamp`. On Robinhood Chain (Arbitrum Orbit) `block.number` is the
  L1 block number and must not be used. The launch timestamp is stored at initialization.
- Profile, snapshotted at launch and immutable: `startBps`, `durationSeconds`. The tax
  decays linearly to zero: `taxBps = startBps × remaining / durationSeconds` while
  `remaining = launchTime + durationSeconds − now > 0`, else zero. Both the rate and the
  tax amount round down (in the buyer's favor).
- Default profile proposed for the first deployment: 5000 bps over 60 seconds. Defaults are
  timelock-configurable for future launches inside the §3.1 caps.
- Layering: the tax is the outermost layer on the ETH actually consumed. It is taken first;
  the remainder goes through the unchanged v1 fee split and curve math. On a final buy the
  required gross is rounded up so the protocol is never short, and the refunded excess is
  never taxed.
- The developer buy inside the launch transaction is exempt. No exemption list exists or
  can be added later.
- Destination: the snapshotted protocol treasury's pull-claim balance on the curve.
- `quoteBuy`, the backend quote endpoint, `internal/curve` and the web quote all apply the
  same `currentTax` rule. The exact arithmetic lives in the implementation's doc comment
  (mirrored by name in Go); v2 curve vectors (§9) lock it.

## 5. Fees and recipients

### 5.1 Curve phase
Unchanged from v1: trade fee 1%, split 0.50% protocol / 0.50% creator, pull-claimed. The
anti-snipe tax is additional and goes only to the protocol.

### 5.2 Pool phase
- The pool's static LP fee is 1% (fee `10000`).
- The locker's position earns LP fees in both currencies. Split 50% creator / 50% protocol,
  snapshotted at launch (`lpCreatorShareBps`), future default timelock-configurable.
- Anyone may add liquidity to the pool afterwards and earns its pro-rata share of fees; the
  locker's share of fees shrinks accordingly. The UI states this.
- The UI shows every cost line separately: curve fee, anti-snipe tax (with time left), pool
  LP fee.

### 5.3 Creator fee recipient
- `creator` is the launching address and never changes; it is identity, not payout.
- `feeRecipient` starts as the creator and receives both the curve creator fees and the
  locker creator share — one recipient, one place.
- Change is two-step: the current recipient calls `proposeFeeRecipient(next)`, then `next`
  calls `acceptFeeRecipient()`. A pending proposal can be replaced or cancelled by the
  current recipient. No admin can propose, override or reverse. Balances already credited
  stay with whoever they were credited to: curve creator fees are credited per account at
  credit time, and each recipient claims only its own balance.

## 6. Graduation into Uniswap V4

### 6.1 Pool key
`currency0 = native ETH (address(0))`, `currency1 = token`, `fee = 10000`,
`tickSpacing = 200`, `hooks = GraduationGuardHook`. Fixed per deployment and snapshotted
per launch.

### 6.2 Atomic sequence (inside the final buy)
1. Mark the curve and token graduated (ends the transfer guard and curve trading).
2. Initialize the pool at the price implied by `graduationEth` and `lpTokens` — the same
   opening price v1 uses for its V2 pair. The guard hook admits only this curve.
3. Mint a full-range position (ticks −887200 to 887200) from `graduationEth` ETH and
   `lpTokens` tokens, directly owned by the locker.
4. Any rounding remainder goes to the protocol treasury, never to the creator and never
   left stranded: the ETH remainder joins the treasury's pull-claim balance; the token
   remainder is transferred to the treasury directly (a plain ERC-20 transfer of the
   curve's own token, which cannot fail or be blocked by the recipient).
5. Emit `Graduated` (§7).

If any step fails the whole final buy reverts; there is no partially graduated state.

### 6.3 Arithmetic hazards the implementation must close
- `sqrtPriceX96` for `lpTokens / graduationEth` overflows a naive `(lpTokens << 192)`;
  compute it with full-precision `mulDiv` and an integer square root, and pin it with
  vectors.
- Liquidity from amounts must use the same rounding as Uniswap's `LiquidityAmounts`, and
  the remainder accounting of step 4 must match the amounts actually settled.
- Direct donations of ETH or tokens to the `PoolManager` or the curve must not change the
  graduation outcome (tests in §9).

## 7. Events — backend contract

v1 events stay as defined. v2 changes and additions:

```solidity
event TokenLaunched(
    // every v1 field, in v1 order, except `lpPair` and `weth` (the pool uses native ETH), then:
    address hook,
    address locker,
    uint16 antiSnipeStartBps,
    uint32 antiSnipeDurationSeconds,
    uint16 lpCreatorShareBps
);

event Trade(
    // every v1 field, in v1 order, then:
    uint256 snipeTax
);

event Graduated(
    address indexed token,
    bytes32 indexed poolId,
    uint256 positionTokenId,
    uint256 ethToPool,
    uint256 tokensToPool,
    uint128 liquidity,
    uint160 sqrtPriceX96,
    uint256 ethRemainder,
    uint256 tokenRemainder
);

event SnipeTaxClaimed(address indexed token, address indexed treasury, uint256 amount);
event FeeRecipientProposed(address indexed token, address indexed current, address indexed next);
event FeeRecipientAccepted(address indexed token, address indexed previous, address indexed next);
event LpFeesCollected(address indexed token, uint256 ethAmount, uint256 tokenAmount);
event LpFeesClaimed(
    address indexed token, address indexed recipient, uint256 ethAmount, uint256 tokenAmount
);
```

- `Trade.ethGross` remains the ETH actually consumed and now includes `snipeTax`;
  `ethGross = snipeTax + protocolFee + creatorFee + ETH to the curve`. Volume stays
  `ethGross`, as in v1.
- Pool-phase trades are read from the singleton `PoolManager` `Swap` event filtered by the
  `poolId` topic; liquidity changes from `ModifyLiquidity`. Standard ERC-20 `Transfer`
  events are indexed as in v1.
- Final field order is fixed in the pre-flight review and then locked like v1's.

## 8. Backend and web impact

- **ABI registry:** engine v2 ABIs keyed by `(factory deployment, engineVersion = 2)`.
- **Indexer:** a V4 decoder for `PoolManager` `Swap`/`ModifyLiquidity` scoped by `poolId`;
  ingestion of the new events; the same canonical ledger and reorg handling.
- **Projections:** token launch config gains the anti-snipe profile and LP split; creator
  fee ledger gains locker balances (ETH and token) and the current `feeRecipient`;
  graduated v2 tokens get a pool registry row (`poolId`, position, hook).
- **Quote endpoint and `internal/curve`:** include `snipeTax`; stdlib-only rule unchanged.
- **Web:** quote and confirmation show the anti-snipe line and countdown; graduated v2
  tokens trade through the Universal Router V4 swap path with Permit2 for sells; the creator
  surface gains locker claims and the recipient change flow; launch facts describe v2
  rules. These are separate plan tasks.

## 9. Verification required before deployment

- Foundry unit tests for every path above, including the final buy with tax and refund.
- Invariants: curve accounting including the tax; the locker's position liquidity never
  decreases; only the registered curve can initialize a guarded pool; graduation is atomic.
- Fuzz: tax decay boundaries (`t = 0`, `t = duration`, after), rounding of tax, fee, and
  liquidity remainders.
- Mainnet-fork tests against the real `PoolManager`/`PositionManager`: launch → buys in and
  after the window → graduation → pool swaps → collect → claims → recipient change.
- Grief tests: a third party initializing the pool before graduation; tokens or ETH sent
  directly to the `PoolManager`, the pool or the curve; a recipient that rejects ETH.
- `contracts/vectors/v2/` generated by Solidity, consumed byte-identical by Go
  (`task verify` extended). Field names follow the v1 vector style (`output.snipeTax`,
  `output.taxBps`, a top-level `graduation` object); graduation values come from the
  `Graduated` event and are cross-checked against the pool's `slot0`.
- Slither clean or triaged; independent external audit before mainnet user funds.

## 10. Deployment prerequisites

- Gate 0 closed.
- V4 `PoolManager`, `PositionManager` and `Permit2` addresses for chain 4663 verified
  against live bytecode and the official deployments feed. Done: the reviewed record is
  `contracts/deployments/config/robinhood-mainnet-v4.json` (addresses, runtime code hashes,
  pinned fork block and hash, evidence). `PoolManager` matches the pinned build outside its
  immutable and `Permit2` matches the canonical bytecode outside its cached chain id and
  domain separator. `PositionManager` is a different source revision than the pinned
  periphery build, so it is identified by the feed, the docs, its code hash and its wiring
  (`poolManager()`, `permit2()`), and exercised end to end by the fork suite.
  `DeployLaunchpadV2` refuses any chain without such a record and any code hash drift.
- Guard hook address mined with only the `BEFORE_INITIALIZE` flag.
- Reviewed v2 deployment manifest with bytecode hashes, as for v1.

## 11. Open questions and risks

1. **Testnet path.** No V4 deployment was found for Robinhood testnet (46630). Decided in
   the plan: mainnet-fork tests only; the v2 deploy script refuses testnet.
2. **Aggregator routing.** Uniswap's routing API may route only allowlisted hooks. Verify
   whether a `beforeInitialize`-only hook is routed. If not, our UI routes directly through
   the Universal Router, but external aggregators may not show the pool.
3. **Collect liveness.** Fees move only when someone calls `collect`; it is permissionless
   and every claim collects first.
4. **Fee dilution.** Third-party liquidity dilutes the locker's fee share (§5.2).
5. **Default profile tuning.** 5000 bps over 60 s is a starting point; revisit with live
   launch data before changing the future default.
