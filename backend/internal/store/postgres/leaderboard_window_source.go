package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
)

// LeaderboardWindowMaxAge is how old a precomputed window ranking may be before a read falls back
// to the live query (docs/specs/2026-11-14-windowed-leaderboard-preflight.md).
const LeaderboardWindowMaxAge = 5 * time.Minute

const leaderboardWindowMaxAgeSeconds = int32(LeaderboardWindowMaxAge / time.Second)

// LeaderboardWindowSource is the store side of stats.LeaderboardRefresher.
type LeaderboardWindowSource struct {
	Adapter *Adapter
	ChainID int64
}

// RefreshLeaderboardWindow rewrites one window's precomputed ranking in a single transaction,
// serialized per chain, and reports how many wallets it ranked.
func (s LeaderboardWindowSource) RefreshLeaderboardWindow(ctx context.Context, windowDays int) (int64, error) {
	if windowDays != 7 && windowDays != 30 {
		return 0, fmt.Errorf("leaderboard window %d is not supported", windowDays)
	}
	if s.Adapter == nil || s.Adapter.beginner == nil {
		return 0, errors.New("leaderboard window refresh requires a pool that begins transactions")
	}
	var ranked int64
	err := WithinTx(ctx, s.Adapter.beginner, func(ctx context.Context, tx *Adapter) error {
		if err := tx.queries.LockLeaderboardWindows(ctx, s.ChainID); err != nil {
			return fmt.Errorf("lock leaderboard windows: %w", err)
		}
		days := int32(windowDays)
		if err := tx.queries.DeleteLeaderboardWindowRanks(ctx, sqlc.DeleteLeaderboardWindowRanksParams{ChainID: s.ChainID, WindowDays: days}); err != nil {
			return fmt.Errorf("delete %dd leaderboard ranks: %w", windowDays, err)
		}
		n, err := tx.queries.InsertLeaderboardWindowRanks(ctx, sqlc.InsertLeaderboardWindowRanksParams{ChainID: s.ChainID, WindowDays: days})
		if err != nil {
			return fmt.Errorf("rank %dd leaderboard: %w", windowDays, err)
		}
		if err := tx.queries.UpsertLeaderboardWindowRefresh(ctx, sqlc.UpsertLeaderboardWindowRefreshParams{ChainID: s.ChainID, WindowDays: days}); err != nil {
			return fmt.Errorf("stamp %dd leaderboard refresh: %w", windowDays, err)
		}
		ranked = n
		return nil
	})
	return ranked, err
}
