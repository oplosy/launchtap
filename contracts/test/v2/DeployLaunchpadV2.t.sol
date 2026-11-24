// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Hooks } from "@uniswap/v4-core/src/libraries/Hooks.sol";
import { DeployLaunchpadV2 } from "../../script/DeployLaunchpadV2.s.sol";
import { DeploymentValidation } from "../../script/deployment/DeploymentValidation.sol";
import { LaunchFactoryV2 } from "../../src/v2/LaunchFactoryV2.sol";
import { LaunchTypesV2 } from "../../src/v2/types/LaunchTypesV2.sol";
import { V4TestBase } from "./utils/V4TestBase.sol";

/// @dev Runs the deployment step after the record is chosen, against a locally built v4 stack.
contract DeployLaunchpadV2Harness is DeployLaunchpadV2 {
    function deployWith(V4Dependencies memory dependencies, DeploymentValidation.Target target)
        external
        returns (DeploymentResult memory)
    {
        return _deploy(dependencies, target);
    }
}

/// @notice The v2 deploy script is fail-closed on chains without a reviewed v4 record. The
/// mainnet path itself runs in `fork-test` and as a `forge script --fork-url` dry run.
// The deploy result is irrelevant in the refusal tests: every call is expected to revert.
// forge-lint: disable-start(unused-return)
contract DeployLaunchpadV2Test is V4TestBase {
    string internal constant TESTNET_V4_RECORD = "deployments/config/robinhood-testnet-v4.json";
    address internal constant DEPLOYER = address(0xDE91);
    address internal constant PAUSE_AUTHORITY = address(0xA0A0);
    address internal constant TIMELOCK = address(0x7171);
    address internal constant TREASURY = address(0x7EA5);

    DeployLaunchpadV2 internal deployScript;

    function setUp() public {
        deployScript = new DeployLaunchpadV2();
        vm.setEnv("DEPLOYER", vm.toString(DEPLOYER));
        vm.setEnv("PAUSE_AUTHORITY", vm.toString(PAUSE_AUTHORITY));
        vm.setEnv("TIMELOCK", vm.toString(TIMELOCK));
        vm.setEnv("PROTOCOL_TREASURY", vm.toString(TREASURY));
    }

    function test_refusesLocalChain() public {
        vm.expectRevert(bytes("no reviewed Uniswap v4 dependencies for this chain"));
        deployScript.run();
    }

    function test_robinhoodTestnetNeedsReviewedRecordAndPinnedCode() public {
        vm.chainId(46_630);
        if (vm.isFile(TESTNET_V4_RECORD)) {
            // With the reviewed record present, the pinned code must also be live.
            vm.expectRevert(bytes("poolManager has no runtime code"));
        } else {
            vm.expectRevert(bytes("no reviewed Uniswap v4 dependencies for this chain"));
        }
        deployScript.run();
    }

    function test_refusesMainnetIdWithoutLiveDependencies() public {
        // Chain id alone is not enough: the pinned code must be present.
        vm.chainId(4663);
        vm.expectRevert(bytes("poolManager has no runtime code"));
        deployScript.run();
    }

    function test_deploysWiredSystemOnTestnetStack() public {
        deployV4();
        DeployLaunchpadV2Harness harness = new DeployLaunchpadV2Harness();
        vm.chainId(46_630);

        DeployLaunchpadV2.DeploymentResult memory result = harness.deployWith(
            DeployLaunchpadV2.V4Dependencies({
                poolManager: poolManager, positionManager: positionManager, permit2: permit2
            }),
            DeploymentValidation.Target.RobinhoodTestnet
        );

        LaunchFactoryV2 factory = LaunchFactoryV2(result.launchFactory);
        assertEq(address(factory.POOL_MANAGER()), address(poolManager));
        assertEq(address(factory.POSITION_MANAGER()), address(positionManager));
        assertEq(address(factory.PERMIT2()), address(permit2));
        assertEq(factory.hook(), result.hook);
        assertEq(factory.locker(), result.locker);
        assertEq(factory.timelock(), TIMELOCK);
        assertEq(factory.pauseAuthority(), PAUSE_AUTHORITY);
        assertEq(uint160(result.hook) & Hooks.ALL_HOOK_MASK, uint160(Hooks.BEFORE_INITIALIZE_FLAG));

        LaunchTypesV2.FactoryDefaultsV2 memory defaults = factory.futureDefaults();
        assertEq(defaults.parameters.protocolShareBps, 3000);
        assertEq(defaults.lpCreatorShareBps, 7000);
    }
}
// forge-lint: disable-end(unused-return)
