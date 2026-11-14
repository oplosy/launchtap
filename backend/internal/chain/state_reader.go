package chain

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// stateReaderABI holds the view functions the reconciliation pass reads. The curve getters have
// the same signatures on engine 1 (IBondingCurveV1) and engine 2 (IBondingCurveV2).
const stateReaderABI = `[
{"type":"function","name":"phase","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint8"}]},
{"type":"function","name":"virtualEthReserve","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"virtualTokenReserve","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"totalSupply","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"balanceOf","stateMutability":"view","inputs":[{"name":"account","type":"address"}],"outputs":[{"name":"","type":"uint256"}]}
]`

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

// BlockHash reads the canonical hash at a block number.
func (r StateReader) BlockHash(ctx context.Context, block uint64) (common.Hash, error) {
	header, err := r.Client.HeaderByNumber(ctx, block)
	if err != nil {
		return common.Hash{}, err
	}
	return header.Hash(), nil
}
