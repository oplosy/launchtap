// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IERC721 } from "@openzeppelin/contracts/token/ERC721/IERC721.sol";
import { SafeCast } from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import { Pool } from "@uniswap/v4-core/src/libraries/Pool.sol";
import { StateLibrary } from "@uniswap/v4-core/src/libraries/StateLibrary.sol";
import { TickMath } from "@uniswap/v4-core/src/libraries/TickMath.sol";
import { PoolSwapTest } from "@uniswap/v4-core/src/test/PoolSwapTest.sol";
import { PoolId } from "@uniswap/v4-core/src/types/PoolId.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { SwapParams } from "@uniswap/v4-core/src/types/PoolOperation.sol";
import { AntiSnipe } from "../../src/libraries/AntiSnipe.sol";
import { CurveMath } from "../../src/libraries/CurveMath.sol";
import { GraduationMath } from "../../src/libraries/GraduationMath.sol";
import { ILaunchErrors } from "../../src/interfaces/ILaunchErrors.sol";
import { LaunchTypes } from "../../src/types/LaunchTypes.sol";
import { ILaunchEventsV2 } from "../../src/v2/interfaces/ILaunchEventsV2.sol";
import { BondingCurveV2TestBase } from "./utils/BondingCurveV2TestBase.sol";

// Quotes and pool reads are compared field by field; unused tuple members are intentional.
// Test contracts send ETH to fixed test routers and assert on events/reverts directly.
// forge-lint: disable-start(unused-return, arbitrary-send-eth, reentrancy-events, erc20-unchecked-transfer)
contract BondingCurveV2GraduationTest is BondingCurveV2TestBase {
    using StateLibrary for *;

    address internal constant ATTACKER = address(0xBAD);

    function test_finalBuyInWindow_taxOnlyOnUsedEth() public {
        vm.warp(LAUNCH_TIMESTAMP + 30);
        uint16 taxBps = curve.currentTaxBps();
        assertEq(taxBps, 2500);

        uint256 supplied = 20 ether;
        (, uint256 net) = AntiSnipe.split(supplied, taxBps);
        CurveMath.BuyQuote memory v1 = _v1QuoteBuy(net);
        assertTrue(v1.graduates);
        uint256 expectedGross = AntiSnipe.grossForNet(v1.ethGrossUsed, taxBps);
        uint256 expectedTax = expectedGross - v1.ethGrossUsed;

        (uint256 used, uint256 out,,, uint256 tax, uint256 refund, bool graduates) =
            curve.quoteBuy(supplied);
        assertTrue(graduates);
        assertEq(used, expectedGross);
        assertEq(tax, expectedTax);
        assertEq(refund, supplied - expectedGross);
        assertEq(out, CURVE_TOKENS);
        // Tax is charged on consumed ETH only: the refund is never taxed.
        assertLe(tax, (expectedGross * taxBps) / 10_000 + 1);

        uint256 balanceBefore = ALICE.balance;
        _buy(ALICE, supplied);
        assertEq(balanceBefore - ALICE.balance, expectedGross);
        assertEq(curve.unclaimedSnipeTax(), expectedTax);
        assertEq(uint8(curve.phase()), uint8(LaunchTypes.Phase.Graduated));
    }

    function test_graduate_initializesGuardedPoolAtExpectedPrice() public {
        vm.warp(LAUNCH_TIMESTAMP + SNIPE_DURATION);
        uint160 expectedSqrtPrice = GraduationMath.sqrtPriceX96(GRADUATION_ETH, LP_TOKENS);
        (uint128 expectedLiquidity, uint256 ethUsed, uint256 tokenUsed) =
            GraduationMath.fullRangePosition(expectedSqrtPrice, GRADUATION_ETH, LP_TOKENS);
        uint256 positionId = positionManager.nextTokenId();
        PoolKey memory key = _poolKey();
        PoolId poolId = key.toId();
        uint256 protocolFeesBefore = curve.unclaimedProtocolFees();
        (,, uint256 finalProtocolFee,,,,) = curve.quoteBuy(10 ether);

        vm.expectEmit(true, true, false, true, address(curve));
        emit ILaunchEventsV2.Graduated(
            address(token),
            PoolId.unwrap(poolId),
            positionId,
            ethUsed,
            tokenUsed,
            expectedLiquidity,
            expectedSqrtPrice,
            GRADUATION_ETH - ethUsed,
            LP_TOKENS - tokenUsed
        );
        _buy(ALICE, 10 ether);

        (uint160 sqrtPrice,,,) = poolManager.getSlot0(poolId);
        assertEq(sqrtPrice, expectedSqrtPrice);
        assertEq(locker.positionOf(address(token)), positionId);
        assertEq(IERC721(address(positionManager)).ownerOf(positionId), address(locker));
        assertEq(positionManager.getPositionLiquidity(positionId), expectedLiquidity);
        assertEq(address(poolManager).balance, ethUsed);
        assertEq(token.balanceOf(address(poolManager)), tokenUsed);
        assertEq(token.balanceOf(TREASURY), LP_TOKENS - tokenUsed);
        assertEq(
            curve.unclaimedProtocolFees(),
            protocolFeesBefore + finalProtocolFee + GRADUATION_ETH - ethUsed
        );
        assertEq(token.balanceOf(address(curve)), 0);
        assertTrue(token.graduated());
    }

    function test_graduate_isAtomic() public {
        // A registry that briefly admits a third party lets it open the pool first.
        factory.setCurveOfToken(address(token), ATTACKER);
        vm.prank(ATTACKER);
        poolManager.initialize(_poolKey(), TickMath.MIN_SQRT_PRICE);
        factory.setCurveOfToken(address(token), address(curve));

        vm.prank(ALICE);
        vm.expectRevert(Pool.PoolAlreadyInitialized.selector);
        curve.buy{ value: 10 ether }(ALICE, ALICE, 0, block.timestamp);

        assertEq(uint8(curve.phase()), uint8(LaunchTypes.Phase.Curve));
        assertFalse(token.graduated());
        assertEq(token.balanceOf(address(curve)), TOTAL_SUPPLY);
    }

    function test_afterGraduation_curveTradingReverts() public {
        uint256 bought = _buy(ALICE, 10 ether);
        bytes memory wrongPhase = abi.encodeWithSelector(
            ILaunchErrors.WrongPhase.selector,
            uint8(LaunchTypes.Phase.Curve),
            uint8(LaunchTypes.Phase.Graduated)
        );

        vm.prank(BOB);
        vm.expectRevert(wrongPhase);
        curve.buy{ value: 1 ether }(BOB, BOB, 0, block.timestamp);

        vm.startPrank(ALICE);
        assertTrue(token.approve(address(curve), bought));
        vm.expectRevert(wrongPhase);
        curve.sell(bought, ALICE, 0, block.timestamp);
        vm.stopPrank();

        vm.expectRevert(wrongPhase);
        curve.quoteBuy(1 ether);
    }

    function test_tradingPause_appliesToCurveOnly() public {
        _buy(ALICE, 10 ether);
        factory.setTradingPaused(true);

        PoolSwapTest swapRouter = new PoolSwapTest(poolManager);
        vm.prank(BOB);
        swapRouter.swap{ value: 1 ether }(
            _poolKey(),
            SwapParams({
                zeroForOne: true,
                amountSpecified: -SafeCast.toInt256(1 ether),
                sqrtPriceLimitX96: TickMath.MIN_SQRT_PRICE + 1
            }),
            PoolSwapTest.TestSettings({ takeClaims: false, settleUsingBurn: false }),
            ""
        );
        assertGt(token.balanceOf(BOB), 0);
    }

    function test_poolManagerDonations_doNotChangeGraduation() public {
        uint256 bought = _buy(ALICE, 1 ether);
        vm.deal(address(poolManager), 7 ether);

        vm.prank(ALICE);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.TransferRestricted.selector, ALICE, ALICE, address(poolManager)
            )
        );
        token.transfer(address(poolManager), bought);

        uint160 expectedSqrtPrice = GraduationMath.sqrtPriceX96(GRADUATION_ETH, LP_TOKENS);
        (uint128 expectedLiquidity,,) =
            GraduationMath.fullRangePosition(expectedSqrtPrice, GRADUATION_ETH, LP_TOKENS);
        uint256 positionId = positionManager.nextTokenId();
        _buy(BOB, 10 ether);

        (uint160 sqrtPrice,,,) = poolManager.getSlot0(_poolKey().toId());
        assertEq(sqrtPrice, expectedSqrtPrice);
        assertEq(positionManager.getPositionLiquidity(positionId), expectedLiquidity);
    }

    function test_receive_rejectsUnexpectedSenders() public {
        vm.deal(STRANGER, 1 ether);
        vm.prank(STRANGER);
        (bool ok,) = address(curve).call{ value: 1 }("");
        assertFalse(ok);
    }
}
// forge-lint: disable-end(unused-return, arbitrary-send-eth, reentrancy-events, erc20-unchecked-transfer)
