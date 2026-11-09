package swapbackfill

import (
	"context"
	"errors"
	"time"

	"github.com/Contictus/launchtap/backend/internal/redact"
)

// Defaults shared by the manual command and the indexer's scheduler: one RPC call of
// DefaultBatch hashes at most DefaultRate times per second.
const (
	DefaultRate  = 5
	DefaultBatch = 50
)

// RateSleeper returns a Deps.Sleep that waits 1/rate seconds, or less if ctx ends first.
func RateSleeper(rate float64) func(context.Context) error {
	interval := time.Duration(float64(time.Second) / rate)
	return func(ctx context.Context) error {
		timer := time.NewTimer(interval)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
}

// Scheduler runs bounded Run passes periodically inside the indexer, so a sender lookup that
// missed during ingest (a lagging RPC node returning null for a just-mined transaction) is
// retried without the manual command. Passes run one at a time on the caller's goroutine.
//
// Every pass is limited to swaps at or below the indexer's committed safe head: those blocks
// can no longer be reorged out by ingest, so the pass never races the write path. A pass resumes
// after the cursor where the previous capped pass stopped and wraps to the oldest row once a pass
// reaches the end, so transactions that never resolve cannot starve newer misses behind the cap.
// A failing pass is logged (redacted) and counted, never returned: it must not stop ingest or
// mark the indexer unhealthy. A Scheduler is not safe for concurrent use.
type Scheduler struct {
	Deps Deps
	// Interval between passes; 0 or less disables the scheduler. The first pass runs one
	// interval after Run starts.
	Interval time.Duration
	// Batch is the transaction hashes per RPC call; MaxTransactions caps one pass.
	Batch           int
	MaxTransactions int
	// SafeBlock returns the indexer's committed safe block; ok is false before one exists.
	SafeBlock func(context.Context) (block int64, ok bool, err error)
	// Backlog, when set, counts NULL-sender swaps at or below the safe head after each pass;
	// OnBacklog receives the count.
	Backlog   func(ctx context.Context, throughBlock int64) (int64, error)
	OnBacklog func(pending int64)
	// OnFilled receives the number of transactions whose sender a pass wrote (optional).
	OnFilled func(transactions int)
	// OnError is called once per failed pass (optional).
	OnError func()
	// Secrets are exact strings (the RPC and database URLs) removed from logged errors.
	Secrets []string

	// cursor is where the next pass resumes; nil starts at the oldest row.
	cursor *Cursor
}

// Run executes one pass per Interval until ctx is cancelled and then returns ctx.Err(). With a
// non-positive Interval it returns nil at once.
func (s *Scheduler) Run(ctx context.Context) error {
	if s.Interval <= 0 {
		return nil
	}
	timer := time.NewTimer(s.Interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		s.Pass(ctx)
		timer.Reset(s.Interval)
	}
}

// Pass runs one bounded backfill pass up to the safe head and logs one summary line. Unresolved
// transactions are not a pass failure (a later pass retries them); a shutdown cancellation is not
// one either. After a failed pass the cursor is kept, so the same range is retried.
func (s *Scheduler) Pass(ctx context.Context) {
	safe, ok, err := s.SafeBlock(ctx)
	if err != nil {
		s.failed(ctx, "read safe head", err)
		return
	}
	if !ok {
		return
	}
	summary, err := Run(ctx, s.Deps, Options{Batch: s.Batch, ThroughBlock: &safe, MaxTransactions: s.MaxTransactions, Start: s.cursor, QuietUnresolved: true})
	if summary.Attributed > 0 && s.OnFilled != nil {
		s.OnFilled(summary.Attributed)
	}
	if err != nil && !errors.Is(err, ErrSkipped) {
		s.failed(ctx, "backfill", err)
		return
	}
	resumedFrom := s.cursor
	if summary.ReachedEnd {
		s.cursor = nil
	} else {
		next := summary.Cursor
		s.cursor = &next
	}
	if summary.Listed > 0 {
		s.Deps.Log.Info("sender backfill pass finished", "safe_block", safe, "resumed", resumedFrom != nil,
			"capped", !summary.ReachedEnd, "transactions_listed", summary.Listed, "transactions_attributed", summary.Attributed,
			"swaps_updated", summary.Updated, "transactions_unresolved", summary.Skipped)
	}
	if s.Backlog != nil {
		pending, err := s.Backlog(ctx, safe)
		if err != nil {
			s.failed(ctx, "count backlog", err)
			return
		}
		if s.OnBacklog != nil {
			s.OnBacklog(pending)
		}
	}
}

func (s *Scheduler) failed(ctx context.Context, stage string, err error) {
	if ctx.Err() != nil {
		return
	}
	if s.OnError != nil {
		s.OnError()
	}
	s.Deps.Log.Warn("sender backfill pass failed; retrying next interval", "stage", stage, "error", redact.Error(err, s.Secrets...))
}
