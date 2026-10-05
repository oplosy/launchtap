package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReadSnapshot is the immutable identity attached to every collection response.
type ReadSnapshot struct {
	Identity pagination.Snapshot
	State    SyncState
}

// WithReadSnapshot executes readFn in one read-only REPEATABLE READ transaction.
// The transaction is always rolled back: this helper is a read boundary and
// therefore cannot commit after a cancelled handler or a successful read.
func WithReadSnapshot(ctx context.Context, pool *pgxpool.Pool, chainID int64, deploymentID string, readFn func(context.Context, *Adapter, ReadSnapshot) error) error {
	if pool == nil || readFn == nil {
		return errors.New("read snapshot requires pool and callback")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin read snapshot: %w", err)
	}
	defer func() { _ = rollbackTx(tx) }()

	adapter := NewAdapter(tx)
	state, err := adapter.GetSyncState(ctx, chainID, deploymentID)
	if err != nil {
		return readSnapshotWatermarkError(err)
	}
	identity, err := observedIdentity(state)
	if err != nil {
		return err
	}
	if err := readFn(ctx, adapter, ReadSnapshot{Identity: identity, State: state}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func readSnapshotWatermarkError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: sync state is not initialized", pagination.ErrSnapshotUnavailable)
	}
	return fmt.Errorf("read snapshot watermark: %w", err)
}

func observedIdentity(state SyncState) (pagination.Snapshot, error) {
	hasNumber := state.ObservedNumber.Valid
	hasHash := state.ObservedHash != nil
	hasTime := state.ObservedAt.Valid
	if !hasNumber && !hasHash && !hasTime {
		return pagination.Snapshot{}, fmt.Errorf("%w: no observed canonical block", pagination.ErrSnapshotUnavailable)
	}
	if !hasNumber || !hasHash || !hasTime {
		return pagination.Snapshot{}, errors.New("snapshot has incomplete observed canonical block")
	}
	if state.ObservedNumber.Int64 < 0 {
		return pagination.Snapshot{}, errors.New("snapshot has negative observed block number")
	}
	return pagination.Snapshot{ChainID: state.ChainID, BlockNumber: state.ObservedNumber.Int64, BlockHash: [32]byte(*state.ObservedHash)}, nil
}

// canonicalCursorCheck confirms an older cursor block is still the indexed canonical block at
// its height. Indexed blocks above a reorg ancestor are deleted, so a missing row or a
// different hash both mean the cursor was taken on an abandoned branch.
func (adapter *Adapter) canonicalCursorCheck(ctx context.Context) pagination.CanonicalCheck {
	return func(snapshot pagination.Snapshot) (bool, error) {
		block, err := adapter.GetIndexedBlockByNumber(ctx, snapshot.ChainID, snapshot.BlockNumber)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return block.BlockHash == snapshot.BlockHash, nil
	}
}
