// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Test } from "forge-std/Test.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { IERC721 } from "@openzeppelin/contracts/token/ERC721/IERC721.sol";
import { SafeCast } from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import { IHooks } from "@uniswap/v4-core/src/interfaces/IHooks.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { StateLibrary } from "@uniswap/v4-core/src/libraries/StateLibrary.sol";
import { TickMath } from "@uniswap/v4-core/src/libraries/TickMath.sol";
import { PoolSwapTest } from "@uniswap/v4-core/src/test/PoolSwapTest.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { SwapParams } from "@uniswap/v4-core/src/types/PoolOperation.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { IAllowanceTransfer } from "permit2/src/interfaces/IAllowanceTransfer.sol";
import { GraduationMath } from "../src/libraries/GraduationMath.sol";
import { LaunchTypes } from "../src/types/LaunchTypes.sol";
import { BondingCurveV2 } from "../src/v2/BondingCurveV2.sol";
import { GraduationGuardHook } from "../src/v2/GraduationGuardHook.sol";
import { LaunchFactoryV2 } from "../src/v2/LaunchFactoryV2.sol";
import { LpFeeLocker } from "../src/v2/LpFeeLocker.sol";
import { IBondingCurveV2 } from "../src/v2/interfaces/IBondingCurveV2.sol";
import { LaunchTypesV2 } from "../src/v2/types/LaunchTypesV2.sol";
import { MineGuardHook } from "../script/v2/MineGuardHook.s.sol";

/// @dev `PositionManager` exposes its Permit2 through `Permit2Forwarder`, which
/// `IPositionManager` does not include.
interface IPermit2Holder {
    function permit2() external view returns (address);
}

/// @notice v2 acceptance against the live Uniswap v4 deployment on Robinhood Chain. Every
/// dependency address and runtime code hash comes from the reviewed
/// `deployments/config/robinhood-mainnet-v4.json`; `scripts/check-fork.ps1` runs this suite at
/// that record's fork block.
// Tests send ETH to contracts under test and read pool state field by field.
// forge-lint: disable-start(unused-return, arbitrary-send-eth, erc20-unchecked-transfer)
contract RobinhoodMainnetV2ForkTest is Test {
    using StateLibrary for IPoolManager;

    string private constant DEPENDENCIES = "deployments/config/robinhood-mainnet-v4.json";
    uint256 private constant TOTAL_SUPPLY = 1_000_000_000 ether;
    uint256 private constant CURVE_TOKENS = 800_000_000 ether;
    uint256 private constant LP_TOKENS = 200_000_000 ether;
    uint256 private constant GRADUATION_ETH = 4.2 ether;
    uint256 private constant INITIAL_VIRTUAL_ETH = 1.4 ether;
    uint256 private constant INITIAL_VIRTUAL_TOKEN = 1_066_666_666_666_666_666_666_666_667;
    uint16 private constant SNIPE_START_BPS = 5000;
    uint32 private constant SNIPE_DURATION = 60;
    uint16 private constant LP_CREATOR_SHARE_BPS = 5000;

    address private immutable CREATOR = makeAddr("creator");
    address private immutable TREASURY = makeAddr("treasury");
    address private immutable TRADER = makeAddr("trader");
    address private immutable NEXT_RECIPIENT = makeAddr("nextRecipient");
    address private immutable OUTSIDER = makeAddr("outsider");
    address private immutable PAUSE_AUTHORITY = makeAddr("pauseAuthority");
    address private immutable TIMELOCK = makeAddr("timelock");

    IPoolManager private poolManager;
    IPositionManager private positionManager;
    IAllowanceTransfer private permit2;
    LaunchFactoryV2 private factory;
    GraduationGuardHook private hook;
    LpFeeLocker private locker;
    PoolSwapTest private swapRouter;

    function setUp() public {
        string memory json = vm.readFile(DEPENDENCIES);
        assertEq(block.chainid, vm.parseJsonUint(json, ".chainId"), "fork chain id");
        poolManager = IPoolManager(vm.parseJsonAddress(json, ".poolManager"));
        positionManager = IPositionManager(vm.parseJsonAddress(json, ".positionManager"));
        permit2 = IAllowanceTransfer(vm.parseJsonAddress(json, ".permit2"));

        _assertCodeHash(json, ".poolManager", ".bytecodeHashes.poolManager");
        _assertCodeHash(json, ".positionManager", ".bytecodeHashes.positionManager");
        _assertCodeHash(json, ".permit2", ".bytecodeHashes.permit2");
        _assertCodeHash(json, ".universalRouter", ".bytecodeHashes.universalRouter");
        assertEq(address(positionManager.poolManager()), address(poolManager), "posm pool manager");
        assertEq(
            IPermit2Holder(address(positionManager)).permit2(), address(permit2), "posm permit2"
        );

        BondingCurveV2 implementation = new BondingCurveV2();
        factory = new LaunchFactoryV2(_initialization(address(implementation)));
        (, bytes32 salt) = new MineGuardHook().mine(address(this), poolManager, factory);
        hook = new GraduationGuardHook{ salt: salt }(poolManager, factory);
        locker = new LpFeeLocker(positionManager, factory);
        factory.setDependencies(address(hook), address(locker));
        swapRouter = new PoolSwapTest(poolManager);

        vm.deal(CREATOR, 10 ether);
        vm.deal(TRADER, 100 ether);
    }

    function test_fullLifecycleOnLiveUniswapV4() public {
        vm.prank(CREATOR);
        (address tokenAddress, address curveAddress) = factory.launch(
            LaunchTypes.LaunchRequest({
                name: "Fork Token",
                symbol: "FORK",
                engineVersion: 2,
                developerBuyGross: 0,
                minDeveloperTokensOut: 0,
                deadline: block.timestamp
            })
        );
        IBondingCurveV2 curve = IBondingCurveV2(curveAddress);
        IERC20 token = IERC20(tokenAddress);
        PoolKey memory key = _poolKey(tokenAddress);

        // Nobody but the curve can open the guarded pool, also on the live PoolManager.
        vm.prank(OUTSIDER);
        vm.expectRevert();
        poolManager.initialize(key, TickMath.MIN_SQRT_PRICE);

        // Inside the window the buy is taxed; after it, it is not.
        vm.warp(curve.launchTime() + 10);
        _curveBuy(curve, 0.5 ether);
        uint256 snipeTax = curve.unclaimedSnipeTax();
        assertGt(snipeTax, 0, "taxed buy");
        vm.warp(curve.launchTime() + SNIPE_DURATION);
        _curveBuy(curve, 0.5 ether);
        assertEq(curve.unclaimedSnipeTax(), snipeTax, "untaxed buy");

        // Graduation opens the pool at the deterministic price through the live managers.
        _curveBuy(curve, 10 ether);
        assertEq(uint8(curve.phase()), uint8(LaunchTypes.Phase.Graduated));
        (uint160 sqrtPriceX96,,,) = poolManager.getSlot0(key.toId());
        assertEq(sqrtPriceX96, GraduationMath.sqrtPriceX96(GRADUATION_ETH, LP_TOKENS));
        uint256 positionId = locker.positionOf(tokenAddress);
        assertEq(IERC721(address(positionManager)).ownerOf(positionId), address(locker));
        uint128 liquidity = positionManager.getPositionLiquidity(positionId);
        assertGt(liquidity, 0);

        _swapBothWays(key, token);
        (uint256 ethFees, uint256 tokenFees) = locker.collect(tokenAddress);
        assertGt(ethFees, 0, "eth lp fees");
        assertGt(tokenFees, 0, "token lp fees");

        _claimEverything(curve, tokenAddress, CREATOR);

        _changeRecipientAndClaim(curve, key, token);

        assertEq(positionManager.getPositionLiquidity(positionId), liquidity, "liquidity locked");
        assertEq(token.totalSupply(), TOTAL_SUPPLY);
    }

    /// @dev After a recipient change, new LP fees go to the new recipient only.
    function _changeRecipientAndClaim(IBondingCurveV2 curve, PoolKey memory key, IERC20 token)
        private
    {
        address tokenAddress = address(token);
        vm.prank(CREATOR);
        curve.proposeFeeRecipient(NEXT_RECIPIENT);
        vm.prank(NEXT_RECIPIENT);
        curve.acceptFeeRecipient();
        _swapBothWays(key, token);
        vm.prank(CREATOR);
        (uint256 creatorEth, uint256 creatorTokens) = locker.claim(tokenAddress);
        assertEq(creatorEth + creatorTokens, 0, "old recipient earns nothing new");
        vm.prank(NEXT_RECIPIENT);
        (uint256 nextEth, uint256 nextTokens) = locker.claim(tokenAddress);
        assertGt(nextEth, 0, "new recipient eth");
        assertGt(nextTokens, 0, "new recipient tokens");
    }

    function _claimEverything(IBondingCurveV2 curve, address token, address creator) private {
        uint256 creatorBalance = creator.balance;
        vm.prank(creator);
        (uint256 lpEth,) = locker.claim(token);
        vm.prank(creator);
        uint256 curveFees = curve.claimCreatorFees();
        assertEq(creator.balance, creatorBalance + lpEth + curveFees, "creator payouts");
        assertGt(lpEth, 0, "creator lp eth");

        vm.startPrank(TREASURY);
        (uint256 protocolLpEth,) = locker.claim(token);
        assertGt(protocolLpEth, 0, "protocol lp eth");
        assertGt(curve.claimProtocolFees(), 0, "protocol curve fees");
        assertGt(curve.claimSnipeTax(), 0, "snipe tax");
        vm.stopPrank();
    }

    function _curveBuy(IBondingCurveV2 curve, uint256 ethIn) private {
        vm.prank(TRADER);
        curve.buy{ value: ethIn }(TRADER, TRADER, 0, block.timestamp);
    }

    function _swapBothWays(PoolKey memory key, IERC20 token) private {
        PoolSwapTest.TestSettings memory settings =
            PoolSwapTest.TestSettings({ takeClaims: false, settleUsingBurn: false });
        vm.startPrank(TRADER);
        swapRouter.swap{ value: 1 ether }(
            key,
            SwapParams({
                zeroForOne: true,
                amountSpecified: -SafeCast.toInt256(1 ether),
                sqrtPriceLimitX96: TickMath.MIN_SQRT_PRICE + 1
            }),
            settings,
            ""
        );
        uint256 tokensIn = token.balanceOf(TRADER) / 10;
        token.approve(address(swapRouter), tokensIn);
        swapRouter.swap(
            key,
            SwapParams({
                zeroForOne: false,
                amountSpecified: -SafeCast.toInt256(tokensIn),
                sqrtPriceLimitX96: TickMath.MAX_SQRT_PRICE - 1
            }),
            settings,
            ""
        );
        vm.stopPrank();
    }

    function _assertCodeHash(string memory json, string memory addressKey, string memory hashKey)
        private
        view
    {
        address dependency = vm.parseJsonAddress(json, addressKey);
        assertEq(dependency.codehash, vm.parseJsonBytes32(json, hashKey), addressKey);
    }

    function _poolKey(address token) private view returns (PoolKey memory) {
        return PoolKey({
            currency0: Currency.wrap(address(0)),
            currency1: Currency.wrap(token),
            fee: hook.POOL_FEE(),
            tickSpacing: hook.TICK_SPACING(),
            hooks: IHooks(address(hook))
        });
    }

    function _initialization(address implementation)
        private
        view
        returns (LaunchTypesV2.FactoryInitializationV2 memory)
    {
        return LaunchTypesV2.FactoryInitializationV2({
            pauseAuthority: PAUSE_AUTHORITY,
            timelock: TIMELOCK,
            protocolTreasury: TREASURY,
            engineVersion: 2,
            implementation: implementation,
            poolManager: poolManager,
            positionManager: positionManager,
            permit2: permit2,
            defaults: LaunchTypesV2.FactoryDefaultsV2({
                parameters: LaunchTypes.CurveParameters({
                    totalSupply: TOTAL_SUPPLY,
                    curveTokens: CURVE_TOKENS,
                    lpTokens: LP_TOKENS,
                    graduationEth: GRADUATION_ETH,
                    initialVirtualEth: INITIAL_VIRTUAL_ETH,
                    initialVirtualToken: INITIAL_VIRTUAL_TOKEN,
                    tradeFeeBps: 100,
                    protocolShareBps: 5000
                }),
                antiSnipe: LaunchTypesV2.AntiSnipeProfile({
                    startBps: SNIPE_START_BPS, durationSeconds: SNIPE_DURATION
                }),
                lpCreatorShareBps: LP_CREATOR_SHARE_BPS,
                launchFee: 0
            })
        });
    }
}
// forge-lint: disable-end(unused-return, arbitrary-send-eth, erc20-unchecked-transfer)
