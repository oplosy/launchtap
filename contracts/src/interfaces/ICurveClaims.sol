// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

interface ICurveClaims {
    function claimCreatorFees() external returns (uint256 amount);
    function claimCreatorFeesTo(address recipient) external returns (uint256 amount);
    function claimProtocolFees() external returns (uint256 amount);
    function claimRefund() external returns (uint256 amount);
    function claimRefundTo(address recipient) external returns (uint256 amount);
    function unclaimedCreatorFees() external view returns (uint256);
    function unclaimedProtocolFees() external view returns (uint256);
    function pendingRefund(address account) external view returns (uint256);
}
