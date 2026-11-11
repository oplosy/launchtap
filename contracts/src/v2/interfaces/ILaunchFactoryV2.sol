// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { IAllowanceTransfer } from "permit2/src/interfaces/IAllowanceTransfer.sol";
import { IFactoryClaims } from "../../interfaces/IFactoryClaims.sol";
import { ILaunchErrors } from "../../interfaces/ILaunchErrors.sol";
import { ILaunchPause } from "../../interfaces/ILaunchPause.sol";
import { LaunchTypes } from "../../types/LaunchTypes.sol";
import { LaunchTypesV2 } from "../types/LaunchTypesV2.sol";
import { ICurveRegistry } from "./ICurveRegistry.sol";
import { ILaunchEventsV2 } from "./ILaunchEventsV2.sol";

interface ILaunchFactoryV2 is
    IFactoryClaims,
    ILaunchErrors,
    ILaunchPause,
    ICurveRegistry,
    ILaunchEventsV2
{
    error DependenciesNotSet();
    error DependenciesAlreadySet();
    error UnauthorizedDeployer(address caller, address deployer);
    error InvalidDependency(bytes32 field, address dependency);

    // Factory administration events, unchanged from v1 `ILaunchEvents`.
    event LaunchFeesClaimed(address indexed treasury, uint256 amount);
    event LaunchPauseSet(bool paused);
    event TradingPauseSet(bool paused);
    event EngineConfigured(
        uint16 indexed engineVersion, address indexed implementation, bool enabled
    );
    event FutureDefaultsConfigured(bytes32 indexed configHash);
    event FutureTreasuryConfigured(address indexed previousTreasury, address indexed newTreasury);

    function launch(LaunchTypes.LaunchRequest calldata request)
        external
        payable
        returns (address token, address curve);

    function setDependencies(address hook, address locker) external;
    function configureEngine(uint16 engineVersion, address implementation, bool enabled) external;
    function setFutureDefaults(LaunchTypesV2.FactoryDefaultsV2 calldata defaults) external;
    function setFutureTreasury(address treasury) external;

    function DEPLOYER() external view returns (address);
    function POOL_MANAGER() external view returns (IPoolManager);
    function POSITION_MANAGER() external view returns (IPositionManager);
    function PERMIT2() external view returns (IAllowanceTransfer);
    function hook() external view returns (address);
    function locker() external view returns (address);
    function curveImplementation(uint16 engineVersion) external view returns (address);
    function engineEnabled(uint16 engineVersion) external view returns (bool);
    function futureDefaults() external view returns (LaunchTypesV2.FactoryDefaultsV2 memory);
    function futureDefaultsHash() external view returns (bytes32);
    function protocolTreasury() external view returns (address);
    function launchFee() external view returns (uint256);
    function pauseAuthority() external view returns (address);
    function timelock() external view returns (address);
}
