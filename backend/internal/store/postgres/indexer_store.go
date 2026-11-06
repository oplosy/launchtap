package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IndexerStore is the application bridge between the neutral indexer ports and
// the PostgreSQL adapter. It is kept here so generated types never escape.
type IndexerStore struct {
	Pool         *pgxpool.Pool
	Beginner     TransactionBeginner
	Owner        *Ownership
	ChainID      int64
	DeploymentID string
	// Factory is the configured launch factory; it is excluded from wallet position tracking.
	Factory common.Address
	// OnNotify, when set, is told the outcome of each NOTIFY (channel is market_dirty or api_refresh).
	OnNotify func(channel string, ok bool)
}

func (s IndexerStore) Transaction(ctx context.Context, fn func(context.Context, indexer.UnitOfWork) error) error {
	beginner := s.Beginner
	if beginner == nil {
		beginner = s.Pool
	}
	transactionContext := context.WithValue(ctx, refreshDeploymentContextKey{}, s.DeploymentID)
	if s.Owner != nil {
		if err := s.Owner.WithinTx(transactionContext, func(ctx context.Context, a *Adapter) error { return fn(ctx, a.WithFactory(s.Factory)) }); err != nil {
			return err
		}
		return s.notifyRefresh(ctx)
	}
	if err := WithinTx(transactionContext, beginner, func(ctx context.Context, a *Adapter) error { return fn(ctx, a.WithFactory(s.Factory)) }); err != nil {
		return err
	}
	return s.notifyRefresh(ctx)
}

func (s IndexerStore) notifyRefresh(ctx context.Context) error {
	if s.Pool != nil && s.ChainID > 0 && s.DeploymentID != "" {
		_, err := s.Pool.Exec(ctx, `SELECT pg_notify('market_dirty', $1)`, fmt.Sprintf(`{"chain_id":%d,"deployment_id":%q}`, s.ChainID, s.DeploymentID))
		s.notified("market_dirty", err)
		if err != nil {
			slog.Warn("market dirty notification failed", "error", err)
		}
		_, err = s.Pool.Exec(ctx, `SELECT pg_notify('api_refresh', $1)`, fmt.Sprintf(`{"type":"token","chain_id":%d,"deployment_id":%q}`, s.ChainID, s.DeploymentID))
		s.notified("api_refresh", err)
		if err != nil {
			slog.Warn("API refresh notification failed", "error", err)
		}
	}
	return nil
}
func (s IndexerStore) notified(channel string, err error) {
	if s.OnNotify != nil {
		s.OnNotify(channel, err == nil)
	}
}

func (a *Adapter) ReadState(ctx context.Context, chainID int64, deployment string) (indexer.State, error) {
	state, err := a.GetSyncState(ctx, chainID, deployment)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return indexer.State{ChainID: chainID, DeploymentID: deployment}, nil
		}
		return indexer.State{}, fmt.Errorf("read indexer state: %w", err)
	}
	return indexer.State{ChainID: chainID, DeploymentID: deployment, Observed: syncBlock(chainID, state.ObservedNumber, state.ObservedHash, state.ObservedAt), Safe: syncBlock(chainID, state.SafeNumber, state.SafeHash, state.SafeAt), Finalized: syncBlock(chainID, state.FinalizedNumber, state.FinalizedHash, state.FinalizedAt)}, nil
}
func syncBlock(chainID int64, n pgtype.Int8, h *Hash, at pgtype.Timestamptz) *ledger.IndexedBlock {
	if !n.Valid || h == nil || !at.Valid {
		return nil
	}
	return &ledger.IndexedBlock{ChainID: chainID, BlockNumber: n.Int64, BlockHash: common.Hash(*h), BlockTime: at.Time}
}
func (a *Adapter) WriteState(ctx context.Context, state indexer.State) error {
	if state.Observed == nil {
		return nil
	}
	to := func(b *ledger.IndexedBlock) (pgtype.Int8, *Hash, pgtype.Timestamptz) {
		if b == nil {
			return pgtype.Int8{}, nil, pgtype.Timestamptz{}
		}
		h := Hash(b.BlockHash)
		return pgtype.Int8{Int64: b.BlockNumber, Valid: true}, &h, pgtype.Timestamptz{Time: b.BlockTime, Valid: true}
	}
	on, oh, oa := to(state.Observed)
	sn, sh, sa := to(state.Safe)
	fn, fh, fa := to(state.Finalized)
	_, err := a.UpsertSyncState(ctx, SyncState{ChainID: state.ChainID, DeploymentID: state.DeploymentID, ObservedNumber: on, ObservedHash: oh, ObservedAt: oa, SafeNumber: sn, SafeHash: sh, SafeAt: sa, FinalizedNumber: fn, FinalizedHash: fh, FinalizedAt: fa})
	return err
}
func (a *Adapter) ReadBlock(ctx context.Context, chainID, n int64) (ledger.IndexedBlock, bool, error) {
	b, err := a.GetIndexedBlockByNumber(ctx, chainID, n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ledger.IndexedBlock{}, false, nil
		}
		return ledger.IndexedBlock{}, false, err
	}
	return b, true, nil
}
func (a *Adapter) PromoteBlocks(ctx context.Context, chainID int64, safe, final *ledger.IndexedBlock, previousSafe, previousFinalized int64) error {
	if safe != nil {
		if err := a.queries.PromoteIndexedBlocks(ctx, sqlc.PromoteIndexedBlocksParams{ChainID: chainID, BlockNumber: safe.BlockNumber, Column3: "safe", BlockNumber_2: previousSafe}); err != nil {
			return err
		}
	}
	if final != nil {
		if err := a.queries.PromoteIndexedBlocks(ctx, sqlc.PromoteIndexedBlocksParams{ChainID: chainID, BlockNumber: final.BlockNumber, Column3: "finalized", BlockNumber_2: previousFinalized}); err != nil {
			return err
		}
	}
	return nil
}
func (a *Adapter) TokenIdentities(ctx context.Context, chainID int64) ([]indexer.TokenIdentity, error) {
	rows, err := a.queries.ListTokenIdentities(ctx, chainID)
	if err != nil {
		return nil, err
	}
	result := make([]indexer.TokenIdentity, 0, len(rows))
	for _, row := range rows {
		result = append(result, indexer.TokenIdentity{Token: common.Address(row.TokenAddress), Curve: common.Address(row.CurveAddress), Pair: common.Address(row.LpPair), EngineVersion: uint16(row.EngineVersion), Launch: ledger.EventCoordinates{ChainID: chainID, BlockNumber: row.LaunchBlockNumber, BlockHash: common.Hash(row.LaunchBlockHash), BlockTime: row.LaunchBlockTime.Time}})
	}
	return result, nil
}
