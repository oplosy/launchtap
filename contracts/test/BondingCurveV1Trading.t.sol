// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Test } from "forge-std/Test.sol";
import { Vm } from "forge-std/Vm.sol";
import { Clones } from "@openzeppelin/contracts/proxy/Clones.sol";
import { ERC20 } from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { SafeCast } from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import { BondingCurveV1 } from "../src/BondingCurveV1.sol";
import { ILaunchErrors } from "../src/interfaces/ILaunchErrors.sol";
import { ILaunchEvents } from "../src/interfaces/ILaunchEvents.sol";
import { LaunchToken } from "../src/LaunchToken.sol";
import { LaunchTypes } from "../src/types/LaunchTypes.sol";
import { BondingCurveV1Harness } from "./harness/BondingCurveV1Harness.sol";

contract TradingV2Factory {
    address private _pair;

    // Zero is used by validation tests outside this fixture.
    // forge-lint: disable-next-line(missing-zero-check)
    function setPair(address pair_) external {
        _pair = pair_;
    }

    function getPair(address, address) external view returns (address) {
        return _pair;
    }
}

contract TradingV2Pair {
    using SafeCast for uint256;

    address public immutable factory;
    address public immutable token0;
    address public immutable token1;
    uint256 public totalSupply;
    mapping(address account => uint256 amount) public balanceOf;

    uint112 private _reserve0;
    uint112 private _reserve1;

    // The production initializer, not this fixture, owns nonzero validation.
    // forge-lint: disable-next-line(missing-zero-check)
    constructor(address factory_, address token0_, address token1_) {
        factory = factory_;
        token0 = token0_;
        token1 = token1_;
    }

    function getReserves()
        external
        view
        returns (uint112 reserve0, uint112 reserve1, uint32 blockTimestampLast)
    {
        return (_reserve0, _reserve1, 0);
    }

    function mint(address to) external returns (uint256 liquidity) {
        uint256 balance0 = IERC20(token0).balanceOf(address(this));
        uint256 balance1 = IERC20(token1).balanceOf(address(this));
        uint256 amount0 = balance0 - _reserve0;
        uint256 amount1 = balance1 - _reserve1;
        liquidity = amount0 < amount1 ? amount0 : amount1;
        totalSupply += liquidity;
        balanceOf[to] += liquidity;
        _reserve0 = balance0.toUint112();
        _reserve1 = balance1.toUint112();
    }
}

// This WETH fixture intentionally retains ETH as backing for its minted test balance.
// forge-lint: disable-next-line(locked-ether)
contract TradingWETH is ERC20 {
    constructor() ERC20("Wrapped Ether", "WETH") { }

    function deposit() external payable {
        _mint(msg.sender, msg.value);
    }
}

contract TradingFactory {
    bool public tradingPaused;

    function setTradingPaused(bool paused) external {
        tradingPaused = paused;
    }

    function initializeCurve(
        BondingCurveV1Harness curve,
        LaunchTypes.CurveInitialization calldata initialization
    ) external {
        curve.initialize(initialization);
    }
}

contract RejectEther {
    receive() external payable {
        revert("ETH_REJECTED");
    }
}

// This fixture deliberately retains a successfully claimed refund.
// forge-lint: disable-next-line(locked-ether)
contract ToggleEtherRecipient {
    bool private _reject = true;

    function setReject(bool reject_) external {
        _reject = reject_;
    }

    function claim(BondingCurveV1 curve) external returns (uint256) {
        return curve.claimRefund();
    }

    receive() external payable {
        if (_reject) revert("ETH_REJECTED");
    }
}

contract AggregatorBuyer {
    BondingCurveV1 private immutable _curve;

    constructor(BondingCurveV1 curve_) {
        _curve = curve_;
    }

    function buyWithPartialValue(
        uint256 curveCallValue,
        address tokenRecipient,
        address refundRecipient,
        uint256 minTokensOut,
        uint256 deadline
    ) external returns (uint256 tokensOut, uint256 ethGrossUsed) {
        // The aggregator intentionally forwards only the declared curve-call value.
        // forge-lint: disable-next-line(arbitrary-send-eth)
        (tokensOut, ethGrossUsed) = _curve.buy{ value: curveCallValue }(
            tokenRecipient, refundRecipient, minTokensOut, deadline
        );
    }
}

// This adversarial fixture deliberately has no successful ETH withdrawal path.
// forge-lint: disable-next-line(locked-ether)
contract ReentrantRefundRecipient {
    BondingCurveV1 private immutable _curve;

    constructor(BondingCurveV1 curve_) {
        _curve = curve_;
    }

    function claim() external {
        // forge-lint: disable-next-line(unused-return)
        _curve.claimRefund();
    }

    receive() external payable {
        // forge-lint: disable-next-line(unused-return)
        _curve.claimRefund();
    }
}

contract BondingCurveV1TradingTest is Test {
    struct ExpectedTrade {
        address token;
        address trader;
        bool isBuy;
        uint256 ethGross;
        uint256 ethRefund;
        uint256 tokenAmount;
        uint256 protocolFee;
        uint256 creatorFee;
        uint256 newEthReserve;
        uint256 newTokenReserve;
    }

    uint256 private constant TOTAL_SUPPLY = 1_000_000_000 ether;
    uint256 private constant CURVE_TOKENS = 800_000_000 ether;
    uint256 private constant LP_TOKENS = 200_000_000 ether;
    uint256 private constant GRADUATION_ETH = 4.2 ether;
    uint256 private constant INITIAL_VIRTUAL_ETH = 1.4 ether;
    uint256 private constant INITIAL_VIRTUAL_TOKEN = 1_066_666_666_666_666_666_666_666_667;
    uint16 private constant TRADE_FEE_BPS = 100;
    uint16 private constant PROTOCOL_SHARE_BPS = 5000;
    uint256 private constant DEADLINE = type(uint256).max;

    address private constant CREATOR = address(0x2000);
    address private constant TREASURY = address(0x3000);
    address private constant ALICE = address(0xA11CE);
    address private constant BOB = address(0xB0B);

    bytes32 private constant REFUND_CREDITED_TOPIC =
        keccak256("RefundCredited(address,address,uint256)");
    bytes32 private constant TRADE_TOPIC = keccak256(
        "Trade(address,address,bool,uint256,uint256,uint256,uint256,uint256,uint256,uint256)"
    );
    bytes32 private constant FIELD_TOKEN_RECIPIENT = "tokenRecipient";
    bytes32 private constant FIELD_REFUND_RECIPIENT = "refundRecipient";
    bytes32 private constant FIELD_CLAIM_RECIPIENT = "claimRecipient";

    BondingCurveV1Harness private implementation;
    BondingCurveV1Harness private curve;
    LaunchToken private launchToken;
    TradingFactory private launchFactory;
    TradingV2Factory private uniswapFactory;
    TradingV2Pair private pair;
    TradingWETH private weth;
    ExpectedTrade private expectedTrade;

    function setUp() external {
        implementation = new BondingCurveV1Harness();
        launchFactory = new TradingFactory();
        uniswapFactory = new TradingV2Factory();
        weth = new TradingWETH();
        curve = BondingCurveV1Harness(Clones.clone(address(implementation)));
        launchToken = new LaunchToken("Launch", "LCH", address(curve), TOTAL_SUPPLY);
        pair = new TradingV2Pair(address(uniswapFactory), address(launchToken), address(weth));
        uniswapFactory.setPair(address(pair));
        launchToken.initializePair(address(pair));

        LaunchTypes.CurveInitialization memory initialization = LaunchTypes.CurveInitialization({
            factory: address(launchFactory),
            implementation: address(implementation),
            token: address(launchToken),
            creator: CREATOR,
            protocolTreasury: TREASURY,
            weth: address(weth),
            uniswapFactory: address(uniswapFactory),
            lpPair: address(pair),
            parameters: _defaultParameters()
        });
        launchFactory.initializeCurve(curve, initialization);

        vm.deal(ALICE, 100 ether);
        vm.deal(BOB, 100 ether);
    }

    function testNormalBuyUpdatesStateFeesAndEventExactly() external {
        uint256 supplied = 1 ether;
        (
            uint256 grossUsed,
            uint256 tokensOut,
            uint256 protocolFee,
            uint256 creatorFee,
            uint256 refund,
            // The graduation flag is tested in dedicated final-fill cases.
            // forge-lint: disable-next-line(unused-return)
        ) = curve.quoteBuy(supplied);
        uint256 expectedX = INITIAL_VIRTUAL_ETH + supplied - protocolFee - creatorFee;
        uint256 expectedY = INITIAL_VIRTUAL_TOKEN - tokensOut;

        _expectTrade(
            address(launchToken),
            ALICE,
            true,
            grossUsed,
            refund,
            tokensOut,
            protocolFee,
            creatorFee,
            expectedX,
            expectedY
        );

        vm.prank(ALICE);
        (
            uint256 actualTokensOut,
            uint256 actualGrossUsed
            // forge-lint: disable-next-line(arbitrary-send-eth)
        ) = curve.buy{ value: supplied }(ALICE, ALICE, tokensOut, DEADLINE);
        _assertExpectedTrade();

        assertEq(actualTokensOut, tokensOut);
        assertEq(actualGrossUsed, supplied);
        assertEq(refund, 0);
        assertEq(launchToken.balanceOf(ALICE), tokensOut);
        assertEq(curve.virtualEthReserve(), expectedX);
        assertEq(curve.virtualTokenReserve(), expectedY);
        assertEq(curve.unclaimedProtocolFees(), protocolFee);
        assertEq(curve.unclaimedCreatorFees(), creatorFee);
        _assertExactAccounting();
    }

    function testFinalFillImmediateRefundUsesTradeRefundWithoutCredit() external {
        uint256 supplied = 10 ether;
        (
            uint256 grossUsed,
            uint256 tokensOut,
            uint256 protocolFee,
            uint256 creatorFee,
            uint256 refund,
            bool graduates
        ) = curve.quoteBuy(supplied);
        uint256 aliceBefore = ALICE.balance;

        _expectTrade(
            address(launchToken),
            ALICE,
            true,
            grossUsed,
            refund,
            tokensOut,
            protocolFee,
            creatorFee,
            INITIAL_VIRTUAL_ETH + GRADUATION_ETH,
            INITIAL_VIRTUAL_TOKEN - CURVE_TOKENS
        );
        vm.prank(ALICE);
        (
            uint256 actualTokensOut,
            uint256 actualGrossUsed
            // forge-lint: disable-next-line(arbitrary-send-eth)
        ) = curve.buy{ value: supplied }(ALICE, ALICE, tokensOut, DEADLINE);
        Vm.Log[] memory logs = _assertExpectedTrade();

        assertTrue(graduates);
        assertGt(refund, 0);
        assertEq(grossUsed + refund, supplied);
        assertEq(actualGrossUsed, grossUsed);
        assertEq(actualTokensOut, CURVE_TOKENS);
        assertEq(ALICE.balance, aliceBefore - grossUsed);
        assertEq(curve.pendingRefund(ALICE), 0);
        assertEq(_countTopic(logs, REFUND_CREDITED_TOPIC), 0);
        assertEq(curve.unclaimedProtocolFees(), protocolFee);
        assertEq(curve.unclaimedCreatorFees(), creatorFee);
        _assertExactAccounting();
    }

    function testFinalFillFailedRefundEmitsTradeAndCreditsSameExcess() external {
        RejectEther rejector = new RejectEther();
        uint256 supplied = 10 ether;
        (
            uint256 grossUsed,
            uint256 tokensOut,
            uint256 protocolFee,
            uint256 creatorFee,
            uint256 refund,
            // forge-lint: disable-next-line(unused-return)
        ) = curve.quoteBuy(supplied);

        _expectTrade(
            address(launchToken),
            ALICE,
            true,
            grossUsed,
            refund,
            tokensOut,
            protocolFee,
            creatorFee,
            INITIAL_VIRTUAL_ETH + GRADUATION_ETH,
            INITIAL_VIRTUAL_TOKEN - CURVE_TOKENS
        );
        vm.prank(ALICE);
        // This call intentionally tests a user-selected refund recipient.
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: supplied }(ALICE, address(rejector), tokensOut, DEADLINE);
        Vm.Log[] memory logs = _assertExpectedTrade();

        assertEq(grossUsed + refund, supplied);
        _assertIndexedAmountEvent(
            logs, REFUND_CREDITED_TOPIC, address(launchToken), address(rejector), refund
        );
        assertEq(curve.pendingRefund(address(rejector)), refund);
        assertEq(curve.totalPendingRefunds(), refund);
        _assertExactAccounting();
    }

    function testFactoryDeveloperBuyUsesCreatorAsTraderTokenAndRefundRecipient() external {
        uint256 supplied = 0.01 ether;
        (
            uint256 grossUsed,
            uint256 tokensOut,
            uint256 protocolFee,
            uint256 creatorFee,
            uint256 refund,
            // forge-lint: disable-next-line(unused-return)
        ) = curve.quoteBuy(supplied);

        _expectTrade(
            address(launchToken),
            CREATOR,
            true,
            grossUsed,
            refund,
            tokensOut,
            protocolFee,
            creatorFee,
            INITIAL_VIRTUAL_ETH + grossUsed - protocolFee - creatorFee,
            INITIAL_VIRTUAL_TOKEN - tokensOut
        );

        uint256 actualTokensOut;
        uint256 actualGrossUsed;
        vm.deal(address(launchFactory), supplied);
        vm.prank(address(launchFactory));
        // The test intentionally funds the factory-routed developer buy.
        (actualTokensOut, actualGrossUsed) =
        // forge-lint: disable-next-line(arbitrary-send-eth)
        curve.buyFor{ value: supplied }(CREATOR, CREATOR, CREATOR, tokensOut, DEADLINE);
        _assertExpectedTrade();

        assertEq(actualTokensOut, tokensOut);
        assertEq(actualGrossUsed, grossUsed);
        assertEq(launchToken.balanceOf(CREATOR), tokensOut);
        assertEq(curve.pendingRefund(CREATOR), 0);
    }

    function testAggregatorAccountingUsesCurveCallValueInsteadOfOuterValue() external {
        AggregatorBuyer aggregator = new AggregatorBuyer(curve);
        uint256 outerValue = 2 ether;
        uint256 curveCallValue = 1 ether;
        (
            uint256 grossUsed,
            uint256 tokensOut,
            uint256 protocolFee,
            uint256 creatorFee,
            uint256 refund,
            // forge-lint: disable-next-line(unused-return)
        ) = curve.quoteBuy(curveCallValue);

        _expectTrade(
            address(launchToken),
            address(aggregator),
            true,
            grossUsed,
            refund,
            tokensOut,
            protocolFee,
            creatorFee,
            INITIAL_VIRTUAL_ETH + grossUsed - protocolFee - creatorFee,
            INITIAL_VIRTUAL_TOKEN - tokensOut
        );

        uint256 actualTokensOut;
        uint256 actualGrossUsed;
        vm.deal(address(aggregator), outerValue);
        vm.prank(ALICE);
        // The funded aggregator intentionally forwards only the nested curve-call value.
        (actualTokensOut, actualGrossUsed) =
            aggregator.buyWithPartialValue(curveCallValue, ALICE, ALICE, tokensOut, DEADLINE);
        _assertExpectedTrade();

        assertEq(actualTokensOut, tokensOut);
        assertEq(actualGrossUsed, grossUsed);
        assertEq(grossUsed, curveCallValue);
        assertEq(address(aggregator).balance, outerValue - curveCallValue);
        assertEq(address(curve).balance, curveCallValue);
        _assertExactAccounting();
    }

    function testFullSellRestoresInitialReservesAndNeverPaysVirtualEth() external {
        uint256 supplied = 1 ether;
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        (uint256 tokensOut,) = curve.buy{ value: supplied }(ALICE, ALICE, 0, DEADLINE);
        (uint256 ethOut, uint256 ethGross, uint256 protocolFee, uint256 creatorFee) =
            curve.quoteSell(tokensOut);

        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        launchToken.approve(address(curve), tokensOut);
        uint256 aliceBefore = ALICE.balance;

        _expectTrade(
            address(launchToken),
            ALICE,
            false,
            ethGross,
            0,
            tokensOut,
            protocolFee,
            creatorFee,
            INITIAL_VIRTUAL_ETH,
            INITIAL_VIRTUAL_TOKEN
        );
        vm.prank(ALICE);
        uint256 actualEthOut = curve.sell(tokensOut, ALICE, ethOut, DEADLINE);
        _assertExpectedTrade();

        assertEq(actualEthOut, ethOut);
        assertEq(ALICE.balance, aliceBefore + ethOut);
        assertEq(curve.virtualEthReserve(), INITIAL_VIRTUAL_ETH);
        assertEq(curve.virtualTokenReserve(), INITIAL_VIRTUAL_TOKEN);
        assertEq(curve.realCurveEth(), 0);
        assertEq(curve.tokensSold(), 0);
        assertEq(launchToken.balanceOf(ALICE), 0);
        _assertExactAccounting();
    }

    function testSellRejectsOversellBeforeTokenTransfer() external {
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        (uint256 tokensOut,) = curve.buy{ value: 1 ether }(ALICE, ALICE, 0, DEADLINE);
        uint256 attempted = tokensOut + 1;

        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        launchToken.approve(address(curve), attempted);
        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.Oversell.selector, attempted, tokensOut)
        );
        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        curve.sell(attempted, ALICE, 0, DEADLINE);

        assertEq(launchToken.balanceOf(ALICE), tokensOut);
    }

    function testSellFailedDeliveryCreditsRecipientAndDoesNotBlockAnotherTrader() external {
        ToggleEtherRecipient recipient = new ToggleEtherRecipient();
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        (uint256 aliceTokens,) = curve.buy{ value: 1 ether }(ALICE, ALICE, 0, DEADLINE);
        uint256 sold = aliceTokens / 2;
        // Only net output is needed for refund-credit verification.
        // forge-lint: disable-next-line(unused-return)
        (uint256 ethOut,,,) = curve.quoteSell(sold);

        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        launchToken.approve(address(curve), sold);
        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        curve.sell(sold, address(recipient), ethOut, DEADLINE);

        assertEq(curve.pendingRefund(address(recipient)), ethOut);
        vm.prank(BOB);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        (uint256 bobTokens,) = curve.buy{ value: 0.1 ether }(BOB, BOB, 0, DEADLINE);
        assertGt(bobTokens, 0);

        recipient.setReject(false);
        vm.expectEmit(true, true, false, true, address(curve));
        // forge-lint: disable-next-line(reentrancy-events)
        emit ILaunchEvents.RefundClaimed(address(launchToken), address(recipient), ethOut);
        uint256 recipientBefore = address(recipient).balance;
        assertEq(recipient.claim(curve), ethOut);
        assertEq(address(recipient).balance, recipientBefore + ethOut);
        assertEq(curve.pendingRefund(address(recipient)), 0);
        _assertExactAccounting();
    }

    function testReentrantRefundRecipientGetsCreditAndClaimStateIsPreserved() external {
        ReentrantRefundRecipient recipient = new ReentrantRefundRecipient(curve);
        uint256 supplied = 10 ether;
        // Only the refund leg is needed for this adversarial recipient test.
        // forge-lint: disable-next-line(unused-return)
        (,,,, uint256 refund,) = curve.quoteBuy(supplied);

        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: supplied }(ALICE, address(recipient), 0, DEADLINE);
        assertEq(curve.pendingRefund(address(recipient)), refund);

        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.EthTransferFailed.selector, address(recipient), refund
            )
        );
        recipient.claim();

        assertEq(curve.pendingRefund(address(recipient)), refund);
        assertEq(curve.totalPendingRefunds(), refund);
        _assertExactAccounting();
    }

    function testClaimsAreAuthorizedAndAvailableWhilePausedAndGraduated() external {
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 10 ether }(ALICE, ALICE, 0, DEADLINE);
        uint256 creatorFees = curve.unclaimedCreatorFees();
        uint256 protocolFees = curve.unclaimedProtocolFees();
        launchFactory.setTradingPaused(true);

        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.UnauthorizedCreatorClaim.selector, ALICE, CREATOR)
        );
        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        curve.claimCreatorFees();

        uint256 creatorBefore = CREATOR.balance;
        vm.expectEmit(true, true, false, true, address(curve));
        // forge-lint: disable-next-line(reentrancy-events)
        emit ILaunchEvents.CreatorFeesClaimed(address(launchToken), CREATOR, creatorFees);
        vm.prank(CREATOR);
        assertEq(curve.claimCreatorFees(), creatorFees);
        assertEq(CREATOR.balance, creatorBefore + creatorFees);

        uint256 treasuryBefore = TREASURY.balance;
        vm.expectEmit(true, true, false, true, address(curve));
        // forge-lint: disable-next-line(reentrancy-events)
        emit ILaunchEvents.ProtocolFeesClaimed(address(launchToken), TREASURY, protocolFees);
        vm.prank(TREASURY);
        assertEq(curve.claimProtocolFees(), protocolFees);
        assertEq(TREASURY.balance, treasuryBefore + protocolFees);

        vm.expectRevert(ILaunchErrors.NothingToClaim.selector);
        vm.prank(CREATOR);
        // forge-lint: disable-next-line(unused-return)
        curve.claimCreatorFees();
        _assertExactAccounting();
    }

    function testPausedDeadlineAndSlippageChecksLeaveStateUnchanged() external {
        uint256 initialTokenBalance = launchToken.balanceOf(address(curve));
        launchFactory.setTradingPaused(true);
        vm.expectRevert(ILaunchErrors.TradingPaused.selector);
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 1 ether }(ALICE, ALICE, 0, DEADLINE);

        launchFactory.setTradingPaused(false);
        uint256 expired = block.timestamp - 1;
        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.DeadlineExpired.selector, expired, block.timestamp)
        );
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 1 ether }(ALICE, ALICE, 0, expired);

        // Only token output is needed for the slippage boundary.
        // forge-lint: disable-next-line(unused-return)
        (, uint256 tokensOut,,,,) = curve.quoteBuy(1 ether);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.SlippageExceeded.selector, tokensOut + 1, tokensOut
            )
        );
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 1 ether }(ALICE, ALICE, tokensOut + 1, DEADLINE);

        assertEq(curve.virtualEthReserve(), INITIAL_VIRTUAL_ETH);
        assertEq(curve.virtualTokenReserve(), INITIAL_VIRTUAL_TOKEN);
        assertEq(launchToken.balanceOf(address(curve)), initialTokenBalance);
        assertEq(address(curve).balance, 0);
    }

    function testSellDeadlineAndSlippageChecksLeaveStateAndTokensUnchanged() external {
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        (uint256 tokensOut,) = curve.buy{ value: 1 ether }(ALICE, ALICE, 0, DEADLINE);
        // Only net output is needed for the slippage boundary.
        // forge-lint: disable-next-line(unused-return)
        (uint256 ethOut,,,) = curve.quoteSell(tokensOut);
        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        launchToken.approve(address(curve), tokensOut);
        uint256 xBefore = curve.virtualEthReserve();
        uint256 yBefore = curve.virtualTokenReserve();

        uint256 expired = block.timestamp - 1;
        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.DeadlineExpired.selector, expired, block.timestamp)
        );
        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        curve.sell(tokensOut, ALICE, 0, expired);

        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.SlippageExceeded.selector, ethOut + 1, ethOut)
        );
        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        curve.sell(tokensOut, ALICE, ethOut + 1, DEADLINE);

        assertEq(curve.virtualEthReserve(), xBefore);
        assertEq(curve.virtualTokenReserve(), yBefore);
        assertEq(launchToken.balanceOf(ALICE), tokensOut);
    }

    function testSellRemainsAvailableWhileTradingIsPaused() external {
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        (uint256 tokensOut,) = curve.buy{ value: 1 ether }(ALICE, ALICE, 0, DEADLINE);
        // Only net output is needed for the exit assertion.
        // forge-lint: disable-next-line(unused-return)
        (uint256 ethOut,,,) = curve.quoteSell(tokensOut);
        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        launchToken.approve(address(curve), tokensOut);

        launchFactory.setTradingPaused(true);
        vm.expectRevert(ILaunchErrors.TradingPaused.selector);
        vm.prank(BOB);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 1 ether }(BOB, BOB, 0, DEADLINE);

        uint256 aliceBefore = ALICE.balance;
        vm.prank(ALICE);
        assertEq(curve.sell(tokensOut, ALICE, ethOut, DEADLINE), ethOut);
        assertEq(ALICE.balance, aliceBefore + ethOut);
        assertEq(launchToken.balanceOf(ALICE), 0);
        _assertExactAccounting();
    }

    function testBuyRejectsPairAndCurveAsTokenRecipient() external {
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.InvalidRecipient.selector, FIELD_TOKEN_RECIPIENT, address(pair)
            )
        );
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 1 gwei }(address(pair), ALICE, 0, DEADLINE);

        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.InvalidRecipient.selector, FIELD_TOKEN_RECIPIENT, address(curve)
            )
        );
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 1 gwei }(address(curve), ALICE, 0, DEADLINE);

        assertEq(launchToken.balanceOf(address(pair)), 0);
        assertEq(curve.virtualEthReserve(), INITIAL_VIRTUAL_ETH);
    }

    function testRefundCanBeClaimedToAnotherRecipient() external {
        RejectEther rejecting = new RejectEther();
        uint256 supplied = 10 ether;
        // Only the refund leg is needed for this recipient test.
        // forge-lint: disable-next-line(unused-return)
        (,,,, uint256 refund,) = curve.quoteBuy(supplied);
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: supplied }(ALICE, address(rejecting), 0, DEADLINE);
        assertEq(curve.pendingRefund(address(rejecting)), refund);

        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.ZeroAddress.selector, FIELD_CLAIM_RECIPIENT)
        );
        vm.prank(address(rejecting));
        // forge-lint: disable-next-line(unused-return)
        curve.claimRefundTo(address(0));

        uint256 bobBefore = BOB.balance;
        vm.expectEmit(true, true, false, true, address(curve));
        // forge-lint: disable-next-line(reentrancy-events)
        emit ILaunchEvents.RefundClaimed(address(launchToken), address(rejecting), refund);
        vm.prank(address(rejecting));
        assertEq(curve.claimRefundTo(BOB), refund);
        assertEq(BOB.balance, bobBefore + refund);
        assertEq(curve.pendingRefund(address(rejecting)), 0);
        _assertExactAccounting();
    }

    function testCreatorFeesCanBeClaimedToAnotherRecipient() external {
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 1 ether }(ALICE, ALICE, 0, DEADLINE);
        uint256 creatorFees = curve.unclaimedCreatorFees();

        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.UnauthorizedCreatorClaim.selector, ALICE, CREATOR)
        );
        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        curve.claimCreatorFeesTo(ALICE);

        uint256 bobBefore = BOB.balance;
        vm.expectEmit(true, true, false, true, address(curve));
        // forge-lint: disable-next-line(reentrancy-events)
        emit ILaunchEvents.CreatorFeesClaimed(address(launchToken), CREATOR, creatorFees);
        vm.prank(CREATOR);
        assertEq(curve.claimCreatorFeesTo(BOB), creatorFees);
        assertEq(BOB.balance, bobBefore + creatorFees);
        assertEq(curve.unclaimedCreatorFees(), 0);
        _assertExactAccounting();
    }

    function testForcedEthDoesNotChangeCurveOrFeeAccounting() external {
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 1 ether }(ALICE, ALICE, 0, DEADLINE);
        uint256 xBefore = curve.virtualEthReserve();
        uint256 yBefore = curve.virtualTokenReserve();
        uint256 creatorFees = curve.unclaimedCreatorFees();
        uint256 protocolFees = curve.unclaimedProtocolFees();
        uint256 forced = 7 ether;

        vm.deal(address(curve), address(curve).balance + forced);

        assertEq(curve.virtualEthReserve(), xBefore);
        assertEq(curve.virtualTokenReserve(), yBefore);
        assertEq(curve.unclaimedCreatorFees(), creatorFees);
        assertEq(curve.unclaimedProtocolFees(), protocolFees);
        assertEq(address(curve).balance, curve.realCurveEth() + creatorFees + protocolFees + forced);
    }

    function testRejectsZeroRecipientsAndUnauthorizedBuyFor() external {
        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.ZeroAddress.selector, FIELD_TOKEN_RECIPIENT)
        );
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 1 ether }(address(0), ALICE, 0, DEADLINE);

        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.ZeroAddress.selector, FIELD_REFUND_RECIPIENT)
        );
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 1 ether }(ALICE, address(0), 0, DEADLINE);

        vm.expectRevert(abi.encodeWithSelector(ILaunchErrors.UnauthorizedFactory.selector, ALICE));
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buyFor{ value: 1 ether }(ALICE, ALICE, ALICE, 0, DEADLINE);
    }

    function testFuzzBuySellAccountingMatchesAllBuckets(uint96 grossSeed, uint96 sellSeed)
        external
    {
        uint256 supplied = bound(uint256(grossSeed), 1e12, 3 ether);
        vm.prank(ALICE);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        (uint256 tokensOut,) = curve.buy{ value: supplied }(ALICE, ALICE, 0, DEADLINE);
        uint256 tokensIn = bound(uint256(sellSeed), (tokensOut + 1) / 2, tokensOut);
        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        launchToken.approve(address(curve), tokensIn);
        vm.prank(ALICE);
        // forge-lint: disable-next-line(unused-return)
        curve.sell(tokensIn, ALICE, 0, DEADLINE);

        assertGe(
            curve.virtualEthReserve() * curve.virtualTokenReserve(),
            INITIAL_VIRTUAL_ETH * INITIAL_VIRTUAL_TOKEN
        );
        _assertExactAccounting();
    }

    function _assertExactAccounting() private view {
        uint256 curveEth = curve.phase() == LaunchTypes.Phase.Curve ? curve.realCurveEth() : 0;
        uint256 required = curveEth + curve.unclaimedCreatorFees() + curve.unclaimedProtocolFees()
            + curve.totalPendingRefunds();
        assertEq(address(curve).balance, required);
    }

    function _expectTrade(
        address token,
        address trader,
        bool isBuy,
        uint256 ethGross,
        uint256 ethRefund,
        uint256 tokenAmount,
        uint256 protocolFee,
        uint256 creatorFee,
        uint256 newEthReserve,
        uint256 newTokenReserve
    ) private {
        expectedTrade = ExpectedTrade({
            token: token,
            trader: trader,
            isBuy: isBuy,
            ethGross: ethGross,
            ethRefund: ethRefund,
            tokenAmount: tokenAmount,
            protocolFee: protocolFee,
            creatorFee: creatorFee,
            newEthReserve: newEthReserve,
            newTokenReserve: newTokenReserve
        });
        vm.recordLogs();
    }

    function _assertExpectedTrade() private view returns (Vm.Log[] memory logs) {
        logs = vm.getRecordedLogs();
        bytes32 expectedDataHash = keccak256(
            abi.encode(
                expectedTrade.isBuy,
                expectedTrade.ethGross,
                expectedTrade.ethRefund,
                expectedTrade.tokenAmount,
                expectedTrade.protocolFee,
                expectedTrade.creatorFee,
                expectedTrade.newEthReserve,
                expectedTrade.newTokenReserve
            )
        );

        uint256 matches = 0;
        for (uint256 i = 0; i < logs.length; ++i) {
            if (logs[i].emitter != address(curve)) continue;
            if (logs[i].topics.length != 3 || logs[i].topics[0] != TRADE_TOPIC) continue;
            assertEq(abi.decode(abi.encode(logs[i].topics[1]), (address)), expectedTrade.token);
            assertEq(abi.decode(abi.encode(logs[i].topics[2]), (address)), expectedTrade.trader);
            assertEq(keccak256(logs[i].data), expectedDataHash);
            ++matches;
        }
        assertEq(matches, 1);
    }

    function _assertIndexedAmountEvent(
        Vm.Log[] memory logs,
        bytes32 topic,
        address indexedToken,
        address indexedAccount,
        uint256 amount
    ) private view {
        uint256 matches = 0;
        for (uint256 i = 0; i < logs.length; ++i) {
            if (logs[i].emitter != address(curve)) continue;
            if (logs[i].topics.length != 3 || logs[i].topics[0] != topic) continue;
            assertEq(abi.decode(abi.encode(logs[i].topics[1]), (address)), indexedToken);
            assertEq(abi.decode(abi.encode(logs[i].topics[2]), (address)), indexedAccount);
            assertEq(abi.decode(logs[i].data, (uint256)), amount);
            ++matches;
        }
        assertEq(matches, 1);
    }

    function _countTopic(Vm.Log[] memory logs, bytes32 topic) private pure returns (uint256 count) {
        for (uint256 i = 0; i < logs.length; ++i) {
            if (logs[i].topics.length > 0 && logs[i].topics[0] == topic) ++count;
        }
    }

    function _defaultParameters() private pure returns (LaunchTypes.CurveParameters memory) {
        return LaunchTypes.CurveParameters({
            totalSupply: TOTAL_SUPPLY,
            curveTokens: CURVE_TOKENS,
            lpTokens: LP_TOKENS,
            graduationEth: GRADUATION_ETH,
            initialVirtualEth: INITIAL_VIRTUAL_ETH,
            initialVirtualToken: INITIAL_VIRTUAL_TOKEN,
            tradeFeeBps: TRADE_FEE_BPS,
            protocolShareBps: PROTOCOL_SHARE_BPS
        });
    }
}
