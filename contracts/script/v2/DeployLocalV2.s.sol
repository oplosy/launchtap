// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Script } from "forge-std/Script.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { PoolSwapTest } from "@uniswap/v4-core/src/test/PoolSwapTest.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { IAllowanceTransfer } from "permit2/src/interfaces/IAllowanceTransfer.sol";
import { LaunchTypes } from "../../src/types/LaunchTypes.sol";
import { BondingCurveV2 } from "../../src/v2/BondingCurveV2.sol";
import { GraduationGuardHook } from "../../src/v2/GraduationGuardHook.sol";
import { LaunchFactoryV2 } from "../../src/v2/LaunchFactoryV2.sol";
import { LpFeeLocker } from "../../src/v2/LpFeeLocker.sol";
import { LaunchTypesV2 } from "../../src/v2/types/LaunchTypesV2.sol";
import { DeploymentValidation } from "../deployment/DeploymentValidation.sol";
import { MineGuardHook } from "./MineGuardHook.s.sol";

/// @notice Deploys a local Uniswap V4 stack and the v2 engine on Anvil for the indexer e2e.
/// @dev Anvil only (31337). Permit2 must already sit at its canonical address (the wrapper sets
/// its runtime code with `anvil_setCode`, since the pinned helper only etches). PoolManager and
/// PositionManager come from the pinned `out-v4` build; `PoolSwapTest` is a test-only V4 swap
/// router for e2e trades and is never part of a reviewed deployment.
contract DeployLocalV2 is Script {
    uint256 private constant ANVIL = 31_337;
    uint16 private constant ENGINE_VERSION = 2;
    address private constant PERMIT2 = 0x000000000022D473030F116dDEE9F6B43aC78BA3;
    address private constant CREATE2_DEPLOYER = 0x4e59b44847b379578588920cA78FbF26c0B4956C;
    uint256 private constant UNSUBSCRIBE_GAS_LIMIT = 100_000;
    string private constant POOL_MANAGER_ARTIFACT = "out-v4/PoolManager.sol/PoolManager.json";
    string private constant POSITION_MANAGER_ARTIFACT =
        "out-v4/PositionManager.sol/PositionManager.json";

    struct LocalDeployment {
        address deployer;
        address pauseAuthority;
        address timelock;
        address protocolTreasury;
        address poolManager;
        address positionManager;
        address permit2;
        address curveImplementation;
        address launchFactory;
        address hook;
        address locker;
        address swapRouter;
    }

    function run() external returns (LocalDeployment memory result) {
        require(block.chainid == ANVIL, "local v2 deployment runs only on Anvil (31337)");
        require(PERMIT2.code.length != 0, "Permit2 is not deployed at its canonical address");
        result.deployer = vm.envAddress("DEPLOYER");
        result.pauseAuthority = vm.envAddress("PAUSE_AUTHORITY");
        result.timelock = vm.envAddress("TIMELOCK");
        result.protocolTreasury = vm.envAddress("PROTOCOL_TREASURY");
        DeploymentValidation.validateAuthorities(
            DeploymentValidation.Target.Anvil,
            result.deployer,
            result.pauseAuthority,
            result.timelock,
            result.protocolTreasury
        );
        result.permit2 = PERMIT2;

        vm.startBroadcast(result.deployer);
        result.poolManager = deployCode(POOL_MANAGER_ARTIFACT, abi.encode(result.deployer));
        result.positionManager = deployCode(
            POSITION_MANAGER_ARTIFACT,
            abi.encode(result.poolManager, PERMIT2, UNSUBSCRIBE_GAS_LIMIT, address(0), address(0))
        );
        result.swapRouter = address(new PoolSwapTest(IPoolManager(result.poolManager)));
        BondingCurveV2 implementation = new BondingCurveV2();
        LaunchFactoryV2 factory = new LaunchFactoryV2(_initialization(result, implementation));
        vm.stopBroadcast();

        // Mining is local computation; only the hook is broadcast, through the CREATE2 proxy
        // that a broadcast `new ...{ salt: ... }` uses.
        (address minedHook, bytes32 salt) =
            new MineGuardHook().mine(CREATE2_DEPLOYER, IPoolManager(result.poolManager), factory);

        vm.startBroadcast(result.deployer);
        GraduationGuardHook hook =
            new GraduationGuardHook{ salt: salt }(IPoolManager(result.poolManager), factory);
        LpFeeLocker locker = new LpFeeLocker(IPositionManager(result.positionManager), factory);
        factory.setDependencies(address(hook), address(locker));
        vm.stopBroadcast();

        require(address(hook) == minedHook, "hook address differs from the mined address");
        result.curveImplementation = address(implementation);
        result.launchFactory = address(factory);
        result.hook = address(hook);
        result.locker = address(locker);
    }

    function _initialization(LocalDeployment memory result, BondingCurveV2 implementation)
        private
        pure
        returns (LaunchTypesV2.FactoryInitializationV2 memory)
    {
        return LaunchTypesV2.FactoryInitializationV2({
            pauseAuthority: result.pauseAuthority,
            timelock: result.timelock,
            protocolTreasury: result.protocolTreasury,
            engineVersion: ENGINE_VERSION,
            implementation: address(implementation),
            poolManager: IPoolManager(result.poolManager),
            positionManager: IPositionManager(result.positionManager),
            permit2: IAllowanceTransfer(PERMIT2),
            defaults: LaunchTypesV2.FactoryDefaultsV2({
                parameters: LaunchTypes.CurveParameters({
                    totalSupply: 1_000_000_000 ether,
                    curveTokens: 800_000_000 ether,
                    lpTokens: 200_000_000 ether,
                    graduationEth: 4.2 ether,
                    initialVirtualEth: 1.4 ether,
                    initialVirtualToken: 1_066_666_666_666_666_666_666_666_667,
                    tradeFeeBps: 100,
                    protocolShareBps: 5000
                }),
                antiSnipe: LaunchTypesV2.AntiSnipeProfile({ startBps: 5000, durationSeconds: 60 }),
                lpCreatorShareBps: 5000,
                launchFee: 0.0005 ether
            })
        });
    }
}
