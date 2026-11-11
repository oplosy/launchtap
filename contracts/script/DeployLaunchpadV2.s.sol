// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Script } from "forge-std/Script.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { IAllowanceTransfer } from "permit2/src/interfaces/IAllowanceTransfer.sol";
import { LaunchTypes } from "../src/types/LaunchTypes.sol";
import { BondingCurveV2 } from "../src/v2/BondingCurveV2.sol";
import { GraduationGuardHook } from "../src/v2/GraduationGuardHook.sol";
import { LaunchFactoryV2 } from "../src/v2/LaunchFactoryV2.sol";
import { LpFeeLocker } from "../src/v2/LpFeeLocker.sol";
import { LaunchTypesV2 } from "../src/v2/types/LaunchTypesV2.sol";
import { DeploymentValidation } from "./deployment/DeploymentValidation.sol";
import { MineGuardHook } from "./v2/MineGuardHook.s.sol";

/// @dev `PositionManager` exposes Permit2 through `Permit2Forwarder`, outside `IPositionManager`.
interface IPermit2Holder {
    function permit2() external view returns (address);
}

/// @notice Deploys the v2 engine: curve implementation, factory, mined guard hook and LP fee
/// locker, then wires hook and locker into the factory once.
/// @dev Fail-closed: runs only on a chain with a reviewed Uniswap v4 dependency record whose
/// pinned runtime code hashes still match (spec §10, §11.1). Robinhood testnet has none, so it
/// is refused. Signing goes through `--account`/hardware wallet; no raw keys are accepted.
contract DeployLaunchpadV2 is Script {
    uint16 private constant ENGINE_VERSION = 2;
    uint256 private constant ROBINHOOD_MAINNET = 4663;
    string private constant ROBINHOOD_MAINNET_V4 = "deployments/config/robinhood-mainnet-v4.json";
    address private constant CREATE2_DEPLOYER = 0x4e59b44847b379578588920cA78FbF26c0B4956C;

    uint256 private constant TOTAL_SUPPLY = 1_000_000_000 ether;
    uint256 private constant CURVE_TOKENS = 800_000_000 ether;
    uint256 private constant LP_TOKENS = 200_000_000 ether;
    uint256 private constant GRADUATION_ETH = 4.2 ether;
    uint256 private constant INITIAL_VIRTUAL_ETH = 1.4 ether;
    uint256 private constant INITIAL_VIRTUAL_TOKEN = 1_066_666_666_666_666_666_666_666_667;
    uint16 private constant TRADE_FEE_BPS = 100;
    uint16 private constant PROTOCOL_SHARE_BPS = 5000;
    uint16 private constant SNIPE_START_BPS = 5000;
    uint32 private constant SNIPE_DURATION = 60;
    uint16 private constant LP_CREATOR_SHARE_BPS = 5000;
    uint256 private constant LAUNCH_FEE = 0.0005 ether;

    struct V4Dependencies {
        IPoolManager poolManager;
        IPositionManager positionManager;
        IAllowanceTransfer permit2;
    }

    struct DeploymentResult {
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
    }

    function run() external returns (DeploymentResult memory result) {
        V4Dependencies memory dependencies = _verifiedDependencies(block.chainid);
        result.deployer = vm.envAddress("DEPLOYER");
        result.pauseAuthority = vm.envAddress("PAUSE_AUTHORITY");
        result.timelock = vm.envAddress("TIMELOCK");
        result.protocolTreasury = vm.envAddress("PROTOCOL_TREASURY");
        DeploymentValidation.validateAuthorities(
            DeploymentValidation.Target.RobinhoodMainnet,
            result.deployer,
            result.pauseAuthority,
            result.timelock,
            result.protocolTreasury
        );
        result.poolManager = address(dependencies.poolManager);
        result.positionManager = address(dependencies.positionManager);
        result.permit2 = address(dependencies.permit2);

        vm.startBroadcast(result.deployer);
        BondingCurveV2 implementation = new BondingCurveV2();
        LaunchFactoryV2 factory = new LaunchFactoryV2(
            LaunchTypesV2.FactoryInitializationV2({
                pauseAuthority: result.pauseAuthority,
                timelock: result.timelock,
                protocolTreasury: result.protocolTreasury,
                engineVersion: ENGINE_VERSION,
                implementation: address(implementation),
                poolManager: dependencies.poolManager,
                positionManager: dependencies.positionManager,
                permit2: dependencies.permit2,
                defaults: _defaults()
            })
        );
        vm.stopBroadcast();

        // Mining is local computation; only the hook itself is broadcast, through the
        // canonical CREATE2 proxy that broadcast `new ...{ salt: ... }` uses.
        (address minedHook, bytes32 salt) =
            new MineGuardHook().mine(CREATE2_DEPLOYER, dependencies.poolManager, factory);

        vm.startBroadcast(result.deployer);
        GraduationGuardHook hook =
            new GraduationGuardHook{ salt: salt }(dependencies.poolManager, factory);
        LpFeeLocker locker = new LpFeeLocker(dependencies.positionManager, factory);
        factory.setDependencies(address(hook), address(locker));
        vm.stopBroadcast();

        require(address(hook) == minedHook, "hook address differs from the mined address");
        result.curveImplementation = address(implementation);
        result.launchFactory = address(factory);
        result.hook = address(hook);
        result.locker = address(locker);
        _assertDeployment(result, factory);
    }

    function _verifiedDependencies(uint256 chainId)
        private
        view
        returns (V4Dependencies memory dependencies)
    {
        require(chainId == ROBINHOOD_MAINNET, "no reviewed Uniswap v4 dependencies for this chain");
        string memory json = vm.readFile(ROBINHOOD_MAINNET_V4);
        require(vm.parseJsonUint(json, ".chainId") == chainId, "dependency record chain mismatch");
        require(vm.parseJsonBool(json, ".reviewed"), "dependency record not reviewed");

        dependencies.poolManager = IPoolManager(_pinned(json, "poolManager"));
        dependencies.positionManager = IPositionManager(_pinned(json, "positionManager"));
        dependencies.permit2 = IAllowanceTransfer(_pinned(json, "permit2"));
        require(
            address(dependencies.positionManager.poolManager())
                == address(dependencies.poolManager),
            "PositionManager is wired to another PoolManager"
        );
        require(
            IPermit2Holder(address(dependencies.positionManager)).permit2()
                == address(dependencies.permit2),
            "PositionManager is wired to another Permit2"
        );
    }

    /// @return dependency Address of `name` whose live runtime code hash equals the pinned one.
    function _pinned(string memory json, string memory name)
        private
        view
        returns (address dependency)
    {
        dependency = vm.parseJsonAddress(json, string.concat(".", name));
        bytes32 expected = vm.parseJsonBytes32(json, string.concat(".bytecodeHashes.", name));
        require(dependency.code.length != 0, string.concat(name, " has no runtime code"));
        require(dependency.codehash == expected, string.concat(name, " runtime code hash mismatch"));
    }

    function _assertDeployment(DeploymentResult memory result, LaunchFactoryV2 factory)
        private
        view
    {
        require(factory.DEPLOYER() == result.deployer, "factory deployer mismatch");
        require(factory.pauseAuthority() == result.pauseAuthority, "pause authority mismatch");
        require(factory.timelock() == result.timelock, "timelock mismatch");
        require(factory.protocolTreasury() == result.protocolTreasury, "protocol treasury mismatch");
        require(
            factory.curveImplementation(ENGINE_VERSION) == result.curveImplementation,
            "implementation mismatch"
        );
        require(factory.engineEnabled(ENGINE_VERSION), "engine disabled");
        require(factory.hook() == result.hook, "hook not wired");
        require(factory.locker() == result.locker, "locker not wired");
        require(address(factory.POOL_MANAGER()) == result.poolManager, "pool manager mismatch");
        require(
            address(factory.POSITION_MANAGER()) == result.positionManager,
            "position manager mismatch"
        );
        require(address(factory.PERMIT2()) == result.permit2, "permit2 mismatch");
        require(factory.launchFee() == LAUNCH_FEE, "launch fee mismatch");
    }

    function _defaults() private pure returns (LaunchTypesV2.FactoryDefaultsV2 memory) {
        return LaunchTypesV2.FactoryDefaultsV2({
            parameters: LaunchTypes.CurveParameters({
                totalSupply: TOTAL_SUPPLY,
                curveTokens: CURVE_TOKENS,
                lpTokens: LP_TOKENS,
                graduationEth: GRADUATION_ETH,
                initialVirtualEth: INITIAL_VIRTUAL_ETH,
                initialVirtualToken: INITIAL_VIRTUAL_TOKEN,
                tradeFeeBps: TRADE_FEE_BPS,
                protocolShareBps: PROTOCOL_SHARE_BPS
            }),
            antiSnipe: LaunchTypesV2.AntiSnipeProfile({
                startBps: SNIPE_START_BPS, durationSeconds: SNIPE_DURATION
            }),
            lpCreatorShareBps: LP_CREATOR_SHARE_BPS,
            launchFee: LAUNCH_FEE
        });
    }
}
