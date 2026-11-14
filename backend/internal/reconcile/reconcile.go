// Package reconcile compares indexed projections with the contracts' own state at the block the
// projections were indexed through (docs/specs/2026-11-14-state-reconciliation-preflight.md).
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Default sampling limits: tokens per pass and largest holders per token.
const (
	DefaultTokens  = 25
	DefaultHolders = 5
)

// Indexed phases as stored in tokens.phase, and the on-chain LaunchTypes.Phase values.
const (
	PhaseCurve     = "curve"
	PhaseGraduated = "graduated"
)

var chainPhases = map[uint8]string{0: PhaseCurve, 1: PhaseGraduated}

// Check names used in mismatches and logs.
const (
	CheckPhase        = "phase"
	CheckReserveETH   = "curve_eth_reserve"
	CheckReserveToken = "curve_token_reserve"
	CheckSupply       = "holder_balance_sum"
	CheckHolder       = "holder_balance"
)

// Holder is one indexed holder balance.
type Holder struct {
	Address common.Address
	Balance *big.Int
}

// Reserves are the indexed virtual curve reserves.
type Reserves struct {
	ETH, Token *big.Int
}

// Token is the indexed state of one sampled token.
type Token struct {
	Address, Curve common.Address
	Phase          string
	// CurveReserves is set while the indexed reserve source is the curve.
	CurveReserves *Reserves
	// HolderSum is the sum of every indexed holder balance of the token.
	HolderSum  *big.Int
	TopHolders []Holder
}

// Snapshot is the indexed state of the sampled tokens as of one indexed block.
type Snapshot struct {
	Block     uint64
	BlockHash common.Hash
	Tokens    []Token
}

// Store reads one consistent snapshot. ok is false while nothing has been indexed yet.
type Store interface {
	Snapshot(ctx context.Context, tokens, holders int) (snapshot Snapshot, ok bool, err error)
}

// Chain reads contract state at a block number.
type Chain interface {
	Phase(ctx context.Context, curve common.Address, block uint64) (uint8, error)
	VirtualReserves(ctx context.Context, curve common.Address, block uint64) (eth, token *big.Int, err error)
	TotalSupply(ctx context.Context, token common.Address, block uint64) (*big.Int, error)
	BalanceOf(ctx context.Context, token, holder common.Address, block uint64) (*big.Int, error)
	BlockHash(ctx context.Context, block uint64) (common.Hash, error)
}

// Mismatch is one indexed value that differs from the chain.
type Mismatch struct {
	Check   string
	Token   common.Address
	Subject common.Address
	Indexed string
	Chain   string
}

func (m Mismatch) String() string {
	return fmt.Sprintf("%s token=%s subject=%s indexed=%s chain=%s", m.Check, m.Token.Hex(), m.Subject.Hex(), m.Indexed, m.Chain)
}

// Outcome is how a pass ended.
type Outcome string

const (
	OutcomeOK    Outcome = "ok"
	OutcomeDrift Outcome = "drift"
	// OutcomeReorg discards a pass whose pinned block left the canonical chain during the pass.
	OutcomeReorg Outcome = "reorg"
	// OutcomeIdle is a pass with nothing indexed yet; it compares nothing.
	OutcomeIdle  Outcome = "idle"
	OutcomeError Outcome = "error"
)

// Result describes one pass.
type Result struct {
	Outcome    Outcome
	Block      uint64
	Tokens     int
	Checks     int
	Mismatches []Mismatch
}

// Limits bound one pass.
type Limits struct{ Tokens, Holders int }

// Pass compares one snapshot with the chain. A returned error means the pass compared nothing
// conclusive (Outcome error); drift is reported in the result, not as an error.
func Pass(ctx context.Context, store Store, chain Chain, limits Limits) (Result, error) {
	if store == nil || chain == nil {
		return Result{Outcome: OutcomeError}, errors.New("reconcile pass requires a store and a chain reader")
	}
	if limits.Tokens <= 0 {
		limits.Tokens = DefaultTokens
	}
	if limits.Holders <= 0 {
		limits.Holders = DefaultHolders
	}
	snapshot, ok, err := store.Snapshot(ctx, limits.Tokens, limits.Holders)
	if err != nil {
		return Result{Outcome: OutcomeError}, fmt.Errorf("read indexed snapshot: %w", err)
	}
	if !ok {
		return Result{Outcome: OutcomeIdle}, nil
	}
	result := Result{Block: snapshot.Block, Tokens: len(snapshot.Tokens)}
	compare := func(check string, token, subject common.Address, indexed, onchain *big.Int) {
		result.Checks++
		if indexed == nil || onchain == nil || indexed.Cmp(onchain) != 0 {
			result.Mismatches = append(result.Mismatches, Mismatch{Check: check, Token: token, Subject: subject, Indexed: text(indexed), Chain: text(onchain)})
		}
	}
	for _, token := range snapshot.Tokens {
		phase, err := chain.Phase(ctx, token.Curve, snapshot.Block)
		if err != nil {
			return Result{Outcome: OutcomeError, Block: snapshot.Block}, fmt.Errorf("read phase of %s: %w", token.Address.Hex(), err)
		}
		result.Checks++
		if chainPhase, known := chainPhases[phase]; !known || chainPhase != token.Phase {
			result.Mismatches = append(result.Mismatches, Mismatch{Check: CheckPhase, Token: token.Address, Subject: token.Curve, Indexed: token.Phase, Chain: phaseText(phase)})
		}
		if token.Phase == PhaseCurve && token.CurveReserves != nil {
			eth, tokens, err := chain.VirtualReserves(ctx, token.Curve, snapshot.Block)
			if err != nil {
				return Result{Outcome: OutcomeError, Block: snapshot.Block}, fmt.Errorf("read reserves of %s: %w", token.Address.Hex(), err)
			}
			compare(CheckReserveETH, token.Address, token.Curve, token.CurveReserves.ETH, eth)
			compare(CheckReserveToken, token.Address, token.Curve, token.CurveReserves.Token, tokens)
		}
		supply, err := chain.TotalSupply(ctx, token.Address, snapshot.Block)
		if err != nil {
			return Result{Outcome: OutcomeError, Block: snapshot.Block}, fmt.Errorf("read supply of %s: %w", token.Address.Hex(), err)
		}
		compare(CheckSupply, token.Address, token.Address, token.HolderSum, supply)
		for _, holder := range token.TopHolders {
			balance, err := chain.BalanceOf(ctx, token.Address, holder.Address, snapshot.Block)
			if err != nil {
				return Result{Outcome: OutcomeError, Block: snapshot.Block}, fmt.Errorf("read balance of %s: %w", holder.Address.Hex(), err)
			}
			compare(CheckHolder, token.Address, holder.Address, holder.Balance, balance)
		}
	}
	// The reads ran at a block number; if that number now names another block, the comparison
	// mixed two chains and says nothing about drift.
	hash, err := chain.BlockHash(ctx, snapshot.Block)
	if err != nil {
		return Result{Outcome: OutcomeError, Block: snapshot.Block}, fmt.Errorf("recheck pinned block %d: %w", snapshot.Block, err)
	}
	if hash != snapshot.BlockHash {
		return Result{Outcome: OutcomeReorg, Block: snapshot.Block, Tokens: result.Tokens}, nil
	}
	result.Outcome = OutcomeOK
	if len(result.Mismatches) > 0 {
		result.Outcome = OutcomeDrift
	}
	return result, nil
}

func text(value *big.Int) string {
	if value == nil {
		return "missing"
	}
	return value.String()
}

func phaseText(phase uint8) string {
	if name, ok := chainPhases[phase]; ok {
		return name
	}
	return fmt.Sprintf("unknown(%d)", phase)
}
