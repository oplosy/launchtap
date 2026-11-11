// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Test } from "forge-std/Test.sol";
import { AntiSnipe } from "../../src/libraries/AntiSnipe.sol";
import { AntiSnipeHarness } from "../harness/AntiSnipeHarness.sol";

contract AntiSnipeTest is Test {
    AntiSnipeHarness internal harness;

    function setUp() public {
        harness = new AntiSnipeHarness();
    }

    function test_taxBps_fullAtLaunch() public view {
        assertEq(harness.taxBps(5000, 60, 1000, 1000), 5000);
    }

    function test_taxBps_beforeLaunchIsTreatedAsLaunch() public view {
        assertEq(harness.taxBps(5000, 60, 1000, 999), 5000);
    }

    function test_taxBps_zeroAtEnd() public view {
        assertEq(harness.taxBps(5000, 60, 1000, 1060), 0);
        assertEq(harness.taxBps(5000, 60, 1000, 5000), 0);
    }

    function test_taxBps_floors() public view {
        assertEq(harness.taxBps(5000, 60, 1000, 1030), 2500);
        assertEq(harness.taxBps(5000, 60, 1000, 1059), 83);
    }

    function test_taxBps_zeroDuration() public view {
        assertEq(harness.taxBps(5000, 0, 1000, 1000), 0);
    }

    function test_split() public view {
        (uint256 tax, uint256 net) = harness.split(1e18, 2500);
        assertEq(tax, 0.25e18);
        assertEq(net, 0.75e18);

        (tax, net) = harness.split(3, 5000);
        assertEq(tax, 1);
        assertEq(net, 2);
    }

    function test_split_rejectsFullTax() public {
        vm.expectRevert(abi.encodeWithSelector(AntiSnipe.AntiSnipeBpsTooHigh.selector, 10_000));
        // forge-lint: disable-next-line(unused-return)
        harness.split(1e18, 10_000);
    }

    function test_grossForNet_zeroNetIsZero() public view {
        assertEq(harness.grossForNet(0, 5000), 0);
    }

    function testFuzz_grossForNet_isMinimal(uint128 net, uint16 bps) public view {
        // bound(..., 0, 9000) fits uint16.
        // forge-lint: disable-next-line(unsafe-typecast)
        bps = uint16(bound(bps, 0, 9000));
        uint256 gross = harness.grossForNet(net, bps);

        (uint256 taxAtGross, uint256 netAtGross) = harness.split(gross, bps);
        assertEq(taxAtGross + netAtGross, gross);
        assertGe(netAtGross, net);
        if (gross > 0) {
            (uint256 taxBelow, uint256 netBelow) = harness.split(gross - 1, bps);
            assertEq(taxBelow + netBelow, gross - 1);
            assertLt(netBelow, net);
        }
    }
}
