// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { ILaunchErrors } from "../../src/interfaces/ILaunchErrors.sol";
import { IBondingCurveV2 } from "../../src/v2/interfaces/IBondingCurveV2.sol";
import { ILaunchEventsV2 } from "../../src/v2/interfaces/ILaunchEventsV2.sol";
import { BondingCurveV2TestBase } from "./utils/BondingCurveV2TestBase.sol";

/// @dev Has no receive function, so ETH payouts to it fail.
contract EthRejectingRecipient {
    function accept(IBondingCurveV2 curve) external {
        curve.acceptFeeRecipient();
    }

    function claim(IBondingCurveV2 curve) external returns (uint256) {
        return curve.claimCreatorFees();
    }
}

// Tests assert on events/reverts directly and send ETH to fixed test contracts.
// forge-lint: disable-start(reentrancy-events, unused-return, unsafe-typecast)
contract BondingCurveV2RecipientTest is BondingCurveV2TestBase {
    address internal constant NEW_RECIPIENT = address(0x4E1);

    function setUp() public override {
        super.setUp();
        vm.warp(LAUNCH_TIMESTAMP + SNIPE_DURATION);
    }

    function test_proposeAccept_movesFutureCreatorFees() public {
        _buy(ALICE, 1 ether);
        uint256 creditedToCreator = curve.creatorFeesOwed(CREATOR);
        assertGt(creditedToCreator, 0);

        vm.expectEmit(true, true, true, true, address(curve));
        emit ILaunchEventsV2.FeeRecipientProposed(address(token), CREATOR, NEW_RECIPIENT);
        vm.prank(CREATOR);
        curve.proposeFeeRecipient(NEW_RECIPIENT);
        assertEq(curve.pendingFeeRecipient(), NEW_RECIPIENT);
        assertEq(curve.feeRecipient(), CREATOR);

        vm.expectEmit(true, true, true, true, address(curve));
        emit ILaunchEventsV2.FeeRecipientAccepted(address(token), CREATOR, NEW_RECIPIENT);
        vm.prank(NEW_RECIPIENT);
        curve.acceptFeeRecipient();
        assertEq(curve.feeRecipient(), NEW_RECIPIENT);
        assertEq(curve.pendingFeeRecipient(), address(0));
        assertEq(curve.creator(), CREATOR);

        _buy(BOB, 1 ether);
        // Already credited balances stay with whoever they were credited to.
        assertEq(curve.creatorFeesOwed(CREATOR), creditedToCreator);
        assertGt(curve.creatorFeesOwed(NEW_RECIPIENT), 0);
        assertEq(
            curve.unclaimedCreatorFees(), creditedToCreator + curve.creatorFeesOwed(NEW_RECIPIENT)
        );

        vm.prank(CREATOR);
        assertEq(curve.claimCreatorFees(), creditedToCreator);
        assertEq(CREATOR.balance, creditedToCreator);
    }

    function test_propose_onlyCurrentRecipient() public {
        vm.prank(STRANGER);
        vm.expectRevert(
            abi.encodeWithSelector(
                IBondingCurveV2.UnauthorizedFeeRecipient.selector, STRANGER, CREATOR
            )
        );
        curve.proposeFeeRecipient(STRANGER);

        vm.prank(CREATOR);
        curve.proposeFeeRecipient(NEW_RECIPIENT);
        vm.prank(NEW_RECIPIENT);
        curve.acceptFeeRecipient();

        // The creator is identity, not payout: after the handover it cannot propose.
        vm.prank(CREATOR);
        vm.expectRevert(
            abi.encodeWithSelector(
                IBondingCurveV2.UnauthorizedFeeRecipient.selector, CREATOR, NEW_RECIPIENT
            )
        );
        curve.proposeFeeRecipient(CREATOR);
    }

    function test_propose_rejectsZeroAddress() public {
        vm.prank(CREATOR);
        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.ZeroAddress.selector, bytes32("feeRecipient"))
        );
        curve.proposeFeeRecipient(address(0));
    }

    function test_accept_onlyPendingRecipient() public {
        vm.prank(NEW_RECIPIENT);
        vm.expectRevert(
            abi.encodeWithSelector(
                IBondingCurveV2.UnauthorizedPendingFeeRecipient.selector, NEW_RECIPIENT, address(0)
            )
        );
        curve.acceptFeeRecipient();

        vm.prank(CREATOR);
        curve.proposeFeeRecipient(NEW_RECIPIENT);
        vm.prank(STRANGER);
        vm.expectRevert(
            abi.encodeWithSelector(
                IBondingCurveV2.UnauthorizedPendingFeeRecipient.selector, STRANGER, NEW_RECIPIENT
            )
        );
        curve.acceptFeeRecipient();
    }

    function test_proposal_canBeReplacedAndCancelled() public {
        vm.startPrank(CREATOR);
        curve.proposeFeeRecipient(STRANGER);
        curve.proposeFeeRecipient(NEW_RECIPIENT);
        assertEq(curve.pendingFeeRecipient(), NEW_RECIPIENT);

        vm.expectEmit(true, true, true, true, address(curve));
        emit ILaunchEventsV2.FeeRecipientProposed(address(token), CREATOR, address(0));
        curve.cancelFeeRecipientProposal();
        vm.stopPrank();
        assertEq(curve.pendingFeeRecipient(), address(0));

        vm.prank(NEW_RECIPIENT);
        vm.expectRevert(
            abi.encodeWithSelector(
                IBondingCurveV2.UnauthorizedPendingFeeRecipient.selector, NEW_RECIPIENT, address(0)
            )
        );
        curve.acceptFeeRecipient();

        vm.prank(STRANGER);
        vm.expectRevert(
            abi.encodeWithSelector(
                IBondingCurveV2.UnauthorizedFeeRecipient.selector, STRANGER, CREATOR
            )
        );
        curve.cancelFeeRecipientProposal();
    }

    function test_rejectingRecipient_onlyBlocksItsOwnClaim() public {
        EthRejectingRecipient rejector = new EthRejectingRecipient();
        vm.prank(CREATOR);
        curve.proposeFeeRecipient(address(rejector));
        rejector.accept(curve);

        _buy(ALICE, 1 ether);
        uint256 owedToRejector = curve.creatorFeesOwed(address(rejector));
        assertGt(owedToRejector, 0);

        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.EthTransferFailed.selector, address(rejector), owedToRejector
            )
        );
        rejector.claim(curve);

        // Trading and every other claim keep working.
        _buy(BOB, 1 ether);
        vm.prank(TREASURY);
        assertGt(curve.claimProtocolFees(), 0);
    }

    function test_claimCreatorFeesTo_paysChosenAddress() public {
        _buy(ALICE, 1 ether);
        uint256 owed = curve.creatorFeesOwed(CREATOR);

        vm.expectEmit(true, true, false, true, address(curve));
        emit ILaunchEventsV2.CreatorFeesClaimed(address(token), CREATOR, owed);
        vm.prank(CREATOR);
        assertEq(curve.claimCreatorFeesTo(NEW_RECIPIENT), owed);
        assertEq(NEW_RECIPIENT.balance, owed);
        assertEq(curve.creatorFeesOwed(CREATOR), 0);
    }

    function test_claimCreatorFees_strangerHasNothing() public {
        _buy(ALICE, 1 ether);
        vm.prank(STRANGER);
        vm.expectRevert(ILaunchErrors.NothingToClaim.selector);
        curve.claimCreatorFees();
    }
}
// forge-lint: disable-end(reentrancy-events, unused-return, unsafe-typecast)
