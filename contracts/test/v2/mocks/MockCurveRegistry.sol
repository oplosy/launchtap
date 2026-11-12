// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { ICurveRegistry } from "../../../src/v2/interfaces/ICurveRegistry.sol";

contract MockCurveRegistry is ICurveRegistry {
    mapping(address token => address curve) public curveOfToken;

    function setCurve(address token, address curve) external {
        curveOfToken[token] = curve;
    }
}
