// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { IHooks } from "@uniswap/v4-core/src/interfaces/IHooks.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolId } from "@uniswap/v4-core/src/types/PoolId.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
// v4-core SafeCast only: OZ SafeCast shares its name, which breaks Slither IR generation.
import { SafeCast } from "@uniswap/v4-core/src/libraries/SafeCast.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { Actions } from "@uniswap/v4-periphery/src/libraries/Actions.sol";
import { IAllowanceTransfer } from "permit2/src/interfaces/IAllowanceTransfer.sol";
import { ILaunchPause } from "../interfaces/ILaunchPause.sol";
import { AntiSnipe } from "../libraries/AntiSnipe.sol";
import { CurveMath } from "../libraries/CurveMath.sol";
import { GraduationMath } from "../libraries/GraduationMath.sol";
import { LaunchTypes } from "../types/LaunchTypes.sol";
import { IBondingCurveV2 } from "./interfaces/IBondingCurveV2.sol";
import { IFeeRecipientSource } from "./interfaces/IFeeRecipientSource.sol";
import { ILaunchTokenV2 } from "./interfaces/ILaunchTokenV2.sol";
import { ILpFeeLocker } from "./interfaces/ILpFeeLocker.sol";
import { BondingCurveV2Storage } from "./storage/BondingCurveV2Storage.sol";
import { LaunchTypesV2 } from "./types/LaunchTypesV2.sol";

/// @notice v2 bonding curve (spec §3.3, §4–§6): v1 curve arithmetic, fees, refunds and
/// pull-claims, plus an anti-snipe tax on public buys, a two-step creator fee recipient and
/// atomic graduation into a guarded Uniswap v4 native-ETH pool whose full-range position is
/// held by `LpFeeLocker`.
contract BondingCurveV2 is BondingCurveV2Storage, IBondingCurveV2 {
    uint16 public constant ENGINE_VERSION = 2;

    uint256 private constant IMMEDIATE_ETH_SEND_GAS = 50_000;
    uint24 private constant POOL_FEE = 10_000;
    int24 private constant TICK_SPACING = 200;
    uint256 private constant BPS_DENOMINATOR = 10_000;
    bytes32 private constant FIELD_FACTORY = "factory";
    bytes32 private constant FIELD_IMPLEMENTATION = "implementation";
    bytes32 private constant FIELD_TOKEN = "token";
    bytes32 private constant FIELD_CREATOR = "creator";
    bytes32 private constant FIELD_PROTOCOL_TREASURY = "protocolTreasury";
    bytes32 private constant FIELD_POOL_MANAGER = "poolManager";
    bytes32 private constant FIELD_POSITION_MANAGER = "positionManager";
    bytes32 private constant FIELD_PERMIT2 = "permit2";
    bytes32 private constant FIELD_HOOK = "hook";
    bytes32 private constant FIELD_LOCKER = "locker";
    bytes32 private constant FIELD_FEE_RECIPIENT = "feeRecipient";
    bytes32 private constant FIELD_TRADER = "trader";
    bytes32 private constant FIELD_TOKEN_RECIPIENT = "tokenRecipient";
    bytes32 private constant FIELD_REFUND_RECIPIENT = "refundRecipient";
    bytes32 private constant FIELD_ETH_RECIPIENT = "ethRecipient";
    bytes32 private constant FIELD_CLAIM_RECIPIENT = "claimRecipient";

    /// @dev v1 curve quote over the post-tax ETH, plus the outer anti-snipe layer.
    struct BuyQuoteV2 {
        CurveMath.BuyQuote curve;
        uint256 ethGrossUsed;
        uint256 snipeTax;
        uint256 refund;
    }

    struct GraduationResult {
        uint256 positionId;
        uint256 ethSpent;
        uint256 tokensSpent;
        uint128 liquidity;
        uint160 sqrtPriceX96;
    }

    constructor() {
        _implementation = address(this);
        _initialized = true;
    }

    /// @notice Accepts ETH only from the PositionManager, which sweeps any unsettled
    /// graduation ETH back.
    receive() external payable {
        if (msg.sender != _positionManager) revert UnexpectedEthSender(msg.sender);
    }

    /// @inheritdoc IBondingCurveV2
    function initialize(LaunchTypesV2.CurveInitializationV2 calldata initialization)
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
        _validateAddress(address(initialization.poolManager), FIELD_POOL_MANAGER);
        _validateAddress(address(initialization.positionManager), FIELD_POSITION_MANAGER);
        _validateAddress(address(initialization.permit2), FIELD_PERMIT2);
        _validateAddress(initialization.hook, FIELD_HOOK);
        _validateAddress(initialization.locker, FIELD_LOCKER);

        LaunchTypes.CurveParameters calldata parameters = initialization.parameters;
        CurveMath.validateParameters(parameters);
        LaunchTypesV2.AntiSnipeProfile calldata antiSnipe = initialization.antiSnipe;
        if (
            antiSnipe.startBps > LaunchTypesV2.MAX_ANTI_SNIPE_START_BPS
                || antiSnipe.durationSeconds > LaunchTypesV2.MAX_ANTI_SNIPE_DURATION_SECONDS
        ) {
            revert InvalidAntiSnipeProfile(antiSnipe.startBps, antiSnipe.durationSeconds);
        }
        if (initialization.lpCreatorShareBps > BPS_DENOMINATOR) {
            revert InvalidLpCreatorShareBps(initialization.lpCreatorShareBps);
        }
        // Fail at launch, not at graduation, if the opening price is out of v4's range.
        // forge-lint: disable-next-line(unused-return)
        GraduationMath.sqrtPriceX96(parameters.graduationEth, parameters.lpTokens);

        _phase = LaunchTypes.Phase.Curve;
        _tradeFeeBps = parameters.tradeFeeBps;
        _protocolShareBps = parameters.protocolShareBps;
        _antiSnipeStartBps = antiSnipe.startBps;
        _antiSnipeDurationSeconds = antiSnipe.durationSeconds;
        // Checked cast; v4-core SafeCast has no toUint64 (see the import note). The bound
        // check is not a timing decision, and the cast follows it.
        // forge-lint: disable-start(block-timestamp, unsafe-typecast)
        if (block.timestamp > type(uint64).max) revert SafeCast.SafeCastOverflow();
        _launchTime = uint64(block.timestamp);
        // forge-lint: disable-end(block-timestamp, unsafe-typecast)
        _lpCreatorShareBps = initialization.lpCreatorShareBps;
        _factory = initialization.factory;
        _implementation = initialization.implementation;
        _token = initialization.token;
        _creator = initialization.creator;
        _feeRecipient = initialization.creator;
        _protocolTreasury = initialization.protocolTreasury;
        _poolManager = address(initialization.poolManager);
        _positionManager = address(initialization.positionManager);
        _permit2 = address(initialization.permit2);
        _hook = initialization.hook;
        _locker = initialization.locker;
        _totalSupply = parameters.totalSupply;
        _curveTokens = parameters.curveTokens;
        _lpTokens = parameters.lpTokens;
        _graduationEth = parameters.graduationEth;
        _initialVirtualEth = parameters.initialVirtualEth;
        _initialVirtualToken = parameters.initialVirtualToken;
        _invariant =
            CurveMath.checkedMul(parameters.initialVirtualEth, parameters.initialVirtualToken);
        _virtualEthReserve = parameters.initialVirtualEth;
        _virtualTokenReserve = parameters.initialVirtualToken;
    }

    /// @inheritdoc IBondingCurveV2
    function buy(
        address tokenRecipient,
        address refundRecipient,
        uint256 minTokensOut,
        uint256 deadline
    ) external payable nonReentrant returns (uint256 tokensOut, uint256 ethGrossUsed) {
        return _buy(
            msg.sender, tokenRecipient, refundRecipient, minTokensOut, deadline, _currentTaxBps()
        );
    }

    /// @inheritdoc IBondingCurveV2
    function buyFor(
        address trader,
        address tokenRecipient,
        address refundRecipient,
        uint256 minTokensOut,
        uint256 deadline
    ) external payable nonReentrant returns (uint256 tokensOut, uint256 ethGrossUsed) {
        if (msg.sender != _factory) revert UnauthorizedFactory(msg.sender);
        _validateAddress(trader, FIELD_TRADER);
        // The factory calls this only for the developer buy inside the launch transaction,
        // which is exempt from the anti-snipe tax (spec §4).
        return _buy(trader, tokenRecipient, refundRecipient, minTokensOut, deadline, 0);
    }

    /// @inheritdoc IBondingCurveV2
    function sell(uint256 tokensIn, address ethRecipient, uint256 minEthOut, uint256 deadline)
        external
        nonReentrant
        returns (uint256 ethOut)
    {
        // Sells stay open while trading is paused and are never taxed.
        _requireCurvePhase();
        _requireDeadline(deadline);
        _validateAddress(ethRecipient, FIELD_ETH_RECIPIENT);

        CurveMath.SellQuote memory quote = _quoteSell(tokensIn);
        if (quote.ethOut < minEthOut) revert SlippageExceeded(minEthOut, quote.ethOut);

        _virtualEthReserve = quote.newVirtualEth;
        _virtualTokenReserve = quote.newVirtualToken;
        _unclaimedProtocolFees = CurveMath.checkedAdd(_unclaimedProtocolFees, quote.protocolFee);
        _creditCreatorFees(quote.creatorFee);

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
            quote.newVirtualToken,
            0
        );

        bool transferred = ILaunchTokenV2(_token).transferFrom(msg.sender, address(this), tokensIn);
        if (!transferred) revert TokenTransferFailed(_token, address(this), tokensIn);

        _sendOrCredit(ethRecipient, quote.ethOut);
        _assertAccountingInvariant();
        return quote.ethOut;
    }

    /// @inheritdoc IBondingCurveV2
    // `next` is checked by `_validateAddress`, which the linter does not follow.
    // forge-lint: disable-next-line(missing-zero-check)
    function proposeFeeRecipient(address next) external {
        _requireFeeRecipient();
        _validateAddress(next, FIELD_FEE_RECIPIENT);
        _pendingFeeRecipient = next;
        emit FeeRecipientProposed(_token, _feeRecipient, next);
    }

    /// @inheritdoc IBondingCurveV2
    function cancelFeeRecipientProposal() external {
        _requireFeeRecipient();
        _pendingFeeRecipient = address(0);
        emit FeeRecipientProposed(_token, _feeRecipient, address(0));
    }

    /// @inheritdoc IBondingCurveV2
    function acceptFeeRecipient() external {
        address pending = _pendingFeeRecipient;
        if (msg.sender != pending) revert UnauthorizedPendingFeeRecipient(msg.sender, pending);

        address previous = _feeRecipient;
        _feeRecipient = pending;
        _pendingFeeRecipient = address(0);
        emit FeeRecipientAccepted(_token, previous, pending);
    }

    /// @inheritdoc IBondingCurveV2
    function claimCreatorFees() external nonReentrant returns (uint256 amount) {
        amount = _takeCreatorFees(msg.sender);
        _sendClaim(msg.sender, amount);
        _assertAccountingInvariant();
    }

    /// @inheritdoc IBondingCurveV2
    function claimCreatorFeesTo(address recipient) external nonReentrant returns (uint256 amount) {
        _validateAddress(recipient, FIELD_CLAIM_RECIPIENT);
        amount = _takeCreatorFees(msg.sender);
        _sendClaim(recipient, amount);
        _assertAccountingInvariant();
    }

    /// @inheritdoc IBondingCurveV2
    function claimProtocolFees() external nonReentrant returns (uint256 amount) {
        _requireTreasury();
        amount = _unclaimedProtocolFees;
        if (amount == 0) revert NothingToClaim();

        _unclaimedProtocolFees = 0;
        emit ProtocolFeesClaimed(_token, _protocolTreasury, amount);
        _sendClaim(_protocolTreasury, amount);
        _assertAccountingInvariant();
    }

    /// @inheritdoc IBondingCurveV2
    function claimSnipeTax() external nonReentrant returns (uint256 amount) {
        _requireTreasury();
        amount = _unclaimedSnipeTax;
        if (amount == 0) revert NothingToClaim();

        _unclaimedSnipeTax = 0;
        emit SnipeTaxClaimed(_token, _protocolTreasury, amount);
        _sendClaim(_protocolTreasury, amount);
        _assertAccountingInvariant();
    }

    /// @inheritdoc IBondingCurveV2
    function claimRefund() external nonReentrant returns (uint256 amount) {
        amount = _takeRefund(msg.sender);
        _sendClaim(msg.sender, amount);
        _assertAccountingInvariant();
    }

    /// @inheritdoc IBondingCurveV2
    function claimRefundTo(address recipient) external nonReentrant returns (uint256 amount) {
        _validateAddress(recipient, FIELD_CLAIM_RECIPIENT);
        amount = _takeRefund(msg.sender);
        _sendClaim(recipient, amount);
        _assertAccountingInvariant();
    }

    /// @inheritdoc IBondingCurveV2
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
        )
    {
        _requireCurvePhase();
        BuyQuoteV2 memory quote = _quoteBuy(ethGross, _currentTaxBps());
        return (
            quote.ethGrossUsed,
            quote.curve.tokensOut,
            quote.curve.protocolFee,
            quote.curve.creatorFee,
            quote.snipeTax,
            quote.refund,
            quote.curve.graduates
        );
    }

    /// @inheritdoc IBondingCurveV2
    function quoteSell(uint256 tokensIn)
        external
        view
        returns (uint256 ethOut, uint256 ethGross, uint256 protocolFee, uint256 creatorFee)
    {
        _requireCurvePhase();
        CurveMath.SellQuote memory quote = _quoteSell(tokensIn);
        return (quote.ethOut, quote.ethGross, quote.protocolFee, quote.creatorFee);
    }

    /// @inheritdoc IBondingCurveV2
    function currentTaxBps() external view returns (uint16) {
        return _currentTaxBps();
    }

    /// @inheritdoc IFeeRecipientSource
    function feeRecipient() external view returns (address) {
        return _feeRecipient;
    }

    /// @inheritdoc IBondingCurveV2
    function pendingFeeRecipient() external view returns (address) {
        return _pendingFeeRecipient;
    }

    /// @inheritdoc IFeeRecipientSource
    function protocolTreasury() external view returns (address) {
        return _protocolTreasury;
    }

    /// @inheritdoc IFeeRecipientSource
    function lpCreatorShareBps() external view returns (uint16) {
        return _lpCreatorShareBps;
    }

    function creatorFeesOwed(address account) external view returns (uint256) {
        return _creatorFeesOwed[account];
    }

    function unclaimedCreatorFees() external view returns (uint256) {
        return _totalCreatorFees;
    }

    function unclaimedProtocolFees() external view returns (uint256) {
        return _unclaimedProtocolFees;
    }

    function unclaimedSnipeTax() external view returns (uint256) {
        return _unclaimedSnipeTax;
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

    function implementation() external view returns (address) {
        return _implementation;
    }

    function poolManager() external view returns (address) {
        return _poolManager;
    }

    function positionManager() external view returns (address) {
        return _positionManager;
    }

    function hook() external view returns (address) {
        return _hook;
    }

    function locker() external view returns (address) {
        return _locker;
    }

    function launchTime() external view returns (uint64) {
        return _launchTime;
    }

    function antiSnipeProfile() external view returns (LaunchTypesV2.AntiSnipeProfile memory) {
        return LaunchTypesV2.AntiSnipeProfile({
            startBps: _antiSnipeStartBps, durationSeconds: _antiSnipeDurationSeconds
        });
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

    function _buy(
        address trader,
        address tokenRecipient,
        address refundRecipient,
        uint256 minTokensOut,
        uint256 deadline,
        uint16 taxBps
    ) private returns (uint256 tokensOut, uint256 ethGrossUsed) {
        _requireTradingAllowed(deadline);
        _validateAddress(tokenRecipient, FIELD_TOKEN_RECIPIENT);
        // Curve-operated transfers bypass the token guard, so tokens sent to the PoolManager
        // here would be pre-graduation token-side liquidity.
        if (tokenRecipient == _poolManager || tokenRecipient == address(this)) {
            revert InvalidRecipient(FIELD_TOKEN_RECIPIENT, tokenRecipient);
        }
        _validateAddress(refundRecipient, FIELD_REFUND_RECIPIENT);

        BuyQuoteV2 memory quote = _quoteBuy(msg.value, taxBps);
        tokensOut = quote.curve.tokensOut;
        if (tokensOut < minTokensOut) revert SlippageExceeded(minTokensOut, tokensOut);

        _virtualEthReserve = quote.curve.newVirtualEth;
        _virtualTokenReserve = quote.curve.newVirtualToken;
        _unclaimedProtocolFees =
            CurveMath.checkedAdd(_unclaimedProtocolFees, quote.curve.protocolFee);
        _creditCreatorFees(quote.curve.creatorFee);
        _unclaimedSnipeTax = CurveMath.checkedAdd(_unclaimedSnipeTax, quote.snipeTax);

        emit Trade(
            _token,
            trader,
            true,
            quote.ethGrossUsed,
            quote.refund,
            tokensOut,
            quote.curve.protocolFee,
            quote.curve.creatorFee,
            quote.curve.newVirtualEth,
            quote.curve.newVirtualToken,
            quote.snipeTax
        );

        // The contract is non-reentrant and any later graduation failure reverts this transfer.
        // forge-lint: disable-next-line(reentrancy-no-eth)
        bool transferred = ILaunchTokenV2(_token).transfer(tokenRecipient, tokensOut);
        if (!transferred) revert TokenTransferFailed(_token, tokenRecipient, tokensOut);

        if (quote.curve.graduates) _graduate();
        _sendOrCredit(refundRecipient, quote.refund);
        _assertAccountingInvariant();
        return (tokensOut, quote.ethGrossUsed);
    }

    /// @dev The anti-snipe tax is the outermost layer on the ETH actually consumed (spec §4):
    /// `split(ethGross)` feeds the post-tax ETH through v1 `CurveMath.quoteBuy`. On a final
    /// fill the consumed gross is `AntiSnipe.grossForNet(v1 gross used)` — rounded up so the
    /// protocol is never short — and the refunded excess is never taxed.
    function _quoteBuy(uint256 ethGross, uint16 taxBps)
        private
        view
        returns (BuyQuoteV2 memory quote)
    {
        (uint256 tax, uint256 net) = AntiSnipe.split(ethGross, taxBps);
        quote.curve = CurveMath.quoteBuy(
            _invariant,
            _virtualEthReserve,
            _virtualTokenReserve,
            _initialVirtualToken - _curveTokens,
            net,
            _tradeFeeBps,
            _protocolShareBps
        );

        if (quote.curve.graduates) {
            quote.ethGrossUsed = AntiSnipe.grossForNet(quote.curve.ethGrossUsed, taxBps);
            quote.snipeTax = quote.ethGrossUsed - quote.curve.ethGrossUsed;
            quote.refund = ethGross - quote.ethGrossUsed;
        } else {
            quote.ethGrossUsed = ethGross;
            quote.snipeTax = tax;
        }
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

    function _currentTaxBps() private view returns (uint16) {
        return AntiSnipe.taxBps(
            _antiSnipeStartBps, _antiSnipeDurationSeconds, _launchTime, block.timestamp
        );
    }

    // Graduation runs inside nonReentrant buy; the protocol remainder can only be credited
    // after the PositionManager reports what it actually settled.
    // forge-lint: disable-start(reentrancy-no-eth)
    /// @dev Spec §6.2: mark graduated → initialize the guarded pool → mint the full-range
    /// position to the locker → register it → credit remainders to the protocol → emit.
    function _graduate() private {
        _phase = LaunchTypes.Phase.Graduated;
        ILaunchTokenV2(_token).markGraduated();

        PoolKey memory key = _poolKey();
        GraduationResult memory result;
        result.sqrtPriceX96 = GraduationMath.sqrtPriceX96(_graduationEth, _lpTokens);
        // The returned tick is derivable from sqrtPriceX96 and not needed here.
        // forge-lint: disable-next-line(unused-return)
        IPoolManager(_poolManager).initialize(key, result.sqrtPriceX96);

        uint256 ethUsed;
        uint256 tokenUsed;
        (result.liquidity, ethUsed, tokenUsed) =
            GraduationMath.fullRangePosition(result.sqrtPriceX96, _graduationEth, _lpTokens);
        result.positionId = IPositionManager(_positionManager).nextTokenId();
        (result.ethSpent, result.tokensSpent) =
            _mintPosition(key, result.liquidity, ethUsed, tokenUsed);
        if (result.ethSpent > ethUsed || result.tokensSpent > tokenUsed) {
            revert GraduationOverspent(result.ethSpent, result.tokensSpent);
        }

        ILpFeeLocker(_locker).registerPosition(_token, result.positionId);

        uint256 ethRemainder = _graduationEth - result.ethSpent;
        uint256 tokenRemainder = _lpTokens - result.tokensSpent;
        _unclaimedProtocolFees = CurveMath.checkedAdd(_unclaimedProtocolFees, ethRemainder);

        // Pool outcome values are only known after the mint; nonReentrant prevents reordering.
        // forge-lint: disable-start(reentrancy-events)
        emit Graduated(
            _token,
            PoolId.unwrap(key.toId()),
            result.positionId,
            result.ethSpent,
            result.tokensSpent,
            result.liquidity,
            result.sqrtPriceX96,
            ethRemainder,
            tokenRemainder
        );
        // forge-lint: disable-end(reentrancy-events)

        if (tokenRemainder != 0) {
            bool transferred = ILaunchTokenV2(_token).transfer(_protocolTreasury, tokenRemainder);
            if (!transferred) {
                revert TokenTransferFailed(_token, _protocolTreasury, tokenRemainder);
            }
        }
    }

    /// @return ethSpent ETH and `tokensSpent` tokens actually settled, measured as this
    /// contract's balance change (any unsettled ETH is swept back).
    function _mintPosition(PoolKey memory key, uint128 liquidity, uint256 ethMax, uint256 tokenMax)
        private
        returns (uint256 ethSpent, uint256 tokensSpent)
    {
        IERC20 launchToken = IERC20(_token);
        bool approved = launchToken.approve(_permit2, tokenMax);
        if (!approved) revert TokenTransferFailed(_token, _permit2, tokenMax);
        // Checked cast; v4-core SafeCast has no toUint48 (see the import note). The bound
        // check is not a timing decision, and the cast follows it.
        // forge-lint: disable-start(block-timestamp, unsafe-typecast)
        if (block.timestamp > type(uint48).max) revert SafeCast.SafeCastOverflow();
        uint48 expiration = uint48(block.timestamp);
        // forge-lint: disable-end(block-timestamp, unsafe-typecast)
        IAllowanceTransfer(_permit2)
            .approve(_token, _positionManager, SafeCast.toUint160(tokenMax), expiration);

        uint256 ethBefore = address(this).balance;
        uint256 tokensBefore = launchToken.balanceOf(address(this));
        // The PositionManager is a launch snapshot validated by the fork gate.
        bytes memory unlockData = _mintData(key, liquidity, ethMax, tokenMax);
        IPositionManager positionManager_ = IPositionManager(_positionManager);
        // forge-lint: disable-next-line(arbitrary-send-eth)
        positionManager_.modifyLiquidities{ value: ethMax }(unlockData, block.timestamp);
        ethSpent = ethBefore - address(this).balance;
        tokensSpent = tokensBefore - launchToken.balanceOf(address(this));
    }

    // forge-lint: disable-end(reentrancy-no-eth)

    /// @dev `MINT_POSITION` to the locker, `SETTLE_PAIR`, `SWEEP` unsettled ETH back here.
    function _mintData(PoolKey memory key, uint128 liquidity, uint256 ethMax, uint256 tokenMax)
        private
        view
        returns (bytes memory)
    {
        // Action ids are compile-time constants below 0x20; the narrowing cannot truncate.
        // forge-lint: disable-start(unsafe-typecast)
        bytes memory actions = abi.encodePacked(
            uint8(Actions.MINT_POSITION), uint8(Actions.SETTLE_PAIR), uint8(Actions.SWEEP)
        );
        // forge-lint: disable-end(unsafe-typecast)
        bytes[] memory params = new bytes[](3);
        params[0] = abi.encode(
            key,
            GraduationMath.TICK_LOWER,
            GraduationMath.TICK_UPPER,
            uint256(liquidity),
            SafeCast.toUint128(ethMax),
            SafeCast.toUint128(tokenMax),
            _locker,
            bytes("")
        );
        params[1] = abi.encode(key.currency0, key.currency1);
        params[2] = abi.encode(key.currency0, address(this));
        return abi.encode(actions, params);
    }

    function _poolKey() private view returns (PoolKey memory) {
        return PoolKey({
            currency0: Currency.wrap(address(0)),
            currency1: Currency.wrap(_token),
            fee: POOL_FEE,
            tickSpacing: TICK_SPACING,
            hooks: IHooks(_hook)
        });
    }

    function _creditCreatorFees(uint256 amount) private {
        if (amount == 0) return;
        address recipient = _feeRecipient;
        _creatorFeesOwed[recipient] = CurveMath.checkedAdd(_creatorFeesOwed[recipient], amount);
        _totalCreatorFees = CurveMath.checkedAdd(_totalCreatorFees, amount);
    }

    function _takeCreatorFees(address account) private returns (uint256 amount) {
        amount = _creatorFeesOwed[account];
        if (amount == 0) revert NothingToClaim();

        _creatorFeesOwed[account] = 0;
        _totalCreatorFees -= amount;
        emit CreatorFeesClaimed(_token, account, amount);
    }

    function _takeRefund(address account) private returns (uint256 amount) {
        amount = _pendingRefunds[account];
        if (amount == 0) revert NothingToClaim();

        _pendingRefunds[account] = 0;
        _totalPendingRefunds -= amount;
        emit RefundClaimed(_token, account, amount);
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

    function _assertAccountingInvariant() private view {
        uint256 requiredBalance = _phase == LaunchTypes.Phase.Curve
            ? CurveMath.realCurveEth(_virtualEthReserve, _initialVirtualEth)
            : 0;
        requiredBalance = CurveMath.checkedAdd(requiredBalance, _totalCreatorFees);
        requiredBalance = CurveMath.checkedAdd(requiredBalance, _unclaimedProtocolFees);
        requiredBalance = CurveMath.checkedAdd(requiredBalance, _unclaimedSnipeTax);
        requiredBalance = CurveMath.checkedAdd(requiredBalance, _totalPendingRefunds);
        if (address(this).balance < requiredBalance) {
            revert AccountingInvariantFailed(address(this).balance, requiredBalance);
        }
    }

    function _requireFeeRecipient() private view {
        if (msg.sender != _feeRecipient) {
            revert UnauthorizedFeeRecipient(msg.sender, _feeRecipient);
        }
    }

    function _requireTreasury() private view {
        if (msg.sender != _protocolTreasury) {
            revert UnauthorizedProtocolClaim(msg.sender, _protocolTreasury);
        }
    }

    function _validateAddress(address account, bytes32 field) private pure {
        if (account == address(0)) revert ZeroAddress(field);
    }

    function _requireCurvePhase() private view {
        if (!_initialized || _implementation == address(this)) revert NotInitialized();
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
