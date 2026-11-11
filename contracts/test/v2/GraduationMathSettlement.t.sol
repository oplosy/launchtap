// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { ERC20 } from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { SafeCast } from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import { IHooks } from "@uniswap/v4-core/src/interfaces/IHooks.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { TickMath } from "@uniswap/v4-core/src/libraries/TickMath.sol";
import { Actions } from "@uniswap/v4-periphery/src/libraries/Actions.sol";
import { GraduationMath } from "../../src/libraries/GraduationMath.sol";
import { V4TestBase } from "./utils/V4TestBase.sol";

contract SettlementToken is ERC20 {
    constructor(uint256 supply) ERC20("Settlement", "SET") {
        _mint(msg.sender, supply);
    }
}

/// @notice The used amounts from `GraduationMath.fullRangePosition` must equal what the real
/// PoolManager settles (spec §6.3), so the treasury remainder is exact.
contract GraduationMathSettlementTest is V4TestBase {
    function setUp() public {
        deployV4();
    }

    function test_usedAmounts_matchPoolManagerSettlement_v1Defaults() public {
        _assertSettlementMatches(4.2e18, 200_000_000e18);
    }

    function testFuzz_usedAmounts_matchPoolManagerSettlement(uint256 eth, uint256 tok) public {
        uint256 ethAmount = bound(eth, 1e15, 1000 ether);
        uint256 tokenAmount = bound(tok, 1e18, 1e30);
        _assertSettlementMatches(ethAmount, tokenAmount);
    }

    function _assertSettlementMatches(uint256 ethAmount, uint256 tokenAmount) internal {
        SettlementToken token = new SettlementToken(tokenAmount);
        PoolKey memory key = PoolKey({
            currency0: Currency.wrap(address(0)),
            currency1: Currency.wrap(address(token)),
            fee: 10_000,
            tickSpacing: 200,
            hooks: IHooks(address(0))
        });

        uint160 sqrtPrice = GraduationMath.sqrtPriceX96(ethAmount, tokenAmount);
        (uint128 liquidity, uint256 ethUsed, uint256 tokenUsed) =
            GraduationMath.fullRangePosition(sqrtPrice, ethAmount, tokenAmount);
        assertEq(poolManager.initialize(key, sqrtPrice), TickMath.getTickAtSqrtPrice(sqrtPrice));

        assertTrue(IERC20(address(token)).approve(address(permit2), type(uint256).max));
        permit2.approve(
            address(token), address(positionManager), type(uint160).max, type(uint48).max
        );

        bytes memory unlockData = _mintUnlockData(key, liquidity, ethAmount, tokenAmount);
        uint256 ethBefore = address(this).balance;
        uint256 tokenBefore = token.balanceOf(address(this));
        // forge-lint: disable-next-line(arbitrary-send-eth)
        positionManager.modifyLiquidities{ value: ethAmount }(unlockData, block.timestamp);

        assertEq(ethBefore - address(this).balance, ethUsed, "eth settled");
        assertEq(tokenBefore - token.balanceOf(address(this)), tokenUsed, "token settled");
    }

    function _mintUnlockData(
        PoolKey memory key,
        uint128 liquidity,
        uint256 ethAmount,
        uint256 tokenAmount
    ) internal view returns (bytes memory) {
        // forge-lint: disable-next-line(unsafe-typecast)
        uint8 mintPosition = uint8(Actions.MINT_POSITION);
        // forge-lint: disable-next-line(unsafe-typecast)
        uint8 settlePair = uint8(Actions.SETTLE_PAIR);
        // forge-lint: disable-next-line(unsafe-typecast)
        uint8 sweep = uint8(Actions.SWEEP);
        bytes memory actions = abi.encodePacked(mintPosition, settlePair, sweep);
        bytes[] memory params = new bytes[](3);
        params[0] = abi.encode(
            key,
            GraduationMath.TICK_LOWER,
            GraduationMath.TICK_UPPER,
            uint256(liquidity),
            SafeCast.toUint128(ethAmount),
            SafeCast.toUint128(tokenAmount),
            address(this),
            bytes("")
        );
        params[1] = abi.encode(key.currency0, key.currency1);
        params[2] = abi.encode(key.currency0, address(this));

        return abi.encode(actions, params);
    }

    receive() external payable { }
}
