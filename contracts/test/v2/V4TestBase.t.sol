// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { ERC20 } from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { IERC721 } from "@openzeppelin/contracts/token/ERC721/IERC721.sol";
import { IHooks } from "@uniswap/v4-core/src/interfaces/IHooks.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { TickMath } from "@uniswap/v4-core/src/libraries/TickMath.sol";
import { Actions } from "@uniswap/v4-periphery/src/libraries/Actions.sol";
import { LiquidityAmounts } from "@uniswap/v4-periphery/src/libraries/LiquidityAmounts.sol";

import { V4TestBase } from "./utils/V4TestBase.sol";

contract V4TestToken is ERC20 {
    constructor() ERC20("V4 Test", "V4T") {
        _mint(msg.sender, 1_000_000 ether);
    }
}

contract V4TestBaseTest is V4TestBase {
    int24 internal constant TICK_SPACING = 200;
    int24 internal constant MIN_TICK = -887_200;
    int24 internal constant MAX_TICK = 887_200;

    function test_deployV4_initializesAndMintsFullRange() public {
        deployV4();
        V4TestToken token = new V4TestToken();

        PoolKey memory key = PoolKey({
            currency0: Currency.wrap(address(0)),
            currency1: Currency.wrap(address(token)),
            fee: 10_000,
            tickSpacing: TICK_SPACING,
            hooks: IHooks(address(0))
        });
        uint160 sqrtPriceX96 = uint160(2 ** 96);
        assertEq(poolManager.initialize(key, sqrtPriceX96), 0);

        assertTrue(IERC20(address(token)).approve(address(permit2), type(uint256).max));
        permit2.approve(
            address(token), address(positionManager), type(uint160).max, type(uint48).max
        );

        uint128 liquidity = LiquidityAmounts.getLiquidityForAmounts(
            sqrtPriceX96,
            TickMath.getSqrtPriceAtTick(MIN_TICK),
            TickMath.getSqrtPriceAtTick(MAX_TICK),
            1 ether,
            1 ether
        );

        // Action codes are small uint256 constants (0x02, 0x0d); the router reads one byte each.
        // forge-lint: disable-next-line(unsafe-typecast)
        uint8 mintPosition = uint8(Actions.MINT_POSITION);
        // forge-lint: disable-next-line(unsafe-typecast)
        uint8 settlePair = uint8(Actions.SETTLE_PAIR);
        bytes memory actions = abi.encodePacked(mintPosition, settlePair);
        bytes[] memory params = new bytes[](2);
        params[0] = abi.encode(
            key,
            MIN_TICK,
            MAX_TICK,
            uint256(liquidity),
            uint128(1 ether),
            uint128(1 ether),
            address(this),
            bytes("")
        );
        params[1] = abi.encode(key.currency0, key.currency1);

        uint256 tokenId = positionManager.nextTokenId();
        bytes memory unlockData = abi.encode(actions, params);
        // forge-lint: disable-next-line(arbitrary-send-eth)
        positionManager.modifyLiquidities{ value: 1 ether }(unlockData, block.timestamp);

        assertEq(IERC721(address(positionManager)).ownerOf(tokenId), address(this));
        assertGt(positionManager.getPositionLiquidity(tokenId), 0);
    }

    receive() external payable { }
}
