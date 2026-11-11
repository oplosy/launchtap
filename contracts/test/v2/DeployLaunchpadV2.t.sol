// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Test } from "forge-std/Test.sol";
import { DeployLaunchpadV2 } from "../../script/DeployLaunchpadV2.s.sol";

/// @notice The v2 deploy script is fail-closed on chains without a reviewed v4 record. The
/// mainnet path itself runs in `fork-test` and as a `forge script --fork-url` dry run.
// The deploy result is irrelevant here: every call is expected to revert.
// forge-lint: disable-start(unused-return)
contract DeployLaunchpadV2Test is Test {
    DeployLaunchpadV2 internal deployScript;

    function setUp() public {
        deployScript = new DeployLaunchpadV2();
        vm.setEnv("DEPLOYER", vm.toString(address(0xDE91)));
        vm.setEnv("PAUSE_AUTHORITY", vm.toString(address(0xA0A0)));
        vm.setEnv("TIMELOCK", vm.toString(address(0x7171)));
        vm.setEnv("PROTOCOL_TREASURY", vm.toString(address(0x7EA5)));
    }

    function test_refusesLocalChain() public {
        vm.expectRevert(bytes("no reviewed Uniswap v4 dependencies for this chain"));
        deployScript.run();
    }

    function test_refusesRobinhoodTestnet() public {
        vm.chainId(46_630);
        vm.expectRevert(bytes("no reviewed Uniswap v4 dependencies for this chain"));
        deployScript.run();
    }

    function test_refusesMainnetIdWithoutLiveDependencies() public {
        // Chain id alone is not enough: the pinned code must be present.
        vm.chainId(4663);
        vm.expectRevert(bytes("poolManager has no runtime code"));
        deployScript.run();
    }
}
// forge-lint: disable-end(unused-return)
