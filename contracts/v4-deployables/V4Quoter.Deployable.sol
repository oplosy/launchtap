// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;

// Test-only compilation unit for the `v4` Foundry profile: pulls the pinned Uniswap v4 V4Quoter
// into `out-v4/` for the Anvil v2 web gate. The launchpad never deploys this.
import { V4Quoter } from "@uniswap/v4-periphery/src/lens/V4Quoter.sol";
