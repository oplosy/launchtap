// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { ILaunchErrors } from "../../interfaces/ILaunchErrors.sol";
import { LaunchTypes } from "../../types/LaunchTypes.sol";
import { LaunchTypesV2 } from "../types/LaunchTypesV2.sol";
import { IFeeRecipientSource } from "./IFeeRecipientSource.sol";
import { ILaunchEventsV2 } from "./ILaunchEventsV2.sol";

interface IBondingCurveV2 is IFeeRecipientSource, ILaunchErrors, ILaunchEventsV2 {
    error InvalidAntiSnipeProfile(uint16 startBps, uint32 durationSeconds);
    error InvalidLpCreatorShareBps(uint16 lpCreatorShareBps);
    error UnauthorizedFeeRecipient(address caller, address feeRecipient);
    error UnauthorizedPendingFeeRecipient(address caller, address pendingFeeRecipient);
    error UnexpectedEthSender(address sender);
    error GraduationOverspent(uint256 ethSpent, uint256 tokensSpent);

    // forge-lint: disable-next-line(mixed-case-function)
    function ENGINE_VERSION() external view returns (uint16);
    function initialize(LaunchTypesV2.CurveInitializationV2 calldata initialization) external;

    function buy(
        address tokenRecipient,
        address refundRecipient,
        uint256 minTokensOut,
        uint256 deadline
    ) external payable returns (uint256 tokensOut, uint256 ethGrossUsed);

    /// @notice Factory-only developer buy inside the launch transaction; exempt from the
    /// anti-snipe tax (spec §4).
    function buyFor(
        address trader,
        address tokenRecipient,
        address refundRecipient,
        uint256 minTokensOut,
        uint256 deadline
    ) external payable returns (uint256 tokensOut, uint256 ethGrossUsed);

    function sell(uint256 tokensIn, address ethRecipient, uint256 minEthOut, uint256 deadline)
        external
        returns (uint256 ethOut);

    function quoteBuy(uint256 ethGross)
        external
        view
        returns (
            uint256 ethGrossUsed,
            uint256 tokensOut,
            uint256 protocolFee,
            uint256 creatorFee,
            uint256 snipeTax,
            uint256 refund,
            bool graduates
        );

    function quoteSell(uint256 tokensIn)
        external
        view
        returns (uint256 ethOut, uint256 ethGross, uint256 protocolFee, uint256 creatorFee);

    /// @notice Anti-snipe tax rate that applies to a public buy in the current block.
    function currentTaxBps() external view returns (uint16);

    function proposeFeeRecipient(address next) external;
    function cancelFeeRecipientProposal() external;
    function acceptFeeRecipient() external;
    function pendingFeeRecipient() external view returns (address);

    /// @notice Pays the caller's credited creator fees.
    function claimCreatorFees() external returns (uint256 amount);
    function claimCreatorFeesTo(address recipient) external returns (uint256 amount);
    function claimProtocolFees() external returns (uint256 amount);
    function claimSnipeTax() external returns (uint256 amount);
    function claimRefund() external returns (uint256 amount);
    function claimRefundTo(address recipient) external returns (uint256 amount);

    function creatorFeesOwed(address account) external view returns (uint256);
    function unclaimedCreatorFees() external view returns (uint256);
    function unclaimedProtocolFees() external view returns (uint256);
    function unclaimedSnipeTax() external view returns (uint256);
    function pendingRefund(address account) external view returns (uint256);

    function phase() external view returns (LaunchTypes.Phase);
    function token() external view returns (address);
    function creator() external view returns (address);
    function implementation() external view returns (address);
    function poolManager() external view returns (address);
    function positionManager() external view returns (address);
    function hook() external view returns (address);
    function locker() external view returns (address);
    function launchTime() external view returns (uint64);
    function antiSnipeProfile() external view returns (LaunchTypesV2.AntiSnipeProfile memory);
    function virtualEthReserve() external view returns (uint256);
    function virtualTokenReserve() external view returns (uint256);
    function realCurveEth() external view returns (uint256);
    function tokensSold() external view returns (uint256);
}
