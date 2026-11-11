// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { ERC20 } from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import { ILaunchTokenV2 } from "./interfaces/ILaunchTokenV2.sol";

/// @notice Fixed-supply v2 launch token (spec §3.2). No owner, mint, tax, blacklist or pause.
contract LaunchTokenV2 is ERC20, ILaunchTokenV2 {
    bytes32 private constant FIELD_CURVE = "curve";
    bytes32 private constant FIELD_POOL_MANAGER = "poolManager";

    address private immutable CURVE;
    address private immutable POOL_MANAGER;
    bool private _graduated;

    /// @param curve_ Bonding curve that receives the whole supply and graduates the token.
    /// @param poolManager_ Uniswap v4 PoolManager guarded until graduation.
    constructor(
        string memory name_,
        string memory symbol_,
        address curve_,
        address poolManager_,
        uint256 totalSupply_
    ) ERC20(name_, symbol_) {
        if (curve_ == address(0)) revert ZeroAddress(FIELD_CURVE);
        if (poolManager_ == address(0)) revert ZeroAddress(FIELD_POOL_MANAGER);

        CURVE = curve_;
        POOL_MANAGER = poolManager_;
        _mint(curve_, totalSupply_);
    }

    /// @inheritdoc ILaunchTokenV2
    function curve() external view returns (address) {
        return CURVE;
    }

    /// @inheritdoc ILaunchTokenV2
    function poolManager() external view returns (address) {
        return POOL_MANAGER;
    }

    /// @inheritdoc ILaunchTokenV2
    function graduated() external view returns (bool) {
        return _graduated;
    }

    /// @inheritdoc ILaunchTokenV2
    function markGraduated() external {
        if (msg.sender != CURVE) revert UnauthorizedCurve(msg.sender);
        if (_graduated) revert AlreadyGraduated();

        // The curve emits Graduated in the same transaction.
        // forge-lint: disable-next-line(missing-events-access-control)
        _graduated = true;
    }

    /// @dev Mirrors v1 `LaunchToken._update` with the v4 PoolManager in place of the pair:
    /// until graduation only the curve may operate transfers that touch the curve's inventory
    /// or send tokens into the PoolManager, so nobody can supply token-side liquidity before
    /// graduation. The constructor mint is exempt.
    function _update(address from, address to, uint256 value) internal override {
        address operator = _msgSender();
        if (
            !_graduated && from != address(0) && operator != CURVE
                && (from == CURVE || to == CURVE || to == POOL_MANAGER)
        ) {
            revert TransferRestricted(operator, from, to);
        }

        super._update(from, to, value);
    }
}
