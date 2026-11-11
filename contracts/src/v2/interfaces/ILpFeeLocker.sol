// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IERC721Receiver } from "@openzeppelin/contracts/token/ERC721/IERC721Receiver.sol";

/// @notice Holds each graduated v2 token's full-range position forever and splits its LP
/// fees into pull-claim balances (spec §3.5, §5.2).
interface ILpFeeLocker is IERC721Receiver {
    event LpFeesCollected(address indexed token, uint256 ethAmount, uint256 tokenAmount);
    event LpFeesClaimed(
        address indexed token, address indexed recipient, uint256 ethAmount, uint256 tokenAmount
    );

    error UnauthorizedRegistrar(address caller);
    error PositionAlreadyRegistered(address token);
    error PositionNotOwned(uint256 tokenId);
    error PositionTokenMismatch(address expected, address actual);
    error UnknownToken(address token);
    error NotPositionManager(address caller);
    error NotPoolManager(address caller);
    error EthTransferFailed(address recipient);

    /// @notice Records `tokenId` as `token`'s locked position. Only `token`'s curve, once.
    function registerPosition(address token, uint256 tokenId) external;

    /// @notice Collects the position's accrued fees and credits the split. Permissionless.
    function collect(address token) external returns (uint256 ethAmount, uint256 tokenAmount);

    /// @notice Collects, then pays the caller's ETH and token balances for `token`.
    function claim(address token) external returns (uint256 ethAmount, uint256 tokenAmount);

    function positionOf(address token) external view returns (uint256 tokenId);

    /// @notice Credited, unclaimed balances (excludes fees not yet collected).
    function owed(address token, address account)
        external
        view
        returns (uint256 ethAmount, uint256 tokenAmount);
}
