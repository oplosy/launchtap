// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Script, console2 } from "forge-std/Script.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { Hooks } from "@uniswap/v4-core/src/libraries/Hooks.sol";
import { HookMiner } from "@uniswap/v4-periphery/test/shared/HookMiner.sol";
import { GraduationGuardHook } from "../../src/v2/GraduationGuardHook.sol";
import { ICurveRegistry } from "../../src/v2/interfaces/ICurveRegistry.sol";

/// @notice Mines the CREATE2 salt that places `GraduationGuardHook` at an address carrying
/// exactly the `BEFORE_INITIALIZE_FLAG` permission bit.
/// @dev `forge script script/v2/MineGuardHook.s.sol` with `POOL_MANAGER` and `LAUNCH_FACTORY`
/// set prints the address and salt for the canonical CREATE2 deployer proxy.
/// `DeployLaunchpadV2` reuses `mine` and deploys through the same proxy.
contract MineGuardHook is Script {
    address internal constant CREATE2_DEPLOYER = 0x4e59b44847b379578588920cA78FbF26c0B4956C;

    function run() external view returns (address hook, bytes32 salt) {
        IPoolManager poolManager = IPoolManager(vm.envAddress("POOL_MANAGER"));
        ICurveRegistry factory = ICurveRegistry(vm.envAddress("LAUNCH_FACTORY"));
        (hook, salt) = mine(CREATE2_DEPLOYER, poolManager, factory);
        console2.log("GraduationGuardHook", hook);
        console2.logBytes32(salt);
    }

    /// @return hook Address `deployer` creates with `salt` and the given constructor args.
    /// @return salt First salt whose hook address has exactly `BEFORE_INITIALIZE_FLAG` set.
    function mine(address deployer, IPoolManager poolManager, ICurveRegistry factory)
        public
        view
        returns (address hook, bytes32 salt)
    {
        return HookMiner.find(
            deployer,
            Hooks.BEFORE_INITIALIZE_FLAG,
            type(GraduationGuardHook).creationCode,
            abi.encode(address(poolManager), address(factory))
        );
    }
}
