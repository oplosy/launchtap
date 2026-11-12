// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { IERC721 } from "@openzeppelin/contracts/token/ERC721/IERC721.sol";
import { StateLibrary } from "@uniswap/v4-core/src/libraries/StateLibrary.sol";
import { PoolId } from "@uniswap/v4-core/src/types/PoolId.sol";
import { LaunchTypes } from "../../src/types/LaunchTypes.sol";
import { IBondingCurveV2 } from "../../src/v2/interfaces/IBondingCurveV2.sol";
import { V2Handler } from "./handlers/V2Handler.sol";
import { V2SystemBase } from "./utils/V2SystemBase.sol";

// Invariant views read pool state field by field and sum refunds over the fixed actor set.
// forge-lint: disable-start(unused-return, calls-loop)
contract V2InvariantTest is V2SystemBase {
    using StateLibrary for *;

    IBondingCurveV2 internal curve;
    IERC20 internal token;
    PoolId internal poolId;
    V2Handler internal handler;

    function setUp() public {
        deployV2System();
        (address tokenAddress, address curveAddress) = launchToken("INV");
        curve = IBondingCurveV2(curveAddress);
        token = IERC20(tokenAddress);
        poolId = poolKeyOf(tokenAddress).toId();

        address[] memory actors = new address[](4);
        actors[0] = CREATOR;
        actors[1] = address(0xA11CE);
        actors[2] = address(0xB0B);
        actors[3] = address(0xCA401);
        handler = new V2Handler(
            curve, poolManager, positionManager, locker, poolKeyOf(tokenAddress), TREASURY, actors
        );

        bytes4[] memory selectors = new bytes4[](12);
        selectors[0] = V2Handler.buy.selector;
        selectors[1] = V2Handler.sell.selector;
        selectors[2] = V2Handler.graduate.selector;
        selectors[3] = V2Handler.claimCreatorFees.selector;
        selectors[4] = V2Handler.claimTreasury.selector;
        selectors[5] = V2Handler.changeFeeRecipient.selector;
        selectors[6] = V2Handler.swapEthForToken.selector;
        selectors[7] = V2Handler.swapTokenForEth.selector;
        selectors[8] = V2Handler.collectLpFees.selector;
        selectors[9] = V2Handler.claimLpFees.selector;
        selectors[10] = V2Handler.initializeFromOutside.selector;
        selectors[11] = V2Handler.forceEthIntoCurve.selector;
        targetContract(address(handler));
        targetSelector(FuzzSelector({ addr: address(handler), selectors: selectors }));
    }

    /// @dev Guards against a handler whose bounds make every interesting path a no-op.
    function test_handlerReachesEveryPhase() public {
        handler.initializeFromOutside(0, 1);
        handler.buy(1, 0.5 ether, 0);
        handler.sell(1, type(uint256).max);
        handler.buy(2, 1 ether, 0);
        handler.changeFeeRecipient(2);
        handler.claimCreatorFees(0);
        handler.forceEthIntoCurve(1 ether);
        handler.graduate(3);
        handler.swapEthForToken(1, 0.5 ether);
        handler.swapTokenForEth(3, 1e24);
        handler.collectLpFees();
        handler.claimLpFees(2, false);
        handler.claimLpFees(0, true);
        handler.claimTreasury(true);
        handler.claimTreasury(false);
        handler.initializeFromOutside(0, 1);

        assertEq(handler.graduations(), 1);
        assertEq(handler.poolSwaps(), 2);
        assertGt(handler.taxedBuys(), 0);
        assertGt(handler.lastLiquidity(), 0);
        invariant_curveEthIsFullyAccounted();
        invariant_lockerLiquidityNeverDecreases();
        invariant_poolOpensOnlyThroughCurve();
        invariant_supplyIsFixed();
    }

    function invariant_curveEthIsFullyAccounted() public view {
        uint256 required = curve.phase() == LaunchTypes.Phase.Curve ? curve.realCurveEth() : 0;
        required += curve.unclaimedCreatorFees();
        required += curve.unclaimedProtocolFees();
        required += curve.unclaimedSnipeTax();
        address[] memory actors = handler.actors();
        for (uint256 i = 0; i < actors.length; ++i) {
            required += curve.pendingRefund(actors[i]);
        }
        assertEq(address(curve).balance, required + handler.forcedEth());
    }

    function invariant_lockerLiquidityNeverDecreases() public view {
        assertFalse(handler.liquidityDecreased());
        if (curve.phase() == LaunchTypes.Phase.Graduated) {
            uint256 positionId = locker.positionOf(address(token));
            assertEq(IERC721(address(positionManager)).ownerOf(positionId), address(locker));
            assertEq(positionManager.getPositionLiquidity(positionId), handler.lastLiquidity());
        }
    }

    function invariant_poolOpensOnlyThroughCurve() public view {
        assertFalse(handler.foreignInitializeSucceeded());
        (uint160 sqrtPrice,,,) = poolManager.getSlot0(poolId);
        assertEq(sqrtPrice != 0, curve.phase() == LaunchTypes.Phase.Graduated);
    }

    function invariant_supplyIsFixed() public view {
        assertEq(token.totalSupply(), TOTAL_SUPPLY);
    }
}
// forge-lint: disable-end(unused-return, calls-loop)
