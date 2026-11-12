// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Vm } from "forge-std/Vm.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { BondingCurveV1 } from "../../src/BondingCurveV1.sol";
import { ILaunchErrors } from "../../src/interfaces/ILaunchErrors.sol";
import { CurveMath } from "../../src/libraries/CurveMath.sol";
import { LaunchTypes } from "../../src/types/LaunchTypes.sol";
import { BondingCurveV2 } from "../../src/v2/BondingCurveV2.sol";
import { GraduationGuardHook } from "../../src/v2/GraduationGuardHook.sol";
import { LaunchFactoryV2 } from "../../src/v2/LaunchFactoryV2.sol";
import { LaunchTokenV2 } from "../../src/v2/LaunchTokenV2.sol";
import { LpFeeLocker } from "../../src/v2/LpFeeLocker.sol";
import { IBondingCurveV2 } from "../../src/v2/interfaces/IBondingCurveV2.sol";
import { ILaunchEventsV2 } from "../../src/v2/interfaces/ILaunchEventsV2.sol";
import { ILaunchFactoryV2 } from "../../src/v2/interfaces/ILaunchFactoryV2.sol";
import { LaunchTypesV2 } from "../../src/v2/types/LaunchTypesV2.sol";
import { MineGuardHook } from "../../script/v2/MineGuardHook.s.sol";
import { V4TestBase } from "./utils/V4TestBase.sol";

// Tests send ETH to the factory under test and assert on events/reverts directly.
// forge-lint: disable-start(arbitrary-send-eth, unused-return, unsafe-typecast, reentrancy-events)
contract LaunchFactoryV2Test is V4TestBase {
    uint256 internal constant TOTAL_SUPPLY = 1_000_000_000 ether;
    uint256 internal constant CURVE_TOKENS = 800_000_000 ether;
    uint256 internal constant LP_TOKENS = 200_000_000 ether;
    uint256 internal constant GRADUATION_ETH = 4.2 ether;
    uint256 internal constant INITIAL_VIRTUAL_ETH = 1.4 ether;
    uint256 internal constant INITIAL_VIRTUAL_TOKEN = 1_066_666_666_666_666_666_666_666_667;
    uint16 internal constant TRADE_FEE_BPS = 100;
    uint16 internal constant PROTOCOL_SHARE_BPS = 5000;
    uint16 internal constant SNIPE_START_BPS = 5000;
    uint32 internal constant SNIPE_DURATION = 60;
    uint16 internal constant LP_CREATOR_SHARE_BPS = 5000;
    uint256 internal constant LAUNCH_FEE = 0.001 ether;
    uint256 internal constant LAUNCH_TIMESTAMP = 1_000_000;
    uint16 internal constant ENGINE_V2 = 2;

    address internal constant PAUSE_AUTHORITY = address(0xA0);
    address internal constant TIMELOCK = address(0x71);
    address internal constant TREASURY = address(0x7EA5);
    address internal constant NEXT_TREASURY = address(0x7EA6);
    address internal constant CREATOR = address(0xC4EA);
    address internal constant STRANGER = address(0x5757);

    LaunchFactoryV2 internal factory;
    BondingCurveV2 internal implementation;
    GraduationGuardHook internal hook;
    LpFeeLocker internal locker;

    function setUp() public {
        vm.warp(LAUNCH_TIMESTAMP);
        deployV4();
        implementation = new BondingCurveV2();
        factory = new LaunchFactoryV2(_factoryInitialization());
        (hook, locker) = _deployDependencies(factory);
        factory.setDependencies(address(hook), address(locker));
        vm.deal(CREATOR, 100 ether);
    }

    function test_launch_snapshotsDefaultsIntoCurve() public {
        (address token, address curveAddress) = _launch(0, 0);
        IBondingCurveV2 curve = IBondingCurveV2(curveAddress);

        assertEq(factory.curveOfToken(token), curveAddress);
        assertEq(LaunchTokenV2(token).curve(), curveAddress);
        assertEq(LaunchTokenV2(token).poolManager(), address(poolManager));
        assertEq(LaunchTokenV2(token).balanceOf(curveAddress), TOTAL_SUPPLY);
        assertEq(curve.token(), token);
        assertEq(curve.creator(), CREATOR);
        assertEq(curve.implementation(), address(implementation));
        assertEq(curve.launchTime(), LAUNCH_TIMESTAMP);
        _assertSnapshot(curve, TREASURY, SNIPE_START_BPS, SNIPE_DURATION, LP_CREATOR_SHARE_BPS);

        LaunchTypesV2.FactoryDefaultsV2 memory next = _defaults();
        next.antiSnipe = LaunchTypesV2.AntiSnipeProfile({ startBps: 9000, durationSeconds: 300 });
        next.lpCreatorShareBps = 10_000;
        next.launchFee = 0;
        vm.startPrank(TIMELOCK);
        factory.setFutureDefaults(next);
        factory.setFutureTreasury(NEXT_TREASURY);
        vm.stopPrank();

        // The existing launch keeps its snapshot; only new launches see the new defaults.
        _assertSnapshot(curve, TREASURY, SNIPE_START_BPS, SNIPE_DURATION, LP_CREATOR_SHARE_BPS);
        vm.prank(CREATOR);
        (, address nextCurve) = factory.launch(_request(0, 0));
        _assertSnapshot(IBondingCurveV2(nextCurve), NEXT_TREASURY, 9000, 300, 10_000);
    }

    function test_setFutureDefaults_enforcesHardCaps() public {
        LaunchTypesV2.FactoryDefaultsV2 memory defaults = _defaults();
        vm.startPrank(TIMELOCK);

        defaults.antiSnipe.startBps = 9001;
        vm.expectRevert(
            abi.encodeWithSelector(
                IBondingCurveV2.InvalidAntiSnipeProfile.selector, uint16(9001), SNIPE_DURATION
            )
        );
        factory.setFutureDefaults(defaults);

        defaults.antiSnipe.startBps = SNIPE_START_BPS;
        defaults.antiSnipe.durationSeconds = 301;
        vm.expectRevert(
            abi.encodeWithSelector(
                IBondingCurveV2.InvalidAntiSnipeProfile.selector, SNIPE_START_BPS, uint32(301)
            )
        );
        factory.setFutureDefaults(defaults);

        defaults.antiSnipe.durationSeconds = SNIPE_DURATION;
        defaults.lpCreatorShareBps = 10_001;
        vm.expectRevert(
            abi.encodeWithSelector(
                IBondingCurveV2.InvalidLpCreatorShareBps.selector, uint16(10_001)
            )
        );
        factory.setFutureDefaults(defaults);

        defaults.parameters.lpTokens = 0;
        defaults.lpCreatorShareBps = LP_CREATOR_SHARE_BPS;
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.InvalidSupplyAllocation.selector, TOTAL_SUPPLY, CURVE_TOKENS, 0
            )
        );
        factory.setFutureDefaults(defaults);
        vm.stopPrank();
    }

    function test_setFutureDefaults_onlyTimelock() public {
        vm.prank(STRANGER);
        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.UnauthorizedTimelock.selector, STRANGER, TIMELOCK)
        );
        factory.setFutureDefaults(_defaults());

        vm.prank(PAUSE_AUTHORITY);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.UnauthorizedTimelock.selector, PAUSE_AUTHORITY, TIMELOCK
            )
        );
        factory.setFutureTreasury(NEXT_TREASURY);
    }

    function test_futureDefaultsHash_tracksConfiguration() public {
        LaunchTypesV2.FactoryDefaultsV2 memory next = _defaults();
        next.lpCreatorShareBps = 0;

        vm.expectEmit(true, false, false, false, address(factory));
        emit ILaunchFactoryV2.FutureDefaultsConfigured(keccak256(abi.encode(next)));
        vm.prank(TIMELOCK);
        factory.setFutureDefaults(next);

        assertEq(factory.futureDefaultsHash(), keccak256(abi.encode(next)));
        assertEq(factory.futureDefaults().lpCreatorShareBps, 0);
    }

    function test_developerBuy_isUntaxedInLaunchSecond() public {
        uint256 gross = 0.01 ether;
        CurveMath.BuyQuote memory untaxed = _freshQuote(gross);
        assertLe(untaxed.tokensOut, TOTAL_SUPPLY / 100);

        (address token, address curveAddress) = _launch(gross, untaxed.tokensOut);
        IBondingCurveV2 curve = IBondingCurveV2(curveAddress);

        // The anti-snipe window is open at the launch second, yet the developer buy is exempt.
        assertEq(curve.currentTaxBps(), SNIPE_START_BPS);
        assertEq(LaunchTokenV2(token).balanceOf(CREATOR), untaxed.tokensOut);
        assertEq(curve.unclaimedSnipeTax(), 0);
        assertEq(curve.creatorFeesOwed(CREATOR), untaxed.creatorFee);
        assertEq(CREATOR.balance, 100 ether - LAUNCH_FEE - gross);
    }

    function test_developerBuy_cappedAtOnePercent() public {
        uint256 gross = 0.02 ether;
        CurveMath.BuyQuote memory untaxed = _freshQuote(gross);
        assertGt(untaxed.tokensOut, TOTAL_SUPPLY / 100);

        vm.prank(CREATOR);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.DeveloperBuyCapExceeded.selector,
                untaxed.tokensOut,
                TOTAL_SUPPLY / 100
            )
        );
        factory.launch{ value: LAUNCH_FEE + gross }(_request(gross, 0));
    }

    function test_developerBuy_respectsSlippageAndPause() public {
        uint256 gross = 0.01 ether;
        CurveMath.BuyQuote memory untaxed = _freshQuote(gross);

        vm.prank(CREATOR);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.SlippageExceeded.selector, untaxed.tokensOut + 1, untaxed.tokensOut
            )
        );
        factory.launch{ value: LAUNCH_FEE + gross }(_request(gross, untaxed.tokensOut + 1));

        vm.prank(CREATOR);
        vm.expectRevert(abi.encodeWithSelector(ILaunchErrors.SlippageExceeded.selector, 1, 0));
        factory.launch{ value: LAUNCH_FEE }(_request(0, 1));

        vm.prank(PAUSE_AUTHORITY);
        factory.setTradingPaused(true);
        vm.prank(CREATOR);
        vm.expectRevert(ILaunchErrors.TradingPaused.selector);
        factory.launch{ value: LAUNCH_FEE + gross }(_request(gross, 0));

        // Launching without a developer buy stays possible while trading is paused.
        _launch(0, 0);
    }

    function test_launch_rejectsWrongValueEngineAndPause() public {
        vm.startPrank(CREATOR);
        vm.expectRevert(
            abi.encodeWithSelector(ILaunchErrors.LaunchValueMismatch.selector, LAUNCH_FEE, 0)
        );
        factory.launch(_request(0, 0));

        LaunchTypes.LaunchRequest memory request = _request(0, 0);
        request.engineVersion = 1;
        vm.expectRevert(abi.encodeWithSelector(ILaunchErrors.UnknownEngine.selector, uint16(1)));
        factory.launch{ value: LAUNCH_FEE }(request);
        vm.stopPrank();

        vm.prank(TIMELOCK);
        factory.configureEngine(ENGINE_V2, address(implementation), false);
        vm.prank(CREATOR);
        vm.expectRevert(abi.encodeWithSelector(ILaunchErrors.EngineDisabled.selector, ENGINE_V2));
        factory.launch{ value: LAUNCH_FEE }(_request(0, 0));

        vm.startPrank(TIMELOCK);
        factory.configureEngine(ENGINE_V2, address(implementation), true);
        vm.stopPrank();
        vm.prank(PAUSE_AUTHORITY);
        factory.setLaunchesPaused(true);
        vm.prank(CREATOR);
        vm.expectRevert(ILaunchErrors.LaunchesPaused.selector);
        factory.launch{ value: LAUNCH_FEE }(_request(0, 0));
    }

    function test_configureEngine_rejectsVersionMismatch() public {
        BondingCurveV1 v1Implementation = new BondingCurveV1();
        vm.prank(TIMELOCK);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.EngineVersionMismatch.selector, ENGINE_V2, uint16(1)
            )
        );
        factory.configureEngine(ENGINE_V2, address(v1Implementation), true);
    }

    function test_launch_beforeDependencies_reverts() public {
        LaunchFactoryV2 fresh = new LaunchFactoryV2(_factoryInitialization());
        vm.prank(CREATOR);
        vm.expectRevert(ILaunchFactoryV2.DependenciesNotSet.selector);
        fresh.launch{ value: LAUNCH_FEE }(_request(0, 0));
    }

    function test_setDependencies_onceAndDeployerOnly() public {
        LaunchFactoryV2 fresh = new LaunchFactoryV2(_factoryInitialization());
        (GraduationGuardHook freshHook, LpFeeLocker freshLocker) = _deployDependencies(fresh);

        vm.prank(STRANGER);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchFactoryV2.UnauthorizedDeployer.selector, STRANGER, address(this)
            )
        );
        fresh.setDependencies(address(freshHook), address(freshLocker));

        fresh.setDependencies(address(freshHook), address(freshLocker));
        assertEq(fresh.hook(), address(freshHook));
        assertEq(fresh.locker(), address(freshLocker));

        vm.expectRevert(ILaunchFactoryV2.DependenciesAlreadySet.selector);
        fresh.setDependencies(address(freshHook), address(freshLocker));
    }

    function test_setDependencies_rejectsForeignOrMissingContracts() public {
        LaunchFactoryV2 fresh = new LaunchFactoryV2(_factoryInitialization());
        (GraduationGuardHook freshHook, LpFeeLocker freshLocker) = _deployDependencies(fresh);

        // `hook` and `locker` from setUp are bound to the other factory.
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchFactoryV2.InvalidDependency.selector, bytes32("hook"), address(hook)
            )
        );
        fresh.setDependencies(address(hook), address(freshLocker));

        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchFactoryV2.InvalidDependency.selector, bytes32("locker"), address(locker)
            )
        );
        fresh.setDependencies(address(freshHook), address(locker));

        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchFactoryV2.InvalidDependency.selector, bytes32("hook"), STRANGER
            )
        );
        fresh.setDependencies(STRANGER, address(freshLocker));

        // A locker on another PositionManager is rejected even when it names this factory.
        LpFeeLocker otherManagerLocker = new LpFeeLocker(_secondPositionManager(), fresh);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchFactoryV2.InvalidDependency.selector,
                bytes32("locker"),
                address(otherManagerLocker)
            )
        );
        fresh.setDependencies(address(freshHook), address(otherManagerLocker));
    }

    function test_tokenLaunched_fieldsMatchSnapshot() public {
        vm.recordLogs();
        (address token, address curve) = _launch(0, 0);
        Vm.Log[] memory logs = vm.getRecordedLogs();

        Vm.Log memory launched;
        uint256 matches = 0;
        for (uint256 i = 0; i < logs.length; ++i) {
            if (logs[i].topics[0] == ILaunchEventsV2.TokenLaunched.selector) {
                launched = logs[i];
                ++matches;
            }
        }
        assertEq(matches, 1);
        assertEq(launched.emitter, address(factory));
        assertEq(launched.topics.length, 4);
        assertEq(launched.topics[1], bytes32(uint256(uint160(token))));
        assertEq(launched.topics[2], bytes32(uint256(uint160(curve))));
        assertEq(launched.topics[3], bytes32(uint256(uint160(CREATOR))));

        LaunchTypesV2.TokenLaunchedData memory data = abi.decode(
            bytes.concat(abi.encode(uint256(0x20)), launched.data),
            (LaunchTypesV2.TokenLaunchedData)
        );
        assertEq(keccak256(abi.encode(data)), keccak256(abi.encode(_expectedLaunchData())));
    }

    function test_claimLaunchFees_paysTreasuryOfLaunch() public {
        _launch(0, 0);
        vm.prank(TIMELOCK);
        factory.setFutureTreasury(NEXT_TREASURY);
        _launch(0, 0);

        assertEq(factory.launchFeesByTreasury(TREASURY), LAUNCH_FEE);
        assertEq(factory.launchFeesByTreasury(NEXT_TREASURY), LAUNCH_FEE);

        vm.expectEmit(true, false, false, true, address(factory));
        emit ILaunchFactoryV2.LaunchFeesClaimed(TREASURY, LAUNCH_FEE);
        vm.prank(TREASURY);
        assertEq(factory.claimLaunchFees(), LAUNCH_FEE);
        assertEq(TREASURY.balance, LAUNCH_FEE);

        vm.prank(TREASURY);
        vm.expectRevert(ILaunchErrors.NothingToClaim.selector);
        factory.claimLaunchFees();
    }

    function test_pause_onlyPauseAuthority() public {
        vm.prank(TIMELOCK);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.UnauthorizedPauseAuthority.selector, TIMELOCK, PAUSE_AUTHORITY
            )
        );
        factory.setTradingPaused(true);

        vm.prank(PAUSE_AUTHORITY);
        factory.setTradingPaused(true);
        assertTrue(factory.tradingPaused());
    }

    function test_constructor_rejectsSharedAuthorities() public {
        LaunchTypesV2.FactoryInitializationV2 memory initialization = _factoryInitialization();
        initialization.timelock = PAUSE_AUTHORITY;
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.InvalidAuthority.selector, bytes32("timelock"), PAUSE_AUTHORITY
            )
        );
        new LaunchFactoryV2(initialization);

        initialization = _factoryInitialization();
        initialization.defaults.antiSnipe.startBps = 9001;
        vm.expectRevert(
            abi.encodeWithSelector(
                IBondingCurveV2.InvalidAntiSnipeProfile.selector, uint16(9001), SNIPE_DURATION
            )
        );
        new LaunchFactoryV2(initialization);
    }

    function _assertSnapshot(
        IBondingCurveV2 curve,
        address treasury,
        uint16 startBps,
        uint32 durationSeconds,
        uint16 lpCreatorShareBps
    ) internal view {
        assertEq(curve.protocolTreasury(), treasury);
        assertEq(curve.antiSnipeProfile().startBps, startBps);
        assertEq(curve.antiSnipeProfile().durationSeconds, durationSeconds);
        assertEq(curve.lpCreatorShareBps(), lpCreatorShareBps);
        assertEq(curve.hook(), address(hook));
        assertEq(curve.locker(), address(locker));
        assertEq(curve.poolManager(), address(poolManager));
        assertEq(curve.positionManager(), address(positionManager));
        assertEq(curve.virtualEthReserve(), INITIAL_VIRTUAL_ETH);
        assertEq(curve.virtualTokenReserve(), INITIAL_VIRTUAL_TOKEN);
    }

    function _launch(uint256 developerBuyGross, uint256 minTokensOut)
        internal
        returns (address token, address curve)
    {
        uint256 value = factory.launchFee() + developerBuyGross;
        vm.prank(CREATOR);
        (token, curve) = factory.launch{ value: value }(_request(developerBuyGross, minTokensOut));
    }

    function _request(uint256 developerBuyGross, uint256 minTokensOut)
        internal
        view
        returns (LaunchTypes.LaunchRequest memory)
    {
        return LaunchTypes.LaunchRequest({
            name: "Launch",
            symbol: "LCH",
            engineVersion: ENGINE_V2,
            developerBuyGross: developerBuyGross,
            minDeveloperTokensOut: minTokensOut,
            deadline: block.timestamp
        });
    }

    function _deployDependencies(LaunchFactoryV2 target)
        internal
        returns (GraduationGuardHook deployedHook, LpFeeLocker deployedLocker)
    {
        (, bytes32 salt) = new MineGuardHook().mine(address(this), poolManager, target);
        deployedHook = new GraduationGuardHook{ salt: salt }(poolManager, target);
        deployedLocker = new LpFeeLocker(positionManager, target);
    }

    function _secondPositionManager() internal returns (IPositionManager) {
        address secondPoolManager = deployCode(POOL_MANAGER_ARTIFACT, abi.encode(address(this)));
        return IPositionManager(
            deployCode(
                POSITION_MANAGER_ARTIFACT,
                abi.encode(
                    secondPoolManager,
                    address(permit2),
                    UNSUBSCRIBE_GAS_LIMIT,
                    address(0),
                    address(0)
                )
            )
        );
    }

    function _freshQuote(uint256 gross) internal pure returns (CurveMath.BuyQuote memory) {
        return CurveMath.quoteBuy(
            INITIAL_VIRTUAL_ETH * INITIAL_VIRTUAL_TOKEN,
            INITIAL_VIRTUAL_ETH,
            INITIAL_VIRTUAL_TOKEN,
            INITIAL_VIRTUAL_TOKEN - CURVE_TOKENS,
            gross,
            TRADE_FEE_BPS,
            PROTOCOL_SHARE_BPS
        );
    }

    function _expectedLaunchData()
        internal
        view
        returns (LaunchTypesV2.TokenLaunchedData memory data)
    {
        data.protocolTreasury = TREASURY;
        data.engineVersion = ENGINE_V2;
        data.name = "Launch";
        data.symbol = "LCH";
        data.totalSupply = TOTAL_SUPPLY;
        data.virtualEth = INITIAL_VIRTUAL_ETH;
        data.virtualToken = INITIAL_VIRTUAL_TOKEN;
        data.curveTokens = CURVE_TOKENS;
        data.lpTokens = LP_TOKENS;
        data.graduationEth = GRADUATION_ETH;
        data.launchFeePaid = LAUNCH_FEE;
        data.tradeFeeBps = TRADE_FEE_BPS;
        data.protocolShareBps = PROTOCOL_SHARE_BPS;
        data.hook = address(hook);
        data.locker = address(locker);
        data.antiSnipeStartBps = SNIPE_START_BPS;
        data.antiSnipeDurationSeconds = SNIPE_DURATION;
        data.lpCreatorShareBps = LP_CREATOR_SHARE_BPS;
    }

    function _factoryInitialization()
        internal
        view
        returns (LaunchTypesV2.FactoryInitializationV2 memory)
    {
        return LaunchTypesV2.FactoryInitializationV2({
            pauseAuthority: PAUSE_AUTHORITY,
            timelock: TIMELOCK,
            protocolTreasury: TREASURY,
            engineVersion: ENGINE_V2,
            implementation: address(implementation),
            poolManager: poolManager,
            positionManager: positionManager,
            permit2: permit2,
            defaults: _defaults()
        });
    }

    function _defaults() internal pure returns (LaunchTypesV2.FactoryDefaultsV2 memory) {
        return LaunchTypesV2.FactoryDefaultsV2({
            parameters: LaunchTypes.CurveParameters({
                totalSupply: TOTAL_SUPPLY,
                curveTokens: CURVE_TOKENS,
                lpTokens: LP_TOKENS,
                graduationEth: GRADUATION_ETH,
                initialVirtualEth: INITIAL_VIRTUAL_ETH,
                initialVirtualToken: INITIAL_VIRTUAL_TOKEN,
                tradeFeeBps: TRADE_FEE_BPS,
                protocolShareBps: PROTOCOL_SHARE_BPS
            }),
            antiSnipe: LaunchTypesV2.AntiSnipeProfile({
                startBps: SNIPE_START_BPS, durationSeconds: SNIPE_DURATION
            }),
            lpCreatorShareBps: LP_CREATOR_SHARE_BPS,
            launchFee: LAUNCH_FEE
        });
    }
}
// forge-lint: disable-end(arbitrary-send-eth, unused-return, unsafe-typecast, reentrancy-events)
