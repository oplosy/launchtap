package funding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// RPC implements Chain over JSON-RPC. Blocks and traces are decoded as raw JSON, not as typed
// transactions, so chain-specific transaction types (L2 deposits, system transactions) never
// fail decoding.
type RPC struct {
	Client *gethrpc.Client
}

func blockTag(block uint64) string { return hexutil.EncodeUint64(block) }

func (r RPC) NonceAt(ctx context.Context, a common.Address, block uint64) (uint64, error) {
	var out hexutil.Uint64
	if err := r.Client.CallContext(ctx, &out, "eth_getTransactionCount", a, blockTag(block)); err != nil {
		return 0, err
	}
	return uint64(out), nil
}

func (r RPC) BalanceAt(ctx context.Context, a common.Address, block uint64) (*big.Int, error) {
	var out hexutil.Big
	if err := r.Client.CallContext(ctx, &out, "eth_getBalance", a, blockTag(block)); err != nil {
		return nil, err
	}
	return (*big.Int)(&out), nil
}

func (r RPC) HasCode(ctx context.Context, a common.Address, block uint64) (bool, error) {
	var out hexutil.Bytes
	if err := r.Client.CallContext(ctx, &out, "eth_getCode", a, blockTag(block)); err != nil {
		return false, err
	}
	return len(out) > 0, nil
}

type rawTransaction struct {
	Hash  common.Hash     `json:"hash"`
	From  common.Address  `json:"from"`
	To    *common.Address `json:"to"`
	Value *hexutil.Big    `json:"value"`
}

func (r RPC) TopLevelTransfers(ctx context.Context, block uint64) ([]Transfer, error) {
	var raw json.RawMessage
	if err := r.Client.CallContext(ctx, &raw, "eth_getBlockByNumber", blockTag(block), true); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("block %d not found", block)
	}
	var body struct {
		Transactions []rawTransaction `json:"transactions"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("decode block %d: %w", block, err)
	}
	var out []Transfer
	for _, tx := range body.Transactions {
		if tx.To == nil || tx.Value == nil || tx.Value.ToInt().Sign() <= 0 {
			continue
		}
		out = append(out, Transfer{TxHash: tx.Hash, From: tx.From, To: *tx.To, Value: tx.Value.ToInt()})
	}
	return out, nil
}

type callFrame struct {
	Type  string          `json:"type"`
	From  common.Address  `json:"from"`
	To    *common.Address `json:"to"`
	Value *hexutil.Big    `json:"value"`
	Error string          `json:"error"`
	Calls []callFrame     `json:"calls"`
}

type traceResult struct {
	TxHash common.Hash `json:"txHash"`
	Result *callFrame  `json:"result"`
	Error  string      `json:"error"`
}

func (r RPC) TracedTransfers(ctx context.Context, block uint64) ([]Transfer, error) {
	var traces []traceResult
	err := r.Client.CallContext(ctx, &traces, "debug_traceBlockByNumber", blockTag(block), map[string]any{"tracer": "callTracer"})
	if err != nil {
		return nil, err
	}
	var out []Transfer
	for i, tr := range traces {
		if tr.Error != "" {
			return nil, fmt.Errorf("trace transaction %d: %s", i, tr.Error)
		}
		if tr.Result == nil {
			return nil, errors.New("trace result missing")
		}
		out = appendFrame(out, tr.TxHash, *tr.Result)
	}
	return out, nil
}

// appendFrame walks a call tree depth-first in execution order. A reverted frame and everything
// under it moved no value. DELEGATECALL and STATICCALL frames carry no transfer of their own.
func appendFrame(out []Transfer, tx common.Hash, f callFrame) []Transfer {
	if f.Error != "" {
		return out
	}
	if f.To != nil && f.Value != nil && f.Value.ToInt().Sign() > 0 && f.Type != "DELEGATECALL" && f.Type != "STATICCALL" {
		out = append(out, Transfer{TxHash: tx, From: f.From, To: *f.To, Value: f.Value.ToInt()})
	}
	for _, c := range f.Calls {
		out = appendFrame(out, tx, c)
	}
	return out
}
