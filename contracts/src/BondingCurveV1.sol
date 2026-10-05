// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { BondingCurveV1Storage } from "./storage/BondingCurveV1Storage.sol";
import { CurveMath } from "./libraries/CurveMath.sol";
import { ILaunchErrors } from "./interfaces/ILaunchErrors.sol";
import { ILaunchEvents } from "./interfaces/ILaunchEvents.sol";
import { ILaunchPause } from "./interfaces/ILaunchPause.sol";
import { ILaunchToken } from "./interfaces/ILaunchToken.sol";
import { IUniswapV2Factory } from "./interfaces/external/IUniswapV2Factory.sol";
import { IUniswapV2Pair } from "./interfaces/external/IUniswapV2Pair.sol";
import { IWETH } from "./interfaces/external/IWETH.sol";
import { LaunchTypes } from "./types/LaunchTypes.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";

contract BondingCurveV1 is BondingCurveV1Storage, ILaunchErrors, ILaunchEvents {
    uint256 private constant IMMEDIATE_ETH_SEND_GAS = 50_000;
    address private constant LP_BURN_ADDRESS = 0x000000000000000000000000000000000000dEaD;
    bytes32 private constant FIELD_FACTORY = "factory";
    bytes32 private constant FIELD_IMPLEMENTATION = "implementation";
    bytes32 private constant FIELD_TOKEN = "token";
    bytes32 private constant FIELD_CREATOR = "creator";
    bytes32 private constant FIELD_PROTOCOL_TREASURY = "protocolTreasury";
    bytes32 private constant FIELD_WETH = "weth";
    bytes32 private constant FIELD_UNISWAP_FACTORY = "uniswapFactory";
    bytes32 private constant FIELD_LP_PAIR = "lpPair";
    bytes32 private constant FIELD_TRADER = "trader";
    bytes32 private constant FIELD_TOKEN_RECIPIENT = "tokenRecipient";
    bytes32 private constant FIELD_REFUND_RECIPIENT = "refundRecipient";
    bytes32 private constant FIELD_ETH_RECIPIENT = "ethRecipient";
    bytes32 private constant FIELD_CLAIM_RECIPIENT = "claimRecipient";

    constructor() {
        _implementation = address(this);
        _initialized = true;
    }

    function initialize(LaunchTypes.CurveInitialization calldata initialization)
        external
        nonReentrant
    {
        if (_implementation == address(this)) {
            revert ImplementationInitializationDisabled();
        }
        if (_initialized) revert AlreadyInitialized();
        _initialized = true;

        _validateAddress(initialization.factory, FIELD_FACTORY);
        if (msg.sender != initialization.factory) revert UnauthorizedFactory(msg.sender);
        _validateAddress(initialization.implementation, FIELD_IMPLEMENTATION);
        if (initialization.implementation == address(this)) {
            revert ImplementationInitializationDisabled();
        }
        _validateAddress(initialization.token, FIELD_TOKEN);
        _validateAddress(initialization.creator, FIELD_CREATOR);
        _validateAddress(initialization.protocolTreasury, FIELD_PROTOCOL_TREASURY);
        _validateAddress(initialization.weth, FIELD_WETH);
        _validateAddress(initialization.uniswapFactory, FIELD_UNISWAP_FACTORY);
        _validateAddress(initialization.lpPair, FIELD_LP_PAIR);

        LaunchTypes.CurveParameters calldata parameters = initialization.parameters;
        CurveMath.validateParameters(parameters);
        _validatePair(initialization);

        uint256 invariant =
            CurveMath.checkedMul(parameters.initialVirtualEth, parameters.initialVirtualToken);

        _phase = LaunchTypes.Phase.Curve;
        _tradeFeeBps = parameters.tradeFeeBps;
        _protocolShareBps = parameters.protocolShareBps;
        _factory = initialization.factory;
        _implementation = initialization.implementation;
        _token = initialization.token;
        _creator = initialization.creator;
        _protocolTreasury = initialization.protocolTreasury;
        _weth = initialization.weth;
        _uniswapFactory = initialization.uniswapFactory;
        _lpPair = initialization.lpPair;
        _totalSupply = parameters.totalSupply;
        _curveTokens = parameters.curveTokens;
        _lpTokens = parameters.lpTokens;
        _graduationEth = parameters.graduationEth;
        _initialVirtualEth = parameters.initialVirtualEth;
        _initialVirtualToken = parameters.initialVirtualToken;
        _invariant = invariant;
        _virtualEthReserve = parameters.initialVirtualEth;
        _virtualTokenReserve = parameters.initialVirtualToken;
    }

    function buy(
        address tokenRecipient,
        address refundRecipient,
        uint256 minTokensOut,
        uint256 deadline
    ) external payable nonReentrant returns (uint256 tokensOut, uint256 ethGrossUsed) {
        return _buy(msg.sender, tokenRecipient, refundRecipient, minTokensOut, deadline);
    }

    function buyFor(
        address trader,
        address tokenRecipient,
        address refundRecipient,
        uint256 minTokensOut,
        uint256 deadline
    ) external payable nonReentrant returns (uint256 tokensOut, uint256 ethGrossUsed) {
        if (msg.sender != _factory) revert UnauthorizedFactory(msg.sender);
        _validateAddress(trader, FIELD_TRADER);
        return _buy(trader, tokenRecipient, refundRecipient, minTokensOut, deadline);
    }

    function sell(uint256 tokensIn, address ethRecipient, uint256 minEthOut, uint256 deadline)
        external
        nonReentrant
        returns (uint256 ethOut)
    {
        // Sells stay open while trading is paused so holders can always exit to the curve.
        _requireCurvePhase();
        _requireDeadline(deadline);
        _validateAddress(ethRecipient, FIELD_ETH_RECIPIENT);

        CurveMath.SellQuote memory quote = _quoteSell(tokensIn);
        if (quote.ethOut < minEthOut) revert SlippageExceeded(minEthOut, quote.ethOut);

        _virtualEthReserve = quote.newVirtualEth;
        _virtualTokenReserve = quote.newVirtualToken;
        _unclaimedProtocolFees = CurveMath.checkedAdd(_unclaimedProtocolFees, quote.protocolFee);
        _unclaimedCreatorFees = CurveMath.checkedAdd(_unclaimedCreatorFees, quote.creatorFee);

        emit Trade(
            _token,
            msg.sender,
            false,
            quote.ethGross,
            0,
            tokensIn,
            quote.protocolFee,
            quote.creatorFee,
            quote.newVirtualEth,
            quote.newVirtualToken
        );

        bool transferred = ILaunchToken(_token).transferFrom(msg.sender, address(this), tokensIn);
        if (!transferred) revert TokenTransferFailed(_token, address(this), tokensIn);

        _sendOrCredit(ethRecipient, quote.ethOut);
        _assertAccountingInvariant();
        return quote.ethOut;
    }

    function claimCreatorFees() external nonReentrant returns (uint256 amount) {
        if (msg.sender != _creator) revert UnauthorizedCreatorClaim(msg.sender, _creator);
        amount = _takeCreatorFees();
        _sendClaim(_creator, amount);
        _assertAccountingInvariant();
    }

    function claimCreatorFeesTo(address recipient) external nonReentrant returns (uint256 amount) {
        if (msg.sender != _creator) revert UnauthorizedCreatorClaim(msg.sender, _creator);
        _validateAddress(recipient, FIELD_CLAIM_RECIPIENT);
        amount = _takeCreatorFees();
        _sendClaim(recipient, amount);
        _assertAccountingInvariant();
    }

    function claimProtocolFees() external nonReentrant returns (uint256 amount) {
        if (msg.sender != _protocolTreasury) {
            revert UnauthorizedProtocolClaim(msg.sender, _protocolTreasury);
        }
        amount = _unclaimedProtocolFees;
        if (amount == 0) revert NothingToClaim();

        _unclaimedProtocolFees = 0;
        emit ProtocolFeesClaimed(_token, _protocolTreasury, amount);
        _sendClaim(_protocolTreasury, amount);
        _assertAccountingInvariant();
    }

    function claimRefund() external nonReentrant returns (uint256 amount) {
        amount = _takeRefund(msg.sender);
        _sendClaim(msg.sender, amount);
        _assertAccountingInvariant();
    }

    /// @notice Pays the caller's credited refund to another address; the event names the caller.
    function claimRefundTo(address recipient) external nonReentrant returns (uint256 amount) {
        _validateAddress(recipient, FIELD_CLAIM_RECIPIENT);
        amount = _takeRefund(msg.sender);
        _sendClaim(recipient, amount);
        _assertAccountingInvariant();
    }

    function quoteBuy(uint256 ethGross)
        external
        view
        returns (
            uint256 ethGrossUsed,
            uint256 tokensOut,
            uint256 protocolFee,
            uint256 creatorFee,
            uint256 refund,
            bool graduates
        )
    {
        _requireCurvePhase();
        CurveMath.BuyQuote memory quote = _quoteBuy(ethGross);
        return (
            quote.ethGrossUsed,
            quote.tokensOut,
            quote.protocolFee,
            quote.creatorFee,
            quote.refund,
            quote.graduates
        );
    }

    function quoteSell(uint256 tokensIn)
        external
        view
        returns (uint256 ethOut, uint256 ethGross, uint256 protocolFee, uint256 creatorFee)
    {
        _requireCurvePhase();
        CurveMath.SellQuote memory quote = _quoteSell(tokensIn);
        return (quote.ethOut, quote.ethGross, quote.protocolFee, quote.creatorFee);
    }

    function unclaimedCreatorFees() external view returns (uint256) {
        return _unclaimedCreatorFees;
    }

    function unclaimedProtocolFees() external view returns (uint256) {
        return _unclaimedProtocolFees;
    }

    function pendingRefund(address account) external view returns (uint256) {
        return _pendingRefunds[account];
    }

    function phase() external view returns (LaunchTypes.Phase) {
        return _phase;
    }

    function token() external view returns (address) {
        return _token;
    }

    function creator() external view returns (address) {
        return _creator;
    }

    function protocolTreasury() external view returns (address) {
        return _protocolTreasury;
    }

    function implementation() external view returns (address) {
        return _implementation;
    }

    function weth() external view returns (address) {
        return _weth;
    }

    function uniswapFactory() external view returns (address) {
        return _uniswapFactory;
    }

    function lpPair() external view returns (address) {
        return _lpPair;
    }

    function virtualEthReserve() external view returns (uint256) {
        return _virtualEthReserve;
    }

    function virtualTokenReserve() external view returns (uint256) {
        return _virtualTokenReserve;
    }

    function realCurveEth() external view returns (uint256) {
        return CurveMath.realCurveEth(_virtualEthReserve, _initialVirtualEth);
    }

    function tokensSold() external view returns (uint256) {
        return CurveMath.tokensSold(_initialVirtualToken, _virtualTokenReserve);
    }

    function _takeCreatorFees() private returns (uint256 amount) {
        amount = _unclaimedCreatorFees;
        if (amount == 0) revert NothingToClaim();

        _unclaimedCreatorFees = 0;
        emit CreatorFeesClaimed(_token, _creator, amount);
    }

    function _takeRefund(address account) private returns (uint256 amount) {
        amount = _pendingRefunds[account];
        if (amount == 0) revert NothingToClaim();

        _pendingRefunds[account] = 0;
        _totalPendingRefunds -= amount;
        emit RefundClaimed(_token, account, amount);
    }

    function _buy(
        address trader,
        address tokenRecipient,
        address refundRecipient,
        uint256 minTokensOut,
        uint256 deadline
    ) private returns (uint256 tokensOut, uint256 ethGrossUsed) {
        _requireTradingAllowed(deadline);
        _validateAddress(tokenRecipient, FIELD_TOKEN_RECIPIENT);
        // Curve-operated transfers bypass the token's pair restriction, so tokens sent to the
        // pair here could never be skimmed and would block graduation permanently.
        if (tokenRecipient == _lpPair || tokenRecipient == address(this)) {
            revert InvalidRecipient(FIELD_TOKEN_RECIPIENT, tokenRecipient);
        }
        _validateAddress(refundRecipient, FIELD_REFUND_RECIPIENT);

        CurveMath.BuyQuote memory quote = _quoteBuy(msg.value);
        if (quote.tokensOut < minTokensOut) {
            revert SlippageExceeded(minTokensOut, quote.tokensOut);
        }

        _virtualEthReserve = quote.newVirtualEth;
        _virtualTokenReserve = quote.newVirtualToken;
        _unclaimedProtocolFees = CurveMath.checkedAdd(_unclaimedProtocolFees, quote.protocolFee);
        _unclaimedCreatorFees = CurveMath.checkedAdd(_unclaimedCreatorFees, quote.creatorFee);

        emit Trade(
            _token,
            trader,
            true,
            quote.ethGrossUsed,
            quote.refund,
            quote.tokensOut,
            quote.protocolFee,
            quote.creatorFee,
            quote.newVirtualEth,
            quote.newVirtualToken
        );

        // The contract is non-reentrant and any later graduation failure reverts this transfer.
        // forge-lint: disable-next-line(reentrancy-no-eth)
        bool transferred = ILaunchToken(_token).transfer(tokenRecipient, quote.tokensOut);
        if (!transferred) revert TokenTransferFailed(_token, tokenRecipient, quote.tokensOut);

        if (quote.graduates) _graduate();
        _sendOrCredit(refundRecipient, quote.refund);
        _assertAccountingInvariant();
        return (quote.tokensOut, quote.ethGrossUsed);
    }

    function _quoteBuy(uint256 ethGross) private view returns (CurveMath.BuyQuote memory) {
        return CurveMath.quoteBuy(
            _invariant,
            _virtualEthReserve,
            _virtualTokenReserve,
            _initialVirtualToken - _curveTokens,
            ethGross,
            _tradeFeeBps,
            _protocolShareBps
        );
    }

    function _quoteSell(uint256 tokensIn) private view returns (CurveMath.SellQuote memory) {
        return CurveMath.quoteSell(
            _invariant,
            _virtualEthReserve,
            _virtualTokenReserve,
            CurveMath.tokensSold(_initialVirtualToken, _virtualTokenReserve),
            tokensIn,
            _tradeFeeBps,
            _protocolShareBps
        );
    }

    function _sendOrCredit(address recipient, uint256 amount) private {
        if (amount == 0) return;

        // A gas cap prevents a hostile recipient from consuming the caller's remaining gas.
        bool success;
        assembly ("memory-safe") {
            success := call(IMMEDIATE_ETH_SEND_GAS, recipient, amount, 0, 0, 0, 0)
        }
        if (success) return;

        _pendingRefunds[recipient] = CurveMath.checkedAdd(_pendingRefunds[recipient], amount);
        _totalPendingRefunds = CurveMath.checkedAdd(_totalPendingRefunds, amount);
        // The event must follow the failed probe because successful sends do not create credit.
        // forge-lint: disable-next-line(reentrancy-events)
        emit RefundCredited(_token, recipient, amount);
    }

    function _sendClaim(address recipient, uint256 amount) private {
        bool success;
        assembly ("memory-safe") {
            success := call(gas(), recipient, amount, 0, 0, 0, 0)
        }
        if (!success) revert EthTransferFailed(recipient, amount);
    }

    function _graduate() private {
        _validateGraduationPair();

        _phase = LaunchTypes.Phase.Graduated;
        ILaunchToken(_token).markGraduated();

        // WETH is a non-user-controlled launch snapshot validated again in the fork gate.
        // forge-lint: disable-next-line(arbitrary-send-eth)
        IWETH(_weth).deposit{ value: _graduationEth }();

        bool tokenTransferred = ILaunchToken(_token).transfer(_lpPair, _lpTokens);
        if (!tokenTransferred) revert TokenTransferFailed(_token, _lpPair, _lpTokens);

        bool wethTransferred = IWETH(_weth).transfer(_lpPair, _graduationEth);
        if (!wethTransferred) revert WethTransferFailed(_lpPair, _graduationEth);

        uint256 liquidity = IUniswapV2Pair(_lpPair).mint(LP_BURN_ADDRESS);
        if (liquidity == 0) revert PairLiquidityZero();

        // The returned liquidity is only known after mint; nonReentrant prevents event reordering.
        // forge-lint: disable-next-line(reentrancy-events)
        emit Graduated(_token, _lpPair, _graduationEth, _lpTokens, liquidity);
    }

    function _validateGraduationPair() private view {
        address tokenPair = ILaunchToken(_token).lpPair();
        if (tokenPair != _lpPair) revert PairNotCanonical(_lpPair, tokenPair);

        address expectedPair = IUniswapV2Factory(_uniswapFactory).getPair(_token, _weth);
        if (expectedPair != _lpPair) revert PairNotCanonical(expectedPair, _lpPair);

        IUniswapV2Pair pair = IUniswapV2Pair(_lpPair);
        address actualFactory = pair.factory();
        if (actualFactory != _uniswapFactory) {
            revert PairFactoryMismatch(_uniswapFactory, actualFactory);
        }

        address token0 = pair.token0();
        address token1 = pair.token1();
        bool tokenIs0 = token0 == _token && token1 == _weth;
        bool tokenIs1 = token0 == _weth && token1 == _token;
        if (!tokenIs0 && !tokenIs1) revert PairTokensMismatch(token0, token1);

        uint256 supply = pair.totalSupply();
        if (supply != 0) revert PairSupplyNotZero(supply);

        // The timestamp is irrelevant to launched-token reserve validation.
        // forge-lint: disable-next-line(unused-return)
        (uint112 reserve0, uint112 reserve1,) = pair.getReserves();
        uint256 tokenReserve = tokenIs0 ? uint256(reserve0) : uint256(reserve1);
        if (tokenReserve != 0) revert PairTokenReserveNotZero(tokenReserve);

        uint256 tokenBalance = IERC20(_token).balanceOf(_lpPair);
        if (tokenBalance != 0) revert PairTokenBalanceNotZero(tokenBalance);
    }

    function _assertAccountingInvariant() private view {
        uint256 requiredBalance = _phase == LaunchTypes.Phase.Curve
            ? CurveMath.realCurveEth(_virtualEthReserve, _initialVirtualEth)
            : 0;
        requiredBalance = CurveMath.checkedAdd(requiredBalance, _unclaimedCreatorFees);
        requiredBalance = CurveMath.checkedAdd(requiredBalance, _unclaimedProtocolFees);
        requiredBalance = CurveMath.checkedAdd(requiredBalance, _totalPendingRefunds);
        if (address(this).balance < requiredBalance) {
            revert AccountingInvariantFailed(address(this).balance, requiredBalance);
        }
    }

    function _validatePair(LaunchTypes.CurveInitialization calldata initialization) private view {
        address expectedPair = IUniswapV2Factory(initialization.uniswapFactory)
            .getPair(initialization.token, initialization.weth);
        if (expectedPair != initialization.lpPair) {
            revert PairNotCanonical(expectedPair, initialization.lpPair);
        }

        address actualFactory = IUniswapV2Pair(initialization.lpPair).factory();
        if (actualFactory != initialization.uniswapFactory) {
            revert PairFactoryMismatch(initialization.uniswapFactory, actualFactory);
        }

        address token0 = IUniswapV2Pair(initialization.lpPair).token0();
        address token1 = IUniswapV2Pair(initialization.lpPair).token1();
        bool correctOrder = token0 == initialization.token && token1 == initialization.weth;
        bool reverseOrder = token0 == initialization.weth && token1 == initialization.token;
        if (!correctOrder && !reverseOrder) revert PairTokensMismatch(token0, token1);
    }

    function _validateAddress(address account, bytes32 field) private pure {
        if (account == address(0)) revert ZeroAddress(field);
    }

    function _requireCurvePhase() private view {
        if (!_initialized || _implementation == address(this)) {
            revert NotInitialized();
        }
        if (_phase != LaunchTypes.Phase.Curve) {
            revert WrongPhase(uint8(LaunchTypes.Phase.Curve), uint8(_phase));
        }
    }

    function _requireTradingAllowed(uint256 deadline) private view {
        _requireCurvePhase();
        if (ILaunchPause(_factory).tradingPaused()) revert TradingPaused();
        _requireDeadline(deadline);
    }

    function _requireDeadline(uint256 deadline) private view {
        // Deadlines intentionally use the canonical EVM transaction timestamp.
        // forge-lint: disable-next-line(block-timestamp)
        if (block.timestamp > deadline) revert DeadlineExpired(deadline, block.timestamp);
    }
}
