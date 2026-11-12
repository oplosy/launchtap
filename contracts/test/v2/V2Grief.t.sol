// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { SafeCast } from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { StateLibrary } from "@uniswap/v4-core/src/libraries/StateLibrary.sol";
import { TickMath } from "@uniswap/v4-core/src/libraries/TickMath.sol";
import { PoolSwapTest } from "@uniswap/v4-core/src/test/PoolSwapTest.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { SwapParams } from "@uniswap/v4-core/src/types/PoolOperation.sol";
import { ILaunchErrors } from "../../src/interfaces/ILaunchErrors.sol";
import { IBondingCurveV2 } from "../../src/v2/interfaces/IBondingCurveV2.sol";
import { V2SystemBase } from "./utils/V2SystemBase.sol";

/// @notice Runs two identical launches, disturbs one of them before graduation in every way an
/// outsider can, and requires both to graduate into identical pools and positions.
// forge-lint: disable-start(unused-return, erc20-unchecked-transfer, arbitrary-send-eth, calls-loop)
contract V2GriefTest is V2SystemBase {
    using StateLibrary for *;

    address internal constant ALICE = address(0xA11CE);
    address internal constant GRIEFER = address(0xBAD);
    uint256 internal constant GRIEFER_BUY = 0.5 ether;

    IBondingCurveV2 internal cleanCurve;
    IERC20 internal cleanToken;
    IBondingCurveV2 internal griefCurve;
    IERC20 internal griefToken;
    PoolSwapTest internal swapRouter;

    function setUp() public {
        deployV2System();
        (address token, address curve) = launchToken("CLEAN");
        (cleanToken, cleanCurve) = (IERC20(token), IBondingCurveV2(curve));
        (token, curve) = launchToken("GRIEF");
        (griefToken, griefCurve) = (IERC20(token), IBondingCurveV2(curve));
        swapRouter = new PoolSwapTest(poolManager);
        vm.warp(LAUNCH_TIMESTAMP + SNIPE_DURATION);
        vm.deal(ALICE, 100 ether);
        vm.deal(GRIEFER, 100 ether);
    }

    function test_outsiderCannotOpenGuardedPool() public {
        PoolKey memory key = poolKeyOf(address(griefToken));
        address[3] memory outsiders = [GRIEFER, CREATOR, address(factory)];
        for (uint256 i = 0; i < outsiders.length; ++i) {
            vm.prank(outsiders[i]);
            vm.expectRevert();
            poolManager.initialize(key, TickMath.MIN_SQRT_PRICE);
        }
        // The clean token's curve cannot open the grief token's pool either.
        vm.prank(address(cleanCurve));
        vm.expectRevert();
        poolManager.initialize(key, TickMath.MIN_SQRT_PRICE);
    }

    function test_disturbedLaunch_graduatesIdentically() public {
        _buy(cleanCurve, GRIEFER, GRIEFER_BUY);
        uint256 griefTokens = _buy(griefCurve, GRIEFER, GRIEFER_BUY);
        _disturb(griefTokens);

        _buy(cleanCurve, ALICE, 10 ether);
        _buy(griefCurve, ALICE, 10 ether);

        (uint160 cleanPrice,,,) = poolManager.getSlot0(poolKeyOf(address(cleanToken)).toId());
        (uint160 griefPrice,,,) = poolManager.getSlot0(poolKeyOf(address(griefToken)).toId());
        assertGt(cleanPrice, 0);
        assertEq(griefPrice, cleanPrice);
        assertEq(_liquidity(griefToken), _liquidity(cleanToken));
        assertEq(
            griefToken.balanceOf(address(poolManager)), cleanToken.balanceOf(address(poolManager))
        );
        assertEq(griefToken.balanceOf(TREASURY), cleanToken.balanceOf(TREASURY));
        // Forced ETH stays outside the books: both curves owe exactly the same fees.
        assertEq(griefCurve.unclaimedProtocolFees(), cleanCurve.unclaimedProtocolFees());
        assertEq(griefCurve.unclaimedCreatorFees(), cleanCurve.unclaimedCreatorFees());

        // Identical swaps then accrue identical LP fees, despite the locker donations.
        _swapBothWays(cleanToken);
        _swapBothWays(griefToken);
        (uint256 cleanEth, uint256 cleanTokens) = locker.collect(address(cleanToken));
        (uint256 griefEth, uint256 griefTokensCollected) = locker.collect(address(griefToken));
        assertGt(cleanEth, 0);
        assertGt(cleanTokens, 0);
        assertEq(griefEth, cleanEth);
        assertEq(griefTokensCollected, cleanTokens);
        assertEq(_liquidity(griefToken), _liquidity(cleanToken));
    }

    /// @dev Every pre-graduation action an outsider can take against the grief launch.
    function _disturb(uint256 griefTokens) internal {
        PoolKey memory key = poolKeyOf(address(griefToken));

        vm.startPrank(GRIEFER);
        vm.expectRevert();
        poolManager.initialize(key, TickMath.MIN_SQRT_PRICE + 1);

        vm.expectRevert(IPoolManager.ManagerLocked.selector);
        poolManager.donate(key, 1 ether, 1 ether, "");

        // `sync` is callable while locked; it only writes transient reserves.
        poolManager.sync(Currency.wrap(address(griefToken)));
        poolManager.sync(Currency.wrap(address(0)));

        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.TransferRestricted.selector, GRIEFER, GRIEFER, address(poolManager)
            )
        );
        griefToken.transfer(address(poolManager), griefTokens / 4);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.TransferRestricted.selector, GRIEFER, GRIEFER, address(griefCurve)
            )
        );
        griefToken.transfer(address(griefCurve), griefTokens / 4);

        (bool accepted,) = address(griefCurve).call{ value: 1 ether }("");
        assertFalse(accepted);
        (accepted,) = address(locker).call{ value: 1 ether }("");
        assertFalse(accepted);
        assertTrue(griefToken.transfer(address(locker), griefTokens / 4));
        assertTrue(griefToken.transfer(address(hook), griefTokens / 4));
        vm.stopPrank();

        // Balances that cannot be refused (selfdestruct, coinbase) are forced in directly.
        vm.deal(address(poolManager), address(poolManager).balance + 5 ether);
        vm.deal(address(griefCurve), address(griefCurve).balance + 2 ether);
        vm.deal(address(locker), address(locker).balance + 1 ether);
    }

    function _buy(IBondingCurveV2 curve, address trader, uint256 ethIn)
        internal
        returns (uint256 tokensOut)
    {
        vm.prank(trader);
        (tokensOut,) = curve.buy{ value: ethIn }(trader, trader, 0, block.timestamp);
    }

    function _swapBothWays(IERC20 token) internal {
        PoolKey memory key = poolKeyOf(address(token));
        PoolSwapTest.TestSettings memory settings =
            PoolSwapTest.TestSettings({ takeClaims: false, settleUsingBurn: false });

        vm.startPrank(ALICE);
        swapRouter.swap{ value: 1 ether }(
            key,
            SwapParams({
                zeroForOne: true,
                amountSpecified: -SafeCast.toInt256(1 ether),
                sqrtPriceLimitX96: TickMath.MIN_SQRT_PRICE + 1
            }),
            settings,
            ""
        );
        uint256 tokensIn = 50_000_000 ether;
        token.approve(address(swapRouter), tokensIn);
        swapRouter.swap(
            key,
            SwapParams({
                zeroForOne: false,
                amountSpecified: -SafeCast.toInt256(tokensIn),
                sqrtPriceLimitX96: TickMath.MAX_SQRT_PRICE - 1
            }),
            settings,
            ""
        );
        vm.stopPrank();
    }

    function _liquidity(IERC20 token) internal view returns (uint128) {
        return positionManager.getPositionLiquidity(locker.positionOf(address(token)));
    }
}
// forge-lint: disable-end(unused-return, erc20-unchecked-transfer, arbitrary-send-eth, calls-loop)
