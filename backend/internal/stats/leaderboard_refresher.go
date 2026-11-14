package stats

import (
	"context"
	"time"
)

// DefaultLeaderboardRefreshInterval is how often the rolling-window leaderboards are re-ranked.
const DefaultLeaderboardRefreshInterval = 30 * time.Second

// LeaderboardWindowRefresher is the store side of LeaderboardRefresher.
type LeaderboardWindowRefresher interface {
	// RefreshLeaderboardWindow rewrites the window's precomputed ranking and reports how many
	// wallets it ranked.
	RefreshLeaderboardWindow(ctx context.Context, windowDays int) (int64, error)
}

// LeaderboardRefresher re-ranks each rolling-window leaderboard on start and on every tick
// (docs/specs/2026-11-14-windowed-leaderboard-preflight.md). A timer is the trigger because the
// window moves with time even when no block arrives, and the realized legs it sums are written by
// the position worker. A failed window is reported and retried on the next tick; it never stops
// the loop or the other windows.
type LeaderboardRefresher struct {
	Source      LeaderboardWindowRefresher
	Windows     []int
	Interval    time.Duration // default 30s
	OnRefreshed func(windowDays int, ranked int64)
	OnError     func(windowDays int, err error)
}

func (r LeaderboardRefresher) Run(ctx context.Context) error {
	interval := r.Interval
	if interval <= 0 {
		interval = DefaultLeaderboardRefreshInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for _, window := range r.Windows {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			ranked, err := r.Source.RefreshLeaderboardWindow(ctx, window)
			switch {
			case err != nil:
				if r.OnError != nil && ctx.Err() == nil {
					r.OnError(window, err)
				}
			case r.OnRefreshed != nil:
				r.OnRefreshed(window, ranked)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
