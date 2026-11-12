// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Test } from "forge-std/Test.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { IAllowanceTransfer } from "permit2/src/interfaces/IAllowanceTransfer.sol";
import { DeployPermit2 } from "permit2/test/utils/DeployPermit2.sol";

/// @notice Local Uniswap v4 stack for unit tests.
/// @dev PoolManager and PositionManager come from the `v4` Foundry profile
/// (`FOUNDRY_PROFILE=v4 forge build`), compiled from the pinned submodule with the upstream
/// compiler settings. Permit2 is etched from its canonical precompiled bytecode.
abstract contract V4TestBase is Test, DeployPermit2 {
    string internal constant POOL_MANAGER_ARTIFACT = "out-v4/PoolManager.sol/PoolManager.json";
    string internal constant POSITION_MANAGER_ARTIFACT =
        "out-v4/PositionManager.sol/PositionManager.json";
    uint256 internal constant UNSUBSCRIBE_GAS_LIMIT = 100_000;

    IPoolManager internal poolManager;
    IPositionManager internal positionManager;
    IAllowanceTransfer internal permit2;

    function deployV4() internal {
        permit2 = IAllowanceTransfer(deployPermit2());
        poolManager = IPoolManager(deployCode(POOL_MANAGER_ARTIFACT, abi.encode(address(this))));
        // No token descriptor and no WETH9: launchpad positions are native-ETH pools and
        // never wrap or render token URIs in tests.
        positionManager = IPositionManager(
            deployCode(
                POSITION_MANAGER_ARTIFACT,
                abi.encode(
                    address(poolManager),
                    address(permit2),
                    UNSUBSCRIBE_GAS_LIMIT,
                    address(0),
                    address(0)
                )
            )
        );
    }
}
