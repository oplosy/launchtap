// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IHooks } from "@uniswap/v4-core/src/interfaces/IHooks.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { LaunchTypes } from "../../../src/types/LaunchTypes.sol";
import { BondingCurveV2 } from "../../../src/v2/BondingCurveV2.sol";
import { GraduationGuardHook } from "../../../src/v2/GraduationGuardHook.sol";
import { LaunchFactoryV2 } from "../../../src/v2/LaunchFactoryV2.sol";
import { LpFeeLocker } from "../../../src/v2/LpFeeLocker.sol";
import { LaunchTypesV2 } from "../../../src/v2/types/LaunchTypesV2.sol";
import { MineGuardHook } from "../../../script/v2/MineGuardHook.s.sol";
import { V4TestBase } from "./V4TestBase.sol";

/// @notice The production v2 system (factory, guard hook, LP fee locker, curve
/// implementation) on a local v4 stack, with the v1 default curve parameters.
abstract contract V2SystemBase is V4TestBase {
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
    address internal constant CREATOR = address(0xC4EA);

    LaunchFactoryV2 internal factory;
    BondingCurveV2 internal implementation;
    GraduationGuardHook internal hook;
    LpFeeLocker internal locker;

    function deployV2System() internal {
        vm.warp(LAUNCH_TIMESTAMP);
        deployV4();
        implementation = new BondingCurveV2();
        factory = new LaunchFactoryV2(
            LaunchTypesV2.FactoryInitializationV2({
                pauseAuthority: PAUSE_AUTHORITY,
                timelock: TIMELOCK,
                protocolTreasury: TREASURY,
                engineVersion: ENGINE_V2,
                implementation: address(implementation),
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
                        tradeFeeBps: TRADE_FEE_BPS,
                        protocolShareBps: PROTOCOL_SHARE_BPS
                    }),
                    antiSnipe: LaunchTypesV2.AntiSnipeProfile({
                        startBps: SNIPE_START_BPS, durationSeconds: SNIPE_DURATION
                    }),
                    lpCreatorShareBps: LP_CREATOR_SHARE_BPS,
                    launchFee: LAUNCH_FEE
                })
            })
        );
        (, bytes32 salt) = new MineGuardHook().mine(address(this), poolManager, factory);
        hook = new GraduationGuardHook{ salt: salt }(poolManager, factory);
        locker = new LpFeeLocker(positionManager, factory);
        factory.setDependencies(address(hook), address(locker));
    }

    // The launch fee goes to the factory under test.
    // forge-lint: disable-start(arbitrary-send-eth)
    function launchToken(string memory symbol) internal returns (address token, address curve) {
        vm.deal(CREATOR, CREATOR.balance + LAUNCH_FEE);
        vm.prank(CREATOR);
        (token, curve) = factory.launch{ value: LAUNCH_FEE }(
            LaunchTypes.LaunchRequest({
                name: symbol,
                symbol: symbol,
                engineVersion: ENGINE_V2,
                developerBuyGross: 0,
                minDeveloperTokensOut: 0,
                deadline: block.timestamp
            })
        );
    }
    // forge-lint: disable-end(arbitrary-send-eth)

    function poolKeyOf(address token) internal view returns (PoolKey memory) {
        return PoolKey({
            currency0: Currency.wrap(address(0)),
            currency1: Currency.wrap(token),
            fee: hook.POOL_FEE(),
            tickSpacing: hook.TICK_SPACING(),
            hooks: IHooks(address(hook))
        });
    }
}
