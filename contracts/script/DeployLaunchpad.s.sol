// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Script } from "forge-std/Script.sol";
import { BondingCurveV1 } from "../src/BondingCurveV1.sol";
import { LaunchFactory } from "../src/LaunchFactory.sol";
import { LaunchTypes } from "../src/types/LaunchTypes.sol";
import { DeploymentValidation } from "./deployment/DeploymentValidation.sol";
import { LocalUniswapV2Factory } from "./local/LocalUniswapV2Factory.sol";
import { LocalWETH } from "./local/LocalWETH.sol";
import { LocalUniswapV2Router } from "./local/LocalUniswapV2Router.sol";

contract DeployLaunchpad is Script {
    uint16 private constant ENGINE_VERSION = 1;
    uint256 private constant TOTAL_SUPPLY = 1_000_000_000 ether;
    uint256 private constant CURVE_TOKENS = 800_000_000 ether;
    uint256 private constant LP_TOKENS = 200_000_000 ether;
    uint256 private constant GRADUATION_ETH = 4.2 ether;
    uint256 private constant INITIAL_VIRTUAL_ETH = 1.4 ether;
    uint256 private constant INITIAL_VIRTUAL_TOKEN = 1_066_666_666_666_666_666_666_666_667;
    uint16 private constant TRADE_FEE_BPS = 100;
    // Creator keeps 70% of the 1% curve fee (0.70%); protocol keeps 30% (0.30%).
    uint16 private constant PROTOCOL_SHARE_BPS = 3000;
    uint256 private constant LAUNCH_FEE = 0.0005 ether;

    struct DeploymentResult {
        DeploymentValidation.Target target;
        address deployer;
        address pauseAuthority;
        address timelock;
        address protocolTreasury;
        address weth;
        address uniswapFactory;
        address uniswapRouter;
        bytes32 pairInitCodeHash;
        bytes32 wethRuntimeCodeHash;
        bytes32 uniswapFactoryRuntimeCodeHash;
        address curveImplementation;
        address launchFactory;
    }

    function run() external returns (DeploymentResult memory result) {
        result.target = _target(vm.envString("DEPLOYMENT_TARGET"));
        result.deployer = vm.envAddress("DEPLOYER");
        result.pauseAuthority = vm.envAddress("PAUSE_AUTHORITY");
        result.timelock = vm.envAddress("TIMELOCK");
        result.protocolTreasury = vm.envAddress("PROTOCOL_TREASURY");

        DeploymentValidation.validateChain(result.target, block.chainid);
        DeploymentValidation.validateAuthorities(
            result.target,
            result.deployer,
            result.pauseAuthority,
            result.timelock,
            result.protocolTreasury
        );

        if (result.target == DeploymentValidation.Target.Anvil) {
            vm.startBroadcast(result.deployer);
            LocalWETH localWeth = new LocalWETH();
            LocalUniswapV2Factory localFactory = new LocalUniswapV2Factory();
            LocalUniswapV2Router localRouter =
                new LocalUniswapV2Router(address(localFactory), address(localWeth));
            vm.stopBroadcast();
            result.weth = address(localWeth);
            result.uniswapFactory = address(localFactory);
            result.uniswapRouter = address(localRouter);
            result.pairInitCodeHash = localFactory.pairCodeHash();
        } else {
            result.weth = vm.envAddress("WETH");
            result.uniswapFactory = vm.envAddress("UNISWAP_V2_FACTORY");
            result.pairInitCodeHash = vm.envBytes32("PAIR_INIT_CODE_HASH");
            result.uniswapRouter = address(0);
        }

        DeploymentValidation.DependencyEvidence memory evidence =
            DeploymentValidation.validateDependencies(
                result.target,
                DeploymentValidation.Dependencies({
                    weth: result.weth,
                    uniswapFactory: result.uniswapFactory,
                    pairInitCodeHash: result.pairInitCodeHash,
                    expectedWethRuntimeCodeHash: result.target == DeploymentValidation.Target.Anvil
                        ? bytes32(0)
                        : vm.envBytes32("EXPECTED_WETH_RUNTIME_CODE_HASH"),
                    expectedUniswapFactoryRuntimeCodeHash: result.target
                        == DeploymentValidation.Target.Anvil
                        ? bytes32(0)
                        : vm.envBytes32("EXPECTED_UNISWAP_FACTORY_RUNTIME_CODE_HASH")
                }),
                result.target == DeploymentValidation.Target.Anvil
                    || vm.envBool("DEPENDENCIES_REVIEWED")
            );
        result.wethRuntimeCodeHash = evidence.wethRuntimeCodeHash;
        result.uniswapFactoryRuntimeCodeHash = evidence.uniswapFactoryRuntimeCodeHash;
        require(evidence.pairInitCodeHashVerified, "pair init-code hash not verified");

        vm.startBroadcast(result.deployer);
        BondingCurveV1 implementation = new BondingCurveV1();
        LaunchFactory factory = new LaunchFactory(
            LaunchTypes.FactoryInitialization({
                pauseAuthority: result.pauseAuthority,
                timelock: result.timelock,
                protocolTreasury: result.protocolTreasury,
                engineVersion: ENGINE_VERSION,
                implementation: address(implementation),
                defaults: _defaults(result.weth, result.uniswapFactory)
            })
        );
        vm.stopBroadcast();

        result.curveImplementation = address(implementation);
        result.launchFactory = address(factory);
        _assertDeployment(result, factory);
    }

    function _assertDeployment(DeploymentResult memory result, LaunchFactory factory)
        internal
        view
    {
        require(factory.pauseAuthority() == result.pauseAuthority, "pause authority mismatch");
        require(factory.timelock() == result.timelock, "timelock mismatch");
        require(factory.protocolTreasury() == result.protocolTreasury, "protocol treasury mismatch");
        require(
            factory.curveImplementation(ENGINE_VERSION) == result.curveImplementation,
            "engine mismatch"
        );
        require(factory.engineEnabled(ENGINE_VERSION), "engine disabled");
        require(factory.weth() == result.weth, "WETH mismatch");
        require(factory.uniswapFactory() == result.uniswapFactory, "factory mismatch");
        require(factory.launchFee() == LAUNCH_FEE, "launch fee mismatch");
        require(
            factory.futureDefaults().parameters.protocolShareBps == PROTOCOL_SHARE_BPS,
            "protocol share mismatch"
        );
        require(factory.pauseAuthority() != result.deployer, "deployer retains pause authority");
        require(factory.timelock() != result.deployer, "deployer retains timelock authority");
    }

    function _defaults(address weth, address uniswapFactory)
        internal
        pure
        returns (LaunchTypes.FactoryDefaults memory)
    {
        return LaunchTypes.FactoryDefaults({
            parameters: LaunchTypes.CurveParameters({
                totalSupply: TOTAL_SUPPLY,
                curveTokens: CURVE_TOKENS,
                lpTokens: LP_TOKENS,
                graduationEth: GRADUATION_ETH,
                initialVirtualEth: INITIAL_VIRTUAL_ETH,
                initialVirtualToken: INITIAL_VIRTUAL_TOKEN,
                tradeFeeBps: TRADE_FEE_BPS,
                protocolShareBps: PROTOCOL_SHARE_BPS
            }),
            weth: weth,
            uniswapFactory: uniswapFactory,
            launchFee: LAUNCH_FEE
        });
    }

    function _target(string memory value) private pure returns (DeploymentValidation.Target) {
        bytes32 target = keccak256(bytes(value));
        if (target == keccak256("anvil")) return DeploymentValidation.Target.Anvil;
        if (target == keccak256("robinhood-testnet")) {
            return DeploymentValidation.Target.RobinhoodTestnet;
        }
        if (target == keccak256("robinhood-mainnet")) {
            return DeploymentValidation.Target.RobinhoodMainnet;
        }
        revert("unknown deployment target");
    }
}
