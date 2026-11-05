package stats

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeMarker struct {
	mu      sync.Mutex
	calls   []fakeMarkCall
	results []fakeMarkResult
	done    chan struct{}
}

type fakeMarkCall struct {
	chainID  int64
	from, to time.Time
}

type fakeMarkResult struct {
	marked int64
	err    error
}

func (f *fakeMarker) MarkExpiringWindow(_ context.Context, chainID int64, from, to time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	index := len(f.calls)
	f.calls = append(f.calls, fakeMarkCall{chainID: chainID, from: from, to: to})
	if index+1 == len(f.results) {
		close(f.done)
	}
	if index < len(f.results) {
		return f.results[index].marked, f.results[index].err
	}
	return 0, nil
}

func runSweeper(t *testing.T, sweeper WindowSweeper, marker *fakeMarker) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- sweeper.Run(ctx) }()
	select {
	case <-marker.done:
	case <-ctx.Done():
		t.Fatal("sweeper did not perform the expected number of sweeps")
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v; want context.Canceled", err)
	}
}

func TestSweeperWindowBounds(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	marker := &fakeMarker{results: make([]fakeMarkResult, 1), done: make(chan struct{})}
	runSweeper(t, WindowSweeper{Source: marker, ChainID: 7, Interval: time.Millisecond, Window: 24 * time.Hour, Overlap: 2 * time.Minute, Now: func() time.Time { return now }}, marker)
	call := marker.calls[0]
	if call.chainID != 7 || !call.from.Equal(now.Add(-24*time.Hour-2*time.Minute)) || !call.to.Equal(now.Add(-24*time.Hour)) {
		t.Fatalf("call = %+v; want chain 7, from T-24h-2m, to T-24h", call)
	}
}

func TestSweeperWakesOnlyWhenMarked(t *testing.T) {
	marker := &fakeMarker{results: []fakeMarkResult{{marked: 0}, {marked: 3}}, done: make(chan struct{})}
	var wakes int
	runSweeper(t, WindowSweeper{Source: marker, Interval: time.Millisecond, Wake: func() { wakes++ }}, marker)
	// Run has returned, so the goroutine no longer touches wakes. Further sweeps mark nothing.
	if wakes != 1 {
		t.Fatalf("wakes = %d; want exactly 1 (only the sweep that marked rows)", wakes)
	}
}

func TestSweeperErrorContinues(t *testing.T) {
	boom := errors.New("boom")
	marker := &fakeMarker{results: []fakeMarkResult{{err: boom}, {marked: 1}}, done: make(chan struct{})}
	var errs []error
	var wakes int
	runSweeper(t, WindowSweeper{Source: marker, Interval: time.Millisecond, OnError: func(err error) { errs = append(errs, err) }, Wake: func() { wakes++ }}, marker)
	if len(errs) != 1 || !errors.Is(errs[0], boom) {
		t.Fatalf("OnError calls = %v; want the single sweep error", errs)
	}
	if len(marker.calls) < 2 || wakes != 1 {
		t.Fatalf("calls = %d, wakes = %d; want the next tick to run and wake once", len(marker.calls), wakes)
	}
}
