// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { SafeERC20 } from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import { IERC721 } from "@openzeppelin/contracts/token/ERC721/IERC721.sol";
import { IERC721Receiver } from "@openzeppelin/contracts/token/ERC721/IERC721Receiver.sol";
import { Math } from "@openzeppelin/contracts/utils/math/Math.sol";
import {
    ReentrancyGuardTransient
} from "@openzeppelin/contracts/utils/ReentrancyGuardTransient.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { Actions } from "@uniswap/v4-periphery/src/libraries/Actions.sol";
import { ICurveRegistry } from "./interfaces/ICurveRegistry.sol";
import { IFeeRecipientSource } from "./interfaces/IFeeRecipientSource.sol";
import { ILpFeeLocker } from "./interfaces/ILpFeeLocker.sol";

/// @notice Holds each graduated v2 token's full-range position NFT forever and splits its LP
/// fees between the launch's creator fee recipient and the protocol treasury (spec §3.5).
/// @dev The only PositionManager interaction is fee collection: a zero-liquidity
/// `DECREASE_LIQUIDITY` followed by `TAKE_PAIR` to this contract. No code path decreases
/// nonzero liquidity, burns, transfers or approves a position. No owner, no upgrade.
contract LpFeeLocker is ILpFeeLocker, ReentrancyGuardTransient {
    using SafeERC20 for IERC20;

    uint256 private constant BPS_DENOMINATOR = 10_000;

    IPositionManager public immutable POSITION_MANAGER;
    IPoolManager public immutable POOL_MANAGER;
    ICurveRegistry public immutable FACTORY;

    mapping(address token => uint256 tokenId) private _positionOf;
    mapping(address token => mapping(address account => uint256 amount)) private _ethOwed;
    mapping(address token => mapping(address account => uint256 amount)) private _tokenOwed;

    constructor(IPositionManager positionManager_, ICurveRegistry factory_) {
        POSITION_MANAGER = positionManager_;
        POOL_MANAGER = positionManager_.poolManager();
        FACTORY = factory_;
    }

    /// @notice Accepts ETH only from the PoolManager (fee `TAKE_PAIR`), so stray donations
    /// cannot get stuck here.
    receive() external payable {
        if (msg.sender != address(POOL_MANAGER)) revert NotPoolManager(msg.sender);
    }

    /// @inheritdoc IERC721Receiver
    function onERC721Received(address, address, uint256, bytes calldata)
        external
        view
        returns (bytes4)
    {
        if (msg.sender != address(POSITION_MANAGER)) revert NotPositionManager(msg.sender);
        return IERC721Receiver.onERC721Received.selector;
    }

    /// @inheritdoc ILpFeeLocker
    function registerPosition(address token, uint256 tokenId) external {
        address curve = FACTORY.curveOfToken(token);
        if (curve == address(0) || msg.sender != curve) revert UnauthorizedRegistrar(msg.sender);
        if (_positionOf[token] != 0) revert PositionAlreadyRegistered(token);
        if (IERC721(address(POSITION_MANAGER)).ownerOf(tokenId) != address(this)) {
            revert PositionNotOwned(tokenId);
        }

        // Only the pool key is needed; the packed position info is not.
        // forge-lint: disable-next-line(unused-return)
        (PoolKey memory key,) = POSITION_MANAGER.getPoolAndPositionInfo(tokenId);
        address positionToken = Currency.unwrap(key.currency1);
        if (!key.currency0.isAddressZero() || positionToken != token) {
            revert PositionTokenMismatch(token, positionToken);
        }

        // The curve emits Graduated with the position id in the same transaction.
        // forge-lint: disable-next-line(missing-events-access-control)
        _positionOf[token] = tokenId;
    }

    /// @inheritdoc ILpFeeLocker
    function collect(address token)
        external
        nonReentrant
        returns (uint256 ethAmount, uint256 tokenAmount)
    {
        return _collect(token);
    }

    /// @inheritdoc ILpFeeLocker
    function claim(address token)
        external
        nonReentrant
        returns (uint256 ethAmount, uint256 tokenAmount)
    {
        _collect(token);

        ethAmount = _ethOwed[token][msg.sender];
        tokenAmount = _tokenOwed[token][msg.sender];
        if (ethAmount == 0 && tokenAmount == 0) return (0, 0);

        _ethOwed[token][msg.sender] = 0;
        _tokenOwed[token][msg.sender] = 0;
        // The prior external calls are the nonReentrant collect; payouts follow the event.
        // forge-lint: disable-next-line(reentrancy-events)
        emit LpFeesClaimed(token, msg.sender, ethAmount, tokenAmount);

        if (tokenAmount != 0) IERC20(token).safeTransfer(msg.sender, tokenAmount);
        if (ethAmount != 0) {
            (bool success,) = payable(msg.sender).call{ value: ethAmount }("");
            if (!success) revert EthTransferFailed(msg.sender);
        }
    }

    /// @inheritdoc ILpFeeLocker
    function positionOf(address token) external view returns (uint256) {
        return _positionOf[token];
    }

    /// @inheritdoc ILpFeeLocker
    function owed(address token, address account)
        external
        view
        returns (uint256 ethAmount, uint256 tokenAmount)
    {
        return (_ethOwed[token][account], _tokenOwed[token][account]);
    }

    /// @dev Collected amounts are measured as this contract's balance change across the
    /// PositionManager call, so balances held for other tokens are never re-split. The
    /// creator share is `floor(amount * lpCreatorShareBps / 10_000)`; the protocol gets the
    /// rest, including any odd wei.
    function _collect(address token) private returns (uint256 ethAmount, uint256 tokenAmount) {
        uint256 tokenId = _positionOf[token];
        if (tokenId == 0) revert UnknownToken(token);

        uint256 ethBefore = address(this).balance;
        uint256 tokenBefore = IERC20(token).balanceOf(address(this));

        POSITION_MANAGER.modifyLiquidities(_collectData(tokenId, token), block.timestamp);

        ethAmount = address(this).balance - ethBefore;
        tokenAmount = IERC20(token).balanceOf(address(this)) - tokenBefore;
        if (ethAmount == 0 && tokenAmount == 0) return (0, 0);

        _credit(token, ethAmount, tokenAmount);
        // Every caller of _collect is nonReentrant; the event reports the measured amounts.
        // forge-lint: disable-next-line(reentrancy-events)
        emit LpFeesCollected(token, ethAmount, tokenAmount);
    }

    /// @dev `DECREASE_LIQUIDITY(tokenId, 0, 0, 0, "")` then `TAKE_PAIR(ETH, token, this)`.
    function _collectData(uint256 tokenId, address token) private view returns (bytes memory) {
        // Action ids are compile-time constants below 0x20; the narrowing cannot truncate.
        // forge-lint: disable-start(unsafe-typecast)
        bytes memory actions =
            abi.encodePacked(uint8(Actions.DECREASE_LIQUIDITY), uint8(Actions.TAKE_PAIR));
        // forge-lint: disable-end(unsafe-typecast)
        bytes[] memory params = new bytes[](2);
        params[0] = abi.encode(tokenId, uint256(0), uint128(0), uint128(0), bytes(""));
        params[1] = abi.encode(Currency.wrap(address(0)), Currency.wrap(token), address(this));
        return abi.encode(actions, params);
    }

    function _credit(address token, uint256 ethAmount, uint256 tokenAmount) private {
        IFeeRecipientSource source = IFeeRecipientSource(FACTORY.curveOfToken(token));
        address recipient = source.feeRecipient();
        address treasury = source.protocolTreasury();
        uint16 creatorShareBps = source.lpCreatorShareBps();

        uint256 creatorEth = Math.mulDiv(ethAmount, creatorShareBps, BPS_DENOMINATOR);
        uint256 creatorTokens = Math.mulDiv(tokenAmount, creatorShareBps, BPS_DENOMINATOR);
        _ethOwed[token][recipient] += creatorEth;
        _tokenOwed[token][recipient] += creatorTokens;
        _ethOwed[token][treasury] += ethAmount - creatorEth;
        _tokenOwed[token][treasury] += tokenAmount - creatorTokens;
    }
}
