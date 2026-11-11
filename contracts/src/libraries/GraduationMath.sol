// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Math } from "@openzeppelin/contracts/utils/math/Math.sol";
import { FullMath } from "@uniswap/v4-core/src/libraries/FullMath.sol";
import { SqrtPriceMath } from "@uniswap/v4-core/src/libraries/SqrtPriceMath.sol";
import { TickMath } from "@uniswap/v4-core/src/libraries/TickMath.sol";
import { LiquidityAmounts } from "@uniswap/v4-periphery/src/libraries/LiquidityAmounts.sol";

/// @notice Opening price and full-range position math for v2 graduation into a Uniswap v4
/// native-ETH/token pool (spec §6). Currency0 is native ETH, currency1 is the token.
library GraduationMath {
    /// @dev Widest ticks that are multiples of the pool's tick spacing (200).
    int24 internal constant TICK_LOWER = -887_200;
    int24 internal constant TICK_UPPER = 887_200;

    error GraduationPriceOutOfRange();

    /// @notice `sqrt(tokenAmount / ethAmount) * 2^96`, floored.
    /// @dev Computed as `Math.sqrt(FullMath.mulDiv(tokenAmount, 2^192, ethAmount))` so the
    /// X192 price never overflows a naive shift (spec §6.3). The quotient overflows exactly
    /// when `tokenAmount >= ethAmount * 2^64`, i.e. `tokenAmount >> 64 >= ethAmount`; that and
    /// any result outside `[MIN_SQRT_PRICE, MAX_SQRT_PRICE)` revert `GraduationPriceOutOfRange`.
    function sqrtPriceX96(uint256 ethAmount, uint256 tokenAmount) internal pure returns (uint160) {
        if (ethAmount == 0 || tokenAmount == 0 || (tokenAmount >> 64) >= ethAmount) {
            revert GraduationPriceOutOfRange();
        }

        uint256 priceX192 = FullMath.mulDiv(tokenAmount, 1 << 192, ethAmount);
        uint256 sqrtPrice = Math.sqrt(priceX192);
        if (sqrtPrice < TickMath.MIN_SQRT_PRICE || sqrtPrice >= TickMath.MAX_SQRT_PRICE) {
            revert GraduationPriceOutOfRange();
        }
        // forge-lint: disable-next-line(unsafe-typecast)
        return uint160(sqrtPrice);
    }

    /// @notice Full-range liquidity for the supplied amounts at `sqrtPrice`, and the amounts
    /// the PoolManager will actually settle for it.
    /// @dev Liquidity uses `LiquidityAmounts.getLiquidityForAmounts` (Uniswap's rounding); the
    /// used amounts use `SqrtPriceMath.getAmount{0,1}Delta(..., roundUp = true)`, which is what
    /// `PoolManager.modifyLiquidity` charges for a positive liquidity delta. The caller sends
    /// `supplied - used` of each currency to the protocol treasury (spec §6.2 step 4).
    function fullRangePosition(uint160 sqrtPrice, uint256 ethAmount, uint256 tokenAmount)
        internal
        pure
        returns (uint128 liquidity, uint256 ethUsed, uint256 tokenUsed)
    {
        uint160 sqrtLower = TickMath.getSqrtPriceAtTick(TICK_LOWER);
        uint160 sqrtUpper = TickMath.getSqrtPriceAtTick(TICK_UPPER);

        liquidity = LiquidityAmounts.getLiquidityForAmounts(
            sqrtPrice, sqrtLower, sqrtUpper, ethAmount, tokenAmount
        );
        ethUsed = SqrtPriceMath.getAmount0Delta(sqrtPrice, sqrtUpper, liquidity, true);
        tokenUsed = SqrtPriceMath.getAmount1Delta(sqrtLower, sqrtPrice, liquidity, true);
    }
}
