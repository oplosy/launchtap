// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;

// Test-only compilation unit for the `router` Foundry profile: pulls the pinned Uniswap
// Universal Router 2.1.2 and its UnsupportedProtocol placeholder into `out-router/` so the
// Anvil v2 web gate can deploy them (Plan v2-C D1). The launchpad never deploys these.
import { UniversalRouter } from "lib/universal-router/contracts/UniversalRouter.sol";
import { UnsupportedProtocol } from "lib/universal-router/contracts/deploy/UnsupportedProtocol.sol";
