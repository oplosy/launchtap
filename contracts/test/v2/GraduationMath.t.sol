// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Test } from "forge-std/Test.sol";
import { TickMath } from "@uniswap/v4-core/src/libraries/TickMath.sol";
import { GraduationMath } from "../../src/libraries/GraduationMath.sol";
import { GraduationMathHarness } from "../harness/GraduationMathHarness.sol";

contract GraduationMathTest is Test {
    uint256 internal constant V1_GRADUATION_ETH = 4.2e18;
    uint256 internal constant V1_LP_TOKENS = 200_000_000e18;

    GraduationMathHarness internal harness;

    function setUp() public {
        harness = new GraduationMathHarness();
    }

    function test_ticks_areFullRangeForSpacing200() public pure {
        assertEq(GraduationMath.TICK_LOWER, -887_200);
        assertEq(GraduationMath.TICK_UPPER, 887_200);
        assertEq(GraduationMath.TICK_LOWER % 200, 0);
        assertGe(GraduationMath.TICK_LOWER, TickMath.MIN_TICK);
        assertLe(GraduationMath.TICK_UPPER, TickMath.MAX_TICK);
    }

    function test_sqrtPrice_v1Defaults() public view {
        assertEq(
            harness.sqrtPriceX96(V1_GRADUATION_ETH, V1_LP_TOKENS),
            546_726_262_810_727_612_002_490_081_201_916
        );
    }

    function test_sqrtPrice_noOverflowAtCaps() public {
        // tokens/eth >= 2^64 would overflow the X192 quotient: must be a typed revert.
        vm.expectRevert(GraduationMath.GraduationPriceOutOfRange.selector);
        // forge-lint: disable-next-line(unused-return)
        harness.sqrtPriceX96(1, 1e30 * 1e18);
    }

    function test_sqrtPrice_rejectsZeroAmounts() public {
        vm.expectRevert(GraduationMath.GraduationPriceOutOfRange.selector);
        // forge-lint: disable-next-line(unused-return)
        harness.sqrtPriceX96(0, V1_LP_TOKENS);
        vm.expectRevert(GraduationMath.GraduationPriceOutOfRange.selector);
        // forge-lint: disable-next-line(unused-return)
        harness.sqrtPriceX96(V1_GRADUATION_ETH, 0);
    }

    function test_sqrtPrice_rejectsBelowMinPrice() public {
        // price = 1 / 2^130 is below TickMath.MIN_SQRT_PRICE^2.
        vm.expectRevert(GraduationMath.GraduationPriceOutOfRange.selector);
        // forge-lint: disable-next-line(unused-return)
        harness.sqrtPriceX96(2 ** 130, 1);
    }

    function test_fullRange_usesNoMoreThanSupplied() public view {
        uint160 sqrtPrice = harness.sqrtPriceX96(V1_GRADUATION_ETH, V1_LP_TOKENS);
        (uint128 liquidity, uint256 ethUsed, uint256 tokenUsed) =
            harness.fullRangePosition(sqrtPrice, V1_GRADUATION_ETH, V1_LP_TOKENS);

        assertGt(liquidity, 0);
        assertLe(ethUsed, V1_GRADUATION_ETH);
        assertLe(tokenUsed, V1_LP_TOKENS);
        assertLe(V1_GRADUATION_ETH - ethUsed, V1_GRADUATION_ETH / 1e9);
        assertLe(V1_LP_TOKENS - tokenUsed, V1_LP_TOKENS / 1e9);
    }

    function testFuzz_fullRange_remaindersBounded(uint96 eth, uint128 tok) public view {
        uint256 ethAmount = bound(eth, 1e12, type(uint96).max);
        // Keep tokens/eth inside [2^-32, 2^60]: well within the v4 price range and the
        // X192 quotient, and far wider than any curve configuration the factory accepts.
        uint256 minTokens = ethAmount >> 32;
        if (minTokens < 1e12) minTokens = 1e12;
        uint256 maxTokens = ethAmount << 60;
        if (maxTokens > type(uint128).max) maxTokens = type(uint128).max;
        uint256 tokenAmount = bound(tok, minTokens, maxTokens);

        uint160 sqrtPrice = harness.sqrtPriceX96(ethAmount, tokenAmount);
        (uint128 liquidity, uint256 ethUsed, uint256 tokenUsed) =
            harness.fullRangePosition(sqrtPrice, ethAmount, tokenAmount);

        assertGt(liquidity, 0);
        assertLe(ethUsed, ethAmount);
        assertLe(tokenUsed, tokenAmount);
    }
}
