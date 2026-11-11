// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { CurveMath } from "../../src/libraries/CurveMath.sol";
import { ILaunchErrors } from "../../src/interfaces/ILaunchErrors.sol";
import { ILaunchEventsV2 } from "../../src/v2/interfaces/ILaunchEventsV2.sol";
import { BondingCurveV2TestBase } from "./utils/BondingCurveV2TestBase.sol";

// Quotes are compared field by field; unused tuple members are intentional.
// Test contracts send ETH to fixed test routers and assert on events/reverts directly.
// forge-lint: disable-start(unused-return, arbitrary-send-eth, reentrancy-events, erc20-unchecked-transfer, unsafe-typecast)
contract BondingCurveV2TradingTest is BondingCurveV2TestBase {
    function test_initialize_snapshotsLaunchAndRecipient() public view {
        assertEq(curve.ENGINE_VERSION(), 2);
        assertEq(curve.launchTime(), LAUNCH_TIMESTAMP);
        assertEq(curve.feeRecipient(), CREATOR);
        assertEq(curve.pendingFeeRecipient(), address(0));
        assertEq(curve.protocolTreasury(), TREASURY);
        assertEq(curve.lpCreatorShareBps(), LP_CREATOR_SHARE_BPS);
        assertEq(curve.antiSnipeProfile().startBps, SNIPE_START_BPS);
        assertEq(curve.antiSnipeProfile().durationSeconds, SNIPE_DURATION);
        assertEq(curve.hook(), address(hook));
        assertEq(curve.locker(), address(locker));
    }

    function test_currentTaxBps_decaysLinearly() public {
        assertEq(curve.currentTaxBps(), 5000);
        vm.warp(LAUNCH_TIMESTAMP + 30);
        assertEq(curve.currentTaxBps(), 2500);
        vm.warp(LAUNCH_TIMESTAMP + 59);
        assertEq(curve.currentTaxBps(), 83);
        vm.warp(LAUNCH_TIMESTAMP + 60);
        assertEq(curve.currentTaxBps(), 0);
    }

    function test_buyAtLaunchSecond_chargesStartTax() public {
        CurveMath.BuyQuote memory v1 = _v1QuoteBuy(0.5 ether);
        (uint256 used, uint256 out,,, uint256 tax, uint256 refund, bool graduates) =
            curve.quoteBuy(1 ether);
        assertEq(tax, 0.5 ether);
        assertEq(used, 1 ether);
        assertEq(out, v1.tokensOut);
        assertEq(refund, 0);
        assertFalse(graduates);

        uint256 received = _buy(ALICE, 1 ether);
        assertEq(received, v1.tokensOut);
        assertEq(token.balanceOf(ALICE), v1.tokensOut);
        assertEq(curve.unclaimedSnipeTax(), 0.5 ether);
        assertEq(curve.unclaimedProtocolFees(), v1.protocolFee);
        assertEq(curve.creatorFeesOwed(CREATOR), v1.creatorFee);
    }

    function test_buyAfterWindow_noTax() public {
        vm.warp(LAUNCH_TIMESTAMP + SNIPE_DURATION);
        CurveMath.BuyQuote memory v1 = _v1QuoteBuy(1 ether);
        (,,,, uint256 tax,,) = curve.quoteBuy(1 ether);
        assertEq(tax, 0);

        assertEq(_buy(ALICE, 1 ether), v1.tokensOut);
        assertEq(curve.unclaimedSnipeTax(), 0);
    }

    function test_devBuy_isExemptFromTax() public {
        CurveMath.BuyQuote memory v1 = _v1QuoteBuy(0.1 ether);
        (uint256 out,) = factory.developerBuy{ value: 0.1 ether }(curve, CREATOR, block.timestamp);
        assertEq(out, v1.tokensOut);
        assertEq(curve.unclaimedSnipeTax(), 0);
    }

    function test_buyFor_onlyFactory() public {
        vm.prank(ALICE);
        vm.expectRevert(abi.encodeWithSelector(ILaunchErrors.UnauthorizedFactory.selector, ALICE));
        curve.buyFor{ value: 1 ether }(ALICE, ALICE, ALICE, 0, block.timestamp);
    }

    function test_sell_neverTaxed() public {
        uint256 bought = _buy(ALICE, 1 ether);
        vm.warp(LAUNCH_TIMESTAMP + 10);
        assertGt(curve.currentTaxBps(), 0);

        uint256 tokensIn = bought / 2;
        CurveMath.SellQuote memory v1 = CurveMath.quoteSell(
            INITIAL_VIRTUAL_ETH * INITIAL_VIRTUAL_TOKEN,
            curve.virtualEthReserve(),
            curve.virtualTokenReserve(),
            curve.tokensSold(),
            tokensIn,
            TRADE_FEE_BPS,
            PROTOCOL_SHARE_BPS
        );
        uint256 taxBefore = curve.unclaimedSnipeTax();

        vm.startPrank(ALICE);
        assertTrue(token.approve(address(curve), tokensIn));
        vm.expectEmit(true, true, false, true, address(curve));
        emit ILaunchEventsV2.Trade(
            address(token),
            ALICE,
            false,
            v1.ethGross,
            0,
            tokensIn,
            v1.protocolFee,
            v1.creatorFee,
            v1.newVirtualEth,
            v1.newVirtualToken,
            0
        );
        uint256 ethOut = curve.sell(tokensIn, ALICE, 0, block.timestamp);
        vm.stopPrank();

        assertEq(ethOut, v1.ethOut);
        assertEq(curve.unclaimedSnipeTax(), taxBefore);
    }

    function testFuzz_quoteMatchesExecution(uint256 amount, uint256 elapsed) public {
        amount = bound(amount, 1e9, 20 ether);
        elapsed = bound(elapsed, 0, 120);
        vm.warp(LAUNCH_TIMESTAMP + elapsed);

        (
            uint256 used,
            uint256 out,
            uint256 protocolFee,
            uint256 creatorFee,
            uint256 tax,
            uint256 refund,
        ) = curve.quoteBuy(amount);
        uint256 balanceBefore = ALICE.balance;
        uint256 protocolBefore = curve.unclaimedProtocolFees();

        assertEq(_buy(ALICE, amount), out);
        assertEq(used + refund, amount);
        assertEq(balanceBefore - ALICE.balance, used);
        assertEq(curve.unclaimedSnipeTax(), tax);
        assertEq(curve.creatorFeesOwed(CREATOR), creatorFee);
        // Graduation can add the pool rounding remainder to the protocol bucket.
        assertGe(curve.unclaimedProtocolFees() - protocolBefore, protocolFee);
    }

    function test_tradeEvent_ethGrossIncludesTax() public {
        vm.warp(LAUNCH_TIMESTAMP + 30);
        uint256 virtualEthBefore = curve.virtualEthReserve();
        (uint256 used, uint256 out, uint256 protocolFee, uint256 creatorFee, uint256 tax,,) =
            curve.quoteBuy(1 ether);
        assertEq(tax, 0.25 ether);

        vm.expectEmit(true, true, false, false, address(curve));
        emit ILaunchEventsV2.Trade(address(token), ALICE, true, 0, 0, 0, 0, 0, 0, 0, 0);
        _buy(ALICE, 1 ether);

        uint256 curveEth = curve.virtualEthReserve() - virtualEthBefore;
        assertEq(used, tax + protocolFee + creatorFee + curveEth);
        assertEq(token.balanceOf(ALICE), out);
    }

    function test_snipeTaxClaim_toTreasuryOnly() public {
        _buy(ALICE, 1 ether);

        vm.prank(STRANGER);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.UnauthorizedProtocolClaim.selector, STRANGER, TREASURY
            )
        );
        curve.claimSnipeTax();

        vm.prank(CREATOR);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.UnauthorizedProtocolClaim.selector, CREATOR, TREASURY
            )
        );
        curve.claimSnipeTax();

        vm.expectEmit(true, true, false, true, address(curve));
        emit ILaunchEventsV2.SnipeTaxClaimed(address(token), TREASURY, 0.5 ether);
        vm.prank(TREASURY);
        assertEq(curve.claimSnipeTax(), 0.5 ether);
        assertEq(TREASURY.balance, 0.5 ether);
        assertEq(curve.unclaimedSnipeTax(), 0);

        vm.prank(TREASURY);
        vm.expectRevert(ILaunchErrors.NothingToClaim.selector);
        curve.claimSnipeTax();
    }

    function test_tradingPause_blocksBuysNotSells() public {
        uint256 bought = _buy(ALICE, 1 ether);
        factory.setTradingPaused(true);

        vm.prank(ALICE);
        vm.expectRevert(ILaunchErrors.TradingPaused.selector);
        curve.buy{ value: 1 ether }(ALICE, ALICE, 0, block.timestamp);

        vm.startPrank(ALICE);
        assertTrue(token.approve(address(curve), bought));
        assertGt(curve.sell(bought, ALICE, 0, block.timestamp), 0);
        vm.stopPrank();
    }

    function test_buy_rejectsCurveAndPoolManagerAsRecipient() public {
        vm.startPrank(ALICE);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.InvalidRecipient.selector, bytes32("tokenRecipient"), address(curve)
            )
        );
        curve.buy{ value: 1 ether }(address(curve), ALICE, 0, block.timestamp);

        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.InvalidRecipient.selector,
                bytes32("tokenRecipient"),
                address(poolManager)
            )
        );
        curve.buy{ value: 1 ether }(address(poolManager), ALICE, 0, block.timestamp);
        vm.stopPrank();
    }
}
// forge-lint: disable-end(unused-return, arbitrary-send-eth, reentrancy-events, erc20-unchecked-transfer, unsafe-typecast)
