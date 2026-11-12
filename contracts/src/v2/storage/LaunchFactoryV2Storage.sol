// SPDX-License-Identifier: MIT
pragma solidity 0.8.36;

import { LaunchTypes } from "../../types/LaunchTypes.sol";
import { LaunchTypesV2 } from "../types/LaunchTypesV2.sol";

abstract contract LaunchFactoryV2Storage {
    struct EngineConfig {
        address implementation;
        bool enabled;
    }

    mapping(uint16 engineVersion => EngineConfig config) internal _engines;
    mapping(address treasury => uint256 amount) internal _launchFeesByTreasury;
    mapping(address token => address curve) internal _curveOfToken;

    LaunchTypes.CurveParameters internal _futureParameters;
    LaunchTypesV2.AntiSnipeProfile internal _futureAntiSnipe;
    uint16 internal _futureLpCreatorShareBps;
    address internal _futureProtocolTreasury;
    address internal _pauseAuthority;
    address internal _timelock;
    address internal _hook;
    address internal _locker;
    uint256 internal _launchFee;
    bool internal _launchesPaused;
    bool internal _tradingPaused;
}
