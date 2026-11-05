package stats

import (
	"context"
	"fmt"
	"time"
)

const (
	DefaultSweepInterval = 60 * time.Second
	DefaultSweepWindow   = 24 * time.Hour
	DefaultSweepOverlap  = 2 * time.Minute
)

// ExpiringWindowMarker is the store side of the sweeper. All times come from the
// database clock, the same one the stats recompute uses for its rolling window.
type ExpiringWindowMarker interface {
	// MarkExpiringWindow marks every token that traded in [from, to) dirty and reports how
	// many tokens were marked.
	MarkExpiringWindow(ctx context.Context, chainID int64, from, to time.Time) (int64, error)
	// MarkAllWithVolume marks every token with a non-zero 24h volume dirty (startup catch-up).
	MarkAllWithVolume(ctx context.Context, chainID int64) (int64, error)
	// DatabaseNow returns the database's current time.
	DatabaseNow(ctx context.Context) (time.Time, error)
}

// WindowSweeper re-marks tokens whose trades are leaving the rolling window,
// so a token that stopped trading still has its windowed stats recomputed.
//
// It keeps an in-memory watermark: each tick marks [watermark-Overlap, now-Window) and
// advances the watermark to now-Window only when that succeeds, so a failed tick is
// covered by the next one. Expiries missed while the process was down are covered by a
// startup catch-up that marks every token with non-zero volume; it is retried on each
// tick until it succeeds, before any windowed sweep.
type WindowSweeper struct {
	Source   ExpiringWindowMarker
	ChainID  int64
	Interval time.Duration // default 60s
	Window   time.Duration // default 24h
	Overlap  time.Duration // default 2m
	Wake     func()
	OnError  func(error)
}

type sweepState struct {
	caughtUp  bool
	watermark time.Time
}

func (s WindowSweeper) Run(ctx context.Context) error {
	interval := s.Interval
	if interval <= 0 {
		interval = DefaultSweepInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var state sweepState
	for {
		if err := s.sweep(ctx, &state); err != nil && s.OnError != nil && ctx.Err() == nil {
			s.OnError(err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s WindowSweeper) sweep(ctx context.Context, state *sweepState) error {
	window, overlap := s.Window, s.Overlap
	if window <= 0 {
		window = DefaultSweepWindow
	}
	if overlap <= 0 {
		overlap = DefaultSweepOverlap
	}
	now, err := s.Source.DatabaseNow(ctx)
	if err != nil {
		return fmt.Errorf("read database clock: %w", err)
	}
	to := now.Add(-window)
	var marked int64
	if !state.caughtUp {
		if marked, err = s.Source.MarkAllWithVolume(ctx, s.ChainID); err != nil {
			return fmt.Errorf("startup catch-up: %w", err)
		}
		state.caughtUp, state.watermark = true, to
	} else {
		if marked, err = s.Source.MarkExpiringWindow(ctx, s.ChainID, state.watermark.Add(-overlap), to); err != nil {
			return fmt.Errorf("window sweep: %w", err)
		}
		state.watermark = to
	}
	if marked > 0 && s.Wake != nil {
		s.Wake()
	}
	return nil
}
