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

    /// @dev Configuration snapshotted into every future launch; changed only by the timelock.
    struct FactoryDefaultsV2 {
        LaunchTypes.CurveParameters parameters;
        AntiSnipeProfile antiSnipe;
        uint16 lpCreatorShareBps;
        uint256 launchFee;
    }

    /// @dev The v4 dependencies are fixed per factory; hook and locker are wired once after
    /// deployment through `setDependencies`, because both are constructed with the factory.
    struct FactoryInitializationV2 {
        address pauseAuthority;
        address timelock;
        address protocolTreasury;
        uint16 engineVersion;
        address implementation;
        IPoolManager poolManager;
        IPositionManager positionManager;
        IAllowanceTransfer permit2;
        FactoryDefaultsV2 defaults;
    }

    /// @dev ABI-identical to the non-indexed fields of `ILaunchEventsV2.TokenLaunched`, in
    /// order. The factory emits the event from this struct's encoding.
    struct TokenLaunchedData {
        address protocolTreasury;
        uint16 engineVersion;
        string name;
        string symbol;
        uint256 totalSupply;
        uint256 virtualEth;
        uint256 virtualToken;
        uint256 curveTokens;
        uint256 lpTokens;
        uint256 graduationEth;
        uint256 launchFeePaid;
        uint16 tradeFeeBps;
        uint16 protocolShareBps;
        address hook;
        address locker;
        uint16 antiSnipeStartBps;
        uint32 antiSnipeDurationSeconds;
        uint16 lpCreatorShareBps;
    }
}
