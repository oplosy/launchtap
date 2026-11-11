// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Clones } from "@openzeppelin/contracts/proxy/Clones.sol";
import { IHooks } from "@uniswap/v4-core/src/interfaces/IHooks.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { CurveMath } from "../../../src/libraries/CurveMath.sol";
import { LaunchTypes } from "../../../src/types/LaunchTypes.sol";
import { BondingCurveV2 } from "../../../src/v2/BondingCurveV2.sol";
import { GraduationGuardHook } from "../../../src/v2/GraduationGuardHook.sol";
import { LaunchTokenV2 } from "../../../src/v2/LaunchTokenV2.sol";
import { LpFeeLocker } from "../../../src/v2/LpFeeLocker.sol";
import { IBondingCurveV2 } from "../../../src/v2/interfaces/IBondingCurveV2.sol";
import { ICurveRegistry } from "../../../src/v2/interfaces/ICurveRegistry.sol";
import { LaunchTypesV2 } from "../../../src/v2/types/LaunchTypesV2.sol";
import { MineGuardHook } from "../../../script/v2/MineGuardHook.s.sol";
import { V4TestBase } from "./V4TestBase.sol";

/// @notice Minimal stand-in for `LaunchFactoryV2` (Task 8): registry, trading pause, launch
/// and the factory-only developer buy.
contract MockLaunchFactoryV2 is ICurveRegistry {
    mapping(address token => address curve) public curveOfToken;
    bool public tradingPaused;

    function setTradingPaused(bool paused) external {
        tradingPaused = paused;
    }

    function setCurveOfToken(address token, address curve) external {
        curveOfToken[token] = curve;
    }

    function launch(
        address implementation,
        LaunchTypesV2.CurveInitializationV2 memory initialization,
        string memory name,
        string memory symbol
    ) external returns (IBondingCurveV2 curve, LaunchTokenV2 token) {
        curve = IBondingCurveV2(Clones.clone(implementation));
        token = new LaunchTokenV2(
            name,
            symbol,
            address(curve),
            address(initialization.poolManager),
            initialization.parameters.totalSupply
        );
        curveOfToken[address(token)] = address(curve);
        initialization.factory = address(this);
        initialization.implementation = implementation;
        initialization.token = address(token);
        curve.initialize(initialization);
    }

    function developerBuy(IBondingCurveV2 curve, address trader, uint256 deadline)
        external
        payable
        returns (uint256 tokensOut, uint256 ethGrossUsed)
    {
        // forge-lint: disable-next-line(arbitrary-send-eth)
        return curve.buyFor{ value: msg.value }(trader, trader, trader, 0, deadline);
    }
}

// Tests assert on events/reverts directly and send ETH to fixed test contracts.
// forge-lint: disable-start(arbitrary-send-eth, unused-return)
abstract contract BondingCurveV2TestBase is V4TestBase {
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
    uint256 internal constant LAUNCH_TIMESTAMP = 1_000_000;

    address internal constant CREATOR = address(0xC4EA);
    address internal constant TREASURY = address(0x7EA5);
    address internal constant ALICE = address(0xA11CE);
    address internal constant BOB = address(0xB0B);
    address internal constant STRANGER = address(0x5757);

    MockLaunchFactoryV2 internal factory;
    GraduationGuardHook internal hook;
    LpFeeLocker internal locker;
    BondingCurveV2 internal implementation;
    IBondingCurveV2 internal curve;
    LaunchTokenV2 internal token;

    function setUp() public virtual {
        vm.warp(LAUNCH_TIMESTAMP);
        deployV4();
        factory = new MockLaunchFactoryV2();
        (, bytes32 salt) = new MineGuardHook().mine(address(this), poolManager, factory);
        hook = new GraduationGuardHook{ salt: salt }(poolManager, factory);
        locker = new LpFeeLocker(positionManager, factory);
        implementation = new BondingCurveV2();
        (curve, token) = factory.launch(address(implementation), _initialization(), "Launch", "LCH");

        vm.deal(ALICE, 100 ether);
        vm.deal(BOB, 100 ether);
    }

    function _initialization() internal view returns (LaunchTypesV2.CurveInitializationV2 memory) {
        return LaunchTypesV2.CurveInitializationV2({
            factory: address(0),
            implementation: address(0),
            token: address(0),
            creator: CREATOR,
            protocolTreasury: TREASURY,
            poolManager: poolManager,
            positionManager: positionManager,
            permit2: permit2,
            hook: address(hook),
            locker: address(locker),
            parameters: _defaultParameters(),
            antiSnipe: LaunchTypesV2.AntiSnipeProfile({
                startBps: SNIPE_START_BPS, durationSeconds: SNIPE_DURATION
            }),
            lpCreatorShareBps: LP_CREATOR_SHARE_BPS
        });
    }

    function _defaultParameters() internal pure returns (LaunchTypes.CurveParameters memory) {
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

    function _poolKey() internal view returns (PoolKey memory) {
        return PoolKey({
            currency0: Currency.wrap(address(0)),
            currency1: Currency.wrap(address(token)),
            fee: 10_000,
            tickSpacing: 200,
            hooks: IHooks(address(hook))
        });
    }

    /// @dev v1 quote over the curve's current reserves, for an untaxed net amount.
    function _v1QuoteBuy(uint256 netGross) internal view returns (CurveMath.BuyQuote memory) {
        return CurveMath.quoteBuy(
            INITIAL_VIRTUAL_ETH * INITIAL_VIRTUAL_TOKEN,
            curve.virtualEthReserve(),
            curve.virtualTokenReserve(),
            INITIAL_VIRTUAL_TOKEN - CURVE_TOKENS,
            netGross,
            TRADE_FEE_BPS,
            PROTOCOL_SHARE_BPS
        );
    }

    function _buy(address trader, uint256 ethIn) internal returns (uint256 tokensOut) {
        vm.prank(trader);
        (tokensOut,) = curve.buy{ value: ethIn }(trader, trader, 0, block.timestamp);
    }
}
// forge-lint: disable-end(arbitrary-send-eth, unused-return)
