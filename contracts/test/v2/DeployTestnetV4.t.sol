// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { Test } from "forge-std/Test.sol";
import { DeployPermit2 } from "permit2/test/utils/DeployPermit2.sol";
import { LocalWETH } from "../../script/local/LocalWETH.sol";
import { DeployTestnetV4, IV4Wiring } from "../../script/v2/DeployTestnetV4.s.sol";

interface IOwned {
    function owner() external view returns (address);
}

/// @dev Exposes the core step and swaps the reviewed-record WETH for a local one, since the
/// reviewed testnet WETH has no code in a test chain.
contract DeployTestnetV4Harness is DeployTestnetV4 {
    address internal weth;

    constructor(address weth_) {
        require(weth_ != address(0), "weth is the zero address");
        weth = weth_;
    }

    function deployCore(address deployer) external returns (V4Stack memory) {
        return _deployCore(deployer);
    }

    function _reviewedWeth() internal view override returns (address) {
        return weth;
    }
}

/// @notice The testnet v4 stack deploys only on Robinhood testnet, only next to the canonical
/// Permit2 and the reviewed testnet WETH, and leaves PositionManager and V4Quoter wired to its
/// own PoolManager. The router step (`out-router`) runs in the testnet `forge script`
/// simulation and the web gate, not here.
// The deploy result is irrelevant in the refusal tests: every call is expected to revert.
// forge-lint: disable-start(unused-return)
contract DeployTestnetV4Test is Test {
    address internal constant DEPLOYER = address(0xDE91);
    address internal constant PERMIT2 = 0x000000000022D473030F116dDEE9F6B43aC78BA3;

    function setUp() public {
        vm.setEnv("DEPLOYER", vm.toString(DEPLOYER));
    }

    function test_refusesOtherChains() public {
        DeployTestnetV4 deployScript = new DeployTestnetV4();
        vm.expectRevert(bytes("testnet v4 stack deploys only on Robinhood testnet (46630)"));
        deployScript.run();

        vm.chainId(4663);
        vm.expectRevert(bytes("testnet v4 stack deploys only on Robinhood testnet (46630)"));
        deployScript.run();
    }

    function test_refusesMissingPermit2() public {
        DeployTestnetV4 deployScript = new DeployTestnetV4();
        vm.chainId(46_630);
        vm.expectRevert(bytes("Permit2 is not deployed at its canonical address"));
        deployScript.run();
    }

    function test_requiresReviewedWethCode() public {
        new DeployPermit2().deployPermit2();
        DeployTestnetV4 deployScript = new DeployTestnetV4();
        vm.chainId(46_630);
        // The reviewed record's WETH address has no code on a test chain.
        vm.expectRevert(bytes("reviewed testnet WETH has no runtime code"));
        deployScript.run();
    }

    function test_deploysWiredCoreStack() public {
        new DeployPermit2().deployPermit2();
        address weth = address(new LocalWETH());
        DeployTestnetV4Harness harness = new DeployTestnetV4Harness(weth);
        vm.chainId(46_630);

        DeployTestnetV4.V4Stack memory stack = harness.deployCore(DEPLOYER);

        assertTrue(stack.poolManager.code.length != 0, "pool manager code");
        assertEq(IOwned(stack.poolManager).owner(), DEPLOYER, "pool manager owner");
        assertEq(IV4Wiring(stack.positionManager).poolManager(), stack.poolManager);
        assertEq(IV4Wiring(stack.positionManager).permit2(), PERMIT2);
        assertEq(IV4Wiring(stack.positionManager).WETH9(), weth);
        assertEq(IV4Wiring(stack.v4Quoter).poolManager(), stack.poolManager);
        assertEq(stack.weth9, weth);
        assertEq(stack.universalRouter, address(0), "router is a separate step");
    }
}
// forge-lint: disable-end(unused-return)
