package chain

import (
	"context"
	"errors"
	"testing"

	"github.com/Contictus/launchtap/backend/deployments"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type codeReader map[common.Address][]byte

func (r codeReader) CodeAt(_ context.Context, address common.Address) ([]byte, error) {
	return r[address], nil
}

func TestVerifyDeploymentBytecode(t *testing.T) {
	t.Parallel()
	addresses := []common.Address{common.HexToAddress("0x1"), common.HexToAddress("0x2"), common.HexToAddress("0x3"), common.HexToAddress("0x4")}
	reader := codeReader{}
	for index, address := range addresses {
		reader[address] = []byte{byte(index + 1)}
	}
	deployment := deployments.Deployment{EngineVersion: 1, Factory: addresses[0], CurveImplementation: addresses[1], WETH: addresses[2], UniV2Factory: addresses[3], BytecodeHashes: deployments.BytecodeHashes{LaunchFactory: crypto.Keccak256Hash(reader[addresses[0]]), BondingCurveV1: crypto.Keccak256Hash(reader[addresses[1]]), WETH: crypto.Keccak256Hash(reader[addresses[2]]), UniswapV2Factory: crypto.Keccak256Hash(reader[addresses[3]])}}
	if err := VerifyDeploymentBytecode(context.Background(), reader, deployment); err != nil {
		t.Fatalf("VerifyDeploymentBytecode() error = %v", err)
	}
	reader[addresses[2]] = []byte{99}
	if err := VerifyDeploymentBytecode(context.Background(), reader, deployment); !errors.Is(err, ErrBytecodeMismatch) {
		t.Fatalf("mismatch error = %v", err)
	}
}

type pairCaller struct{ result []byte }

func (c pairCaller) CallContract(_ context.Context, _ ethereum.CallMsg) ([]byte, error) {
	return c.result, nil
}

func TestVerifyPairAddress(t *testing.T) {
	t.Parallel()
	factory, token, weth := common.HexToAddress("0xfac"), common.HexToAddress("0x100"), common.HexToAddress("0x200")
	initHash := common.HexToHash("0x1234")
	expected := PairAddress(factory, token, weth, initHash)
	result := make([]byte, 32)
	copy(result[12:], expected[:])
	actual, err := VerifyPairAddress(context.Background(), pairCaller{result}, factory, token, weth, initHash)
	if err != nil || actual != expected {
		t.Fatalf("VerifyPairAddress() = %s, %v", actual, err)
	}
	result[31] ^= 1
	if _, err := VerifyPairAddress(context.Background(), pairCaller{result}, factory, token, weth, initHash); !errors.Is(err, ErrPairMismatch) {
		t.Fatalf("mismatch error = %v", err)
	}
}

// An engine-2 deployment has no WETH or Uniswap V2 factory; its runtime identity is the
// factory, the v2 curve implementation and the reviewed V4 dependencies.
func TestVerifyDeploymentBytecodeEngineV2(t *testing.T) {
	t.Parallel()
	reader := codeReader{}
	address := func(n byte) common.Address {
		value := common.Address{19: n}
		reader[value] = []byte{n}
		return value
	}
	hash := func(value common.Address) common.Hash { return crypto.Keccak256Hash(reader[value]) }
	factory, curve, poolManager, positionManager, permit2, hook, locker := address(1), address(2), address(3), address(4), address(5), address(6), address(7)
	deployment := deployments.Deployment{
		EngineVersion: 2, Factory: factory, CurveImplementation: curve,
		BytecodeHashes: deployments.BytecodeHashes{LaunchFactory: hash(factory)},
		V4: &deployments.V4Deployment{
			PoolManager: poolManager, PositionManager: positionManager, Permit2: permit2, Hook: hook, Locker: locker,
			BytecodeHashes: deployments.V4BytecodeHashes{
				BondingCurveV2: hash(curve), PoolManager: hash(poolManager), PositionManager: hash(positionManager),
				Permit2: hash(permit2), Hook: hash(hook), Locker: hash(locker),
			},
		},
	}
	if err := VerifyDeploymentBytecode(context.Background(), reader, deployment); err != nil {
		t.Fatalf("VerifyDeploymentBytecode() error = %v", err)
	}
	for _, target := range []common.Address{curve, poolManager, positionManager, permit2, hook, locker} {
		original := reader[target]
		reader[target] = []byte{99}
		if err := VerifyDeploymentBytecode(context.Background(), reader, deployment); !errors.Is(err, ErrBytecodeMismatch) {
			t.Fatalf("mismatch at %s: error = %v", target.Hex(), err)
		}
		reader[target] = original
	}
	withoutV4 := deployment
	withoutV4.V4 = nil
	if err := VerifyDeploymentBytecode(context.Background(), reader, withoutV4); err == nil {
		t.Fatal("engine-2 deployment without a V4 section verified")
	}
}
