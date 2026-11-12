// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Script } from "forge-std/Script.sol";
import { Vm } from "forge-std/Vm.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { IHooks } from "@uniswap/v4-core/src/interfaces/IHooks.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { TickMath } from "@uniswap/v4-core/src/libraries/TickMath.sol";
import { PoolSwapTest } from "@uniswap/v4-core/src/test/PoolSwapTest.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { SwapParams } from "@uniswap/v4-core/src/types/PoolOperation.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { IAllowanceTransfer } from "permit2/src/interfaces/IAllowanceTransfer.sol";
import { DeployPermit2 } from "permit2/test/utils/DeployPermit2.sol";
import { LaunchTypes } from "../../src/types/LaunchTypes.sol";
import { BondingCurveV2 } from "../../src/v2/BondingCurveV2.sol";
import { GraduationGuardHook } from "../../src/v2/GraduationGuardHook.sol";
import { LaunchFactoryV2 } from "../../src/v2/LaunchFactoryV2.sol";
import { LpFeeLocker } from "../../src/v2/LpFeeLocker.sol";
import { IBondingCurveV2 } from "../../src/v2/interfaces/IBondingCurveV2.sol";
import { ILaunchEventsV2 } from "../../src/v2/interfaces/ILaunchEventsV2.sol";
import { ILaunchFactoryV2 } from "../../src/v2/interfaces/ILaunchFactoryV2.sol";
import { ILpFeeLocker } from "../../src/v2/interfaces/ILpFeeLocker.sol";
import { LaunchTypesV2 } from "../../src/v2/types/LaunchTypesV2.sol";
import { VectorSystemV2 } from "./GenerateCurveVectorsV2.s.sol";

/// @notice Refund recipient that rejects ETH, so the final buy credits a pull refund.
contract RejectingRefundRecipient {
    function claimRefundTo(IBondingCurveV2 curve, address recipient)
        external
        returns (uint256 amount)
    {
        return curve.claimRefundTo(recipient);
    }
}

// Off-chain generator: the JSON loop calls `vm.toString` cheatcodes, a missing topic must abort
// generation, and setup calls only need their side effects (the emitted logs).
// forge-lint: disable-start(calls-loop, require-revert-in-loop, unused-return, arbitrary-send-eth)
/// @notice Generates ABI-decoder logs for engine v2 from a real execution of the v2 system
/// (factory, curve, guard hook, LP fee locker) on a local Uniswap V4 stack.
/// @dev Covers every event the v2 indexer decodes: the launch and admin events, curve trades
/// with and without the anti-snipe tax, a credited and claimed refund, the fee recipient
/// change, graduation with the V4 `Initialize`/`ModifyLiquidity`, V4 `Swap` in both
/// directions, LP fee collect and claim, and every curve claim. Generation fails unless each
/// supported topic appears at least once.
contract GenerateEventFixturesV2 is Script {
    uint16 private constant ENGINE_VERSION = 2;
    uint256 private constant LAUNCH_FEE = 0.0005 ether;
    uint256 private constant DEVELOPER_BUY = 0.001 ether;
    uint256 private constant GENESIS_TIMESTAMP = 1_000_000;
    uint256 private constant UNSUBSCRIBE_GAS_LIMIT = 100_000;
    uint256 private constant SUPPORTED_TOPIC_COUNT = 22;
    string private constant POOL_MANAGER_ARTIFACT = "out-v4/PoolManager.sol/PoolManager.json";
    string private constant POSITION_MANAGER_ARTIFACT =
        "out-v4/PositionManager.sol/PositionManager.json";

    address private constant PAUSE_AUTHORITY = address(0xA11CE);
    address private constant TIMELOCK = address(0x710E10C);
    address private constant TREASURY = address(0x7000);
    address private constant CREATOR = address(0xC0FFEE);
    address private constant NEXT_RECIPIENT = address(0xFEE2);
    address private constant TRADER = address(0xB0B);
    address private constant POOL_OWNER = address(0x0E);

    IPoolManager private _poolManager;
    LaunchFactoryV2 private _factory;
    BondingCurveV2 private _implementation;
    IERC20 private _token;
    IBondingCurveV2 private _curve;
    PoolSwapTest private _swapRouter;
    RejectingRefundRecipient private _rejector;

    function run() external {
        vm.warp(GENESIS_TIMESTAMP);
        vm.recordLogs();
        _deploySystem();
        _swapRouter = new PoolSwapTest(_poolManager);
        _rejector = new RejectingRefundRecipient();

        _launchWithDeveloperBuy();
        _tradeOnCurve();
        _changeFeeRecipient();
        _graduateWithCreditedRefund();
        _swapInPool();
        _collectAndClaim();
        _administer();

        _writeFixtures(vm.getRecordedLogs());
    }

    function _deploySystem() private {
        IAllowanceTransfer permit2 = IAllowanceTransfer(new DeployPermit2().deployPermit2());
        _poolManager = IPoolManager(deployCode(POOL_MANAGER_ARTIFACT, abi.encode(POOL_OWNER)));
        IPositionManager positionManager = IPositionManager(
            deployCode(
                POSITION_MANAGER_ARTIFACT,
                abi.encode(
                    address(_poolManager),
                    address(permit2),
                    UNSUBSCRIBE_GAS_LIMIT,
                    address(0),
                    address(0)
                )
            )
        );
        _implementation = new BondingCurveV2();
        VectorSystemV2 system = new VectorSystemV2(
            LaunchTypesV2.FactoryInitializationV2({
                pauseAuthority: PAUSE_AUTHORITY,
                timelock: TIMELOCK,
                protocolTreasury: TREASURY,
                engineVersion: ENGINE_VERSION,
                implementation: address(_implementation),
                poolManager: _poolManager,
                positionManager: positionManager,
                permit2: permit2,
                defaults: _defaults()
            })
        );
        _factory = system.FACTORY();
    }

    function _launchWithDeveloperBuy() private {
        vm.deal(CREATOR, 1 ether);
        vm.prank(CREATOR);
        (address token, address curve) = _factory.launch{ value: LAUNCH_FEE + DEVELOPER_BUY }(
            LaunchTypes.LaunchRequest({
                name: unicode"Pons 🐸 launch",
                symbol: unicode"PØNS",
                engineVersion: ENGINE_VERSION,
                developerBuyGross: DEVELOPER_BUY,
                minDeveloperTokensOut: 0,
                deadline: type(uint256).max
            })
        );
        _token = IERC20(token);
        _curve = IBondingCurveV2(curve);
    }

    /// @dev A taxed buy inside the anti-snipe window, then a partial sell.
    function _tradeOnCurve() private {
        vm.warp(GENESIS_TIMESTAMP + 10);
        require(_curve.currentTaxBps() != 0, "TAX_WINDOW_EXPECTED");
        vm.deal(TRADER, 100 ether);
        vm.prank(TRADER);
        (uint256 tokensOut,) = _curve.buy{ value: 0.5 ether }(TRADER, TRADER, 0, type(uint256).max);
        vm.prank(TRADER);
        require(_token.approve(address(_curve), tokensOut / 4), "APPROVE_FAILED");
        vm.prank(TRADER);
        _curve.sell(tokensOut / 4, TRADER, 0, type(uint256).max);
    }

    function _changeFeeRecipient() private {
        vm.prank(CREATOR);
        _curve.proposeFeeRecipient(NEXT_RECIPIENT);
        vm.prank(NEXT_RECIPIENT);
        _curve.acceptFeeRecipient();
    }

    /// @dev An untaxed final buy whose overshoot refund cannot be pushed, so it is credited.
    function _graduateWithCreditedRefund() private {
        vm.warp(GENESIS_TIMESTAMP + 120);
        require(_curve.currentTaxBps() == 0, "TAX_WINDOW_NOT_OVER");
        vm.prank(TRADER);
        _curve.buy{ value: 10 ether }(TRADER, address(_rejector), 0, type(uint256).max);
        require(_curve.pendingRefund(address(_rejector)) != 0, "REFUND_NOT_CREDITED");
        _rejector.claimRefundTo(_curve, TRADER);
    }

    function _swapInPool() private {
        PoolKey memory key = PoolKey({
            currency0: Currency.wrap(address(0)),
            currency1: Currency.wrap(address(_token)),
            fee: 10_000,
            tickSpacing: 200,
            hooks: IHooks(_factory.hook())
        });
        PoolSwapTest.TestSettings memory settings =
            PoolSwapTest.TestSettings({ takeClaims: false, settleUsingBurn: false });
        vm.prank(TRADER);
        _swapRouter.swap{ value: 0.2 ether }(
            key,
            SwapParams({
                zeroForOne: true,
                amountSpecified: -0.2 ether,
                sqrtPriceLimitX96: TickMath.MIN_SQRT_PRICE + 1
            }),
            settings,
            ""
        );
        vm.prank(TRADER);
        require(_token.approve(address(_swapRouter), 1_000_000 ether), "APPROVE_FAILED");
        vm.prank(TRADER);
        _swapRouter.swap(
            key,
            SwapParams({
                zeroForOne: false,
                amountSpecified: -1_000_000 ether,
                sqrtPriceLimitX96: TickMath.MAX_SQRT_PRICE - 1
            }),
            settings,
            ""
        );
    }

    function _collectAndClaim() private {
        LpFeeLocker locker = LpFeeLocker(payable(_factory.locker()));
        locker.collect(address(_token));
        vm.prank(NEXT_RECIPIENT);
        locker.claim(address(_token));
        vm.prank(TREASURY);
        locker.claim(address(_token));

        vm.prank(CREATOR);
        _curve.claimCreatorFees();
        vm.prank(NEXT_RECIPIENT);
        _curve.claimCreatorFees();
        vm.prank(TREASURY);
        _curve.claimProtocolFees();
        vm.prank(TREASURY);
        _curve.claimSnipeTax();
    }

    function _administer() private {
        vm.prank(TREASURY);
        require(_factory.claimLaunchFees() == LAUNCH_FEE, "LAUNCH_FEE_FIXTURE_MISMATCH");
        vm.prank(PAUSE_AUTHORITY);
        _factory.setLaunchesPaused(true);
        vm.prank(PAUSE_AUTHORITY);
        _factory.setTradingPaused(true);
        vm.prank(TIMELOCK);
        _factory.configureEngine(ENGINE_VERSION, address(_implementation), true);
        vm.prank(TIMELOCK);
        _factory.setFutureDefaults(_defaults());
        vm.prank(TIMELOCK);
        _factory.setFutureTreasury(address(0x7001));
    }

    function _defaults() private pure returns (LaunchTypesV2.FactoryDefaultsV2 memory) {
        return LaunchTypesV2.FactoryDefaultsV2({
            parameters: LaunchTypes.CurveParameters({
                totalSupply: 1_000_000_000 ether,
                curveTokens: 800_000_000 ether,
                lpTokens: 200_000_000 ether,
                graduationEth: 4.2 ether,
                initialVirtualEth: 1.4 ether,
                initialVirtualToken: 1_066_666_666_666_666_666_666_666_667,
                tradeFeeBps: 100,
                protocolShareBps: 5000
            }),
            antiSnipe: LaunchTypesV2.AntiSnipeProfile({ startBps: 5000, durationSeconds: 60 }),
            lpCreatorShareBps: 5000,
            launchFee: LAUNCH_FEE
        });
    }

    function _writeFixtures(Vm.Log[] memory logs) private {
        bytes32[] memory topics = _supportedTopics();
        bool[] memory seen = new bool[](topics.length);
        string memory json = '{"schemaVersion":1,"engineVersion":2,"logs":[';
        bool first = true;
        for (uint256 index = 0; index < logs.length; ++index) {
            string memory kind = _kind(logs[index]);
            if (bytes(kind).length == 0 || logs[index].topics.length == 0) continue;
            uint256 topicIndex = _topicIndex(topics, logs[index].topics[0]);
            if (topicIndex == type(uint256).max) continue;
            seen[topicIndex] = true;
            if (!first) json = string.concat(json, ",");
            first = false;
            json = string.concat(json, _logJson(logs[index], kind));
        }
        for (uint256 index = 0; index < seen.length; ++index) {
            require(seen[index], string.concat("MISSING_TOPIC_", vm.toString(topics[index])));
        }
        string memory output =
            vm.envOr("EVENT_FIXTURES_V2_OUTPUT", string("./fixtures/v2/event-logs-v2.json"));
        vm.writeFile(output, string.concat(json, "]}\n"));
    }

    function _kind(Vm.Log memory entry) private view returns (string memory) {
        if (entry.emitter == address(_factory)) return "factory";
        if (entry.emitter == address(_curve)) return "curve";
        if (entry.emitter == address(_token)) return "token";
        if (entry.emitter == _factory.locker()) return "locker";
        if (entry.emitter == address(_poolManager)) return "poolManager";
        return "";
    }

    function _topicIndex(bytes32[] memory topics, bytes32 topic) private pure returns (uint256) {
        for (uint256 index = 0; index < topics.length; ++index) {
            if (topics[index] == topic) return index;
        }
        return type(uint256).max;
    }

    function _supportedTopics() private pure returns (bytes32[] memory topics) {
        topics = new bytes32[](SUPPORTED_TOPIC_COUNT);
        topics[0] = ILaunchEventsV2.TokenLaunched.selector;
        topics[1] = ILaunchEventsV2.Trade.selector;
        topics[2] = ILaunchEventsV2.Graduated.selector;
        topics[3] = ILaunchEventsV2.SnipeTaxClaimed.selector;
        topics[4] = ILaunchEventsV2.FeeRecipientProposed.selector;
        topics[5] = ILaunchEventsV2.FeeRecipientAccepted.selector;
        topics[6] = ILaunchEventsV2.CreatorFeesClaimed.selector;
        topics[7] = ILaunchEventsV2.ProtocolFeesClaimed.selector;
        topics[8] = ILaunchEventsV2.RefundCredited.selector;
        topics[9] = ILaunchEventsV2.RefundClaimed.selector;
        topics[10] = ILaunchFactoryV2.LaunchFeesClaimed.selector;
        topics[11] = ILaunchFactoryV2.LaunchPauseSet.selector;
        topics[12] = ILaunchFactoryV2.TradingPauseSet.selector;
        topics[13] = ILaunchFactoryV2.EngineConfigured.selector;
        topics[14] = ILaunchFactoryV2.FutureDefaultsConfigured.selector;
        topics[15] = ILaunchFactoryV2.FutureTreasuryConfigured.selector;
        topics[16] = ILpFeeLocker.LpFeesCollected.selector;
        topics[17] = ILpFeeLocker.LpFeesClaimed.selector;
        topics[18] = IERC20.Transfer.selector;
        topics[19] = IPoolManager.Initialize.selector;
        topics[20] = IPoolManager.ModifyLiquidity.selector;
        topics[21] = IPoolManager.Swap.selector;
    }

    function _logJson(Vm.Log memory entry, string memory kind)
        private
        pure
        returns (string memory json)
    {
        json = string.concat(
            '{"emitterKind":"', kind, '","address":"', vm.toString(entry.emitter), '","topics":['
        );
        for (uint256 index = 0; index < entry.topics.length; ++index) {
            if (index != 0) json = string.concat(json, ",");
            json = string.concat(json, '"', vm.toString(entry.topics[index]), '"');
        }
        json = string.concat(json, '],"data":"', vm.toString(entry.data), '"}');
    }
}
// forge-lint: disable-end(calls-loop, require-revert-in-loop, unused-return, arbitrary-send-eth)
