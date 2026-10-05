// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

interface ILaunchErrors {
    error NotInitialized();
    error AlreadyInitialized();
    error ImplementationInitializationDisabled();
    error AlreadyGraduated();
    error ZeroAddress(bytes32 field);
    error InvalidRecipient(bytes32 field, address recipient);
    error InvalidSupplyAllocation(uint256 totalSupply, uint256 curveTokens, uint256 lpTokens);
    error InvalidCurveAllocation(uint256 curveTokens, uint256 lpTokens);
    error InvalidGraduationEth();
    error InvalidVirtualReserves(
        uint256 initialVirtualEth, uint256 initialVirtualToken, uint256 curveTokens
    );
    error InvalidTradeFeeBps(uint16 tradeFeeBps);
    error InvalidProtocolShareBps(uint16 protocolShareBps);
    error InvalidCurveBoundary(
        uint256 invariant, uint256 finalVirtualToken, uint256 finalVirtualEth
    );
    error ArithmeticOverflow();
    error DivisionByZero();
    error UnauthorizedFactory(address caller);
    error UnauthorizedCurve(address caller);
    error WrongPhase(uint8 expected, uint8 actual);
    error ZeroInput();
    error ZeroOutput();
    error SlippageExceeded(uint256 minimum, uint256 actual);
    error DeadlineExpired(uint256 deadline, uint256 currentTimestamp);
    error Oversell(uint256 tokensIn, uint256 tokensSold);
    error PairNotCanonical(address expected, address actual);
    error PairFactoryMismatch(address expected, address actual);
    error PairTokensMismatch(address token0, address token1);
    error PairSupplyNotZero(uint256 totalSupply);
    error PairTokenReserveNotZero(uint256 tokenReserve);
    error PairTokenBalanceNotZero(uint256 tokenBalance);
    error PairLiquidityZero();
    error LaunchesPaused();
    error TradingPaused();
    error UnauthorizedPauseAuthority(address caller, address authority);
    error UnauthorizedTimelock(address caller, address timelock);
    error InvalidAuthority(bytes32 field, address authority);
    error UnauthorizedCreatorClaim(address caller, address creator);
    error UnauthorizedProtocolClaim(address caller, address treasury);
    error NothingToClaim();
    error TransferRestricted(address operator, address from, address to);
    error AccountingInvariantFailed(uint256 balance, uint256 requiredBalance);
    error CurveInvariantFailed(uint256 virtualEth, uint256 virtualToken, uint256 invariant);
    error DeveloperBuyCapExceeded(uint256 tokensOut, uint256 maximumTokensOut);
    error LaunchValueMismatch(uint256 expectedValue, uint256 actualValue);
    error UnknownEngine(uint16 engineVersion);
    error EngineDisabled(uint16 engineVersion);
    error InvalidEngineImplementation(address implementation);
    error EngineVersionMismatch(uint16 expected, uint16 actual);
    error EthTransferFailed(address recipient, uint256 amount);
    error TokenTransferFailed(address token, address recipient, uint256 amount);
    error WethTransferFailed(address recipient, uint256 amount);
}
