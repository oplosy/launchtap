// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { ReentrancyGuard } from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import { LaunchTypes } from "../../types/LaunchTypes.sol";

/// @dev Clone storage for `BondingCurveV2`. Storage-based reentrancy protection mirrors v1.
abstract contract BondingCurveV2Storage is ReentrancyGuard {
    bool internal _initialized;
    LaunchTypes.Phase internal _phase;
    uint16 internal _tradeFeeBps;
    uint16 internal _protocolShareBps;
    uint16 internal _antiSnipeStartBps;
    uint32 internal _antiSnipeDurationSeconds;
    uint64 internal _launchTime;
    uint16 internal _lpCreatorShareBps;

    address internal _factory;
    address internal _implementation;
    address internal _token;
    address internal _creator;
    address internal _protocolTreasury;
    address internal _poolManager;
    address internal _positionManager;
    address internal _permit2;
    address internal _hook;
    address internal _locker;
    address internal _feeRecipient;
    address internal _pendingFeeRecipient;

    uint256 internal _totalSupply;
    uint256 internal _curveTokens;
    uint256 internal _lpTokens;
    uint256 internal _graduationEth;
    uint256 internal _initialVirtualEth;
    uint256 internal _initialVirtualToken;
    uint256 internal _invariant;
    uint256 internal _virtualEthReserve;
    uint256 internal _virtualTokenReserve;

    uint256 internal _totalCreatorFees;
    uint256 internal _unclaimedProtocolFees;
    uint256 internal _unclaimedSnipeTax;
    uint256 internal _totalPendingRefunds;
    mapping(address account => uint256 amount) internal _creatorFeesOwed;
    mapping(address account => uint256 amount) internal _pendingRefunds;
}
