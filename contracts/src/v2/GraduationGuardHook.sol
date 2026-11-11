// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IHooks } from "@uniswap/v4-core/src/interfaces/IHooks.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { Hooks } from "@uniswap/v4-core/src/libraries/Hooks.sol";
import { BalanceDelta } from "@uniswap/v4-core/src/types/BalanceDelta.sol";
import { BeforeSwapDelta } from "@uniswap/v4-core/src/types/BeforeSwapDelta.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { ModifyLiquidityParams, SwapParams } from "@uniswap/v4-core/src/types/PoolOperation.sol";
import { ICurveRegistry } from "./interfaces/ICurveRegistry.sol";

/// @notice Uniswap v4 hook whose only permission is `beforeInitialize` (spec §3.4). It lets
/// only a launch's own curve initialize that token's pool, so nobody can open the
/// deterministic pool key at a hostile price before graduation. No storage, no owner.
/// @dev Must be deployed at an address whose low 14 bits equal `BEFORE_INITIALIZE_FLAG`
/// (mined with `script/v2/MineGuardHook.s.sol`); the constructor enforces it.
contract GraduationGuardHook is IHooks {
    uint24 public constant POOL_FEE = 10_000;
    int24 public constant TICK_SPACING = 200;

    IPoolManager public immutable POOL_MANAGER;
    ICurveRegistry public immutable FACTORY;

    error UnauthorizedInitializer(address sender);
    error InvalidPoolKey();
    error NotPoolManager(address caller);
    error HookNotImplemented();

    constructor(IPoolManager poolManager_, ICurveRegistry factory_) {
        POOL_MANAGER = poolManager_;
        FACTORY = factory_;
        Hooks.validateHookPermissions(IHooks(address(this)), getHookPermissions());
    }

    function getHookPermissions() public pure returns (Hooks.Permissions memory permissions) {
        permissions.beforeInitialize = true;
    }

    /// @notice Admits only `FACTORY.curveOfToken(currency1)` initializing a native-ETH pool
    /// with the launchpad's fee and tick spacing.
    function beforeInitialize(address sender, PoolKey calldata key, uint160)
        external
        view
        returns (bytes4)
    {
        if (msg.sender != address(POOL_MANAGER)) revert NotPoolManager(msg.sender);
        if (
            !key.currency0.isAddressZero() || key.fee != POOL_FEE || key.tickSpacing != TICK_SPACING
        ) {
            revert InvalidPoolKey();
        }
        address curve = FACTORY.curveOfToken(Currency.unwrap(key.currency1));
        if (sender == address(0) || sender != curve) revert UnauthorizedInitializer(sender);
        return IHooks.beforeInitialize.selector;
    }

    function afterInitialize(address, PoolKey calldata, uint160, int24)
        external
        pure
        returns (bytes4)
    {
        revert HookNotImplemented();
    }

    function beforeAddLiquidity(
        address,
        PoolKey calldata,
        ModifyLiquidityParams calldata,
        bytes calldata
    ) external pure returns (bytes4) {
        revert HookNotImplemented();
    }

    function afterAddLiquidity(
        address,
        PoolKey calldata,
        ModifyLiquidityParams calldata,
        BalanceDelta,
        BalanceDelta,
        bytes calldata
    ) external pure returns (bytes4, BalanceDelta) {
        revert HookNotImplemented();
    }

    function beforeRemoveLiquidity(
        address,
        PoolKey calldata,
        ModifyLiquidityParams calldata,
        bytes calldata
    ) external pure returns (bytes4) {
        revert HookNotImplemented();
    }

    function afterRemoveLiquidity(
        address,
        PoolKey calldata,
        ModifyLiquidityParams calldata,
        BalanceDelta,
        BalanceDelta,
        bytes calldata
    ) external pure returns (bytes4, BalanceDelta) {
        revert HookNotImplemented();
    }

    function beforeSwap(address, PoolKey calldata, SwapParams calldata, bytes calldata)
        external
        pure
        returns (bytes4, BeforeSwapDelta, uint24)
    {
        revert HookNotImplemented();
    }

    function afterSwap(address, PoolKey calldata, SwapParams calldata, BalanceDelta, bytes calldata)
        external
        pure
        returns (bytes4, int128)
    {
        revert HookNotImplemented();
    }

    function beforeDonate(address, PoolKey calldata, uint256, uint256, bytes calldata)
        external
        pure
        returns (bytes4)
    {
        revert HookNotImplemented();
    }

    function afterDonate(address, PoolKey calldata, uint256, uint256, bytes calldata)
        external
        pure
        returns (bytes4)
    {
        revert HookNotImplemented();
    }
}
