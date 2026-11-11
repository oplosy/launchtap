// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Test } from "forge-std/Test.sol";
import { Hooks } from "@uniswap/v4-core/src/libraries/Hooks.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { DeployPermit2 } from "permit2/test/utils/DeployPermit2.sol";
import { LaunchTypes } from "../../src/types/LaunchTypes.sol";
import { GraduationGuardHook } from "../../src/v2/GraduationGuardHook.sol";
import { LaunchFactoryV2 } from "../../src/v2/LaunchFactoryV2.sol";
import { LpFeeLocker } from "../../src/v2/LpFeeLocker.sol";
import { DeployLocalV2 } from "../../script/v2/DeployLocalV2.s.sol";

/// @notice The local v2 deployment backs the Anvil indexer e2e: it must run only on Anvil,
/// require Permit2 at its canonical address, and leave a fully wired, launchable system.
// The deploy result is irrelevant in the refusal tests: every call is expected to revert.
// forge-lint: disable-start(unused-return)
contract DeployLocalV2Test is Test {
    address internal constant DEPLOYER = address(0xDE91);
    address internal constant PAUSE_AUTHORITY = address(0xA0A0);
    address internal constant TIMELOCK = address(0x7171);
    address internal constant TREASURY = address(0x7EA5);
    address internal constant CREATOR = address(0xC4EA);

    DeployLocalV2 internal deployScript;

    function setUp() public {
        deployScript = new DeployLocalV2();
        vm.setEnv("DEPLOYER", vm.toString(DEPLOYER));
        vm.setEnv("PAUSE_AUTHORITY", vm.toString(PAUSE_AUTHORITY));
        vm.setEnv("TIMELOCK", vm.toString(TIMELOCK));
        vm.setEnv("PROTOCOL_TREASURY", vm.toString(TREASURY));
    }

    function test_refusesNonAnvilChain() public {
        vm.chainId(4663);
        vm.expectRevert(bytes("local v2 deployment runs only on Anvil (31337)"));
        deployScript.run();
    }

    function test_refusesMissingPermit2() public {
        vm.expectRevert(bytes("Permit2 is not deployed at its canonical address"));
        deployScript.run();
    }

    function test_deploysWiredLaunchableSystem() public {
        new DeployPermit2().deployPermit2();
        DeployLocalV2.LocalDeployment memory result = deployScript.run();

        LaunchFactoryV2 factory = LaunchFactoryV2(result.launchFactory);
        assertEq(factory.DEPLOYER(), DEPLOYER);
        assertEq(factory.pauseAuthority(), PAUSE_AUTHORITY);
        assertEq(factory.timelock(), TIMELOCK);
        assertEq(factory.protocolTreasury(), TREASURY);
        assertEq(factory.hook(), result.hook);
        assertEq(factory.locker(), result.locker);
        assertEq(address(factory.POOL_MANAGER()), result.poolManager);
        assertEq(address(factory.POSITION_MANAGER()), result.positionManager);
        assertEq(address(factory.PERMIT2()), result.permit2);
        assertEq(factory.curveImplementation(2), result.curveImplementation);
        assertTrue(factory.engineEnabled(2));
        assertEq(
            address(IPositionManager(result.positionManager).poolManager()), result.poolManager
        );
        assertEq(uint160(result.hook) & Hooks.ALL_HOOK_MASK, uint160(Hooks.BEFORE_INITIALIZE_FLAG));
        assertEq(address(GraduationGuardHook(result.hook).FACTORY()), result.launchFactory);
        assertEq(address(LpFeeLocker(payable(result.locker)).FACTORY()), result.launchFactory);
        assertTrue(result.swapRouter.code.length != 0);

        uint256 launchFee = factory.launchFee();
        vm.deal(CREATOR, launchFee);
        vm.prank(CREATOR);
        // The factory address comes from the deployment under test, not from a user.
        // forge-lint: disable-next-item(arbitrary-send-eth)
        (address token, address curve) = factory.launch{ value: launchFee }(
            LaunchTypes.LaunchRequest({
                name: "Local",
                symbol: "LOC",
                engineVersion: 2,
                developerBuyGross: 0,
                minDeveloperTokensOut: 0,
                deadline: block.timestamp
            })
        );
        assertEq(factory.curveOfToken(token), curve);
    }
}
// forge-lint: disable-end(unused-return)
