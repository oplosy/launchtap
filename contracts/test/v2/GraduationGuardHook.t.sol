// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IHooks } from "@uniswap/v4-core/src/interfaces/IHooks.sol";
import { Hooks } from "@uniswap/v4-core/src/libraries/Hooks.sol";
import { CustomRevert } from "@uniswap/v4-core/src/libraries/CustomRevert.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { ModifyLiquidityParams } from "@uniswap/v4-core/src/types/PoolOperation.sol";
import { GraduationGuardHook } from "../../src/v2/GraduationGuardHook.sol";
import { MineGuardHook } from "../../script/v2/MineGuardHook.s.sol";
import { MockCurveRegistry } from "./mocks/MockCurveRegistry.sol";
import { V4TestBase } from "./utils/V4TestBase.sol";

contract GraduationGuardHookTest is V4TestBase {
    address internal constant TOKEN = address(0x7000);
    address internal constant OTHER_TOKEN = address(0x6000);
    address internal constant CURVE = address(0xC0DE);
    address internal constant CREATOR = address(0xC4EA);
    uint160 internal constant SQRT_PRICE = 2 ** 96;

    MockCurveRegistry internal registry;
    GraduationGuardHook internal hook;

    function setUp() public {
        deployV4();
        registry = new MockCurveRegistry();
        registry.setCurve(TOKEN, CURVE);

        (address expected, bytes32 salt) =
            new MineGuardHook().mine(address(this), poolManager, registry);
        hook = new GraduationGuardHook{ salt: salt }(poolManager, registry);
        assertEq(address(hook), expected);
    }

    function test_minedAddress_hasOnlyBeforeInitializeFlag() public view {
        assertEq(uint160(address(hook)) & Hooks.ALL_HOOK_MASK, Hooks.BEFORE_INITIALIZE_FLAG);
        Hooks.Permissions memory permissions = hook.getHookPermissions();
        assertTrue(permissions.beforeInitialize);
        assertFalse(permissions.afterInitialize);
        assertFalse(permissions.beforeAddLiquidity);
        assertFalse(permissions.beforeSwap);
        assertFalse(permissions.afterSwap);
        assertFalse(permissions.beforeDonate);
    }

    function test_constructor_rejectsAddressWithWrongFlags() public {
        address predicted = vm.computeCreateAddress(address(this), vm.getNonce(address(this)));
        // A CREATE address with exactly the right low 14 bits would be a 1-in-16384 fluke.
        assertTrue(uint160(predicted) & Hooks.ALL_HOOK_MASK != Hooks.BEFORE_INITIALIZE_FLAG);
        vm.expectRevert(abi.encodeWithSelector(Hooks.HookAddressNotValid.selector, predicted));
        new GraduationGuardHook(poolManager, registry);
    }

    function test_registeredCurve_initializes() public {
        vm.prank(CURVE);
        assertEq(poolManager.initialize(_key(TOKEN), SQRT_PRICE), 0);
    }

    function test_creator_cannotInitialize() public {
        _expectWrapped(
            abi.encodeWithSelector(GraduationGuardHook.UnauthorizedInitializer.selector, CREATOR)
        );
        vm.prank(CREATOR);
        // forge-lint: disable-next-line(unused-return)
        poolManager.initialize(_key(TOKEN), SQRT_PRICE);
    }

    function test_factory_cannotInitialize() public {
        _expectWrapped(
            abi.encodeWithSelector(
                GraduationGuardHook.UnauthorizedInitializer.selector, address(registry)
            )
        );
        vm.prank(address(registry));
        // forge-lint: disable-next-line(unused-return)
        poolManager.initialize(_key(TOKEN), SQRT_PRICE);
    }

    function test_unknownToken_cannotBeInitialized() public {
        _expectWrapped(
            abi.encodeWithSelector(GraduationGuardHook.UnauthorizedInitializer.selector, CURVE)
        );
        vm.prank(CURVE);
        // forge-lint: disable-next-line(unused-return)
        poolManager.initialize(_key(OTHER_TOKEN), SQRT_PRICE);
    }

    function test_wrongFee_reverts() public {
        PoolKey memory key = _key(TOKEN);
        key.fee = 3000;
        _expectWrapped(abi.encodeWithSelector(GraduationGuardHook.InvalidPoolKey.selector));
        vm.prank(CURVE);
        // forge-lint: disable-next-line(unused-return)
        poolManager.initialize(key, SQRT_PRICE);
    }

    function test_wrongTickSpacing_reverts() public {
        PoolKey memory key = _key(TOKEN);
        key.tickSpacing = 60;
        _expectWrapped(abi.encodeWithSelector(GraduationGuardHook.InvalidPoolKey.selector));
        vm.prank(CURVE);
        // forge-lint: disable-next-line(unused-return)
        poolManager.initialize(key, SQRT_PRICE);
    }

    function test_nonNativeCurrency0_reverts() public {
        PoolKey memory key = _key(TOKEN);
        key.currency0 = Currency.wrap(OTHER_TOKEN);
        _expectWrapped(abi.encodeWithSelector(GraduationGuardHook.InvalidPoolKey.selector));
        vm.prank(CURVE);
        // forge-lint: disable-next-line(unused-return)
        poolManager.initialize(key, SQRT_PRICE);
    }

    function test_beforeInitialize_onlyPoolManager() public {
        vm.expectRevert(abi.encodeWithSelector(GraduationGuardHook.NotPoolManager.selector, CURVE));
        vm.prank(CURVE);
        // forge-lint: disable-next-line(unused-return)
        hook.beforeInitialize(CURVE, _key(TOKEN), SQRT_PRICE);
    }

    function test_otherCallbacks_revert() public {
        ModifyLiquidityParams memory params;
        vm.expectRevert(GraduationGuardHook.HookNotImplemented.selector);
        vm.prank(address(poolManager));
        // forge-lint: disable-next-line(unused-return)
        hook.beforeAddLiquidity(CURVE, _key(TOKEN), params, "");
    }

    function _key(address token) internal view returns (PoolKey memory) {
        return PoolKey({
            currency0: Currency.wrap(address(0)),
            currency1: Currency.wrap(token),
            fee: 10_000,
            tickSpacing: 200,
            hooks: IHooks(address(hook))
        });
    }

    function _expectWrapped(bytes memory reason) internal {
        vm.expectRevert(
            abi.encodeWithSelector(
                CustomRevert.WrappedError.selector,
                address(hook),
                IHooks.beforeInitialize.selector,
                reason,
                abi.encodeWithSelector(Hooks.HookCallFailed.selector)
            )
        );
    }
}
