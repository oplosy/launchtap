// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Clones } from "@openzeppelin/contracts/proxy/Clones.sol";
import { ReentrancyGuard } from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { Hooks } from "@uniswap/v4-core/src/libraries/Hooks.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { IAllowanceTransfer } from "permit2/src/interfaces/IAllowanceTransfer.sol";
import { CurveMath } from "../libraries/CurveMath.sol";
import { GraduationMath } from "../libraries/GraduationMath.sol";
import { LaunchTypes } from "../types/LaunchTypes.sol";
import { GraduationGuardHook } from "./GraduationGuardHook.sol";
import { LaunchTokenV2 } from "./LaunchTokenV2.sol";
import { LpFeeLocker } from "./LpFeeLocker.sol";
import { IBondingCurveV2 } from "./interfaces/IBondingCurveV2.sol";
import { ILaunchFactoryV2 } from "./interfaces/ILaunchFactoryV2.sol";
import { LaunchFactoryV2Storage } from "./storage/LaunchFactoryV2Storage.sol";
import { LaunchTypesV2 } from "./types/LaunchTypesV2.sol";

/// @notice Non-upgradeable v2 launch coordinator: deploys the token and curve clone, snapshots
/// the future-launch configuration into the curve, and is the token-to-curve registry the
/// graduation guard hook and the LP fee locker trust. Admin semantics mirror v1 `LaunchFactory`.
contract LaunchFactoryV2 is LaunchFactoryV2Storage, ReentrancyGuard, ILaunchFactoryV2 {
    uint256 private constant DEVELOPER_BUY_CAP_DIVISOR = 100;
    uint16 private constant BPS_DENOMINATOR = 10_000;
    bytes32 private constant FIELD_PAUSE_AUTHORITY = "pauseAuthority";
    bytes32 private constant FIELD_TIMELOCK = "timelock";
    bytes32 private constant FIELD_PROTOCOL_TREASURY = "protocolTreasury";
    bytes32 private constant FIELD_IMPLEMENTATION = "implementation";
    bytes32 private constant FIELD_POOL_MANAGER = "poolManager";
    bytes32 private constant FIELD_POSITION_MANAGER = "positionManager";
    bytes32 private constant FIELD_PERMIT2 = "permit2";
    bytes32 private constant FIELD_HOOK = "hook";
    bytes32 private constant FIELD_LOCKER = "locker";

    /// @notice The only account allowed to wire the hook and locker, once.
    address public immutable DEPLOYER;
    IPoolManager public immutable POOL_MANAGER;
    IPositionManager public immutable POSITION_MANAGER;
    IAllowanceTransfer public immutable PERMIT2;

    struct LaunchSnapshot {
        LaunchTypes.CurveParameters parameters;
        LaunchTypesV2.AntiSnipeProfile antiSnipe;
        uint16 lpCreatorShareBps;
        address implementation;
        address protocolTreasury;
        uint256 launchFee;
    }

    constructor(LaunchTypesV2.FactoryInitializationV2 memory initialization) {
        _validateAddress(initialization.pauseAuthority, FIELD_PAUSE_AUTHORITY);
        _validateAddress(initialization.timelock, FIELD_TIMELOCK);
        _validateAddress(initialization.protocolTreasury, FIELD_PROTOCOL_TREASURY);
        _validateAddress(address(initialization.poolManager), FIELD_POOL_MANAGER);
        _validateAddress(address(initialization.positionManager), FIELD_POSITION_MANAGER);
        _validateAddress(address(initialization.permit2), FIELD_PERMIT2);
        if (initialization.pauseAuthority == msg.sender) {
            revert InvalidAuthority(FIELD_PAUSE_AUTHORITY, msg.sender);
        }
        if (
            initialization.timelock == msg.sender
                || initialization.timelock == initialization.pauseAuthority
        ) {
            revert InvalidAuthority(FIELD_TIMELOCK, initialization.timelock);
        }
        if (
            address(initialization.positionManager.poolManager())
                != address(initialization.poolManager)
        ) {
            revert InvalidDependency(
                FIELD_POSITION_MANAGER, address(initialization.positionManager)
            );
        }

        DEPLOYER = msg.sender;
        POOL_MANAGER = initialization.poolManager;
        POSITION_MANAGER = initialization.positionManager;
        PERMIT2 = initialization.permit2;
        _pauseAuthority = initialization.pauseAuthority;
        _timelock = initialization.timelock;
        _futureProtocolTreasury = initialization.protocolTreasury;

        _setFutureDefaults(initialization.defaults);
        _configureEngine(initialization.engineVersion, initialization.implementation, true);

        emit FutureTreasuryConfigured(address(0), initialization.protocolTreasury);
        emit FutureDefaultsConfigured(_hashDefaults(initialization.defaults));
    }

    /// @notice Wires the guard hook and LP fee locker, both of which are constructed with this
    /// factory's address and therefore deploy after it.
    // The shared validator below performs the zero-address checks.
    // forge-lint: disable-next-line(missing-zero-check)
    function setDependencies(address hook_, address locker_) external nonReentrant {
        if (msg.sender != DEPLOYER) revert UnauthorizedDeployer(msg.sender, DEPLOYER);
        if (_hook != address(0)) revert DependenciesAlreadySet();
        _validateAddress(hook_, FIELD_HOOK);
        _validateAddress(locker_, FIELD_LOCKER);
        _validateHook(hook_);
        _validateLocker(locker_);

        _hook = hook_;
        _locker = locker_;
    }

    function launch(LaunchTypes.LaunchRequest calldata request)
        external
        payable
        nonReentrant
        returns (address token, address curve)
    {
        if (_hook == address(0)) revert DependenciesNotSet();
        if (_launchesPaused) revert LaunchesPaused();

        LaunchSnapshot memory snapshot = _snapshot(request.engineVersion);
        uint256 expectedValue = CurveMath.checkedAdd(snapshot.launchFee, request.developerBuyGross);
        if (msg.value != expectedValue) revert LaunchValueMismatch(expectedValue, msg.value);
        if (request.developerBuyGross != 0 && _tradingPaused) revert TradingPaused();
        if (request.developerBuyGross == 0 && request.minDeveloperTokensOut != 0) {
            revert SlippageExceeded(request.minDeveloperTokensOut, 0);
        }

        curve = Clones.clone(snapshot.implementation);
        token = address(
            new LaunchTokenV2(
                request.name,
                request.symbol,
                curve,
                address(POOL_MANAGER),
                snapshot.parameters.totalSupply
            )
        );
        // Registered before initialization so the registry is never behind a live curve.
        _curveOfToken[token] = curve;
        _initializeCurve(curve, token, snapshot);

        _launchFeesByTreasury[snapshot.protocolTreasury] = CurveMath.checkedAdd(
            _launchFeesByTreasury[snapshot.protocolTreasury], snapshot.launchFee
        );

        _emitTokenLaunched(token, curve, _launchData(request, snapshot));
        _executeDeveloperBuy(curve, request, snapshot.parameters.totalSupply);
    }

    function claimLaunchFees() external nonReentrant returns (uint256 amount) {
        amount = _launchFeesByTreasury[msg.sender];
        if (amount == 0) revert NothingToClaim();

        _launchFeesByTreasury[msg.sender] = 0;
        emit LaunchFeesClaimed(msg.sender, amount);

        // The treasury is the authenticated caller; claims intentionally forward all gas.
        // forge-lint: disable-next-line(arbitrary-send-eth, low-level-calls)
        (bool success,) = payable(msg.sender).call{ value: amount }("");
        if (!success) revert EthTransferFailed(msg.sender, amount);
    }

    function setLaunchesPaused(bool paused) external nonReentrant {
        _requirePauseAuthority();
        _launchesPaused = paused;
        emit LaunchPauseSet(paused);
    }

    function setTradingPaused(bool paused) external nonReentrant {
        _requirePauseAuthority();
        _tradingPaused = paused;
        emit TradingPauseSet(paused);
    }

    function configureEngine(uint16 engineVersion, address implementation_, bool enabled)
        external
        nonReentrant
    {
        _requireTimelock();
        _configureEngine(engineVersion, implementation_, enabled);
    }

    function setFutureDefaults(LaunchTypesV2.FactoryDefaultsV2 calldata defaults)
        external
        nonReentrant
    {
        _requireTimelock();
        _setFutureDefaults(defaults);
        emit FutureDefaultsConfigured(_hashDefaults(defaults));
    }

    // The shared validator below performs the zero-address check.
    // forge-lint: disable-next-line(missing-zero-check)
    function setFutureTreasury(address treasury) external nonReentrant {
        _requireTimelock();
        _validateAddress(treasury, FIELD_PROTOCOL_TREASURY);

        address previousTreasury = _futureProtocolTreasury;
        _futureProtocolTreasury = treasury;
        emit FutureTreasuryConfigured(previousTreasury, treasury);
    }

    function curveOfToken(address token) external view returns (address) {
        return _curveOfToken[token];
    }

    function hook() external view returns (address) {
        return _hook;
    }

    function locker() external view returns (address) {
        return _locker;
    }

    function curveImplementation(uint16 engineVersion) external view returns (address) {
        return _engines[engineVersion].implementation;
    }

    function engineEnabled(uint16 engineVersion) external view returns (bool) {
        return _engines[engineVersion].enabled;
    }

    function futureDefaults()
        external
        view
        returns (LaunchTypesV2.FactoryDefaultsV2 memory defaults)
    {
        return _futureDefaults();
    }

    function futureDefaultsHash() external view returns (bytes32) {
        return _hashDefaults(_futureDefaults());
    }

    function protocolTreasury() external view returns (address) {
        return _futureProtocolTreasury;
    }

    function launchFee() external view returns (uint256) {
        return _launchFee;
    }

    function pauseAuthority() external view returns (address) {
        return _pauseAuthority;
    }

    function timelock() external view returns (address) {
        return _timelock;
    }

    function launchesPaused() external view returns (bool) {
        return _launchesPaused;
    }

    function tradingPaused() external view returns (bool) {
        return _tradingPaused;
    }

    function launchFeesByTreasury(address treasury) external view returns (uint256) {
        return _launchFeesByTreasury[treasury];
    }

    function _snapshot(uint16 engineVersion) private view returns (LaunchSnapshot memory snapshot) {
        EngineConfig storage engine = _engines[engineVersion];
        if (engine.implementation == address(0)) revert UnknownEngine(engineVersion);
        if (!engine.enabled) revert EngineDisabled(engineVersion);

        snapshot.parameters = _futureParameters;
        snapshot.antiSnipe = _futureAntiSnipe;
        snapshot.lpCreatorShareBps = _futureLpCreatorShareBps;
        snapshot.implementation = engine.implementation;
        snapshot.protocolTreasury = _futureProtocolTreasury;
        snapshot.launchFee = _launchFee;
    }

    function _initializeCurve(address curve, address token, LaunchSnapshot memory snapshot)
        private
    {
        IBondingCurveV2(curve)
            .initialize(
                LaunchTypesV2.CurveInitializationV2({
                    factory: address(this),
                    implementation: snapshot.implementation,
                    token: token,
                    creator: msg.sender,
                    protocolTreasury: snapshot.protocolTreasury,
                    poolManager: POOL_MANAGER,
                    positionManager: POSITION_MANAGER,
                    permit2: PERMIT2,
                    hook: _hook,
                    locker: _locker,
                    parameters: snapshot.parameters,
                    antiSnipe: snapshot.antiSnipe,
                    lpCreatorShareBps: snapshot.lpCreatorShareBps
                })
            );
    }

    /// @dev The curve quote applies the live anti-snipe tax, but the factory's `buyFor` is
    /// untaxed, so the 1% cap is enforced on the executed amounts instead of a pre-quote.
    function _executeDeveloperBuy(
        address curve,
        LaunchTypes.LaunchRequest calldata request,
        uint256 totalSupply
    ) private {
        if (request.developerBuyGross == 0) return;

        (uint256 tokensOut, uint256 grossUsed) = _callDeveloperBuy(
            IBondingCurveV2(curve),
            msg.sender,
            request.developerBuyGross,
            request.minDeveloperTokensOut,
            request.deadline
        );
        uint256 maximumTokensOut = totalSupply / DEVELOPER_BUY_CAP_DIVISOR;
        if (tokensOut > maximumTokensOut) {
            revert DeveloperBuyCapExceeded(tokensOut, maximumTokensOut);
        }
        if (grossUsed != request.developerBuyGross) {
            revert LaunchValueMismatch(request.developerBuyGross, grossUsed);
        }
    }

    function _callDeveloperBuy(
        IBondingCurveV2 curve,
        address creator,
        uint256 gross,
        uint256 minTokensOut,
        uint256 deadline
    ) private returns (uint256 tokensOut, uint256 grossUsed) {
        // The value only reaches the factory-created, already initialized curve clone.
        // forge-lint: disable-next-line(arbitrary-send-eth)
        return curve.buyFor{ value: gross }(creator, creator, creator, minTokensOut, deadline);
    }

    function _launchData(LaunchTypes.LaunchRequest calldata request, LaunchSnapshot memory snapshot)
        private
        view
        returns (LaunchTypesV2.TokenLaunchedData memory data)
    {
        data.protocolTreasury = snapshot.protocolTreasury;
        data.engineVersion = request.engineVersion;
        data.name = request.name;
        data.symbol = request.symbol;
        data.totalSupply = snapshot.parameters.totalSupply;
        data.virtualEth = snapshot.parameters.initialVirtualEth;
        data.virtualToken = snapshot.parameters.initialVirtualToken;
        data.curveTokens = snapshot.parameters.curveTokens;
        data.lpTokens = snapshot.parameters.lpTokens;
        data.graduationEth = snapshot.parameters.graduationEth;
        data.launchFeePaid = snapshot.launchFee;
        data.tradeFeeBps = snapshot.parameters.tradeFeeBps;
        data.protocolShareBps = snapshot.parameters.protocolShareBps;
        data.hook = _hook;
        data.locker = _locker;
        data.antiSnipeStartBps = snapshot.antiSnipe.startBps;
        data.antiSnipeDurationSeconds = snapshot.antiSnipe.durationSeconds;
        data.lpCreatorShareBps = snapshot.lpCreatorShareBps;
    }

    /// @dev Emits `ILaunchEventsV2.TokenLaunched`. Its 21 arguments exceed the legacy
    /// codegen stack, so the non-indexed payload is the ABI encoding of `TokenLaunchedData`,
    /// whose fields match the event's non-indexed fields in order.
    function _emitTokenLaunched(
        address token,
        address curve,
        LaunchTypesV2.TokenLaunchedData memory data
    ) private {
        bytes memory encoded = abi.encode(data);
        bytes32 signature = TokenLaunched.selector;
        address creator = msg.sender;

        // `abi.encode` of a dynamic struct starts with the tuple's 0x20 offset word; the event
        // data is the tuple encoding that follows it.
        assembly ("memory-safe") {
            log4(add(encoded, 0x40), sub(mload(encoded), 0x20), signature, token, curve, creator)
        }
    }

    function _validateHook(address hook_) private view {
        if (
            hook_.code.length == 0
                || uint160(hook_) & Hooks.ALL_HOOK_MASK != Hooks.BEFORE_INITIALIZE_FLAG
                || address(GraduationGuardHook(hook_).FACTORY()) != address(this)
                || address(GraduationGuardHook(hook_).POOL_MANAGER()) != address(POOL_MANAGER)
        ) {
            revert InvalidDependency(FIELD_HOOK, hook_);
        }
    }

    function _validateLocker(address locker_) private view {
        if (
            locker_.code.length == 0
                || address(LpFeeLocker(payable(locker_)).FACTORY()) != address(this)
                || address(LpFeeLocker(payable(locker_)).POSITION_MANAGER())
                    != address(POSITION_MANAGER)
        ) {
            revert InvalidDependency(FIELD_LOCKER, locker_);
        }
    }

    function _configureEngine(uint16 engineVersion, address implementation_, bool enabled) private {
        _validateAddress(implementation_, FIELD_IMPLEMENTATION);
        if (implementation_.code.length == 0) {
            revert InvalidEngineImplementation(implementation_);
        }

        uint16 actualVersion = 0;
        try IBondingCurveV2(implementation_).ENGINE_VERSION() returns (uint16 version) {
            actualVersion = version;
        } catch {
            revert InvalidEngineImplementation(implementation_);
        }
        if (actualVersion != engineVersion) {
            revert EngineVersionMismatch(engineVersion, actualVersion);
        }

        _engines[engineVersion] =
            EngineConfig({ implementation: implementation_, enabled: enabled });
        emit EngineConfigured(engineVersion, implementation_, enabled);
    }

    /// @dev Enforces the same bounds `BondingCurveV2.initialize` re-checks, so a bad
    /// configuration fails when it is set, not at the next launch.
    function _setFutureDefaults(LaunchTypesV2.FactoryDefaultsV2 memory defaults) private {
        CurveMath.validateParameters(defaults.parameters);
        LaunchTypesV2.AntiSnipeProfile memory antiSnipe = defaults.antiSnipe;
        if (
            antiSnipe.startBps > LaunchTypesV2.MAX_ANTI_SNIPE_START_BPS
                || antiSnipe.durationSeconds > LaunchTypesV2.MAX_ANTI_SNIPE_DURATION_SECONDS
        ) {
            revert IBondingCurveV2.InvalidAntiSnipeProfile(
                antiSnipe.startBps, antiSnipe.durationSeconds
            );
        }
        if (defaults.lpCreatorShareBps > BPS_DENOMINATOR) {
            revert IBondingCurveV2.InvalidLpCreatorShareBps(defaults.lpCreatorShareBps);
        }
        // forge-lint: disable-next-line(unused-return)
        GraduationMath.sqrtPriceX96(defaults.parameters.graduationEth, defaults.parameters.lpTokens);

        _futureParameters = defaults.parameters;
        _futureAntiSnipe = antiSnipe;
        _futureLpCreatorShareBps = defaults.lpCreatorShareBps;
        _launchFee = defaults.launchFee;
    }

    function _futureDefaults()
        private
        view
        returns (LaunchTypesV2.FactoryDefaultsV2 memory defaults)
    {
        defaults.parameters = _futureParameters;
        defaults.antiSnipe = _futureAntiSnipe;
        defaults.lpCreatorShareBps = _futureLpCreatorShareBps;
        defaults.launchFee = _launchFee;
    }

    function _hashDefaults(LaunchTypesV2.FactoryDefaultsV2 memory defaults)
        private
        pure
        returns (bytes32)
    {
        return keccak256(abi.encode(defaults));
    }

    function _requirePauseAuthority() private view {
        if (msg.sender != _pauseAuthority) {
            revert UnauthorizedPauseAuthority(msg.sender, _pauseAuthority);
        }
    }

    function _requireTimelock() private view {
        if (msg.sender != _timelock) revert UnauthorizedTimelock(msg.sender, _timelock);
    }

    function _validateAddress(address account, bytes32 field) private pure {
        if (account == address(0)) revert ZeroAddress(field);
    }
}
