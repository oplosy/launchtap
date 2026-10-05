# Contract Core — Design Spec

**Date:** 2026-09-01
**Status:** Design closed; implementation requires the normal high-risk pre-flight review
**Scope:** Factory, fixed-supply token, bonding-curve clone, fee custody, graduation,
Uniswap v2 integration, events, and contract-level invariants.
**Out of scope:** Backend implementation, frontend, forum, post-graduation protocol fees,
stock-token pairs, limit orders, referral fees, and upgradeable proxies.

This document is normative for contract behavior. The backend mirrors this design but
does not redefine it.

## 1. System and trust boundaries

- Users submit every launch, buy, sell, claim, and metadata authorization from their own
  wallet. No backend key signs user transactions and no backend process holds market funds.
- A non-upgradeable factory creates a non-upgradeable fixed-supply token and an EIP-1167
  curve clone for each launch.
- The curve clone holds the unsold token inventory, real curve ETH, and unclaimed fees.
  Virtual ETH and token reserves are accounting values, not separately deposited assets.
- Graduation wraps exactly the real curve ETH target, contributes the reserved token
  allocation to the canonical Uniswap v2 pair, and mints the launch LP position directly
  to an irrecoverable burn address.
- Factory governance can pause operations and change defaults for future launches. It
  cannot change an existing launch's economic parameters, implementation, pair, creator,
  fee recipients, or move its market reserves.

## 2. Components

### 2.1 `LaunchFactory`

Responsibilities:

- Holds versioned curve implementation addresses and future-launch defaults.
- Accrues launch fees in `launchFeesByTreasury[treasury]` for pull-based withdrawal, so a
  future treasury change cannot redirect previously accrued fees.
- Clones a curve, deploys a token, obtains the canonical Uniswap v2 pair, initializes the
  launch atomically, and optionally executes the developer buy.
- Emits the complete launch snapshot.
- Exposes separate emergency pause flags for new launches and curve buying.

Launch event ordering is fixed: token construction and its initial `Transfer(0, curve, S)`
may occur first; the factory then wires the canonical pair into the token
(`initializePair`) and initializes the curve; only after all three are initialized does it
emit `TokenLaunched`; only then may it execute the optional developer buy. Canonical pair
initialization on the token therefore completes before `TokenLaunched`, before any developer
buy, and before control returns to an external caller, so no token holder other than the
curve can exist while the pair address is still unset. Thus every `Trade` and `Graduated`
event has a preceding `TokenLaunched` in the same transaction, while the standard initial
mint `Transfer` may precede it.

The factory is non-upgradeable. A new factory deployment is required for factory logic
changes. A timelock-controlled registry may enable a new curve implementation version for
future launches; existing clones are unaffected.

### 2.2 `LaunchToken`

- ERC-20, 18 decimals, fixed supply. Minting occurs exactly once in the constructor and
  no mint or burn entrypoint exists afterward.
- The entire supply is minted to the curve. The curve sells `T_r` during the curve phase
  and retains `L` for graduation.
- Stores the curve and canonical pair once. Those addresses cannot be changed.
- Before graduation, ordinary transfers between users are allowed, but transfers into or
  out of the curve or canonical pair are allowed only when initiated by the curve. This
  prevents direct token donation from corrupting curve inventory and prevents a third
  party from seeding the canonical pair before graduation.
- The curve calls `markGraduated()` once. After that transition, the pair restriction is
  removed and the token behaves as an ordinary fixed-supply ERC-20.

The transfer restriction is part of the token's public behavior and must be documented to
users; the token is not a completely unrestricted ERC-20 during the curve phase.
The initial mint from the zero address is exempt from the curve-entry restriction.

### 2.3 `BondingCurveV1`

- Standard EIP-1167 minimal clone with independent storage.
- Factory calls `initialize` in the same transaction that creates the clone. Initialization
  is single-use; the implementation contract disables initialization in its constructor.
- Per-launch fields are **write-once initialized storage**, not Solidity `immutable`
  variables. No setter exists for them.
- Phase is `Curve` or `Graduated`; the transition is one-way.
- Buy, sell, fee-claim, and graduation entrypoints are reentrancy-protected and update
  internal state before any ETH transfer or external protocol interaction.

## 3. Launch parameters and validation

Default V1 values:

| Parameter | Symbol | Value |
|---|---:|---:|
| Total supply | `S` | `1_000_000_000e18` |
| Curve allocation | `T_r` | `800_000_000e18` |
| LP allocation | `L` | `200_000_000e18` |
| Graduation ETH | `G` | `4.2 ether` |
| Initial virtual ETH | `x0` | `1.4 ether` |
| Initial virtual token | `y0` | `1_066_666_666_666_666_666_666_666_667` base units |
| Trade fee | `feeBps` | `100` |
| Protocol share of fee | `protocolShareBps` | `5_000` |
| Launch fee | — | `0.0005 ether` |
| Developer-buy cap | — | `1%` of `S` |

All stored values are integers in wei/token base units. At the V1 defaults `y0` is the
ceiling of the rational derivation. It produces `yFinal =
266_666_666_666_666_666_666_666_667`, `xFinal = 5.6 ether`, and
`xFinal - x0 = G` exactly. A floor-rounded `y0` misses the graduation target by one wei and
is invalid.

Initialization rejects a parameter set unless all of the following hold:

- `S = T_r + L`, `T_r > L > 0`, `G > 0`, and `y0 > T_r`.
- `feeBps < 10_000` and `protocolShareBps <= 10_000`.
- `K = x0 * y0`, `yFinal = y0 - T_r`, `xFinal = x0 + G`,
  `ceilDiv(K, yFinal) = xFinal`, and `ceilDiv(K, xFinal) = yFinal` exactly in integer
  units. This makes the integer graduation boundary reversible under the two quote formulas.
- Token, creator, treasury, WETH, pair, factory, and implementation addresses are nonzero
  and the pair contains exactly the token and configured WETH.
- The pair is the address returned by the configured Uniswap v2 factory for token/WETH.

Defaults may change through timelocked factory governance for future launches. Every value,
including the engine version, treasury, WETH, and pair, is snapshotted at launch.

For an optional developer buy, the factory separates `launchFee` from the supplied value,
calls the curve's factory-only launch-buy path with creator as trader, token recipient, and
refund recipient, and requires `tokensOut <= 1% of S`. The resulting `Trade.trader` is the
creator, not the factory. Launch input declares `developerBuyGross` and requires
`msg.value = launchFee + developerBuyGross`; ambiguous overpayment is rejected.

## 4. Curve arithmetic

State:

```text
x = virtual ETH reserve
y = virtual token reserve
K = x0 * y0, constant for the launch
realCurveEth = x - x0
tokensSold = y0 - y
```

All arithmetic uses unsigned 256-bit integers. Every multiplication is checked for overflow.
`ceilDiv(a, b)` is defined as `a / b + (a % b == 0 ? 0 : 1)` and rejects `b = 0`.

### 4.1 Buy

For the gross ETH amount actually consumed by the trade:

```text
totalFee   = floor(ethGrossUsed * feeBps / 10_000)
protocolFee = floor(totalFee * protocolShareBps / 10_000)
creatorFee  = totalFee - protocolFee
dxEffective = ethGrossUsed - totalFee
newX        = x + dxEffective
newY        = ceilDiv(K, newX)
tokensOut   = y - newY
```

Rounding therefore never sends more tokens than the invariant permits. A buy rejects zero
output and enforces the caller's `minTokensOut` and `deadline`.

A buy that does not cross the graduation boundary consumes the entire supplied value:
`ethGross` equals `ethGrossUsed` and the reported `ethRefund` is `0`.

### 4.2 Final buy and refund

A buy is not allowed to move below `yFinal = y0 - T_r`. If the submitted value would cross
the boundary, the contract:

1. Calculates `dxNeeded = ceilDiv(K, yFinal) - x`.
2. Calculates the smallest exact gross amount:

   ```text
   ethGrossUsed = floor((dxNeeded - 1) * 10_000 / (10_000 - feeBps)) + 1
   ```

   For `dxNeeded > 0` and `feeBps < 10_000`, this guarantees
   `ethGrossUsed - floor(ethGrossUsed * feeBps / 10_000) = dxNeeded`.
3. Uses only that gross amount, sets `ethGross = ethGrossUsed` and
   `ethRefund = suppliedValue - ethGrossUsed`, sets `y = yFinal`, and graduates in the same
   transaction.
4. Returns `ethRefund` to the caller. When that immediate transfer succeeds, the excess is
   recorded only by `Trade.ethRefund`. When it fails, `Trade.ethRefund` still carries the
   same value and the amount is additionally recorded as a pull-based pending refund with a
   `RefundCredited` event.

For every buy, `ethGross + ethRefund` equals the ETH supplied to the curve for that trade —
`msg.value` on a direct buy, and the curve-call value on a developer buy or an
aggregator/smart-account buy. Executed market volume is `ethGross` only; `ethRefund` is
never counted as volume.

The gross-for-exact-net helper uses checked/full-precision multiplication and is tested over
the full supported fee range; it must never silently overfill or change `G`.

### 4.3 Sell

```text
newY       = y + tokensIn
newX       = ceilDiv(K, newY)
ethGross   = x - newX
totalFee   = floor(ethGross * feeBps / 10_000)
protocolFee = floor(totalFee * protocolShareBps / 10_000)
creatorFee  = totalFee - protocolFee
ethOut      = ethGross - totalFee
```

The contract rejects `tokensIn = 0` and `tokensIn > tokensSold`; virtual inventory can
never be redeemed for real ETH. It pulls the tokens, updates state and fee accruals, then
sends ETH. The caller's `minEthOut` and `deadline` are enforced on-chain.

A sell has no excess input, so its `Trade` event always reports `ethRefund = 0`, including
when delivery of `ethOut` fails and becomes a pull-based pending refund with a
`RefundCredited` event.

### 4.4 Accounting invariant

At every successful external-call boundary during the curve phase:

```text
address(this).balance
  >= realCurveEth
   + unclaimedCreatorFees
   + unclaimedProtocolFees
   + pendingRefunds
```

The preferred path returns refunds immediately. If an ETH transfer fails, the amount is
recorded as a pull-based pending refund; this applies to final-buy excess and sell proceeds.
`Trade.ethRefund` records final-buy excess on both the immediate and the failed path (§4.2,
§7).
A failed recipient cannot block market state. Forced ETH above accounted balances is ignored
by curve/fee/graduation math and remains permanently inaccessible because there is no rescue
function.

## 5. Fees

- Launch fees accrue in the factory and are withdrawn by the snapshotted protocol treasury.
- Creator and protocol trade fees accrue separately in each curve clone.
- `claimCreatorFees`, `claimProtocolFees`, and `claimRefund` use pull payment, checks-effects-
  interactions, and reentrancy protection. `claimCreatorFeesTo` and `claimRefundTo` let the
  same authorized caller pay out to another address, so an account that cannot receive ETH
  does not lose its balance; the claim events still name the credited account.
- Claims remain available while launches or trading are paused and after graduation.
- Claim failure affects only that claim. It cannot make buy, sell, or graduation unavailable.
- V1 takes no fee from the post-graduation Uniswap market.

## 6. Graduation and pair-grief handling

Graduation occurs inside the final buy transaction after the curve state reaches exactly
`tokensSold = T_r` and `realCurveEth = G`.

The factory creates or resolves the canonical Uniswap v2 token/WETH pair during launch,
before control returns to an external caller. Because Uniswap pair creation is permissionless,
the design assumes the pair may already exist.

Before graduation the curve verifies:

- Pair factory, `token0`, and `token1` match the snapshotted deployment.
- Pair `totalSupply == 0`.
- The pair's launched-token reserve and balance are zero. The token's transfer restriction
  makes a nonzero value an invariant violation.

Curve-operated transfers are exempt from the token restriction, so `buy`/`buyFor` reject the
canonical pair and the curve itself as `tokenRecipient` (`InvalidRecipient`). Without this,
a dust buy delivered to the pair could never be skimmed and would make every final buy revert
with `PairTokenBalanceNotZero`, freezing graduation permanently (2026-10-05 review).

A third party can donate WETH to an empty pair and call `sync`. Such a donation cannot be
withdrawn through this launch and does not reduce launch-owned funds. V1 accepts the donation
as extra pool ETH rather than allowing it to permanently block graduation. Consequently the
no-price-gap property is exact when there is no donation; a WETH donation can only raise the
opening pool price. `Sync` is the authoritative opening-reserve record.

The curve does **not** call Router02 for graduation. It performs the exact sequence:

1. Set phase to `Graduated` and mark the token graduated.
2. Wrap exactly `G` native ETH into the snapshotted WETH.
3. Transfer exactly `L` token units and `G` WETH units to the canonical pair.
4. Call `pair.mint(LP_BURN_ADDRESS)` directly.
5. Require returned liquidity to be nonzero and emit `Graduated`.

`LP_BURN_ADDRESS` is a protocol constant (`0x000000000000000000000000000000000000dEaD`).
The launch contract never receives or approves the LP token. “LP burned” refers to this
initial launch position; anyone may add independent liquidity later and own the LP minted
for that later contribution.

## 7. Events — backend contract

```solidity
event TokenLaunched(
    address indexed token,
    address indexed curve,
    address indexed creator,
    address lpPair,
    address weth,
    address protocolTreasury,
    uint16 engineVersion,
    string name,
    string symbol,
    uint256 totalSupply,
    uint256 virtualEth,
    uint256 virtualToken,
    uint256 curveTokens,
    uint256 lpTokens,
    uint256 graduationEth,
    uint256 launchFeePaid,
    uint16 tradeFeeBps,
    uint16 protocolShareBps
);

event Trade(
    address indexed token,
    address indexed trader,
    bool isBuy,
    uint256 ethGross,
    uint256 ethRefund,
    uint256 tokenAmount,
    uint256 protocolFee,
    uint256 creatorFee,
    uint256 newEthReserve,
    uint256 newTokenReserve
);

event Graduated(
    address indexed token,
    address indexed lpPair,
    uint256 ethToPool,
    uint256 tokensToPool,
    uint256 lpLiquidityBurned
);

event CreatorFeesClaimed(
    address indexed token, address indexed creator, uint256 amount
);
event ProtocolFeesClaimed(
    address indexed token, address indexed treasury, uint256 amount
);
event LaunchFeesClaimed(address indexed treasury, uint256 amount);
event RefundCredited(address indexed token, address indexed account, uint256 amount);
event RefundClaimed(address indexed token, address indexed account, uint256 amount);
event LaunchPauseSet(bool paused);
event TradingPauseSet(bool paused);
event EngineConfigured(uint16 indexed engineVersion, address indexed implementation, bool enabled);
event FutureDefaultsConfigured(bytes32 indexed configHash);
event FutureTreasuryConfigured(address indexed previousTreasury, address indexed newTreasury);
```

`Trade.ethGross` is the ETH actually consumed by the trade; `Trade.ethRefund` is the excess
returned to the buyer, placed directly after `ethGross`. By path:

- Normal buy, no graduation boundary crossed: `ethRefund = 0`.
- Final buy, immediate refund succeeds: `ethRefund` is the excess above `ethGrossUsed`.
- Final buy, immediate refund fails: the same `ethRefund`, plus a `RefundCredited` event for
  the pull-payment balance.
- Any sell: `ethRefund = 0`, including when `ethOut` delivery fails and is credited.

For every buy, the ETH supplied to the curve for that trade equals `ethGross + ethRefund`.
Executed/gross market volume is `ethGross` only; `ethRefund` is never added to volume.

Events contain no timestamp; block headers are authoritative. Standard ERC-20 `Transfer`
and Uniswap v2 `Mint`, `Burn`, `Swap`, and `Sync` events are also indexed. Event consumers
select the ABI by `(factory deployment, engineVersion)`. Governance events are required for
auditability but are not part of V1 market aggregation.

## 8. Administration and emergency behavior

- Immediate pause authority: a multisig. `pauseLaunches` blocks launch creation;
  `pauseTrading` blocks buys (including the developer buy) but never sells, so holders can
  always exit to the curve while an incident is handled (product decision 2026-10-05).
  Neither flag blocks fee/refund claims.
- Timelock authority: future defaults, enabling a new engine implementation, and future
  launch treasury configuration.
- Existing clone parameters and implementation are immutable by interface. There is no
  rescue, arbitrary-call, token-sweep, reserve-withdrawal, or admin-graduation function.
- Graduation is reached only through curve state. The administrator cannot force a launch
  to graduate or redirect its liquidity.
- The factory takes its final `pauseAuthority` (multisig) and `timelock` addresses as
  constructor arguments and stores them at construction. The deployer holds no pause,
  timelock, upgrade, or ownership power at any point, and there is no post-deployment
  authority-handover step. A production deployment is valid only if these addresses are the
  reviewed multisig and timelock and the constructor rejects zero addresses.

## 9. Required verification before deployment

- Unit and fuzz tests for every rounding boundary, zero output, maximum sell, final-buy
  refund, fee split, and accounting invariant.
- Stateful invariant tests proving supply is fixed, `x*y >= K`, `y >= yFinal`, real curve
  ETH cannot become negative, and graduation occurs at most once.
- Adversarial tests for direct token donations, pre-created pair, WETH-only pair donation,
  pair with existing LP supply, reverting ETH recipients, reentrancy, paused operations,
  and claim availability while paused.
- Differential vectors generated by the Solidity implementation and consumed unchanged by
  the Go mirror.
- Fork test against the exact Robinhood mainnet Uniswap v2 Factory, Pair bytecode, and WETH.
- Slither, compiler warnings as errors where supported, independent model review, and an
  external audit before mainnet funds are accepted.

## 10. Deployment-specific prerequisites

- Robinhood mainnet WETH, Uniswap v2 Factory, and Router addresses are known, but graduation
  uses Factory/Pair/WETH rather than Router.
- Robinhood testnet does not reuse the verified mainnet addresses. A project-owned test
  Uniswap v2 Factory/WETH deployment or separately verified official deployment must be
  recorded before testnet graduation is enabled.
- Each deployment produces a versioned manifest containing chain id, factory, start block,
  engine version, curve implementation, Uniswap factory, WETH, pair init-code hash, burn
  address, deploy transaction, and bytecode hashes. Backend and frontend embed the reviewed
  manifest; environment variables may select a deployment but may not silently override its
  addresses.

## 11. Primary references checked for design closure

- EIP-1167 minimal proxy: https://eips.ethereum.org/EIPS/eip-1167
- OpenZeppelin initialization guidance:
  https://docs.openzeppelin.com/upgrades-plugins/writing-upgradeable
- Solidity integer arithmetic and security considerations:
  https://docs.soliditylang.org/en/latest/types.html and
  https://docs.soliditylang.org/en/latest/security-considerations.html
- Uniswap v2 Factory, Pair, and Router02 behavior:
  https://github.com/Uniswap/v2-core/blob/master/contracts/UniswapV2Factory.sol,
  https://github.com/Uniswap/v2-core/blob/master/contracts/UniswapV2Pair.sol, and
  https://github.com/Uniswap/v2-periphery/blob/master/contracts/UniswapV2Router02.sol
- Robinhood and Uniswap deployment records:
  https://docs.robinhood.com/chain/contracts/ and
  https://developers.uniswap.org/docs/protocols/v2/deployments
