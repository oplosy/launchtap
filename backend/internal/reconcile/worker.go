package reconcile

import (
	"context"
	"errors"
	"time"
)

// Default cadence: the first pass a minute after start, then every ten minutes.
const (
	DefaultInterval     = 10 * time.Minute
	DefaultInitialDelay = time.Minute
	// passTimeout bounds one pass so a stalled RPC cannot hold the loop.
	passTimeout = 2 * time.Minute
)

// Worker runs Pass on a fixed cadence until its context ends. It only reads; a failed pass is
// reported and the next one retries.
type Worker struct {
	Store        Store
	Chain        Chain
	Limits       Limits
	Interval     time.Duration
	InitialDelay time.Duration
	// OnResult receives every pass, including failed ones (Outcome error).
	OnResult func(Result)
	// OnError receives the error of a failed pass.
	OnError func(error)
}

// Run blocks until ctx ends and returns its error.
func (w Worker) Run(ctx context.Context) error {
	if w.Store == nil || w.Chain == nil {
		return errors.New("reconcile worker requires a store and a chain reader")
	}
	interval, delay := w.Interval, w.InitialDelay
	if interval <= 0 {
		interval = DefaultInterval
	}
	if delay <= 0 {
		delay = DefaultInitialDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		w.once(ctx)
		timer.Reset(interval)
	}
}

func (w Worker) once(ctx context.Context) {
	passCtx, cancel := context.WithTimeout(ctx, passTimeout)
	defer cancel()
	result, err := Pass(passCtx, w.Store, w.Chain, w.Limits)
	if err != nil && ctx.Err() != nil {
		return // shutting down: not a failed pass
	}
	if err != nil && w.OnError != nil {
		w.OnError(err)
	}
	if w.OnResult != nil {
		w.OnResult(result)
	}
}
