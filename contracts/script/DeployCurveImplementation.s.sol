// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Script, console2 } from "forge-std/Script.sol";
import { BondingCurveV1 } from "../src/BondingCurveV1.sol";
import { LaunchFactory } from "../src/LaunchFactory.sol";
import { IBondingCurveV1 } from "../src/interfaces/IBondingCurveV1.sol";
import { ILaunchErrors } from "../src/interfaces/ILaunchErrors.sol";
import { LaunchTypes } from "../src/types/LaunchTypes.sol";
import { DeploymentValidation } from "./deployment/DeploymentValidation.sol";

/// @notice Deploys a replacement BondingCurveV1 implementation for an existing LaunchFactory.
/// Only the implementation creation is broadcast. The engine switch stays a separate timelock
/// action; this script prints its calldata and proves it on the forked chain first, together
/// with the graduation-DoS regression (a buy delivered to the canonical pair must revert).
contract DeployCurveImplementation is Script {
    uint16 private constant ENGINE_VERSION = 1;
    bool private constant ENGINE_ENABLED = true;
    bytes32 private constant FIELD_TOKEN_RECIPIENT = "tokenRecipient";

    error AuthorityOverlap(address deployer);
    error EngineSwitchNotProven(address expected, address actual);
    error PairRecipientAccepted(address pair);

    function run() external returns (address implementation, bytes memory configureCalldata) {
        DeploymentValidation.Target target = _target(vm.envString("DEPLOYMENT_TARGET"));
        DeploymentValidation.validateChain(target, block.chainid);
        address deployer = vm.envAddress("DEPLOYER");
        LaunchFactory factory = LaunchFactory(vm.envAddress("LAUNCH_FACTORY"));
        address timelock = factory.timelock();
        if (deployer == timelock || deployer == factory.pauseAuthority()) {
            revert AuthorityOverlap(deployer);
        }
        address previous = factory.curveImplementation(ENGINE_VERSION);

        vm.startBroadcast(deployer);
        BondingCurveV1 deployed = new BondingCurveV1();
        vm.stopBroadcast();
        implementation = address(deployed);
        configureCalldata = abi.encodeCall(
            LaunchFactory.configureEngine, (ENGINE_VERSION, implementation, ENGINE_ENABLED)
        );

        // Everything below runs only against the local fork state and is never broadcast.
        vm.prank(timelock);
        factory.configureEngine(ENGINE_VERSION, implementation, ENGINE_ENABLED);
        if (factory.curveImplementation(ENGINE_VERSION) != implementation) {
            revert EngineSwitchNotProven(
                implementation, factory.curveImplementation(ENGINE_VERSION)
            );
        }
        _proveRecipientGuard(factory);

        console2.log("Previous curve implementation:", previous);
        console2.log("New curve implementation:", implementation);
        console2.log("Runtime code hash:");
        console2.logBytes32(implementation.codehash);
        console2.log("Timelock:", timelock);
        console2.log("configureEngine calldata for the timelock:");
        console2.logBytes(configureCalldata);
    }

    function _proveRecipientGuard(LaunchFactory factory) private {
        address probe = address(0xBEEF);
        uint256 launchFee = factory.launchFee();
        vm.deal(probe, launchFee + 1 ether);
        vm.startPrank(probe);
        // Fork-only probe: the fee reaches the reviewed factory and nothing is broadcast.
        // forge-lint: disable-start(arbitrary-send-eth, unused-return)
        (, address curve, address pair) = factory.launch{ value: launchFee }(
            LaunchTypes.LaunchRequest({
                name: "Upgrade Probe",
                symbol: "PROBE",
                engineVersion: ENGINE_VERSION,
                developerBuyGross: 0,
                minDeveloperTokensOut: 0,
                deadline: block.timestamp
            })
        );
        // forge-lint: disable-end(arbitrary-send-eth, unused-return)
        // The probe buy targets the curve the factory just created on the fork.
        // forge-lint: disable-next-line(arbitrary-send-eth)
        try IBondingCurveV1(curve).buy{ value: 1 gwei }(pair, probe, 0, block.timestamp) returns (
            uint256, uint256
        ) {
            revert PairRecipientAccepted(pair);
        } catch (bytes memory reason) {
            bytes memory expected = abi.encodeWithSelector(
                ILaunchErrors.InvalidRecipient.selector, FIELD_TOKEN_RECIPIENT, pair
            );
            if (keccak256(reason) != keccak256(expected)) revert PairRecipientAccepted(pair);
        }
        vm.stopPrank();
    }

    function _target(string memory value) private pure returns (DeploymentValidation.Target) {
        bytes32 target = keccak256(bytes(value));
        if (target == keccak256("anvil")) return DeploymentValidation.Target.Anvil;
        if (target == keccak256("robinhood-testnet")) {
            return DeploymentValidation.Target.RobinhoodTestnet;
        }
        if (target == keccak256("robinhood-mainnet")) {
            return DeploymentValidation.Target.RobinhoodMainnet;
        }
        revert("unknown deployment target");
    }
}
