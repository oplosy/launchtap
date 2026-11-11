// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

/// @notice Token-to-curve registry consulted by the graduation guard hook. Implemented by
/// `LaunchFactoryV2`.
interface ICurveRegistry {
    /// @return The curve launched with `token`, or the zero address for unknown tokens.
    function curveOfToken(address token) external view returns (address);
}
