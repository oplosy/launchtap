// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;

// Test-only compilation unit for the `v4` Foundry profile: pulls the pinned Uniswap v4
// PoolManager into `out-v4/` so tests can deploy it. Kept in its own file so each deployable
// compiles with its upstream optimizer settings. The launchpad never deploys this.
import { PoolManager } from "@uniswap/v4-core/src/PoolManager.sol";
