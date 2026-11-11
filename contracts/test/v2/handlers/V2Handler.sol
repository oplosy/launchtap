// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Test } from "forge-std/Test.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { SafeCast } from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { TickMath } from "@uniswap/v4-core/src/libraries/TickMath.sol";
import { PoolSwapTest } from "@uniswap/v4-core/src/test/PoolSwapTest.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { SwapParams } from "@uniswap/v4-core/src/types/PoolOperation.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { LaunchTypes } from "../../../src/types/LaunchTypes.sol";
import { LpFeeLocker } from "../../../src/v2/LpFeeLocker.sol";
import { IBondingCurveV2 } from "../../../src/v2/interfaces/IBondingCurveV2.sol";

// Handler actions discard trade/claim results on purpose; the invariants read state instead.
// The treasury is a fixed test constant.
// forge-lint: disable-start(unused-return, missing-zero-check)
/// @notice Drives one v2 launch through its whole life: taxed and untaxed curve trades,
/// claims, recipient changes, graduation, pool swaps and LP fee collection, plus attempts to
/// open the guarded pool from outside the curve. Every action is bounded so it cannot revert.
contract V2Handler is Test {
    uint256 private constant MIN_SELL_TOKENS = 1e15;
    address private constant ATTACKER = address(0xBAD);

    IBondingCurveV2 public immutable CURVE;
    IERC20 public immutable TOKEN;
    IPoolManager public immutable POOL_MANAGER;
    IPositionManager public immutable POSITION_MANAGER;
    LpFeeLocker public immutable LOCKER;
    PoolSwapTest public immutable SWAP_ROUTER;
    address public immutable TREASURY;
    PoolKey internal _key;

    address[] internal _actors;

    uint256 public forcedEth;
    uint128 public lastLiquidity;
    bool public liquidityDecreased;
    bool public foreignInitializeSucceeded;
    uint256 public graduations;
    uint256 public poolSwaps;
    uint256 public taxedBuys;

    constructor(
        IBondingCurveV2 curve,
        IPoolManager poolManager,
        IPositionManager positionManager,
        LpFeeLocker locker,
        PoolKey memory key,
        address treasury,
        address[] memory actors_
    ) {
        CURVE = curve;
        TOKEN = IERC20(curve.token());
        POOL_MANAGER = poolManager;
        POSITION_MANAGER = positionManager;
        LOCKER = locker;
        SWAP_ROUTER = new PoolSwapTest(poolManager);
        TREASURY = treasury;
        _key = key;
        _actors = actors_;
    }

    function actors() external view returns (address[] memory) {
        return _actors;
    }

    function buy(uint256 actorSeed, uint256 amountSeed, uint256 elapsedSeed) external {
        if (_graduated()) return;
        vm.warp(block.timestamp + bound(elapsedSeed, 0, 20));
        address actor = _actor(actorSeed);
        uint256 amount = bound(amountSeed, 1e9, 3 ether);
        if (CURVE.currentTaxBps() != 0) ++taxedBuys;

        vm.deal(actor, actor.balance + amount);
        vm.prank(actor);
        CURVE.buy{ value: amount }(actor, actor, 0, block.timestamp);
        _afterAction();
    }

    function sell(uint256 actorSeed, uint256 amountSeed) external {
        if (_graduated()) return;
        address actor = _actor(actorSeed);
        uint256 balance = TOKEN.balanceOf(actor);
        if (balance < MIN_SELL_TOKENS) return;
        uint256 tokensIn = bound(amountSeed, MIN_SELL_TOKENS, balance);

        vm.startPrank(actor);
        TOKEN.approve(address(CURVE), tokensIn);
        CURVE.sell(tokensIn, actor, 0, block.timestamp);
        vm.stopPrank();
        _afterAction();
    }

    function graduate(uint256 actorSeed) external {
        if (_graduated()) return;
        address actor = _actor(actorSeed);
        vm.deal(actor, actor.balance + 10 ether);
        vm.prank(actor);
        CURVE.buy{ value: 10 ether }(actor, actor, 0, block.timestamp);
        _afterAction();
    }

    function claimCreatorFees(uint256 actorSeed) external {
        address actor = _actor(actorSeed);
        if (CURVE.creatorFeesOwed(actor) == 0) return;
        vm.prank(actor);
        CURVE.claimCreatorFees();
        _afterAction();
    }

    function claimTreasury(bool snipeTax) external {
        vm.startPrank(TREASURY);
        if (snipeTax && CURVE.unclaimedSnipeTax() != 0) CURVE.claimSnipeTax();
        if (!snipeTax && CURVE.unclaimedProtocolFees() != 0) CURVE.claimProtocolFees();
        vm.stopPrank();
        _afterAction();
    }

    function changeFeeRecipient(uint256 actorSeed) external {
        address next = _actor(actorSeed);
        address current = CURVE.feeRecipient();
        if (next == current) return;
        vm.prank(current);
        CURVE.proposeFeeRecipient(next);
        vm.prank(next);
        CURVE.acceptFeeRecipient();
        _afterAction();
    }

    function swapEthForToken(uint256 actorSeed, uint256 amountSeed) external {
        if (!_graduated()) return;
        address actor = _actor(actorSeed);
        uint256 amount = bound(amountSeed, 1e12, 1 ether);
        vm.deal(actor, actor.balance + amount);
        vm.prank(actor);
        SWAP_ROUTER.swap{ value: amount }(_key, _swapParams(true, amount), _settings(), "");
        ++poolSwaps;
        _afterAction();
    }

    function swapTokenForEth(uint256 actorSeed, uint256 amountSeed) external {
        if (!_graduated()) return;
        address actor = _actor(actorSeed);
        uint256 balance = TOKEN.balanceOf(actor);
        if (balance < MIN_SELL_TOKENS) return;
        uint256 amount = bound(amountSeed, MIN_SELL_TOKENS, balance);

        vm.startPrank(actor);
        TOKEN.approve(address(SWAP_ROUTER), amount);
        SWAP_ROUTER.swap(_key, _swapParams(false, amount), _settings(), "");
        vm.stopPrank();
        ++poolSwaps;
        _afterAction();
    }

    function collectLpFees() external {
        if (!_graduated()) return;
        LOCKER.collect(address(TOKEN));
        _afterAction();
    }

    function claimLpFees(uint256 actorSeed, bool asTreasury) external {
        if (!_graduated()) return;
        vm.prank(asTreasury ? TREASURY : _actor(actorSeed));
        LOCKER.claim(address(TOKEN));
        _afterAction();
    }

    /// @dev Tries to open a guarded pool for this token from outside the curve, with the
    /// canonical key or a variant of it. The guard must reject every attempt.
    function initializeFromOutside(uint256 variantSeed, uint256 priceSeed) external {
        PoolKey memory key = _key;
        uint256 variant = variantSeed % 3;
        if (variant == 1) key.fee = 3000;
        if (variant == 2) key.tickSpacing = 60;
        uint160 sqrtPrice = SafeCast.toUint160(
            bound(priceSeed, TickMath.MIN_SQRT_PRICE, TickMath.MAX_SQRT_PRICE - 1)
        );

        vm.prank(ATTACKER);
        try POOL_MANAGER.initialize(key, sqrtPrice) {
            foreignInitializeSucceeded = true;
        } catch { }
        _afterAction();
    }

    function forceEthIntoCurve(uint256 amountSeed) external {
        uint256 amount = bound(amountSeed, 1, 1 ether);
        forcedEth += amount;
        vm.deal(address(CURVE), address(CURVE).balance + amount);
        _afterAction();
    }

    function _afterAction() private {
        if (!_graduated()) return;
        if (graduations == 0) graduations = 1;
        uint128 liquidity = POSITION_MANAGER.getPositionLiquidity(LOCKER.positionOf(address(TOKEN)));
        if (liquidity < lastLiquidity) liquidityDecreased = true;
        lastLiquidity = liquidity;
    }

    function _graduated() private view returns (bool) {
        return CURVE.phase() == LaunchTypes.Phase.Graduated;
    }

    function _actor(uint256 seed) private view returns (address) {
        return _actors[seed % _actors.length];
    }

    function _swapParams(bool zeroForOne, uint256 amountIn)
        private
        pure
        returns (SwapParams memory)
    {
        return SwapParams({
            zeroForOne: zeroForOne,
            amountSpecified: -SafeCast.toInt256(amountIn),
            sqrtPriceLimitX96: zeroForOne
                ? TickMath.MIN_SQRT_PRICE + 1
                : TickMath.MAX_SQRT_PRICE - 1
        });
    }

    function _settings() private pure returns (PoolSwapTest.TestSettings memory) {
        return PoolSwapTest.TestSettings({ takeClaims: false, settleUsingBurn: false });
    }
}
// forge-lint: disable-end(unused-return, missing-zero-check)
