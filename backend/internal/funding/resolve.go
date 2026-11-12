// Package funding finds who sent a wallet its first ETH
// (docs/specs/2026-11-12-funding-source-clusters-design.md). It needs an archive RPC with call
// tracing; every answer is a fact about finalized history.
package funding

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Status of a wallet's funding as of Result.CheckedBlock.
type Status string

const (
	// StatusFunded: Funder sent the wallet's first ETH in TxHash at Block.
	StatusFunded Status = "funded"
	// StatusUnfunded: no ETH ever arrived up to CheckedBlock.
	StatusUnfunded Status = "unfunded"
	// StatusContract: the wallet has code; the nonce rule does not apply.
	StatusContract Status = "contract"
	// StatusUnknown: balance appeared (at Block, when known) without a value transfer the RPC
	// shows, or the wallet sent a transaction while holding nothing (sponsored).
	StatusUnknown Status = "unknown"
)

// Transfer is a value transfer seen in a block: a top-level transaction or a traced call.
type Transfer struct {
	TxHash common.Hash
	From   common.Address
	To     common.Address
	Value  *big.Int
}

// Chain is the archive RPC surface Resolve needs.
type Chain interface {
	NonceAt(ctx context.Context, a common.Address, block uint64) (uint64, error)
	BalanceAt(ctx context.Context, a common.Address, block uint64) (*big.Int, error)
	HasCode(ctx context.Context, a common.Address, block uint64) (bool, error)
	// TracedTransfers lists successful value transfers in the block's call traces, in order,
	// top-level calls included.
	TracedTransfers(ctx context.Context, block uint64) ([]Transfer, error)
	// TopLevelTransfers lists the block's transactions with value.
	TopLevelTransfers(ctx context.Context, block uint64) ([]Transfer, error)
}

type Result struct {
	Status       Status
	Funder       common.Address
	Block        uint64
	TxHash       common.Hash
	CheckedBlock uint64
}

// Resolve finds a wallet's first funding at or below head. The nonce only grows, so the first
// sending block is found by binary search; before it an EOA cannot lose ETH, so its balance only
// grows and the first funded block is found the same way.
func Resolve(ctx context.Context, chain Chain, wallet common.Address, head uint64) (Result, error) {
	out := Result{CheckedBlock: head}
	code, err := chain.HasCode(ctx, wallet, head)
	if err != nil {
		return Result{}, fmt.Errorf("read code: %w", err)
	}
	if code {
		out.Status = StatusContract
		return out, nil
	}
	upper := head
	nonce, err := chain.NonceAt(ctx, wallet, head)
	if err != nil {
		return Result{}, fmt.Errorf("read nonce: %w", err)
	}
	sent := nonce > 0
	if sent {
		firstSend, err := firstBlock(ctx, head, func(b uint64) (bool, error) {
			n, err := chain.NonceAt(ctx, wallet, b)
			return n > 0, err
		})
		if err != nil {
			return Result{}, fmt.Errorf("search first send: %w", err)
		}
		if firstSend == 0 {
			out.Status = StatusUnknown
			return out, nil
		}
		upper = firstSend - 1
	}
	positive := func(b uint64) (bool, error) {
		v, err := chain.BalanceAt(ctx, wallet, b)
		return v != nil && v.Sign() > 0, err
	}
	funded, err := positive(upper)
	if err != nil {
		return Result{}, fmt.Errorf("read balance: %w", err)
	}
	if !funded {
		if sent {
			out.Status = StatusUnknown
		} else {
			out.Status = StatusUnfunded
		}
		return out, nil
	}
	block, err := firstBlock(ctx, upper, positive)
	if err != nil {
		return Result{}, fmt.Errorf("search first balance: %w", err)
	}
	out.Block = block
	traced, err := chain.TracedTransfers(ctx, block)
	if err != nil {
		return Result{}, fmt.Errorf("trace block %d: %w", block, err)
	}
	if t, ok := firstTo(traced, wallet); ok {
		out.Status, out.Funder, out.TxHash = StatusFunded, t.From, t.TxHash
		return out, nil
	}
	// System transactions (e.g. deposits) may not appear in call traces.
	top, err := chain.TopLevelTransfers(ctx, block)
	if err != nil {
		return Result{}, fmt.Errorf("read block %d: %w", block, err)
	}
	if t, ok := firstTo(top, wallet); ok {
		out.Status, out.Funder, out.TxHash = StatusFunded, t.From, t.TxHash
		return out, nil
	}
	out.Status = StatusUnknown
	return out, nil
}

func firstTo(transfers []Transfer, wallet common.Address) (Transfer, bool) {
	for _, t := range transfers {
		if t.To == wallet && t.From != wallet && t.Value != nil && t.Value.Sign() > 0 {
			return t, true
		}
	}
	return Transfer{}, false
}

// firstBlock returns the lowest block in [0, hi] where pred holds; pred(hi) must hold and pred
// must be monotonic.
func firstBlock(ctx context.Context, hi uint64, pred func(uint64) (bool, error)) (uint64, error) {
	lo := uint64(0)
	for lo < hi {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		mid := lo + (hi-lo)/2
		ok, err := pred(mid)
		if err != nil {
			return 0, err
		}
		if ok {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo, nil
}
