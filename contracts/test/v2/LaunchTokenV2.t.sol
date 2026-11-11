// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Test } from "forge-std/Test.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { ILaunchErrors } from "../../src/interfaces/ILaunchErrors.sol";
import { LaunchTokenV2 } from "../../src/v2/LaunchTokenV2.sol";

contract LaunchTokenV2Test is Test {
    string private constant NAME = "Launch Token";
    string private constant SYMBOL = "LCH";
    bytes32 private constant FIELD_CURVE = "curve";
    bytes32 private constant FIELD_POOL_MANAGER = "poolManager";
    uint256 private constant SUPPLY = 1_000_000_000 ether;
    address private constant CURVE = address(0xC0DE);
    address private constant POOL_MANAGER = address(0x9000);
    address private constant ALICE = address(0xA11CE);
    address private constant BOB = address(0xB0B);
    address private constant ATTACKER = address(0xBAD);

    LaunchTokenV2 private token;

    function setUp() external {
        token = new LaunchTokenV2(NAME, SYMBOL, CURVE, POOL_MANAGER, SUPPLY);
        vm.prank(CURVE);
        assertTrue(token.transfer(ALICE, 1000 ether));
    }

    function test_constructor_mintsEntireSupplyToCurveOnce() external {
        vm.expectEmit(true, true, false, true);
        // forge-lint: disable-next-line(reentrancy-events)
        emit IERC20.Transfer(address(0), CURVE, SUPPLY);
        LaunchTokenV2 deployed = new LaunchTokenV2(NAME, SYMBOL, CURVE, POOL_MANAGER, SUPPLY);

        assertEq(deployed.totalSupply(), SUPPLY);
        assertEq(deployed.balanceOf(CURVE), SUPPLY);
        assertEq(deployed.curve(), CURVE);
        assertEq(deployed.poolManager(), POOL_MANAGER);
        assertFalse(deployed.graduated());
    }

    function test_constructor_rejectsZeroCurveAndPoolManager() external {
        vm.expectRevert(abi.encodeWithSelector(ILaunchErrors.ZeroAddress.selector, FIELD_CURVE));
        new LaunchTokenV2(NAME, SYMBOL, address(0), POOL_MANAGER, SUPPLY);
        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.ZeroAddress.selector, FIELD_POOL_MANAGER)
        );
        new LaunchTokenV2(NAME, SYMBOL, CURVE, address(0), SUPPLY);
    }

    function test_preGraduation_walletToWalletSucceeds() external {
        vm.prank(ALICE);
        assertTrue(token.transfer(BOB, 1 ether));
        assertEq(token.balanceOf(BOB), 1 ether);
    }

    function test_preGraduation_transferToCurveReverts() external {
        vm.prank(ALICE);
        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.TransferRestricted.selector, ALICE, ALICE, CURVE)
        );
        // forge-lint: disable-next-line(erc20-unchecked-transfer)
        token.transfer(CURVE, 1 ether);
    }

    function test_preGraduation_transferToPoolManagerReverts() external {
        vm.prank(ALICE);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.TransferRestricted.selector, ALICE, ALICE, POOL_MANAGER
            )
        );
        // forge-lint: disable-next-line(erc20-unchecked-transfer)
        token.transfer(POOL_MANAGER, 1 ether);
    }

    function test_preGraduation_operatorPullingIntoPoolManagerReverts() external {
        vm.prank(ALICE);
        assertTrue(token.approve(ATTACKER, 1 ether));
        vm.prank(ATTACKER);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.TransferRestricted.selector, ATTACKER, ALICE, POOL_MANAGER
            )
        );
        // forge-lint: disable-next-line(arbitrary-send-erc20, erc20-unchecked-transfer)
        token.transferFrom(ALICE, POOL_MANAGER, 1 ether);
    }

    function test_preGraduation_operatorMovingCurveTokensReverts() external {
        vm.prank(CURVE);
        assertTrue(token.approve(ATTACKER, 1 ether));
        vm.prank(ATTACKER);
        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.TransferRestricted.selector, ATTACKER, CURVE, BOB)
        );
        // forge-lint: disable-next-line(arbitrary-send-erc20, erc20-unchecked-transfer)
        token.transferFrom(CURVE, BOB, 1 ether);
    }

    function test_preGraduation_curveMayMoveIntoCurveAndPoolManager() external {
        vm.prank(ALICE);
        assertTrue(token.approve(CURVE, 2 ether));
        vm.startPrank(CURVE);
        // forge-lint: disable-next-line(arbitrary-send-erc20)
        assertTrue(token.transferFrom(ALICE, CURVE, 1 ether));
        // forge-lint: disable-next-line(arbitrary-send-erc20)
        assertTrue(token.transferFrom(ALICE, POOL_MANAGER, 1 ether));
        assertTrue(token.transfer(POOL_MANAGER, 1 ether));
        vm.stopPrank();
        assertEq(token.balanceOf(POOL_MANAGER), 2 ether);
    }

    function test_markGraduated_onlyCurveOnce() external {
        vm.prank(ATTACKER);
        vm.expectRevert(abi.encodeWithSelector(ILaunchErrors.UnauthorizedCurve.selector, ATTACKER));
        token.markGraduated();

        vm.prank(CURVE);
        token.markGraduated();
        assertTrue(token.graduated());

        vm.prank(CURVE);
        vm.expectRevert(ILaunchErrors.AlreadyGraduated.selector);
        token.markGraduated();
    }

    function test_postGraduation_everyTransferUnrestricted() external {
        vm.prank(CURVE);
        token.markGraduated();

        vm.startPrank(ALICE);
        assertTrue(token.transfer(CURVE, 1 ether));
        assertTrue(token.transfer(POOL_MANAGER, 1 ether));
        assertTrue(token.approve(ATTACKER, 1 ether));
        vm.stopPrank();
        vm.prank(ATTACKER);
        // forge-lint: disable-next-line(arbitrary-send-erc20)
        assertTrue(token.transferFrom(ALICE, POOL_MANAGER, 1 ether));
        assertEq(token.balanceOf(POOL_MANAGER), 2 ether);
    }
}
