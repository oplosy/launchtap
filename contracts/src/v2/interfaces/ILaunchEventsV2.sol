// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

/// @notice v2 event contract consumed by the launchpad indexer (spec §7). v1 events keep their
/// v1 definitions; events shared unchanged with v1 are repeated here so v2 contracts expose
/// one complete event surface.
interface ILaunchEventsV2 {
    /// @dev Every v1 field in v1 order except `lpPair` and `weth`, then the v2 additions.
    event TokenLaunched(
        address indexed token,
        address indexed curve,
        address indexed creator,
        address protocolTreasury,
        uint16 engineVersion,
        string name,
        string symbol,
        uint256 totalSupply,
        uint256 virtualEth,
        uint256 virtualToken,
        uint256 curveTokens,
        uint256 lpTokens,
        uint256 graduationEth,
        uint256 launchFeePaid,
        uint16 tradeFeeBps,
        uint16 protocolShareBps,
        address hook,
        address locker,
        uint16 antiSnipeStartBps,
        uint32 antiSnipeDurationSeconds,
        uint16 lpCreatorShareBps
    );

    /// @param ethGross ETH consumed, `snipeTax + protocolFee + creatorFee + ETH to the curve`.
    /// @param snipeTax Anti-snipe tax inside `ethGross`; always zero for sells.
    event Trade(
        address indexed token,
        address indexed trader,
        bool isBuy,
        uint256 ethGross,
        uint256 ethRefund,
        uint256 tokenAmount,
        uint256 protocolFee,
        uint256 creatorFee,
        uint256 newEthReserve,
        uint256 newTokenReserve,
        uint256 snipeTax
    );

    event Graduated(
        address indexed token,
        bytes32 indexed poolId,
        uint256 positionTokenId,
        uint256 ethToPool,
        uint256 tokensToPool,
        uint128 liquidity,
        uint160 sqrtPriceX96,
        uint256 ethRemainder,
        uint256 tokenRemainder
    );

    event SnipeTaxClaimed(address indexed token, address indexed treasury, uint256 amount);
    /// @dev A cancelled proposal is reported with `next == address(0)`.
    event FeeRecipientProposed(
        address indexed token, address indexed current, address indexed next
    );
    event FeeRecipientAccepted(
        address indexed token, address indexed previous, address indexed next
    );

    /// @param account Account whose credited creator fees were paid out.
    event CreatorFeesClaimed(address indexed token, address indexed account, uint256 amount);
    event ProtocolFeesClaimed(address indexed token, address indexed treasury, uint256 amount);
    event RefundCredited(address indexed token, address indexed account, uint256 amount);
    event RefundClaimed(address indexed token, address indexed account, uint256 amount);
}
