package stats

import (
	"context"
	"fmt"
	"time"
)

const DefaultDirtyPollInterval = 5 * time.Second

type Claim struct {
	ChainID int64
	// Wallet is set only for wallet position claims; token claims leave it zero.
	Wallet     [20]byte
	Token      [20]byte
	Generation int64
}
type DirtySource interface {
	Claim(context.Context, string, int32) ([]Claim, error)
	Compute(context.Context, Claim) error
	Complete(context.Context, Claim, string) (bool, error)
}

// BatchComputer can coalesce work shared by multiple claims while preserving
// one result per claim. A failed result leaves that claim dirty for retry.
type BatchComputer interface {
	ComputeBatch(context.Context, []Claim) []error
}

// Worker treats notifications as hints and polls the durable dirty table on a
// fixed interval. A lost NOTIFY therefore delays work only until the next poll.
type Worker struct {
	Source       DirtySource
	WorkerID     string
	PollInterval time.Duration
	BatchSize    int32
	Wake         <-chan struct{}
	OnError      func(Claim, error)
	// OnComputed receives the number of claims completed per drain.
	OnComputed func(count int)
}

func (w Worker) Run(ctx context.Context) error {
	interval := w.PollInterval
	if interval <= 0 {
		interval = DefaultDirtyPollInterval
	}
	batch := w.BatchSize
	if batch <= 0 {
		batch = 32
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := w.drain(ctx, batch); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case <-w.Wake:
		}
	}
}
func (w Worker) drain(ctx context.Context, batch int32) error {
	claims, err := w.Source.Claim(ctx, w.WorkerID, batch)
	if err != nil {
		return err
	}
	completed := 0
	defer func() {
		if w.OnComputed != nil {
			w.OnComputed(completed)
		}
	}()
	if batchComputer, ok := w.Source.(BatchComputer); ok {
		batchErrors := batchComputer.ComputeBatch(ctx, claims)
		if len(batchErrors) != len(claims) {
			return fmt.Errorf("batch compute returned %d results for %d claims", len(batchErrors), len(claims))
		}
		for i, claim := range claims {
			if err := batchErrors[i]; err != nil {
				if w.OnError != nil {
					w.OnError(claim, err)
				}
				continue
			}
			if _, err := w.Source.Complete(ctx, claim, w.WorkerID); err != nil {
				return err
			}
			completed++
		}
		return nil
	}
	for _, claim := range claims {
		if err := w.Source.Compute(ctx, claim); err != nil {
			if w.OnError != nil {
				w.OnError(claim, err)
			}
			continue
		}
		if _, err := w.Source.Complete(ctx, claim, w.WorkerID); err != nil {
			return err
		}
		completed++
	}
	return nil
}
