// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { IAllowanceTransfer } from "permit2/src/interfaces/IAllowanceTransfer.sol";
import { LaunchTypes } from "../../types/LaunchTypes.sol";

library LaunchTypesV2 {
    /// @dev Hard caps no configuration can exceed without a new factory (spec §3.1).
    uint16 internal constant MAX_ANTI_SNIPE_START_BPS = 9000;
    uint32 internal constant MAX_ANTI_SNIPE_DURATION_SECONDS = 300;

    struct AntiSnipeProfile {
        uint16 startBps;
        uint32 durationSeconds;
    }

    /// @dev `permit2` is snapshotted with the other v4 dependencies because PositionManager
    /// pulls token-side liquidity through it.
    struct CurveInitializationV2 {
        address factory;
        address implementation;
        address token;
        address creator;
        address protocolTreasury;
        IPoolManager poolManager;
        IPositionManager positionManager;
        IAllowanceTransfer permit2;
        address hook;
        address locker;
        LaunchTypes.CurveParameters parameters;
        AntiSnipeProfile antiSnipe;
        uint16 lpCreatorShareBps;
    }
}
