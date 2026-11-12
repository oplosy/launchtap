// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { ERC20 } from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import { IERC721Receiver } from "@openzeppelin/contracts/token/ERC721/IERC721Receiver.sol";
import { SafeCast } from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import { IHooks } from "@uniswap/v4-core/src/interfaces/IHooks.sol";
import { TickMath } from "@uniswap/v4-core/src/libraries/TickMath.sol";
import { PoolSwapTest } from "@uniswap/v4-core/src/test/PoolSwapTest.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { SwapParams } from "@uniswap/v4-core/src/types/PoolOperation.sol";
import { Actions } from "@uniswap/v4-periphery/src/libraries/Actions.sol";
import { GraduationMath } from "../../src/libraries/GraduationMath.sol";
import { GraduationGuardHook } from "../../src/v2/GraduationGuardHook.sol";
import { LpFeeLocker } from "../../src/v2/LpFeeLocker.sol";
import { ILpFeeLocker } from "../../src/v2/interfaces/ILpFeeLocker.sol";
import { MineGuardHook } from "../../script/v2/MineGuardHook.s.sol";
import { MockCurveRegistry } from "./mocks/MockCurveRegistry.sol";
import { MockFeeRecipientSource } from "./mocks/MockFeeRecipientSource.sol";
import { V4TestBase } from "./utils/V4TestBase.sol";

contract LockerTestToken is ERC20 {
    constructor() ERC20("Locker Test", "LKT") {
        _mint(msg.sender, 1_000_000_000e18);
    }
}

/// @dev Has no receive function, so ETH payouts to it fail.
contract EthRejectingClaimer {
    function claim(ILpFeeLocker locker, address token) external {
        // forge-lint: disable-next-line(unused-return)
        locker.claim(token);
    }
}

// Tests ignore fee-call returns they read back separately, send ETH to fixed test routers,
// and the fuzz sequence drives swaps/collects in a loop by design.
// forge-lint: disable-start(unused-return, calls-loop, arbitrary-send-eth)
contract LpFeeLockerTest is V4TestBase {
    address internal constant CREATOR = address(0xC4EA);
    address internal constant TREASURY = address(0x7EA5);
    address internal constant STRANGER = address(0x5757);
    address internal constant NEW_RECIPIENT = address(0x4E1);
    uint256 internal constant GRADUATION_ETH = 4.2 ether;
    uint256 internal constant LP_TOKENS = 200_000_000e18;

    MockCurveRegistry internal registry;
    GraduationGuardHook internal hook;
    LpFeeLocker internal locker;
    MockFeeRecipientSource internal curve;
    LockerTestToken internal token;
    PoolSwapTest internal swapRouter;
    PoolKey internal key;
    uint256 internal positionId;
    uint128 internal lockedLiquidity;

    function setUp() public {
        deployV4();
        registry = new MockCurveRegistry();
        (, bytes32 salt) = new MineGuardHook().mine(address(this), poolManager, registry);
        hook = new GraduationGuardHook{ salt: salt }(poolManager, registry);
        locker = new LpFeeLocker(positionManager, registry);

        token = new LockerTestToken();
        curve = new MockFeeRecipientSource(
            poolManager, positionManager, permit2, CREATOR, TREASURY, 5000
        );
        registry.setCurve(address(token), address(curve));
        assertTrue(token.transfer(address(curve), LP_TOKENS));

        key = PoolKey({
            currency0: Currency.wrap(address(0)),
            currency1: Currency.wrap(address(token)),
            fee: 10_000,
            tickSpacing: 200,
            hooks: IHooks(address(hook))
        });
        positionId = curve.graduate{ value: GRADUATION_ETH }(key, LP_TOKENS, locker);
        lockedLiquidity = positionManager.getPositionLiquidity(positionId);

        swapRouter = new PoolSwapTest(poolManager);
        assertTrue(token.approve(address(swapRouter), type(uint256).max));
        vm.deal(address(this), 1000 ether);
    }

    function test_registeredPosition_isOwnedByLocker() public view {
        assertEq(locker.positionOf(address(token)), positionId);
        assertEq(_ownerOf(positionId), address(locker));
        assertGt(lockedLiquidity, 0);
    }

    function test_collect_splitsBothCurrenciesExactly() public {
        _buy(1 ether);
        _sell(1_000_000e18);

        vm.expectEmit(true, false, false, false, address(locker));
        // forge-lint: disable-next-line(reentrancy-events)
        emit ILpFeeLocker.LpFeesCollected(address(token), 0, 0);
        (uint256 ethFees, uint256 tokenFees) = locker.collect(address(token));

        // The locker owns all liquidity, so it earns the whole 1% LP fee (minus rounding).
        assertApproxEqAbs(ethFees, 0.01 ether, 10);
        assertApproxEqAbs(tokenFees, 10_000e18, 10);

        (uint256 creatorEth, uint256 creatorTokens) = locker.owed(address(token), CREATOR);
        (uint256 protocolEth, uint256 protocolTokens) = locker.owed(address(token), TREASURY);
        assertEq(creatorEth, ethFees * 5000 / 10_000);
        assertEq(creatorTokens, tokenFees * 5000 / 10_000);
        assertEq(creatorEth + protocolEth, ethFees);
        assertEq(creatorTokens + protocolTokens, tokenFees);
        // An odd wei of either currency goes to the protocol.
        assertEq(protocolEth - creatorEth, ethFees % 2);
        assertEq(protocolTokens - creatorTokens, tokenFees % 2);
    }

    function test_collect_unknownTokenReverts() public {
        vm.expectRevert(abi.encodeWithSelector(ILpFeeLocker.UnknownToken.selector, STRANGER));
        locker.collect(STRANGER);
    }

    function test_claim_paysRecipientAndTreasury_strangerGetsZero() public {
        _buy(1 ether);
        _sell(1_000_000e18);
        locker.collect(address(token));
        (uint256 creatorEth, uint256 creatorTokens) = locker.owed(address(token), CREATOR);
        (uint256 protocolEth, uint256 protocolTokens) = locker.owed(address(token), TREASURY);

        vm.prank(CREATOR);
        (uint256 paidEth, uint256 paidTokens) = locker.claim(address(token));
        assertEq(paidEth, creatorEth);
        assertEq(paidTokens, creatorTokens);
        assertEq(CREATOR.balance, creatorEth);
        assertEq(token.balanceOf(CREATOR), creatorTokens);

        vm.prank(TREASURY);
        locker.claim(address(token));
        assertEq(TREASURY.balance, protocolEth);
        assertEq(token.balanceOf(TREASURY), protocolTokens);

        vm.prank(STRANGER);
        (paidEth, paidTokens) = locker.claim(address(token));
        assertEq(paidEth, 0);
        assertEq(paidTokens, 0);

        (uint256 leftEth, uint256 leftTokens) = locker.owed(address(token), CREATOR);
        assertEq(leftEth + leftTokens, 0);
    }

    function test_claim_collectsFirst() public {
        _buy(1 ether);
        vm.prank(CREATOR);
        (uint256 paidEth,) = locker.claim(address(token));
        assertApproxEqAbs(paidEth, 0.005 ether, 10);
    }

    function test_recipientChange_keepsCreditedBalances() public {
        _buy(1 ether);
        locker.collect(address(token));
        (uint256 firstShare,) = locker.owed(address(token), CREATOR);

        curve.setFeeRecipient(NEW_RECIPIENT);
        _buy(1 ether);
        locker.collect(address(token));

        (uint256 oldOwed,) = locker.owed(address(token), CREATOR);
        (uint256 newOwed,) = locker.owed(address(token), NEW_RECIPIENT);
        assertEq(oldOwed, firstShare);
        assertGt(newOwed, 0);

        vm.prank(CREATOR);
        (uint256 paidEth,) = locker.claim(address(token));
        assertEq(paidEth, firstShare);
    }

    function test_rejectingRecipient_onlyBlocksItsOwnClaim() public {
        EthRejectingClaimer rejector = new EthRejectingClaimer();
        curve.setFeeRecipient(address(rejector));
        _buy(1 ether);
        locker.collect(address(token));

        vm.expectRevert(
            abi.encodeWithSelector(ILpFeeLocker.EthTransferFailed.selector, address(rejector))
        );
        rejector.claim(locker, address(token));

        vm.prank(TREASURY);
        (uint256 paidEth,) = locker.claim(address(token));
        assertGt(paidEth, 0);
    }

    function test_thirdPartyLiquidity_doesNotTouchLockerPosition() public {
        uint256 thirdPartyId = _mintThirdParty(lockedLiquidity);
        assertEq(positionManager.getPositionLiquidity(positionId), lockedLiquidity);

        _buy(1 ether);
        (uint256 ethFees,) = locker.collect(address(token));
        // Equal liquidity: the locker earns half the LP fee and none of the third party's.
        assertApproxEqAbs(ethFees, 0.005 ether, 1e6);
        assertEq(positionManager.getPositionLiquidity(positionId), lockedLiquidity);
        assertEq(positionManager.getPositionLiquidity(thirdPartyId), lockedLiquidity);
    }

    function testFuzz_lockedLiquidityNeverDecreases(uint8[12] calldata ops, uint256 seed) public {
        for (uint256 i = 0; i < ops.length; ++i) {
            uint256 amount = uint256(keccak256(abi.encode(seed, i)));
            uint256 op = ops[i] % 4;
            if (op == 0) {
                _buy(bound(amount, 1e12, 2 ether));
            } else if (op == 1) {
                _sell(bound(amount, 1e18, 5_000_000e18));
            } else if (op == 2) {
                locker.collect(address(token));
            } else {
                vm.prank(CREATOR);
                locker.claim(address(token));
            }
            assertEq(positionManager.getPositionLiquidity(positionId), lockedLiquidity);
            assertEq(_ownerOf(positionId), address(locker));
        }
    }

    function test_onERC721Received_onlyFromPositionManager() public {
        vm.prank(STRANGER);
        vm.expectRevert(abi.encodeWithSelector(ILpFeeLocker.NotPositionManager.selector, STRANGER));
        locker.onERC721Received(STRANGER, STRANGER, 1, "");

        vm.prank(address(positionManager));
        assertEq(
            locker.onERC721Received(STRANGER, STRANGER, 1, ""),
            IERC721Receiver.onERC721Received.selector
        );
    }

    function test_registerPosition_onlyCurveOnce() public {
        vm.prank(STRANGER);
        vm.expectRevert(
            abi.encodeWithSelector(ILpFeeLocker.UnauthorizedRegistrar.selector, STRANGER)
        );
        locker.registerPosition(address(token), positionId);

        vm.prank(address(curve));
        vm.expectRevert(
            abi.encodeWithSelector(ILpFeeLocker.PositionAlreadyRegistered.selector, address(token))
        );
        locker.registerPosition(address(token), positionId);
    }

    function test_registerPosition_rejectsUnknownToken() public {
        vm.prank(address(curve));
        vm.expectRevert(
            abi.encodeWithSelector(ILpFeeLocker.UnauthorizedRegistrar.selector, address(curve))
        );
        locker.registerPosition(STRANGER, positionId);
    }

    function test_registerPosition_rejectsUnownedPosition() public {
        LockerTestToken other = new LockerTestToken();
        registry.setCurve(address(other), address(this));
        uint256 thirdPartyId = _mintThirdParty(lockedLiquidity);

        vm.expectRevert(
            abi.encodeWithSelector(ILpFeeLocker.PositionNotOwned.selector, thirdPartyId)
        );
        locker.registerPosition(address(other), thirdPartyId);
    }

    function test_registerPosition_rejectsPositionOfAnotherToken() public {
        LockerTestToken other = new LockerTestToken();
        registry.setCurve(address(other), address(this));

        vm.expectRevert(
            abi.encodeWithSelector(
                ILpFeeLocker.PositionTokenMismatch.selector, address(other), address(token)
            )
        );
        locker.registerPosition(address(other), positionId);
    }

    function test_receive_rejectsNonPoolManager() public {
        (bool ok,) = address(locker).call{ value: 1 }("");
        assertFalse(ok);
    }

    function _buy(uint256 ethIn) internal {
        swapRouter.swap{ value: ethIn }(
            key,
            SwapParams({
                zeroForOne: true,
                amountSpecified: -SafeCast.toInt256(ethIn),
                sqrtPriceLimitX96: TickMath.MIN_SQRT_PRICE + 1
            }),
            PoolSwapTest.TestSettings({ takeClaims: false, settleUsingBurn: false }),
            ""
        );
    }

    function _sell(uint256 tokensIn) internal {
        swapRouter.swap(
            key,
            SwapParams({
                zeroForOne: false,
                amountSpecified: -SafeCast.toInt256(tokensIn),
                sqrtPriceLimitX96: TickMath.MAX_SQRT_PRICE - 1
            }),
            PoolSwapTest.TestSettings({ takeClaims: false, settleUsingBurn: false }),
            ""
        );
    }

    function _mintThirdParty(uint128 liquidity) internal returns (uint256 tokenId) {
        assertTrue(token.approve(address(permit2), type(uint256).max));
        permit2.approve(
            address(token), address(positionManager), type(uint160).max, type(uint48).max
        );
        bytes memory actions = abi.encodePacked(
            SafeCast.toUint8(Actions.MINT_POSITION),
            SafeCast.toUint8(Actions.SETTLE_PAIR),
            SafeCast.toUint8(Actions.SWEEP)
        );
        bytes[] memory params = new bytes[](3);
        params[0] = abi.encode(
            key,
            GraduationMath.TICK_LOWER,
            GraduationMath.TICK_UPPER,
            uint256(liquidity),
            type(uint128).max,
            type(uint128).max,
            address(this),
            bytes("")
        );
        params[1] = abi.encode(key.currency0, key.currency1);
        params[2] = abi.encode(key.currency0, address(this));
        tokenId = positionManager.nextTokenId();
        positionManager.modifyLiquidities{ value: 100 ether }(
            abi.encode(actions, params), block.timestamp
        );
    }

    function _ownerOf(uint256 tokenId) internal view returns (address) {
        (bool ok, bytes memory data) = address(positionManager)
            .staticcall(abi.encodeWithSignature("ownerOf(uint256)", tokenId));
        assertTrue(ok);
        return abi.decode(data, (address));
    }

    receive() external payable { }
}
// forge-lint: disable-end(unused-return, calls-loop, arbitrary-send-eth)
