// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Script } from "forge-std/Script.sol";
import { console2 } from "forge-std/console2.sol";

/// @notice Deploys Universal Router 2.1.2 and V4Quoter next to a local v2 stack for the Anvil v2
/// web gate (Plan v2-C D1, Task 9).
/// @dev Anvil only (31337), and only after `DeployLocalV2`. Both come from test-only builds
/// (`out-router`, `out-v4`) and are never part of a reviewed deployment. As in the upstream
/// deploy script, every protocol the router does not use here (WETH9, v2, v3, the v3 position
/// manager, Across) maps to one `UnsupportedProtocol`, which reverts on any call.
contract DeployLocalV4Periphery is Script {
    uint256 private constant ANVIL = 31_337;
    address private constant PERMIT2 = 0x000000000022D473030F116dDEE9F6B43aC78BA3;
    string private constant ROUTER_ARTIFACT = "out-router/UniversalRouter.sol/UniversalRouter.json";
    string private constant UNSUPPORTED_ARTIFACT =
        "out-router/UnsupportedProtocol.sol/UnsupportedProtocol.json";
    string private constant QUOTER_ARTIFACT = "out-v4/V4Quoter.sol/V4Quoter.json";

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

    function run()
        external
        returns (address unsupported, address universalRouter, address v4Quoter)
    {
        require(block.chainid == ANVIL, "local V4 periphery deployment runs only on Anvil");
        require(PERMIT2.code.length != 0, "Permit2 is not deployed at its canonical address");
        address deployer = vm.envAddress("DEPLOYER");
        address poolManager = vm.envAddress("POOL_MANAGER");
        address positionManager = vm.envAddress("POSITION_MANAGER");
        require(poolManager.code.length != 0, "PoolManager has no code");
        require(positionManager.code.length != 0, "PositionManager has no code");

        vm.startBroadcast(deployer);
        unsupported = deployCode(UNSUPPORTED_ARTIFACT);
        universalRouter = deployCode(
            ROUTER_ARTIFACT,
            abi.encode(
                RouterParameters({
                    permit2: PERMIT2,
                    weth9: unsupported,
                    v2Factory: unsupported,
                    v3Factory: unsupported,
                    pairInitCodeHash: bytes32(0),
                    poolInitCodeHash: bytes32(0),
                    v4PoolManager: poolManager,
                    v3NFTPositionManager: unsupported,
                    v4PositionManager: positionManager,
                    spokePool: unsupported
                })
            )
        );
        v4Quoter = deployCode(QUOTER_ARTIFACT, abi.encode(poolManager));
        vm.stopBroadcast();

        // Machine-read by scripts/deploy-v2-local.ps1.
        console2.log("UNIVERSAL_ROUTER", universalRouter);
        console2.log("V4_QUOTER", v4Quoter);
    }
}
