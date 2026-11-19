package chain

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// stateReaderABI holds the view functions the reconciliation pass reads. The curve getters have
// the same signatures on engine 1 (IBondingCurveV1) and engine 2 (IBondingCurveV2). getReserves
// is the Uniswap v2 pair getter; extsload is the Uniswap v4 PoolManager's raw storage read.
const stateReaderABI = `[
{"type":"function","name":"phase","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint8"}]},
{"type":"function","name":"virtualEthReserve","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"virtualTokenReserve","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"totalSupply","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"balanceOf","stateMutability":"view","inputs":[{"name":"account","type":"address"}],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"getReserves","stateMutability":"view","inputs":[],"outputs":[{"name":"reserve0","type":"uint112"},{"name":"reserve1","type":"uint112"},{"name":"blockTimestampLast","type":"uint32"}]},
{"type":"function","name":"extsload","stateMutability":"view","inputs":[{"name":"slot","type":"bytes32"}],"outputs":[{"name":"","type":"bytes32"}]}
]`

// v4PoolsSlot is StateLibrary.POOLS_SLOT in the pinned v4-core: the storage slot of
// PoolManager's `mapping(PoolId => Pool.State) _pools`.
var v4PoolsSlot = common.BigToHash(big.NewInt(6))

// V4PoolStateSlot is the storage slot of a pool's Pool.State, whose first word is slot0
// (StateLibrary._getPoolStateSlot: keccak256(abi.encodePacked(poolId, POOLS_SLOT))).
func V4PoolStateSlot(poolID common.Hash) common.Hash {
	return crypto.Keccak256Hash(poolID.Bytes(), v4PoolsSlot.Bytes())
}

// sqrtPriceFromSlot0 decodes slot0's low 160 bits, StateLibrary.getSlot0's sqrtPriceX96.
func sqrtPriceFromSlot0(word common.Hash) *big.Int {
	return new(big.Int).SetBytes(word[common.HashLength-20:])
}

var stateABI = func() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(stateReaderABI))
	if err != nil {
		panic(fmt.Sprintf("parse state reader ABI: %v", err))
	}
	return parsed
}()

// StateReader reads curve and token state at a block number; it implements reconcile.Chain.
type StateReader struct{ Client *Client }

func (r StateReader) call(ctx context.Context, to common.Address, block uint64, method string, args ...any) ([]any, error) {
	data, err := stateABI.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", method, err)
	}
	output, err := r.Client.CallContractAt(ctx, ethereum.CallMsg{To: &to, Data: data}, block)
	if err != nil {
		return nil, fmt.Errorf("call %s on %s at %d: %w", method, to.Hex(), block, err)
	}
	values, err := stateABI.Unpack(method, output)
	if err != nil {
		return nil, fmt.Errorf("unpack %s from %s: %w", method, to.Hex(), err)
	}
	return values, nil
}

func (r StateReader) uint256(ctx context.Context, to common.Address, block uint64, method string, args ...any) (*big.Int, error) {
	values, err := r.call(ctx, to, block, method, args...)
	if err != nil {
		return nil, err
	}
	value, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("%s on %s returned %T", method, to.Hex(), values[0])
	}
	return value, nil
}

// Phase reads LaunchTypes.Phase from a curve.
func (r StateReader) Phase(ctx context.Context, curve common.Address, block uint64) (uint8, error) {
	values, err := r.call(ctx, curve, block, "phase")
	if err != nil {
		return 0, err
	}
	phase, ok := values[0].(uint8)
	if !ok {
		return 0, fmt.Errorf("phase on %s returned %T", curve.Hex(), values[0])
	}
	return phase, nil
}

// VirtualReserves reads a curve's virtual ETH and token reserves.
func (r StateReader) VirtualReserves(ctx context.Context, curve common.Address, block uint64) (*big.Int, *big.Int, error) {
	eth, err := r.uint256(ctx, curve, block, "virtualEthReserve")
	if err != nil {
		return nil, nil, err
	}
	token, err := r.uint256(ctx, curve, block, "virtualTokenReserve")
	if err != nil {
		return nil, nil, err
	}
	return eth, token, nil
}

// TotalSupply reads an ERC-20 total supply.
func (r StateReader) TotalSupply(ctx context.Context, token common.Address, block uint64) (*big.Int, error) {
	return r.uint256(ctx, token, block, "totalSupply")
}

// BalanceOf reads an ERC-20 balance.
func (r StateReader) BalanceOf(ctx context.Context, token, holder common.Address, block uint64) (*big.Int, error) {
	return r.uint256(ctx, token, block, "balanceOf", holder)
}

// PairReserves reads a Uniswap v2 pair's reserve0 and reserve1.
func (r StateReader) PairReserves(ctx context.Context, pair common.Address, block uint64) (*big.Int, *big.Int, error) {
	values, err := r.call(ctx, pair, block, "getReserves")
	if err != nil {
		return nil, nil, err
	}
	reserve0, ok0 := values[0].(*big.Int)
	reserve1, ok1 := values[1].(*big.Int)
	if !ok0 || !ok1 {
		return nil, nil, fmt.Errorf("getReserves on %s returned %T, %T", pair.Hex(), values[0], values[1])
	}
	return reserve0, reserve1, nil
}

// PoolSqrtPrice reads a Uniswap v4 pool's sqrtPriceX96 from its PoolManager's storage, which works
// without a StateView deployment.
func (r StateReader) PoolSqrtPrice(ctx context.Context, poolManager common.Address, poolID common.Hash, block uint64) (*big.Int, error) {
	values, err := r.call(ctx, poolManager, block, "extsload", V4PoolStateSlot(poolID))
	if err != nil {
		return nil, err
	}
	word, ok := values[0].([32]byte)
	if !ok {
		return nil, fmt.Errorf("extsload on %s returned %T", poolManager.Hex(), values[0])
	}
	return sqrtPriceFromSlot0(word), nil
}

// BlockHash reads the canonical hash at a block number.
func (r StateReader) BlockHash(ctx context.Context, block uint64) (common.Hash, error) {
	header, err := r.Client.HeaderByNumber(ctx, block)
	if err != nil {
		return common.Hash{}, err
	}
	return header.Hash(), nil
}
