package postgres

import (
	"context"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgtype"
)

// Aggregation worker storage: dirty claims, token stats and protocol aggregates.

func (adapter *Adapter) ClaimAggregationDirty(
	ctx context.Context,
	workerID string,
	batchSize int32,
) ([]DirtyClaim, error) {
	rows, err := adapter.queries.ClaimAggregationDirty(ctx, sqlc.ClaimAggregationDirtyParams{
		WorkerID: pgtype.Text{String: workerID, Valid: true}, BatchSize: batchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("claim dirty aggregation: %w", err)
	}
	claims := make([]DirtyClaim, 0, len(rows))
	for _, row := range rows {
		if !row.ClaimedGeneration.Valid {
			return nil, fmt.Errorf("claim dirty aggregation: returned NULL claimed generation")
		}
		claims = append(claims, DirtyClaim{
			ChainID: row.ChainID, TokenAddress: row.TokenAddress,
			ClaimedGeneration: row.ClaimedGeneration.Int64,
		})
	}
	return claims, nil
}

func (adapter *Adapter) CompleteAggregationDirty(
	ctx context.Context,
	claim DirtyClaim,
	workerID string,
) (bool, error) {
	rows, err := adapter.queries.CompleteAggregationDirty(ctx, sqlc.CompleteAggregationDirtyParams{
		ChainID: claim.ChainID, TokenAddress: claim.TokenAddress,
		ClaimedGeneration: claim.ClaimedGeneration,
		WorkerID:          pgtype.Text{String: workerID, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("complete dirty aggregation: %w", err)
	}
	return rows == 1, nil
}

func (adapter *Adapter) RecomputeTokenStats(ctx context.Context, chainID int64, token common.Address) error {
	if err := adapter.queries.RecomputeTokenStats(ctx, sqlc.RecomputeTokenStatsParams{ChainID: chainID, TokenAddress: sqlc.Address(token)}); err != nil {
		return fmt.Errorf("recompute token stats: %w", err)
	}
	return nil
}

func (adapter *Adapter) DeleteTokenStats(ctx context.Context, chainID int64, token common.Address) error {
	if _, err := adapter.queries.DeleteTokenStats(ctx, sqlc.DeleteTokenStatsParams{ChainID: chainID, TokenAddress: sqlc.Address(token)}); err != nil {
		return fmt.Errorf("delete token stats: %w", err)
	}
	return nil
}

// RecomputeProtocolAggregates rebuilds protocol_daily and protocol_stats from the whole history
// and resets the daily watermark. Reorg recovery uses it after deleting canonical rows.
func (adapter *Adapter) RecomputeProtocolAggregates(ctx context.Context, chainID int64) error {
	return adapter.withinProtocolTx(ctx, func(ctx context.Context, transaction *Adapter) error {
		return transaction.refreshProtocolAggregates(ctx, chainID, true)
	})
}

// RefreshProtocolAggregates brings the protocol aggregates up to the indexed head, rebuilding
// only the UTC days of blocks indexed since the last refresh. It falls back to a full rebuild
// when no watermark exists yet or the watermark is above the head.
func (adapter *Adapter) RefreshProtocolAggregates(ctx context.Context, chainID int64) error {
	return adapter.withinProtocolTx(ctx, func(ctx context.Context, transaction *Adapter) error {
		return transaction.refreshProtocolAggregates(ctx, chainID, false)
	})
}

func (adapter *Adapter) withinProtocolTx(ctx context.Context, fn func(context.Context, *Adapter) error) error {
	if adapter.beginner != nil {
		return WithinTx(ctx, adapter.beginner, fn)
	}
	return fn(ctx, adapter)
}

// refreshProtocolAggregates runs within the caller's transaction. The advisory lock is taken
// before the watermark read so a concurrent refresh, including a reorg's full rebuild, is fully
// committed before this one decides which days to rebuild.
func (adapter *Adapter) refreshProtocolAggregates(ctx context.Context, chainID int64, full bool) error {
	if err := adapter.queries.LockProtocolAggregates(ctx, chainID); err != nil {
		return fmt.Errorf("lock protocol aggregates: %w", err)
	}
	bounds, err := adapter.queries.GetProtocolAggregateBounds(ctx, chainID)
	if err != nil {
		return fmt.Errorf("read protocol aggregate bounds: %w", err)
	}
	// -1 means "none" for both values.
	if bounds.DailyThroughBlock < 0 || bounds.DailyThroughBlock > bounds.HeadBlock {
		full = true
	}
	fromDay := pgtype.Date{InfinityModifier: pgtype.NegativeInfinity, Valid: true}
	rebuild := full
	if !full && bounds.HeadBlock > bounds.DailyThroughBlock {
		fromDay, err = adapter.queries.GetProtocolDailyRefreshDay(ctx, sqlc.GetProtocolDailyRefreshDayParams{
			ChainID: chainID, AfterBlock: bounds.DailyThroughBlock, ThroughBlock: bounds.HeadBlock,
		})
		if err != nil {
			return fmt.Errorf("read protocol daily refresh day: %w", err)
		}
		if !fromDay.Valid {
			return fmt.Errorf("no indexed block between %d and head %d", bounds.DailyThroughBlock, bounds.HeadBlock)
		}
		rebuild = true
	}
	if rebuild {
		if err := adapter.queries.ClearProtocolDaily(ctx, sqlc.ClearProtocolDailyParams{ChainID: chainID, FromDay: fromDay}); err != nil {
			return fmt.Errorf("clear protocol daily: %w", err)
		}
		if err := adapter.queries.RecomputeProtocolDaily(ctx, sqlc.RecomputeProtocolDailyParams{ChainID: chainID, FromDay: fromDay}); err != nil {
			return fmt.Errorf("recompute protocol daily: %w", err)
		}
	}
	through := pgtype.Int8{Int64: bounds.HeadBlock, Valid: bounds.HeadBlock >= 0}
	if err := adapter.queries.RecomputeProtocolStats(ctx, sqlc.RecomputeProtocolStatsParams{ChainID: chainID, DailyThroughBlock: through}); err != nil {
		return fmt.Errorf("recompute protocol aggregates: %w", err)
	}
	return nil
}
