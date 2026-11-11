// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

/// @notice Per-launch payout configuration read by `LpFeeLocker`. Implemented by
/// `BondingCurveV2`.
interface IFeeRecipientSource {
    /// @notice Current creator fee recipient (spec §5.3).
    function feeRecipient() external view returns (address);

    /// @notice Protocol treasury snapshotted at launch.
    function protocolTreasury() external view returns (address);

    /// @notice Creator share of LP fees in bps, snapshotted at launch (spec §5.2).
    function lpCreatorShareBps() external view returns (uint16);
}
