package indexer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// PositionPair identifies one wallet position whose ledger inputs changed.
type PositionPair struct{ Wallet, Token common.Address }

type RecoveryUnit interface {
	UnitOfWork
	FindCommonAncestor(context.Context, int64, []common.Hash) (ledger.IndexedBlock, error)
	AffectedTokensAbove(context.Context, int64, int64) ([]common.Address, error)
	AffectedPositionPairsAbove(ctx context.Context, chainID, block int64) ([]PositionPair, error)
	DeleteCanonicalAbove(context.Context, int64, int64) error
	MarkPositionsDirty(ctx context.Context, chainID int64, pairs []PositionPair) error
	RebuildTokenProjections(context.Context, int64, common.Address) error
	DeleteTokenStats(context.Context, int64, common.Address) error
	RecomputeTokenStats(context.Context, int64, common.Address) error
	RecomputeProtocolAggregates(context.Context, int64) error
	WriteState(context.Context, State) error
	RecordReorg(context.Context, ReorgRecord) (int64, error)
	CompleteReorg(context.Context, int64) error
}

func (e *Engine) recoverReorg(ctx context.Context, state State, tip ledger.IndexedBlock) error {
	// Recovery deletes and rebuilds token projections and graduations, whatever its outcome.
	defer e.invalidateIdentities()
	if state.Observed == nil {
		return errors.New("cannot recover reorg without observed tip")
	}
	candidateFloor := e.settings.StartBlock
	if state.Safe != nil && state.Safe.BlockNumber > candidateFloor {
		candidateFloor = state.Safe.BlockNumber
	}
	if tip.BlockNumber < candidateFloor {
		return ErrSafeViolation
	}
	candidates := make([]common.Hash, 0, int(e.settings.ReorgSearchDepth))
	var previous *types.Header
	for number := tip.BlockNumber; number >= candidateFloor && uint64(len(candidates)) < e.settings.ReorgSearchDepth; number-- {
		header, err := e.source.HeaderByNumber(ctx, uint64(number))
		if err != nil {
			return rpcFailure("read reorg candidate header", err)
		}
		block, err := e.block(header)
		if err != nil {
			return fmt.Errorf("invalid reorg candidate header at %d: %w", number, err)
		}
		if block.BlockNumber != number {
			return fmt.Errorf("RPC reorg candidate number %d differs from requested number %d", block.BlockNumber, number)
		}
		hash := header.Hash()
		if len(candidates) == 0 && hash != tip.BlockHash {
			return fmt.Errorf("RPC reorg candidate at detected tip %d changed: expected %s, got %s", tip.BlockNumber, tip.BlockHash, hash)
		}
		if previous != nil {
			if previous.Number.Int64() != block.BlockNumber+1 || previous.ParentHash != hash {
				return fmt.Errorf("RPC reorg candidate chain is discontinuous between blocks %d and %d", previous.Number.Int64(), block.BlockNumber)
			}
		}
		candidates = append(candidates, hash)
		previous = header
		if number == 0 {
			break
		}
	}
	if len(candidates) == 0 {
		return errors.New("no reorg candidates at or above the configured search floor")
	}
	var reorgID int64
	if err := e.store.Transaction(ctx, func(ctx context.Context, u UnitOfWork) error {
		recovery, ok := u.(RecoveryUnit)
		if !ok {
			return errors.New("store does not support reorg recovery")
		}
		ancestor, err := recovery.FindCommonAncestor(ctx, e.settings.ChainID, candidates)
		if err != nil {
			return err
		}
		depth := state.Observed.BlockNumber - ancestor.BlockNumber
		if depth <= 0 {
			return errors.New("reorg ancestor is not below observed tip")
		}
		if state.Safe != nil && ancestor.BlockNumber < state.Safe.BlockNumber {
			return ErrSafeViolation
		}
		// Revalidate the originally detected tip after candidate and ancestor
		// discovery, immediately before the first recovery write. This prevents
		// mixed provider views from creating an incident record or deleting data.
		recheckedHeader, err := e.source.HeaderByNumber(ctx, uint64(tip.BlockNumber))
		if err != nil {
			return rpcFailure("recheck detected reorg tip", err)
		}
		recheckedTip, err := e.block(recheckedHeader)
		if err != nil {
			return fmt.Errorf("invalid rechecked reorg tip at %d: %w", tip.BlockNumber, err)
		}
		if recheckedTip.BlockNumber != tip.BlockNumber || recheckedTip.BlockHash != tip.BlockHash {
			return fmt.Errorf("RPC reorg tip changed before recovery write at %d: expected %s, got %s", tip.BlockNumber, tip.BlockHash, recheckedTip.BlockHash)
		}
		reorgID, err = recovery.RecordReorg(ctx, ReorgRecord{ChainID: e.settings.ChainID, DeploymentID: e.settings.DeploymentID, DetectedTipNumber: tip.BlockNumber, DetectedTipHash: tip.BlockHash, CommonAncestorNumber: ancestor.BlockNumber, CommonAncestorHash: ancestor.BlockHash, Depth: depth, DetectedAt: time.Now().UTC()})
		return err
	}); err != nil {
		return fmt.Errorf("record reorg: %w", err)
	}
	if err := e.store.Transaction(ctx, func(ctx context.Context, u UnitOfWork) error {
		recovery, ok := u.(RecoveryUnit)
		if !ok {
			return errors.New("store does not support reorg recovery")
		}
		ancestor, err := recovery.FindCommonAncestor(ctx, e.settings.ChainID, candidates)
		if err != nil {
			return err
		}
		tokens, err := recovery.AffectedTokensAbove(ctx, e.settings.ChainID, ancestor.BlockNumber)
		if err != nil {
			return err
		}
		pairs, err := recovery.AffectedPositionPairsAbove(ctx, e.settings.ChainID, ancestor.BlockNumber)
		if err != nil {
			return err
		}
		if err := recovery.DeleteCanonicalAbove(ctx, e.settings.ChainID, ancestor.BlockNumber); err != nil {
			return err
		}
		if err := recovery.MarkPositionsDirty(ctx, e.settings.ChainID, pairs); err != nil {
			return err
		}
		for _, token := range tokens {
			if err := recovery.RebuildTokenProjections(ctx, e.settings.ChainID, token); err != nil {
				return err
			}
			if err := recovery.DeleteTokenStats(ctx, e.settings.ChainID, token); err != nil {
				return err
			}
			if err := recovery.RecomputeTokenStats(ctx, e.settings.ChainID, token); err != nil {
				return err
			}
		}
		if err := recovery.RecomputeProtocolAggregates(ctx, e.settings.ChainID); err != nil {
			return err
		}
		state.Observed = &ancestor
		if err := recovery.WriteState(ctx, state); err != nil {
			return err
		}
		return recovery.CompleteReorg(ctx, reorgID)
	}); err != nil {
		return fmt.Errorf("recover reorg %d: %w", reorgID, err)
	}
	return nil
}
