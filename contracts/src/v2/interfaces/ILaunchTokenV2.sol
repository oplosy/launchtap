// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { ILaunchErrors } from "../../interfaces/ILaunchErrors.sol";

interface ILaunchTokenV2 is IERC20, ILaunchErrors {
    /// @notice Bonding curve that received the whole supply and graduates the token.
    function curve() external view returns (address);

    /// @notice Uniswap v4 PoolManager that tokens may not enter before graduation.
    function poolManager() external view returns (address);

    function graduated() external view returns (bool);

    /// @notice Ends the curve-phase transfer guard permanently. Curve only, once.
    function markGraduated() external;
}
