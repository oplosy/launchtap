package stats

import (
	"context"
	"time"
)

const (
	DefaultSweepInterval = 60 * time.Second
	DefaultSweepWindow   = 24 * time.Hour
	DefaultSweepOverlap  = 2 * time.Minute
)

// ExpiringWindowMarker marks every token that traded in [from, to) dirty and
// reports how many tokens were marked.
type ExpiringWindowMarker interface {
	MarkExpiringWindow(ctx context.Context, chainID int64, from, to time.Time) (int64, error)
}

// WindowSweeper re-marks tokens whose trades are leaving the rolling window,
// so a token that stopped trading still has its windowed stats recomputed.
// Each sweep covers [now-Window-Overlap, now-Window); the overlap absorbs
// missed or delayed ticks, and re-marking is idempotent.
type WindowSweeper struct {
	Source   ExpiringWindowMarker
	ChainID  int64
	Interval time.Duration // default 60s
	Window   time.Duration // default 24h
	Overlap  time.Duration // default 2m
	Now      func() time.Time
	Wake     func()
	OnError  func(error)
}

func (s WindowSweeper) Run(ctx context.Context) error {
	interval := s.Interval
	if interval <= 0 {
		interval = DefaultSweepInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.sweep(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s WindowSweeper) sweep(ctx context.Context) {
	window, overlap, now := s.Window, s.Overlap, s.Now
	if window <= 0 {
		window = DefaultSweepWindow
	}
	if overlap <= 0 {
		overlap = DefaultSweepOverlap
	}
	if now == nil {
		now = time.Now
	}
	to := now().Add(-window)
	marked, err := s.Source.MarkExpiringWindow(ctx, s.ChainID, to.Add(-overlap), to)
	if err != nil {
		if s.OnError != nil && ctx.Err() == nil {
			s.OnError(err)
		}
		return
	}
	if marked > 0 && s.Wake != nil {
		s.Wake()
	}
}
