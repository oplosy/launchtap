// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Test } from "forge-std/Test.sol";
import { BondingCurveV1 } from "../src/BondingCurveV1.sol";
import { IBondingCurveV1 } from "../src/interfaces/IBondingCurveV1.sol";
import { ILaunchErrors } from "../src/interfaces/ILaunchErrors.sol";
import { LaunchFactory } from "../src/LaunchFactory.sol";
import { LaunchTypes } from "../src/types/LaunchTypes.sol";
import { DeployLaunchpad } from "../script/DeployLaunchpad.s.sol";
import { DeployCurveImplementation } from "../script/DeployCurveImplementation.s.sol";
import { DeploymentValidation } from "../script/deployment/DeploymentValidation.sol";
import { LocalUniswapV2Factory } from "../script/local/LocalUniswapV2Factory.sol";
import { LocalUniswapV2Pair } from "../script/local/LocalUniswapV2Pair.sol";
import { LocalWETH } from "../script/local/LocalWETH.sol";

contract DeploymentValidationHarness {
    function validateChain(DeploymentValidation.Target target, uint256 chainId) external pure {
        DeploymentValidation.validateChain(target, chainId);
    }

    function validateAuthorities(
        DeploymentValidation.Target target,
        address deployer,
        address pauseAuthority,
        address timelock,
        address protocolTreasury
    ) external pure {
        DeploymentValidation.validateAuthorities(
            target, deployer, pauseAuthority, timelock, protocolTreasury
        );
    }

    function validateDependencies(
        DeploymentValidation.Target target,
        DeploymentValidation.Dependencies memory dependencies,
        bool reviewed
    ) external returns (DeploymentValidation.DependencyEvidence memory) {
        return DeploymentValidation.validateDependencies(target, dependencies, reviewed);
    }

    function expectedPairAddress(
        address factory,
        address tokenA,
        address tokenB,
        bytes32 pairInitCodeHash
    ) external pure returns (address) {
        return DeploymentValidation.expectedPairAddress(factory, tokenA, tokenB, pairInitCodeHash);
    }
}

contract DeployLaunchpadTestHarness is DeployLaunchpad {
    function defaults(address weth, address uniswapFactory)
        external
        pure
        returns (LaunchTypes.FactoryDefaults memory)
    {
        return _defaults(weth, uniswapFactory);
    }

    function assertDeployment(DeploymentResult memory result, LaunchFactory factory) external view {
        _assertDeployment(result, factory);
    }
}

contract CanonicalPairHashFactory {
    function pairCodeHash() external pure returns (bytes32) {
        return 0x96e8ac4277198ff8b6f785478aa9a39f403cb768dd02cbee326c3e7da348845f;
    }
}

contract CodeBearingWETH { }

contract PairWithoutFactoryHashGetter { }

contract FactoryWithoutPairHashGetter {
    mapping(address token0 => mapping(address token1 => address pair)) public getPair;

    function creationCodeHash() external pure returns (bytes32) {
        return keccak256(type(PairWithoutFactoryHashGetter).creationCode);
    }

    function createPair(address tokenA, address tokenB) external returns (address pair) {
        require(tokenA != tokenB, "FallbackV2: identical addresses");
        (address token0, address token1) = tokenA < tokenB ? (tokenA, tokenB) : (tokenB, tokenA);
        require(token0 != address(0), "FallbackV2: zero address");
        require(getPair[token0][token1] == address(0), "FallbackV2: pair exists");

        bytes32 salt = keccak256(abi.encodePacked(token0, token1));
        pair = address(new PairWithoutFactoryHashGetter{ salt: salt }());
        getPair[token0][token1] = pair;
        getPair[token1][token0] = pair;
    }
}

contract DeploymentTest is Test {
    uint16 private constant ENGINE_VERSION = 1;
    bool private constant ENGINE_ENABLED = true;
    uint256 private constant TOTAL_SUPPLY = 1_000_000_000 ether;
    uint256 private constant CURVE_TOKENS = 800_000_000 ether;
    uint256 private constant LP_TOKENS = 200_000_000 ether;
    uint256 private constant GRADUATION_ETH = 4.2 ether;
    uint256 private constant INITIAL_VIRTUAL_ETH = 1.4 ether;
    uint256 private constant INITIAL_VIRTUAL_TOKEN = 1_066_666_666_666_666_666_666_666_667;
    uint256 private constant EXPECTED_LAUNCH_FEE = 0.0005 ether;
    uint16 private constant TRADE_FEE_BPS = 100;
    uint16 private constant PROTOCOL_SHARE_BPS = 5000;
    address private constant LP_BURN_ADDRESS = 0x000000000000000000000000000000000000dEaD;
    address private constant MAINNET_FACTORY = 0x8bcEaA40B9AcdfAedF85AdF4FF01F5Ad6517937f;
    address private constant MAINNET_WETH = 0x0Bd7D308f8E1639FAb988df18A8011f41EAcAD73;
    bytes32 private constant CANONICAL_PAIR_HASH =
        0x96e8ac4277198ff8b6f785478aa9a39f403cb768dd02cbee326c3e7da348845f;
    bytes32 private constant FIELD_PAUSE_AUTHORITY = "pauseAuthority";
    bytes32 private constant FIELD_WETH = "weth";
    bytes32 private constant FIELD_TOKEN_RECIPIENT = "tokenRecipient";
    address private constant PROBE_TOKEN_A = 0x0000000000000000000000000000000000001001;
    address private constant PROBE_TOKEN_B = 0x0000000000000000000000000000000000001002;

    address private constant PAUSE_AUTHORITY = address(0xA11CE);
    address private constant TIMELOCK = address(0xB0B);
    address private constant TREASURY = address(0xCAFE);
    address private constant CREATOR = address(0xC0FFEE);

    DeploymentValidationHarness private validator;

    function setUp() external {
        validator = new DeploymentValidationHarness();
    }

    function testDeploymentScriptUsesAndAssertsSpecifiedLaunchFee() external {
        DeployLaunchpadTestHarness deployment = new DeployLaunchpadTestHarness();
        LocalWETH weth = new LocalWETH();
        LocalUniswapV2Factory uniswapFactory = new LocalUniswapV2Factory();
        BondingCurveV1 implementation = new BondingCurveV1();
        LaunchTypes.FactoryDefaults memory defaults =
            deployment.defaults(address(weth), address(uniswapFactory));
        assertEq(defaults.launchFee, EXPECTED_LAUNCH_FEE);

        LaunchFactory factory = _factoryWithDefaults(address(implementation), defaults);
        assertEq(factory.launchFee(), EXPECTED_LAUNCH_FEE);

        DeployLaunchpad.DeploymentResult memory result = DeployLaunchpad.DeploymentResult({
            target: DeploymentValidation.Target.Anvil,
            deployer: address(0xD3E1),
            pauseAuthority: PAUSE_AUTHORITY,
            timelock: TIMELOCK,
            protocolTreasury: TREASURY,
            weth: address(weth),
            uniswapFactory: address(uniswapFactory),
            uniswapRouter: address(0),
            pairInitCodeHash: bytes32(0),
            wethRuntimeCodeHash: bytes32(0),
            uniswapFactoryRuntimeCodeHash: bytes32(0),
            curveImplementation: address(implementation),
            launchFactory: address(factory)
        });
        deployment.assertDeployment(result, factory);

        defaults.launchFee = 0;
        LaunchFactory zeroFeeFactory = _factoryWithDefaults(address(implementation), defaults);
        vm.expectRevert(bytes("launch fee mismatch"));
        deployment.assertDeployment(result, zeroFeeFactory);
    }

    function testAnvilStackDeploysLaunchpadAndGraduatesThroughLocalPair() external {
        (LocalWETH weth, LocalUniswapV2Factory uniswapFactory) = _validatedLocalStack();
        BondingCurveV1 implementation = new BondingCurveV1();
        LaunchFactory factory =
            _factory(address(implementation), address(weth), address(uniswapFactory));
        assertEq(factory.pauseAuthority(), PAUSE_AUTHORITY);
        assertEq(factory.timelock(), TIMELOCK);
        assertNotEq(factory.pauseAuthority(), address(this));
        assertNotEq(factory.timelock(), address(this));
        _assertLocalGraduation(factory, weth, uniswapFactory);
    }

    function testDustBuyToCanonicalPairCannotBlockLocalGraduation() external {
        (LocalWETH weth, LocalUniswapV2Factory uniswapFactory) = _validatedLocalStack();
        LaunchFactory factory =
            _factory(address(new BondingCurveV1()), address(weth), address(uniswapFactory));
        vm.prank(CREATOR);
        // Only the curve and pair are needed for this attack path.
        // forge-lint: disable-next-line(unused-return)
        (, address curveAddress, address pairAddress) = factory.launch(_request());
        IBondingCurveV1 curve = IBondingCurveV1(curveAddress);

        vm.deal(CREATOR, 6 ether);
        vm.expectRevert(
            abi.encodeWithSelector(
                ILaunchErrors.InvalidRecipient.selector, FIELD_TOKEN_RECIPIENT, pairAddress
            )
        );
        vm.prank(CREATOR);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 1 gwei }(pairAddress, CREATOR, 0, block.timestamp);

        vm.prank(CREATOR);
        // forge-lint: disable-next-line(arbitrary-send-eth, unused-return)
        curve.buy{ value: 5 ether }(CREATOR, CREATOR, 0, block.timestamp);
        assertEq(uint256(curve.phase()), uint256(LaunchTypes.Phase.Graduated));
        assertGt(LocalUniswapV2Pair(pairAddress).balanceOf(LP_BURN_ADDRESS), 0);
    }

    function testCurveImplementationUpgradeScriptSwitchesEngineAndProvesRecipientGuard() external {
        (LocalWETH weth, LocalUniswapV2Factory uniswapFactory) = _validatedLocalStack();
        address previous = address(new BondingCurveV1());
        LaunchFactory factory = _factory(previous, address(weth), address(uniswapFactory));
        vm.setEnv("DEPLOYMENT_TARGET", "anvil");
        vm.setEnv("DEPLOYER", vm.toString(CREATOR));
        vm.setEnv("LAUNCH_FACTORY", vm.toString(address(factory)));

        DeployCurveImplementation script = new DeployCurveImplementation();
        (address implementation, bytes memory configureCalldata) = script.run();

        assertNotEq(implementation, previous);
        assertGt(implementation.code.length, 0);
        assertEq(factory.curveImplementation(ENGINE_VERSION), implementation);
        assertEq(
            configureCalldata,
            abi.encodeCall(
                LaunchFactory.configureEngine, (ENGINE_VERSION, implementation, ENGINE_ENABLED)
            )
        );

        vm.setEnv("DEPLOYER", vm.toString(TIMELOCK));
        vm.expectRevert(
            abi.encodeWithSelector(DeployCurveImplementation.AuthorityOverlap.selector, TIMELOCK)
        );
        // The expected revert precedes any return value.
        // forge-lint: disable-next-line(unused-return)
        script.run();
    }

    function _validatedLocalStack()
        private
        returns (LocalWETH weth, LocalUniswapV2Factory uniswapFactory)
    {
        weth = new LocalWETH();
        uniswapFactory = new LocalUniswapV2Factory();
        bytes32 pairCodeHash = uniswapFactory.pairCodeHash();
        DeploymentValidation.DependencyEvidence memory evidence = validator.validateDependencies(
            DeploymentValidation.Target.Anvil,
            _dependencies(address(weth), address(uniswapFactory), pairCodeHash),
            false
        );
        assertEq(evidence.wethRuntimeCodeHash, address(weth).codehash);
        assertEq(evidence.uniswapFactoryRuntimeCodeHash, address(uniswapFactory).codehash);
        assertTrue(evidence.pairInitCodeHashVerified);
    }

    function _assertLocalGraduation(
        LaunchFactory factory,
        LocalWETH weth,
        LocalUniswapV2Factory uniswapFactory
    ) private {
        vm.deal(CREATOR, 6 ether);
        vm.prank(CREATOR);
        (address tokenAddress, address curveAddress, address pairAddress) =
            factory.launch(_request());
        IBondingCurveV1 curve = IBondingCurveV1(curveAddress);
        vm.prank(CREATOR);
        // The recipient is a fixed test actor, not attacker-controlled input.
        // forge-lint: disable-start(arbitrary-send-eth)
        (uint256 tokensOut, uint256 ethGrossUsed) =
            curve.buy{ value: 5 ether }(CREATOR, CREATOR, 0, block.timestamp);
        // forge-lint: disable-end(arbitrary-send-eth)

        LocalUniswapV2Pair pair = LocalUniswapV2Pair(pairAddress);
        assertEq(curve.token(), tokenAddress);
        assertGt(tokensOut, 0);
        assertGt(ethGrossUsed, 0);
        assertEq(uint256(curve.phase()), uint256(LaunchTypes.Phase.Graduated));
        assertGt(pair.totalSupply(), 0);
        assertGt(pair.balanceOf(LP_BURN_ADDRESS), 0);
        assertEq(uniswapFactory.getPair(curve.token(), address(weth)), pairAddress);
    }

    function testTestnetRejectsMainnetDependencies() external {
        DeploymentValidation.Dependencies memory dependencies =
            _dependencies(MAINNET_WETH, address(new LocalUniswapV2Factory()), CANONICAL_PAIR_HASH);
        vm.expectRevert(
            abi.encodeWithSelector(
                DeploymentValidation.TestnetUsesMainnetDependency.selector, FIELD_WETH, MAINNET_WETH
            )
        );
        // forge-lint: disable-start(unused-return)
        validator.validateDependencies(
            DeploymentValidation.Target.RobinhoodTestnet, dependencies, true
        );
        // forge-lint: disable-end(unused-return)
    }

    function testExternalDependenciesRequireExplicitReview() external {
        LocalWETH weth = new LocalWETH();
        LocalUniswapV2Factory factory = new LocalUniswapV2Factory();
        bytes32 pairCodeHash = factory.pairCodeHash();
        vm.expectRevert(DeploymentValidation.DependencyReviewRequired.selector);
        // forge-lint: disable-start(unused-return)
        validator.validateDependencies(
            DeploymentValidation.Target.RobinhoodTestnet,
            _dependencies(address(weth), address(factory), pairCodeHash),
            false
        );
        // forge-lint: disable-end(unused-return)
    }

    function testPairInitCodeHashMismatchFailsClosed() external {
        LocalWETH weth = new LocalWETH();
        LocalUniswapV2Factory factory = new LocalUniswapV2Factory();
        bytes32 incorrect = bytes32(uint256(factory.pairCodeHash()) + 1);
        vm.expectRevert(
            abi.encodeWithSelector(
                DeploymentValidation.InvalidPairInitCodeHash.selector,
                incorrect,
                factory.pairCodeHash()
            )
        );
        // forge-lint: disable-start(unused-return)
        validator.validateDependencies(
            DeploymentValidation.Target.Anvil,
            _dependencies(address(weth), address(factory), incorrect),
            false
        );
        // forge-lint: disable-end(unused-return)
    }

    function testPairInitCodeHashFallbackAcceptsCreate2Match() external {
        LocalWETH weth = new LocalWETH();
        FactoryWithoutPairHashGetter factory = new FactoryWithoutPairHashGetter();
        bytes32 pairInitCodeHash = factory.creationCodeHash();

        DeploymentValidation.DependencyEvidence memory evidence = validator.validateDependencies(
            DeploymentValidation.Target.Anvil,
            _dependencies(address(weth), address(factory), pairInitCodeHash),
            false
        );

        assertTrue(evidence.pairInitCodeHashVerified);
        assertEq(
            factory.getPair(PROBE_TOKEN_A, PROBE_TOKEN_B),
            validator.expectedPairAddress(
                address(factory), PROBE_TOKEN_A, PROBE_TOKEN_B, pairInitCodeHash
            )
        );
    }

    function testPairInitCodeHashFallbackRejectsCreate2Mismatch() external {
        LocalWETH weth = new LocalWETH();
        FactoryWithoutPairHashGetter factory = new FactoryWithoutPairHashGetter();
        bytes32 actualPairInitCodeHash = factory.creationCodeHash();
        bytes32 incorrectPairInitCodeHash = bytes32(uint256(actualPairInitCodeHash) + 1);
        address expected = validator.expectedPairAddress(
            address(factory), PROBE_TOKEN_A, PROBE_TOKEN_B, incorrectPairInitCodeHash
        );
        address actual = validator.expectedPairAddress(
            address(factory), PROBE_TOKEN_A, PROBE_TOKEN_B, actualPairInitCodeHash
        );

        vm.expectRevert(
            abi.encodeWithSelector(
                DeploymentValidation.PairAddressMismatch.selector, expected, actual
            )
        );
        // forge-lint: disable-start(unused-return)
        validator.validateDependencies(
            DeploymentValidation.Target.Anvil,
            _dependencies(address(weth), address(factory), incorrectPairInitCodeHash),
            false
        );
        // forge-lint: disable-end(unused-return)
    }

    function testReviewedRuntimeCodeHashMismatchFailsClosed() external {
        LocalWETH weth = new LocalWETH();
        LocalUniswapV2Factory factory = new LocalUniswapV2Factory();
        DeploymentValidation.Dependencies memory dependencies =
            _dependencies(address(weth), address(factory), factory.pairCodeHash());
        dependencies.expectedWethRuntimeCodeHash = bytes32(uint256(address(weth).codehash) + 1);
        vm.expectRevert(
            abi.encodeWithSelector(
                DeploymentValidation.DependencyRuntimeCodeHashMismatch.selector,
                FIELD_WETH,
                dependencies.expectedWethRuntimeCodeHash,
                address(weth).codehash
            )
        );
        // forge-lint: disable-start(unused-return)
        validator.validateDependencies(
            DeploymentValidation.Target.RobinhoodTestnet, dependencies, true
        );
        // forge-lint: disable-end(unused-return)
    }

    function testProductionCandidateUsesFinalAuthoritiesAndLeavesNoDeployerAuthority() external {
        CanonicalPairHashFactory canonicalFactory = new CanonicalPairHashFactory();
        CodeBearingWETH codeBearingWeth = new CodeBearingWETH();
        vm.etch(MAINNET_FACTORY, address(canonicalFactory).code);
        vm.etch(MAINNET_WETH, address(codeBearingWeth).code);

        DeploymentValidation.Dependencies memory dependencies =
            _dependencies(MAINNET_WETH, MAINNET_FACTORY, CANONICAL_PAIR_HASH);
        dependencies.expectedWethRuntimeCodeHash = MAINNET_WETH.codehash;
        dependencies.expectedUniswapFactoryRuntimeCodeHash = MAINNET_FACTORY.codehash;
        DeploymentValidation.DependencyEvidence memory evidence = validator.validateDependencies(
            DeploymentValidation.Target.RobinhoodMainnet, dependencies, true
        );
        assertTrue(evidence.pairInitCodeHashVerified);
        validator.validateAuthorities(
            DeploymentValidation.Target.RobinhoodMainnet,
            address(this),
            PAUSE_AUTHORITY,
            TIMELOCK,
            TREASURY
        );

        LaunchFactory factory =
            _factory(address(new BondingCurveV1()), MAINNET_WETH, MAINNET_FACTORY);
        assertEq(factory.pauseAuthority(), PAUSE_AUTHORITY);
        assertEq(factory.timelock(), TIMELOCK);
        assertEq(factory.protocolTreasury(), TREASURY);
        assertNotEq(factory.pauseAuthority(), address(this));
        assertNotEq(factory.timelock(), address(this));
        (bool pauseSuccess,) =
            address(factory).call(abi.encodeCall(LaunchFactory.setTradingPaused, (true)));
        (bool configSuccess,) =
            address(factory).call(abi.encodeCall(LaunchFactory.setFutureTreasury, (address(0xFEE))));
        assertFalse(pauseSuccess);
        assertFalse(configSuccess);
    }

    function testChainAndAuthorityMismatchesFailClosed() external {
        vm.expectRevert(
            abi.encodeWithSelector(
                DeploymentValidation.ChainIdMismatch.selector, uint256(46_630), uint256(31_337)
            )
        );
        validator.validateChain(DeploymentValidation.Target.RobinhoodTestnet, 31_337);

        vm.expectRevert(
            abi.encodeWithSelector(
                DeploymentValidation.AuthorityIsDeployer.selector,
                FIELD_PAUSE_AUTHORITY,
                address(this)
            )
        );
        validator.validateAuthorities(
            DeploymentValidation.Target.RobinhoodMainnet,
            address(this),
            address(this),
            TIMELOCK,
            TREASURY
        );
    }

    function _factory(address implementation, address weth, address uniswapFactory)
        private
        returns (LaunchFactory)
    {
        return new LaunchFactory(
            LaunchTypes.FactoryInitialization({
                pauseAuthority: PAUSE_AUTHORITY,
                timelock: TIMELOCK,
                protocolTreasury: TREASURY,
                engineVersion: ENGINE_VERSION,
                implementation: implementation,
                defaults: LaunchTypes.FactoryDefaults({
                    parameters: _parameters(),
                    weth: weth,
                    uniswapFactory: uniswapFactory,
                    launchFee: 0
                })
            })
        );
    }

    function _factoryWithDefaults(
        address implementation,
        LaunchTypes.FactoryDefaults memory defaults
    ) private returns (LaunchFactory) {
        return new LaunchFactory(
            LaunchTypes.FactoryInitialization({
                pauseAuthority: PAUSE_AUTHORITY,
                timelock: TIMELOCK,
                protocolTreasury: TREASURY,
                engineVersion: ENGINE_VERSION,
                implementation: implementation,
                defaults: defaults
            })
        );
    }

    function _dependencies(address weth, address factory, bytes32 pairInitCodeHash)
        private
        pure
        returns (DeploymentValidation.Dependencies memory)
    {
        return DeploymentValidation.Dependencies({
            weth: weth,
            uniswapFactory: factory,
            pairInitCodeHash: pairInitCodeHash,
            expectedWethRuntimeCodeHash: bytes32(0),
            expectedUniswapFactoryRuntimeCodeHash: bytes32(0)
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

    function _request() private view returns (LaunchTypes.LaunchRequest memory) {
        return LaunchTypes.LaunchRequest({
            name: "Local Token",
            symbol: "LOCAL",
            engineVersion: ENGINE_VERSION,
            developerBuyGross: 0,
            minDeveloperTokensOut: 0,
            deadline: block.timestamp
        });
    }
}
