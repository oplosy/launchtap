// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Script } from "forge-std/Script.sol";
import { console2 } from "forge-std/console2.sol";

/// @dev The read-back surface of the deployed v4 contracts: `ImmutableState.poolManager`,
/// `Permit2Forwarder.permit2` and `NativeWrapper.WETH9`.
interface IV4Wiring {
    function poolManager() external view returns (address);
    function permit2() external view returns (address);
    // forge-lint: disable-next-line(mixed-case-function)
    function WETH9() external view returns (address);
}

/// @notice Deploys a Uniswap v4 stack on Robinhood testnet so engine 2 can be rehearsed on a live
/// chain (`docs/specs/2026-11-18-v2-testnet-path-preflight.md`).
/// @dev Testnet only (46630). It deploys `PoolManager` (owned by the deployer), `PositionManager`
/// and `V4Quoter` from the pinned `out-v4` build, and Universal Router 2.1.2 from `out-router`. The
/// router config matches `DeployLocalV4Periphery`, which the web gate tests: every protocol the
/// router does not use here maps to one `UnsupportedProtocol`. The canonical Permit2 already on
/// testnet is reused; `scripts/bootstrap-testnet-v4.ps1` verifies its runtime code before this
/// runs. The resulting record is a candidate until a human reviews it.
contract DeployTestnetV4 is Script {
    uint256 private constant ROBINHOOD_TESTNET = 46_630;
    address private constant PERMIT2 = 0x000000000022D473030F116dDEE9F6B43aC78BA3;
    uint256 private constant UNSUBSCRIBE_GAS_LIMIT = 100_000;
    string private constant TESTNET_DEPENDENCIES = "deployments/config/robinhood-testnet.json";
    string private constant POOL_MANAGER_ARTIFACT = "out-v4/PoolManager.sol/PoolManager.json";
    string private constant POSITION_MANAGER_ARTIFACT =
        "out-v4/PositionManager.sol/PositionManager.json";
    string private constant QUOTER_ARTIFACT = "out-v4/V4Quoter.sol/V4Quoter.json";
    string private constant ROUTER_ARTIFACT = "out-router/UniversalRouter.sol/UniversalRouter.json";
    string private constant UNSUPPORTED_ARTIFACT =
        "out-router/UnsupportedProtocol.sol/UnsupportedProtocol.json";

    /// @dev Field order of upstream `RouterParameters` (contracts/types/RouterParameters.sol).
    struct RouterParameters {
        address permit2;
        address weth9;
        address v2Factory;
        address v3Factory;
        bytes32 pairInitCodeHash;
        bytes32 poolInitCodeHash;
        address v4PoolManager;
        address v3NFTPositionManager;
        address v4PositionManager;
        address spokePool;
    }

    struct V4Stack {
        address poolManager;
        address positionManager;
        address v4Quoter;
        address unsupported;
        address universalRouter;
        address weth9;
    }

    function run() external returns (V4Stack memory stack) {
        address deployer = vm.envAddress("DEPLOYER");
        stack = _deployCore(deployer);
        _deployRouter(deployer, stack);

        // Machine-read by scripts/bootstrap-testnet-v4.ps1.
        console2.log("POOL_MANAGER", stack.poolManager);
        console2.log("POSITION_MANAGER", stack.positionManager);
        console2.log("V4_QUOTER", stack.v4Quoter);
        console2.log("UNIVERSAL_ROUTER", stack.universalRouter);
    }

    /// @dev PoolManager, PositionManager and V4Quoter: everything engine 2 and the quote path need.
    function _deployCore(address deployer) internal returns (V4Stack memory stack) {
        require(
            block.chainid == ROBINHOOD_TESTNET,
            "testnet v4 stack deploys only on Robinhood testnet (46630)"
        );
        require(deployer != address(0), "deployer is the zero address");
        require(PERMIT2.code.length != 0, "Permit2 is not deployed at its canonical address");
        stack.weth9 = _reviewedWeth();

        vm.startBroadcast(deployer);
        stack.poolManager = deployCode(POOL_MANAGER_ARTIFACT, abi.encode(deployer));
        stack.positionManager = deployCode(
            POSITION_MANAGER_ARTIFACT,
            abi.encode(stack.poolManager, PERMIT2, UNSUBSCRIBE_GAS_LIMIT, address(0), stack.weth9)
        );
        stack.v4Quoter = deployCode(QUOTER_ARTIFACT, abi.encode(stack.poolManager));
        vm.stopBroadcast();

        IV4Wiring positionManager = IV4Wiring(stack.positionManager);
        require(positionManager.poolManager() == stack.poolManager, "PositionManager pool manager");
        require(positionManager.permit2() == PERMIT2, "PositionManager Permit2");
        require(positionManager.WETH9() == stack.weth9, "PositionManager WETH9");
        require(
            IV4Wiring(stack.v4Quoter).poolManager() == stack.poolManager, "V4Quoter pool manager"
        );
    }

    function _deployRouter(address deployer, V4Stack memory stack) internal {
        vm.startBroadcast(deployer);
        stack.unsupported = deployCode(UNSUPPORTED_ARTIFACT);
        stack.universalRouter = deployCode(
            ROUTER_ARTIFACT,
            abi.encode(
                RouterParameters({
                    permit2: PERMIT2,
                    weth9: stack.unsupported,
                    v2Factory: stack.unsupported,
                    v3Factory: stack.unsupported,
                    pairInitCodeHash: bytes32(0),
                    poolInitCodeHash: bytes32(0),
                    v4PoolManager: stack.poolManager,
                    v3NFTPositionManager: stack.unsupported,
                    v4PositionManager: stack.positionManager,
                    spokePool: stack.unsupported
                })
            )
        );
        vm.stopBroadcast();

        require(
            IV4Wiring(stack.universalRouter).poolManager() == stack.poolManager,
            "UniversalRouter pool manager"
        );
    }

    /// @return weth The WETH of the reviewed testnet dependency record, with its pinned code.
    function _reviewedWeth() internal view virtual returns (address weth) {
        string memory json = vm.readFile(TESTNET_DEPENDENCIES);
        require(vm.parseJsonUint(json, ".chainId") == ROBINHOOD_TESTNET, "testnet record chain");
        require(vm.parseJsonBool(json, ".reviewed"), "testnet dependency record not reviewed");
        weth = vm.parseJsonAddress(json, ".weth");
        require(weth.code.length != 0, "reviewed testnet WETH has no runtime code");
        require(
            weth.codehash == vm.parseJsonBytes32(json, ".bytecodeHashes.weth"),
            "reviewed testnet WETH runtime code hash mismatch"
        );
    }
}
