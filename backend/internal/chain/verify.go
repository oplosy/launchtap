package chain

import (
	"bytes"
	"context"
	"fmt"
	"math/big"

	"github.com/Contictus/launchtap/backend/deployments"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type CodeReader interface {
	CodeAt(context.Context, common.Address) ([]byte, error)
}

type ChainIDReader interface {
	ChainID(context.Context) (*big.Int, error)
}

type RuntimeVerifier interface {
	ChainIDReader
	CodeReader
	ContractCaller
}

func VerifyRPCChainID(ctx context.Context, reader ChainIDReader, configuredChainID uint64, deployment deployments.Deployment) error {
	if reader == nil {
		return fmt.Errorf("read RPC chain ID for deployment %q: chain ID reader is nil", deployment.DeploymentID)
	}
	if configuredChainID != deployment.ChainID {
		return &ChainIDMismatchError{ConfiguredChainID: configuredChainID, DeploymentChainID: deployment.ChainID, ActualChainID: "not queried"}
	}
	actual, err := reader.ChainID(ctx)
	if err != nil {
		return fmt.Errorf("read eth_chainId for deployment %q (configured chain %d): %w", deployment.DeploymentID, configuredChainID, err)
	}
	if actual == nil || actual.Sign() <= 0 || actual.Cmp(new(big.Int).SetUint64(configuredChainID)) != 0 {
		actualValue := "nil"
		if actual != nil {
			actualValue = actual.String()
		}
		return &ChainIDMismatchError{ConfiguredChainID: configuredChainID, DeploymentChainID: deployment.ChainID, ActualChainID: actualValue}
	}
	return nil
}

type bytecodeCheck struct {
	name     string
	address  common.Address
	expected common.Hash
}

// VerifyDeploymentBytecode compares the runtime code of every reviewed contract the deployment
// depends on with its manifest hash. Engine 1 checks the Uniswap V2 dependencies; engine 2
// checks the v2 curve implementation and the Uniswap V4 dependencies instead.
func VerifyDeploymentBytecode(ctx context.Context, reader CodeReader, deployment deployments.Deployment) error {
	checks, err := bytecodeChecks(deployment)
	if err != nil {
		return err
	}
	for _, check := range checks {
		code, err := reader.CodeAt(ctx, check.address)
		if err != nil {
			return fmt.Errorf("read %s bytecode at %s: %w", check.name, check.address.Hex(), err)
		}
		actual := crypto.Keccak256Hash(code)
		if len(code) == 0 || actual != check.expected {
			return fmt.Errorf("%w: %s at %s: got %s, want %s", ErrBytecodeMismatch, check.name, check.address.Hex(), actual.Hex(), check.expected.Hex())
		}
	}
	return nil
}

func bytecodeChecks(deployment deployments.Deployment) ([]bytecodeCheck, error) {
	factory := bytecodeCheck{"Factory", deployment.Factory, deployment.BytecodeHashes.LaunchFactory}
	switch deployment.EngineVersion {
	case 1:
		return []bytecodeCheck{
			factory,
			{"CurveImplementation", deployment.CurveImplementation, deployment.BytecodeHashes.BondingCurveV1},
			{"WETH", deployment.WETH, deployment.BytecodeHashes.WETH},
			{"UniV2Factory", deployment.UniV2Factory, deployment.BytecodeHashes.UniswapV2Factory},
		}, nil
	case 2:
		v4 := deployment.V4
		if v4 == nil {
			return nil, fmt.Errorf("%w: engine-2 deployment %q has no V4 section", ErrBytecodeMismatch, deployment.DeploymentID)
		}
		return []bytecodeCheck{
			factory,
			{"CurveImplementation", deployment.CurveImplementation, v4.BytecodeHashes.BondingCurveV2},
			{"PoolManager", v4.PoolManager, v4.BytecodeHashes.PoolManager},
			{"PositionManager", v4.PositionManager, v4.BytecodeHashes.PositionManager},
			{"Permit2", v4.Permit2, v4.BytecodeHashes.Permit2},
			{"Hook", v4.Hook, v4.BytecodeHashes.Hook},
			{"Locker", v4.Locker, v4.BytecodeHashes.Locker},
		}, nil
	default:
		return nil, &EngineVersionError{Version: deployment.EngineVersion}
	}
}

func PairAddress(factory, token, weth common.Address, initCodeHash common.Hash) common.Address {
	token0, token1 := token, weth
	if bytes.Compare(token0[:], token1[:]) > 0 {
		token0, token1 = token1, token0
	}
	salt := crypto.Keccak256Hash(token0[:], token1[:])
	hash := crypto.Keccak256([]byte{0xff}, factory[:], salt[:], initCodeHash[:])
	return common.BytesToAddress(hash[12:])
}

type ContractCaller interface {
	CallContract(context.Context, ethereum.CallMsg) ([]byte, error)
}

func VerifyPairAddress(ctx context.Context, caller ContractCaller, factory, token, weth common.Address, initCodeHash common.Hash) (common.Address, error) {
	expected := PairAddress(factory, token, weth, initCodeHash)
	selector := crypto.Keccak256([]byte("getPair(address,address)"))[:4]
	data := make([]byte, 4+64)
	copy(data, selector)
	copy(data[4+12:4+32], token[:])
	copy(data[4+32+12:], weth[:])
	result, err := caller.CallContract(ctx, ethereum.CallMsg{To: &factory, Data: data})
	if err != nil {
		return common.Address{}, fmt.Errorf("read pair address: %w", err)
	}
	if len(result) != 32 {
		return common.Address{}, fmt.Errorf("%w: getPair returned %d bytes", ErrPairMismatch, len(result))
	}
	for _, value := range result[:12] {
		if value != 0 {
			return common.Address{}, fmt.Errorf("%w: getPair returned a non-padded address", ErrPairMismatch)
		}
	}
	actual := common.BytesToAddress(result[12:])
	if actual != expected {
		return common.Address{}, fmt.Errorf("%w: got %s, want %s", ErrPairMismatch, actual.Hex(), expected.Hex())
	}
	return actual, nil
}

func VerifyLaunchPair(ctx context.Context, caller ContractCaller, factory, weth common.Address, initCodeHash common.Hash, launch TokenLaunched) error {
	if launch.WETH != weth {
		return fmt.Errorf("%w: TokenLaunched WETH for token %s is %s, want %s", ErrPairMismatch, launch.Token.Hex(), launch.WETH.Hex(), weth.Hex())
	}
	pair, err := VerifyPairAddress(ctx, caller, factory, launch.Token, weth, initCodeHash)
	if err != nil {
		return fmt.Errorf("verify factory pair for token %s: %w", launch.Token.Hex(), err)
	}
	if launch.LPPair != pair {
		return fmt.Errorf("%w: TokenLaunched lpPair for token %s is %s, want %s", ErrPairMismatch, launch.Token.Hex(), launch.LPPair.Hex(), pair.Hex())
	}
	return nil
}
