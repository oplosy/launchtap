// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Script } from "forge-std/Script.sol";
import { Vm } from "forge-std/Vm.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { IHooks } from "@uniswap/v4-core/src/interfaces/IHooks.sol";
import { IPoolManager } from "@uniswap/v4-core/src/interfaces/IPoolManager.sol";
import { StateLibrary } from "@uniswap/v4-core/src/libraries/StateLibrary.sol";
import { Currency } from "@uniswap/v4-core/src/types/Currency.sol";
import { PoolKey } from "@uniswap/v4-core/src/types/PoolKey.sol";
import { IPositionManager } from "@uniswap/v4-periphery/src/interfaces/IPositionManager.sol";
import { IAllowanceTransfer } from "permit2/src/interfaces/IAllowanceTransfer.sol";
import { DeployPermit2 } from "permit2/test/utils/DeployPermit2.sol";
import { ILaunchErrors } from "../../src/interfaces/ILaunchErrors.sol";
import { LaunchTypes } from "../../src/types/LaunchTypes.sol";
import { BondingCurveV2 } from "../../src/v2/BondingCurveV2.sol";
import { GraduationGuardHook } from "../../src/v2/GraduationGuardHook.sol";
import { LaunchFactoryV2 } from "../../src/v2/LaunchFactoryV2.sol";
import { LpFeeLocker } from "../../src/v2/LpFeeLocker.sol";
import { IBondingCurveV2 } from "../../src/v2/interfaces/IBondingCurveV2.sol";
import { ILaunchEventsV2 } from "../../src/v2/interfaces/ILaunchEventsV2.sol";
import { LaunchTypesV2 } from "../../src/v2/types/LaunchTypesV2.sol";
import { MineGuardHook } from "./MineGuardHook.s.sol";

/// @notice Launches tokens through the factory and trades as an ordinary account.
contract VectorActorV2 {
    function launch(LaunchFactoryV2 factory)
        external
        returns (IERC20 token, IBondingCurveV2 curve)
    {
        (address tokenAddress, address curveAddress) = factory.launch(
            LaunchTypes.LaunchRequest({
                name: "Vector Token",
                symbol: "VEC",
                engineVersion: 2,
                developerBuyGross: 0,
                minDeveloperTokensOut: 0,
                deadline: block.timestamp
            })
        );
        return (IERC20(tokenAddress), IBondingCurveV2(curveAddress));
    }

    function executeBuy(IBondingCurveV2 curve, uint256 gross)
        external
        returns (uint256 tokensOut, uint256 grossUsed)
    {
        // Value only reaches a curve clone the factory just launched.
        // forge-lint: disable-next-line(arbitrary-send-eth)
        return curve.buy{ value: gross }(address(this), address(this), 0, type(uint256).max);
    }

    function executeSell(IERC20 token, IBondingCurveV2 curve, uint256 tokensIn)
        external
        returns (uint256 ethOut)
    {
        require(token.approve(address(curve), tokensIn), "APPROVE_FAILED");
        return curve.sell(tokensIn, address(this), 0, type(uint256).max);
    }

    receive() external payable { }
}

/// @notice Deploys and wires the factory, guard hook and locker. Scripts cannot rely on their
/// own address, and the factory's deployer and the hook's CREATE2 deployer must be stable.
contract VectorSystemV2 {
    LaunchFactoryV2 public immutable FACTORY;

    constructor(LaunchTypesV2.FactoryInitializationV2 memory initialization) {
        LaunchFactoryV2 factory = new LaunchFactoryV2(initialization);
        IPoolManager poolManager = initialization.poolManager;
        (, bytes32 salt) = new MineGuardHook().mine(address(this), poolManager, factory);
        GraduationGuardHook hook = new GraduationGuardHook{ salt: salt }(poolManager, factory);
        LpFeeLocker locker = new LpFeeLocker(initialization.positionManager, factory);
        factory.setDependencies(address(hook), address(locker));
        FACTORY = factory;
    }
}

// Off-chain generator: the JSON loop calls `vm.toString` cheatcodes, a duplicate graduation
// event must abort generation, and setup buys only need the token amount.
// forge-lint: disable-start(calls-loop, require-revert-in-loop, unused-return)
/// @notice Produces the byte-for-byte v2 fixture consumed by the Go curve mirror.
/// @dev Every expected amount, state, graduation result and revert payload comes from the
/// deployed v2 system (factory, curve, guard hook, locker, local Uniswap v4) through its public
/// view and transaction paths. Inputs only select the coverage cases.
contract GenerateCurveVectorsV2 is Script {
    using StateLibrary for IPoolManager;

    uint256 private constant TOTAL_SUPPLY = 1_000_000_000 ether;
    uint256 private constant CURVE_TOKENS = 800_000_000 ether;
    uint256 private constant LP_TOKENS = 200_000_000 ether;
    uint256 private constant GRADUATION_ETH = 4.2 ether;
    uint256 private constant INITIAL_VIRTUAL_ETH = 1.4 ether;
    uint256 private constant INITIAL_VIRTUAL_TOKEN = 1_066_666_666_666_666_666_666_666_667;
    uint16 private constant ENGINE_VERSION = 2;
    uint16 private constant TRADE_FEE_BPS = 100;
    uint16 private constant PROTOCOL_SHARE_BPS = 5000;
    uint16 private constant SNIPE_START_BPS = 5000;
    uint32 private constant SNIPE_DURATION = 60;
    uint16 private constant LP_CREATOR_SHARE_BPS = 5000;
    uint256 private constant CASE_COUNT = 10;
    uint256 private constant GENESIS_TIMESTAMP = 1_000_000;
    uint256 private constant UNSUBSCRIBE_GAS_LIMIT = 100_000;
    string private constant POOL_MANAGER_ARTIFACT = "out-v4/PoolManager.sol/PoolManager.json";
    string private constant POSITION_MANAGER_ARTIFACT =
        "out-v4/PositionManager.sol/PositionManager.json";

    address private constant PAUSE_AUTHORITY = address(0xA0);
    address private constant TIMELOCK = address(0x71);
    address private constant TREASURY = address(0x7000);
    address private constant POOL_OWNER = address(0x0E);

    struct CurveState {
        LaunchTypes.Phase phase;
        uint256 virtualEth;
        uint256 virtualToken;
        uint256 tokensSold;
        uint256 realCurveEth;
        uint256 protocolFees;
        uint256 creatorFees;
        uint256 snipeTax;
    }

    struct CaseOutput {
        uint256 ethGross;
        uint256 ethRefund;
        uint256 ethOut;
        uint256 tokenAmount;
        uint256 protocolFee;
        uint256 creatorFee;
        uint256 snipeTax;
        uint16 taxBps;
        bool graduates;
    }

    struct GraduationOutput {
        uint256 ethToPool;
        uint256 tokensToPool;
        uint128 liquidity;
        uint160 sqrtPriceX96;
        uint256 ethRemainder;
        uint256 tokenRemainder;
    }

    struct VectorCase {
        string id;
        string operation;
        uint256 secondsSinceLaunch;
        CurveState initialState;
        uint256 inputEthGross;
        uint256 inputTokensIn;
        CaseOutput output;
        CurveState nextState;
        bool graduated;
        GraduationOutput graduation;
        bool reverted;
        string errorName;
        bytes revertData;
    }

    struct BuyQuote {
        uint256 ethGross;
        uint256 tokensOut;
        uint256 protocolFee;
        uint256 creatorFee;
        uint256 snipeTax;
        uint256 refund;
        bool graduates;
    }

    IPoolManager private _poolManager;
    LaunchFactoryV2 private _factory;
    VectorActorV2 private _actor;

    function run() external {
        vm.warp(GENESIS_TIMESTAMP);
        _deploySystem();
        _actor = new VectorActorV2();
        vm.deal(address(_actor), 1000 ether);

        VectorCase[] memory cases = new VectorCase[](CASE_COUNT);
        cases[0] = _buyCase("buy_launch_second", 0, 1 ether);
        cases[1] = _buyCase("buy_one_wei_launch_second", 0, 1);
        cases[2] = _buyCase("buy_mid_window", 30, 1 ether);
        cases[3] = _buyCase("buy_last_window_second", SNIPE_DURATION - 1, 1 ether);
        cases[4] = _buyCase("buy_after_window", SNIPE_DURATION, 1 ether);
        cases[5] = _buyCase("buy_final_in_window", 30, 20 ether);
        cases[6] = _finalExactAfterWindowCase();
        cases[7] = _buyCase("buy_final_refund_after_window", SNIPE_DURATION, 10 ether);
        cases[8] = _sellInWindowCase();
        cases[9] = _invalidBuyZeroInputCase();

        string memory outputPath =
            vm.envOr("CURVE_VECTORS_V2_OUTPUT", string("./vectors/v2/curve-v2.json"));
        vm.writeFile(outputPath, string.concat(_artifactJson(cases), "\n"));
    }

    function _buyCase(string memory id, uint256 secondsSinceLaunch, uint256 suppliedGross)
        private
        returns (VectorCase memory vector)
    {
        (IERC20 token, IBondingCurveV2 curve) = _launch();
        _warpTo(curve, secondsSinceLaunch);
        return _buyCaseOnCurve(id, token, curve, secondsSinceLaunch, suppliedGross);
    }

    function _finalExactAfterWindowCase() private returns (VectorCase memory) {
        (IERC20 token, IBondingCurveV2 curve) = _launch();
        _warpTo(curve, SNIPE_DURATION);
        BuyQuote memory boundary = _quoteBuy(curve, 10 ether);
        require(boundary.graduates && boundary.refund > 0, "FINAL_BOUNDARY_QUOTE_MISSING");
        return _buyCaseOnCurve(
            "buy_final_exact_after_window", token, curve, SNIPE_DURATION, boundary.ethGross
        );
    }

    function _buyCaseOnCurve(
        string memory id,
        IERC20 token,
        IBondingCurveV2 curve,
        uint256 secondsSinceLaunch,
        uint256 suppliedGross
    ) private returns (VectorCase memory vector) {
        vector.id = id;
        vector.operation = "buy";
        vector.secondsSinceLaunch = secondsSinceLaunch;
        vector.initialState = _state(curve);
        vector.inputEthGross = suppliedGross;

        BuyQuote memory quote = _quoteBuy(curve, suppliedGross);
        vector.output = CaseOutput({
            ethGross: quote.ethGross,
            ethRefund: quote.refund,
            ethOut: 0,
            tokenAmount: quote.tokensOut,
            protocolFee: quote.protocolFee,
            creatorFee: quote.creatorFee,
            snipeTax: quote.snipeTax,
            taxBps: curve.currentTaxBps(),
            graduates: quote.graduates
        });

        vm.recordLogs();
        (uint256 actualTokensOut, uint256 actualGrossUsed) = _actor.executeBuy(curve, suppliedGross);
        require(actualTokensOut == quote.tokensOut, "BUY_TOKEN_RESULT_MISMATCH");
        require(actualGrossUsed == quote.ethGross, "BUY_GROSS_RESULT_MISMATCH");
        (vector.graduated, vector.graduation) = _graduationFrom(vm.getRecordedLogs());

        vector.nextState = _state(curve);
        require(
            (vector.nextState.phase == LaunchTypes.Phase.Graduated) == quote.graduates,
            "BUY_PHASE_MISMATCH"
        );
        require(vector.graduated == quote.graduates, "GRADUATION_EVENT_MISMATCH");
        if (vector.graduated) _requirePoolMatches(token, vector.graduation);
    }

    function _sellInWindowCase() private returns (VectorCase memory vector) {
        (IERC20 token, IBondingCurveV2 curve) = _launch();
        (uint256 purchased,) = _actor.executeBuy(curve, 1 ether);
        _warpTo(curve, 10);
        require(curve.currentTaxBps() != 0, "SELL_NOT_IN_WINDOW");
        uint256 tokensIn = purchased / 2;

        vector.id = "sell_in_window";
        vector.operation = "sell";
        vector.secondsSinceLaunch = 10;
        vector.initialState = _state(curve);
        vector.inputTokensIn = tokensIn;

        (uint256 ethOut, uint256 ethGross, uint256 protocolFee, uint256 creatorFee) =
            curve.quoteSell(tokensIn);
        vector.output = CaseOutput({
            ethGross: ethGross,
            ethRefund: 0,
            ethOut: ethOut,
            tokenAmount: tokensIn,
            protocolFee: protocolFee,
            creatorFee: creatorFee,
            snipeTax: 0,
            taxBps: curve.currentTaxBps(),
            graduates: false
        });
        require(_actor.executeSell(token, curve, tokensIn) == ethOut, "SELL_RESULT_MISMATCH");
        vector.nextState = _state(curve);
        require(vector.nextState.snipeTax == vector.initialState.snipeTax, "SELL_WAS_TAXED");
    }

    function _invalidBuyZeroInputCase() private returns (VectorCase memory vector) {
        (, IBondingCurveV2 curve) = _launch();
        vector.id = "invalid_buy_zero_input";
        vector.operation = "buy";
        vector.initialState = _state(curve);

        (bool success, bytes memory revertData) =
            address(curve).staticcall(abi.encodeCall(IBondingCurveV2.quoteBuy, (0)));
        require(!success, "EXPECTED_REVERT_MISSING");
        require(
            keccak256(revertData)
                == keccak256(abi.encodeWithSelector(ILaunchErrors.ZeroInput.selector)),
            "REVERT_DATA_MISMATCH"
        );

        vector.nextState = _state(curve);
        vector.reverted = true;
        vector.errorName = "ZeroInput";
        vector.revertData = revertData;
    }

    function _deploySystem() private {
        // The helper etches canonical Permit2 bytecode at its canonical address.
        IAllowanceTransfer permit2 = IAllowanceTransfer(new DeployPermit2().deployPermit2());
        _poolManager = IPoolManager(deployCode(POOL_MANAGER_ARTIFACT, abi.encode(POOL_OWNER)));
        IPositionManager positionManager = IPositionManager(
            deployCode(
                POSITION_MANAGER_ARTIFACT,
                abi.encode(
                    address(_poolManager),
                    address(permit2),
                    UNSUBSCRIBE_GAS_LIMIT,
                    address(0),
                    address(0)
                )
            )
        );
        BondingCurveV2 implementation = new BondingCurveV2();
        require(implementation.ENGINE_VERSION() == ENGINE_VERSION, "ENGINE_VERSION_MISMATCH");

        VectorSystemV2 system = new VectorSystemV2(
            LaunchTypesV2.FactoryInitializationV2({
                pauseAuthority: PAUSE_AUTHORITY,
                timelock: TIMELOCK,
                protocolTreasury: TREASURY,
                engineVersion: ENGINE_VERSION,
                implementation: address(implementation),
                poolManager: _poolManager,
                positionManager: positionManager,
                permit2: permit2,
                defaults: LaunchTypesV2.FactoryDefaultsV2({
                    parameters: _parameters(),
                    antiSnipe: LaunchTypesV2.AntiSnipeProfile({
                        startBps: SNIPE_START_BPS, durationSeconds: SNIPE_DURATION
                    }),
                    lpCreatorShareBps: LP_CREATOR_SHARE_BPS,
                    launchFee: 0
                })
            })
        );
        _factory = system.FACTORY();
    }

    function _launch() private returns (IERC20 token, IBondingCurveV2 curve) {
        return _actor.launch(_factory);
    }

    function _warpTo(IBondingCurveV2 curve, uint256 secondsSinceLaunch) private {
        vm.warp(uint256(curve.launchTime()) + secondsSinceLaunch);
    }

    function _quoteBuy(IBondingCurveV2 curve, uint256 gross)
        private
        view
        returns (BuyQuote memory quote)
    {
        (
            quote.ethGross,
            quote.tokensOut,
            quote.protocolFee,
            quote.creatorFee,
            quote.snipeTax,
            quote.refund,
            quote.graduates
        ) = curve.quoteBuy(gross);
    }

    function _graduationFrom(Vm.Log[] memory logs)
        private
        pure
        returns (bool found, GraduationOutput memory graduation)
    {
        for (uint256 i = 0; i < logs.length; ++i) {
            if (logs[i].topics[0] != ILaunchEventsV2.Graduated.selector) continue;
            require(!found, "DUPLICATE_GRADUATION_EVENT");
            found = true;
            (
                ,
                graduation.ethToPool,
                graduation.tokensToPool,
                graduation.liquidity,
                graduation.sqrtPriceX96,
                graduation.ethRemainder,
                graduation.tokenRemainder
            ) =
                abi.decode(
                    logs[i].data, (uint256, uint256, uint256, uint128, uint160, uint256, uint256)
                );
        }
    }

    function _requirePoolMatches(IERC20 token, GraduationOutput memory graduation) private view {
        PoolKey memory key = PoolKey({
            currency0: Currency.wrap(address(0)),
            currency1: Currency.wrap(address(token)),
            fee: 10_000,
            tickSpacing: 200,
            hooks: IHooks(_factory.hook())
        });
        // forge-lint: disable-next-line(unused-return)
        (uint160 sqrtPriceX96,,,) = _poolManager.getSlot0(key.toId());
        require(sqrtPriceX96 == graduation.sqrtPriceX96, "POOL_PRICE_MISMATCH");
        require(
            graduation.ethToPool + graduation.ethRemainder == GRADUATION_ETH,
            "GRADUATION_ETH_NOT_CONSERVED"
        );
        require(
            graduation.tokensToPool + graduation.tokenRemainder == LP_TOKENS,
            "GRADUATION_TOKENS_NOT_CONSERVED"
        );
    }

    function _state(IBondingCurveV2 curve) private view returns (CurveState memory) {
        return CurveState({
            phase: curve.phase(),
            virtualEth: curve.virtualEthReserve(),
            virtualToken: curve.virtualTokenReserve(),
            tokensSold: curve.tokensSold(),
            realCurveEth: curve.realCurveEth(),
            protocolFees: curve.unclaimedProtocolFees(),
            creatorFees: curve.unclaimedCreatorFees(),
            snipeTax: curve.unclaimedSnipeTax()
        });
    }

    function _parameters() private pure returns (LaunchTypes.CurveParameters memory) {
        return LaunchTypes.CurveParameters({
            totalSupply: TOTAL_SUPPLY,
            curveTokens: CURVE_TOKENS,
            lpTokens: LP_TOKENS,
            graduationEth: GRADUATION_ETH,
            initialVirtualEth: INITIAL_VIRTUAL_ETH,
            initialVirtualToken: INITIAL_VIRTUAL_TOKEN,
            tradeFeeBps: TRADE_FEE_BPS,
            protocolShareBps: PROTOCOL_SHARE_BPS
        });
    }

    function _artifactJson(VectorCase[] memory cases) private pure returns (string memory json) {
        json = string.concat(
            '{"$schema":"./curve.schema.json","schemaVersion":1,"engineVersion":',
            vm.toString(uint256(ENGINE_VERSION)),
            ',"amountEncoding":"uint256-decimal-string","parameters":',
            _parametersJson(),
            ',"cases":['
        );
        for (uint256 i = 0; i < cases.length; ++i) {
            json = string.concat(json, i == 0 ? "" : ",", _caseJson(cases[i]));
        }
        return string.concat(json, "]}");
    }

    function _parametersJson() private pure returns (string memory json) {
        LaunchTypes.CurveParameters memory parameters = _parameters();
        json = string.concat(
            '{"totalSupply":"',
            vm.toString(parameters.totalSupply),
            '","curveTokens":"',
            vm.toString(parameters.curveTokens),
            '","lpTokens":"',
            vm.toString(parameters.lpTokens),
            '","graduationEth":"',
            vm.toString(parameters.graduationEth),
            '","initialVirtualEth":"',
            vm.toString(parameters.initialVirtualEth),
            '"'
        );
        json = string.concat(
            json,
            ',"initialVirtualToken":"',
            vm.toString(parameters.initialVirtualToken),
            '","tradeFeeBps":',
            vm.toString(uint256(parameters.tradeFeeBps)),
            ',"protocolShareBps":',
            vm.toString(uint256(parameters.protocolShareBps))
        );
        return string.concat(
            json,
            ',"antiSnipeStartBps":',
            vm.toString(uint256(SNIPE_START_BPS)),
            ',"antiSnipeDurationSeconds":',
            vm.toString(uint256(SNIPE_DURATION)),
            ',"lpCreatorShareBps":',
            vm.toString(uint256(LP_CREATOR_SHARE_BPS)),
            "}"
        );
    }

    function _caseJson(VectorCase memory vector) private pure returns (string memory json) {
        json = string.concat(
            '{"id":"',
            vector.id,
            '","operation":"',
            vector.operation,
            '","secondsSinceLaunch":',
            vm.toString(vector.secondsSinceLaunch),
            ',"initialState":',
            _stateJson(vector.initialState),
            ',"input":{"ethGross":"',
            vm.toString(vector.inputEthGross),
            '","tokensIn":"',
            vm.toString(vector.inputTokensIn),
            '"},"output":'
        );
        json = string.concat(
            json,
            vector.reverted ? "null" : _outputJson(vector.output),
            ',"nextState":',
            _stateJson(vector.nextState),
            ',"graduation":',
            vector.graduated ? _graduationJson(vector.graduation) : "null",
            ',"expectedRevert":',
            _revertJson(vector),
            "}"
        );
    }

    function _stateJson(CurveState memory state_) private pure returns (string memory json) {
        json = string.concat(
            '{"phase":"',
            state_.phase == LaunchTypes.Phase.Curve ? "curve" : "graduated",
            '","virtualEth":"',
            vm.toString(state_.virtualEth),
            '","virtualToken":"',
            vm.toString(state_.virtualToken),
            '","tokensSold":"',
            vm.toString(state_.tokensSold),
            '"'
        );
        return string.concat(
            json,
            ',"realCurveEth":"',
            vm.toString(state_.realCurveEth),
            '","protocolFees":"',
            vm.toString(state_.protocolFees),
            '","creatorFees":"',
            vm.toString(state_.creatorFees),
            '","snipeTax":"',
            vm.toString(state_.snipeTax),
            '"}'
        );
    }

    function _outputJson(CaseOutput memory output) private pure returns (string memory json) {
        json = string.concat(
            '{"ethGross":"',
            vm.toString(output.ethGross),
            '","ethRefund":"',
            vm.toString(output.ethRefund),
            '","ethOut":"',
            vm.toString(output.ethOut),
            '","tokenAmount":"',
            vm.toString(output.tokenAmount),
            '"'
        );
        return string.concat(
            json,
            ',"protocolFee":"',
            vm.toString(output.protocolFee),
            '","creatorFee":"',
            vm.toString(output.creatorFee),
            '","snipeTax":"',
            vm.toString(output.snipeTax),
            '","taxBps":',
            vm.toString(uint256(output.taxBps)),
            ',"graduates":',
            output.graduates ? "true" : "false",
            "}"
        );
    }

    function _graduationJson(GraduationOutput memory graduation)
        private
        pure
        returns (string memory json)
    {
        json = string.concat(
            '{"ethToPool":"',
            vm.toString(graduation.ethToPool),
            '","tokensToPool":"',
            vm.toString(graduation.tokensToPool),
            '","liquidity":"',
            vm.toString(uint256(graduation.liquidity)),
            '","sqrtPriceX96":"',
            vm.toString(uint256(graduation.sqrtPriceX96)),
            '"'
        );
        return string.concat(
            json,
            ',"ethRemainder":"',
            vm.toString(graduation.ethRemainder),
            '","tokenRemainder":"',
            vm.toString(graduation.tokenRemainder),
            '"}'
        );
    }

    function _revertJson(VectorCase memory vector) private pure returns (string memory) {
        if (!vector.reverted) return "null";
        return string.concat(
            '{"name":"', vector.errorName, '","data":"', vm.toString(vector.revertData), '"}'
        );
    }
}
// forge-lint: disable-end(calls-loop, require-revert-in-loop, unused-return)
