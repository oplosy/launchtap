// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { SafeCast } from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { Actions } from "@uniswap/v4-periphery/src/libraries/Actions.sol";
import { IAllowanceTransfer } from "permit2/src/interfaces/IAllowanceTransfer.sol";
import { GraduationMath } from "../../../src/libraries/GraduationMath.sol";
import { IFeeRecipientSource } from "../../../src/v2/interfaces/IFeeRecipientSource.sol";
import { ILpFeeLocker } from "../../../src/v2/interfaces/ILpFeeLocker.sol";

// Test double: accepts any address and ignores PoolManager/PositionManager return values.
// forge-lint: disable-start(missing-zero-check, unused-return, arbitrary-send-eth)
/// @notice Stands in for a v2 curve: payout configuration plus a graduation helper that
/// initializes the pool, mints the full-range position to the locker and registers it.
contract MockFeeRecipientSource is IFeeRecipientSource {
    address public feeRecipient;
    address public protocolTreasury;
    uint16 public lpCreatorShareBps;

    IPoolManager internal immutable POOL_MANAGER;
    IPositionManager internal immutable POSITION_MANAGER;
    IAllowanceTransfer internal immutable PERMIT2;

    constructor(
        IPoolManager poolManager_,
        IPositionManager positionManager_,
        IAllowanceTransfer permit2_,
        address feeRecipient_,
        address protocolTreasury_,
        uint16 lpCreatorShareBps_
    ) {
        POOL_MANAGER = poolManager_;
        POSITION_MANAGER = positionManager_;
        PERMIT2 = permit2_;
        feeRecipient = feeRecipient_;
        protocolTreasury = protocolTreasury_;
        lpCreatorShareBps = lpCreatorShareBps_;
    }

    function setFeeRecipient(address next) external {
        feeRecipient = next;
    }

    function graduate(PoolKey memory key, uint256 tokenAmount, ILpFeeLocker locker)
        external
        payable
        returns (uint256 tokenId)
    {
        address token = Currency.unwrap(key.currency1);
        uint160 sqrtPrice = GraduationMath.sqrtPriceX96(msg.value, tokenAmount);
        (uint128 liquidity,,) = GraduationMath.fullRangePosition(sqrtPrice, msg.value, tokenAmount);
        POOL_MANAGER.initialize(key, sqrtPrice);

        require(IERC20(token).approve(address(PERMIT2), tokenAmount), "approve");
        PERMIT2.approve(
            token, address(POSITION_MANAGER), SafeCast.toUint160(tokenAmount), type(uint48).max
        );

        bytes memory actions = abi.encodePacked(
            SafeCast.toUint8(Actions.MINT_POSITION),
            SafeCast.toUint8(Actions.SETTLE_PAIR),
            SafeCast.toUint8(Actions.SWEEP)
        );
        bytes[] memory params = new bytes[](3);
        params[0] = abi.encode(
            key,
            GraduationMath.TICK_LOWER,
            GraduationMath.TICK_UPPER,
            uint256(liquidity),
            SafeCast.toUint128(msg.value),
            SafeCast.toUint128(tokenAmount),
            address(locker),
            bytes("")
        );
        params[1] = abi.encode(key.currency0, key.currency1);
        params[2] = abi.encode(key.currency0, address(this));

        tokenId = POSITION_MANAGER.nextTokenId();
        POSITION_MANAGER.modifyLiquidities{ value: msg.value }(
            abi.encode(actions, params), block.timestamp
        );
        locker.registerPosition(token, tokenId);
    }

    receive() external payable { }
}
// forge-lint: disable-end(missing-zero-check, unused-return, arbitrary-send-eth)
