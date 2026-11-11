// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Math } from "@openzeppelin/contracts/utils/math/Math.sol";
import { ILaunchErrors } from "../interfaces/ILaunchErrors.sol";

/// @notice Anti-snipe tax arithmetic for v2 curve buys (spec §4). Mirrored by name in the
/// backend `internal/curve` package and locked by the v2 curve vectors.
library AntiSnipe {
    uint256 internal constant BPS_DENOMINATOR = 10_000;

    error AntiSnipeBpsTooHigh(uint16 taxBps);

    /// @notice Current tax rate in bps, decaying linearly from `startBps` at `launchTime` to
    /// zero at `launchTime + durationSeconds`.
    /// @dev `remaining = launchTime + durationSeconds - nowTs` while positive, else zero;
    /// `nowTs < launchTime` is treated as `nowTs = launchTime`. The rate is
    /// `floor(startBps * remaining / durationSeconds)`, rounding in the buyer's favor. A zero
    /// duration disables the tax. The result never exceeds `startBps`.
    function taxBps(uint16 startBps, uint32 durationSeconds, uint64 launchTime, uint256 nowTs)
        internal
        pure
        returns (uint16)
    {
        if (durationSeconds == 0 || startBps == 0) return 0;

        uint256 end = uint256(launchTime) + uint256(durationSeconds);
        uint256 effectiveNow = nowTs < launchTime ? launchTime : nowTs;
        if (effectiveNow >= end) return 0;

        uint256 remaining = end - effectiveNow;
        // remaining <= durationSeconds, so the quotient is <= startBps and fits uint16.
        // forge-lint: disable-next-line(unsafe-typecast)
        return uint16((uint256(startBps) * remaining) / uint256(durationSeconds));
    }

    /// @notice Splits `gross` ETH into the tax and the net ETH passed on to the v1 fee split.
    /// @dev `tax = floor(gross * taxBps_ / 10_000)`, rounding in the buyer's favor.
    function split(uint256 gross, uint16 taxBps_) internal pure returns (uint256 tax, uint256 net) {
        _checkBps(taxBps_);
        tax = Math.mulDiv(gross, taxBps_, BPS_DENOMINATOR);
        net = gross - tax;
    }

    /// @notice Smallest `gross` with `split(gross, taxBps_).net >= net`; used on a final buy so
    /// the required gross is rounded up and the protocol is never short.
    /// @dev Same closed form as `CurveMath.exactGrossForNet`:
    /// `gross = floor((net - 1) * 10_000 / (10_000 - taxBps_)) + 1` for `net > 0`.
    function grossForNet(uint256 net, uint16 taxBps_) internal pure returns (uint256 gross) {
        _checkBps(taxBps_);
        if (net == 0) return 0;

        uint256 grossMinusOne =
            Math.mulDiv(net - 1, BPS_DENOMINATOR, BPS_DENOMINATOR - uint256(taxBps_));
        (bool success, uint256 result) = Math.tryAdd(grossMinusOne, 1);
        if (!success) revert ILaunchErrors.ArithmeticOverflow();
        return result;
    }

    function _checkBps(uint16 taxBps_) private pure {
        if (taxBps_ >= BPS_DENOMINATOR) revert AntiSnipeBpsTooHigh(taxBps_);
    }
}
