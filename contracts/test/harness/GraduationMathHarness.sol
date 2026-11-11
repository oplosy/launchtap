// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { GraduationMath } from "../../src/libraries/GraduationMath.sol";

contract GraduationMathHarness {
    function sqrtPriceX96(uint256 ethAmount, uint256 tokenAmount) external pure returns (uint160) {
        return GraduationMath.sqrtPriceX96(ethAmount, tokenAmount);
    }

    function fullRangePosition(uint160 sqrtPrice, uint256 ethAmount, uint256 tokenAmount)
        external
        pure
        returns (uint128 liquidity, uint256 ethUsed, uint256 tokenUsed)
    {
        return GraduationMath.fullRangePosition(sqrtPrice, ethAmount, tokenAmount);
    }
}
