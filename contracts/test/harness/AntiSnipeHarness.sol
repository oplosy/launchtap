// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { AntiSnipe } from "../../src/libraries/AntiSnipe.sol";

contract AntiSnipeHarness {
    function taxBps(uint16 startBps, uint32 durationSeconds, uint64 launchTime, uint256 nowTs)
        external
        pure
        returns (uint16)
    {
        return AntiSnipe.taxBps(startBps, durationSeconds, launchTime, nowTs);
    }

    function split(uint256 gross, uint16 taxBps_) external pure returns (uint256, uint256) {
        return AntiSnipe.split(gross, taxBps_);
    }

    function grossForNet(uint256 net, uint16 taxBps_) external pure returns (uint256) {
        return AntiSnipe.grossForNet(net, taxBps_);
    }
}
