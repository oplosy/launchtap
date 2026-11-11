package deployments

import "github.com/ethereum/go-ethereum/common"

// Environment identifies the operational class of a reviewed deployment.
type Environment string

const (
	EnvironmentLocal      Environment = "local"
	EnvironmentTestnet    Environment = "testnet"
	EnvironmentProduction Environment = "production"
)

// BytecodeHashes contains the reviewed runtime bytecode identities needed at
// indexer startup. Task 3 only validates their shape; it performs no RPC calls.
type BytecodeHashes struct {
	LaunchFactory    common.Hash
	BondingCurveV1   common.Hash
	UniswapV2Factory common.Hash
	WETH             common.Hash
}

// V4BytecodeHashes are the reviewed runtime code identities of an engine-2 deployment's curve
// implementation and Uniswap V4 dependencies.
type V4BytecodeHashes struct {
	BondingCurveV2  common.Hash
	Hook            common.Hash
	Locker          common.Hash
	PoolManager     common.Hash
	PositionManager common.Hash
	Permit2         common.Hash
}

// V4Deployment is the engine-2 section of a deployment: the singleton PoolManager, the
// PositionManager and Permit2 it graduates through, and the per-deployment guard hook and LP
// fee locker.
type V4Deployment struct {
	PoolManager     common.Address
	PositionManager common.Address
	Permit2         common.Address
	Hook            common.Address
	Locker          common.Address
	BytecodeHashes  V4BytecodeHashes
}

// Deployment is the runtime subset of a reviewed deployment manifest. The Uniswap V2 fields
// (UniV2Factory, WETH, PairInitCodeHash, LPBurnAddress and the matching bytecode hashes) are
// set only for engine 1; V4 is set only for engine 2.
type Deployment struct {
	ChainID             uint64
	DeploymentID        string
	Name                string
	Environment         Environment
	Factory             common.Address
	StartBlock          uint64
	EngineVersion       uint16
	CurveImplementation common.Address
	UniV2Factory        common.Address
	WETH                common.Address
	PairInitCodeHash    common.Hash
	LPBurnAddress       common.Address
	BytecodeHashes      BytecodeHashes
	DeployTransaction   common.Hash
	ExplorerBase        string
	V4                  *V4Deployment
}
