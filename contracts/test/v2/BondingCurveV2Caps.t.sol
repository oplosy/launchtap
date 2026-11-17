// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { GraduationMath } from "../../src/libraries/GraduationMath.sol";
import { IBondingCurveV2 } from "../../src/v2/interfaces/IBondingCurveV2.sol";
import { LaunchTypesV2 } from "../../src/v2/types/LaunchTypesV2.sol";
import { BondingCurveV2TestBase } from "./utils/BondingCurveV2TestBase.sol";

/// @notice Owner decision L-1: `initialize` caps the trade fee and requires the pool to open at
/// the curve's final price (within 1 bp), so a direct clone cannot bypass the factory's checks.
// forge-lint: disable-start(unused-return)
contract BondingCurveV2CapsTest is BondingCurveV2TestBase {
    function test_initialize_capsTradeFee() public {
        LaunchTypesV2.CurveInitializationV2 memory initialization = _initialization();
        initialization.parameters.tradeFeeBps = LaunchTypesV2.MAX_TRADE_FEE_BPS + 1;
        vm.expectRevert(
            abi.encodeWithSelector(
                IBondingCurveV2.TradeFeeAboveCap.selector, LaunchTypesV2.MAX_TRADE_FEE_BPS + 1
            )
        );
        factory.launch(address(implementation), initialization, "Capped", "CAP");

        initialization.parameters.tradeFeeBps = LaunchTypesV2.MAX_TRADE_FEE_BPS;
        (IBondingCurveV2 capped,) =
            factory.launch(address(implementation), initialization, "Capped", "CAP");
        assertEq(capped.launchTime(), LAUNCH_TIMESTAMP);
    }

    function test_initialize_requiresPriceContinuity() public {
        // 30,000 extra LP tokens open the pool 1.5 bp below the curve's final price.
        LaunchTypesV2.CurveInitializationV2 memory initialization = _withExtraLpTokens(30_000 ether);
        vm.expectRevert(
            abi.encodeWithSelector(
                GraduationMath.GraduationPriceDiscontinuity.selector,
                (INITIAL_VIRTUAL_ETH + GRADUATION_ETH) * (LP_TOKENS + 30_000 ether),
                GRADUATION_ETH * (INITIAL_VIRTUAL_TOKEN - CURVE_TOKENS)
            )
        );
        factory.launch(address(implementation), initialization, "Gap", "GAP");

        // 10,000 extra (0.5 bp) is inside the tolerance.
        factory.launch(address(implementation), _withExtraLpTokens(10_000 ether), "Gap", "GAP");
    }

    function test_initialize_requiresPriceContinuityFromAbove() public {
        // 30,000 fewer LP tokens open the pool 1.5 bp above the curve's final price.
        LaunchTypesV2.CurveInitializationV2 memory initialization = _initialization();
        initialization.parameters.lpTokens -= 30_000 ether;
        initialization.parameters.totalSupply -= 30_000 ether;
        vm.expectRevert(
            abi.encodeWithSelector(
                GraduationMath.GraduationPriceDiscontinuity.selector,
                (INITIAL_VIRTUAL_ETH + GRADUATION_ETH) * (LP_TOKENS - 30_000 ether),
                GRADUATION_ETH * (INITIAL_VIRTUAL_TOKEN - CURVE_TOKENS)
            )
        );
        factory.launch(address(implementation), initialization, "Gap", "GAP");
    }

    function _withExtraLpTokens(uint256 extra)
        private
        view
        returns (LaunchTypesV2.CurveInitializationV2 memory initialization)
    {
        initialization = _initialization();
        initialization.parameters.lpTokens += extra;
        initialization.parameters.totalSupply += extra;
    }
}
// forge-lint: disable-end(unused-return)
