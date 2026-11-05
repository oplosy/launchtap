package stats

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeMarker struct {
	mu sync.Mutex
	// now is returned by DatabaseNow and advanced by step after each call.
	now        time.Time
	step       time.Duration
	nowErrs    []error
	allResults []fakeMarkResult
	winResults []fakeMarkResult
	allCalls   int
	winCalls   []fakeMarkCall
	// stopAfter closes done once this many windowed calls have been made.
	stopAfter int
	done      chan struct{}
	order     []string
}

type fakeMarkCall struct {
	chainID  int64
	from, to time.Time
}

type fakeMarkResult struct {
	marked int64
	err    error
}

func (f *fakeMarker) DatabaseNow(context.Context) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.nowErrs) > 0 {
		err := f.nowErrs[0]
		f.nowErrs = f.nowErrs[1:]
		if err != nil {
			return time.Time{}, err
		}
	}
	now := f.now
	f.now = f.now.Add(f.step)
	return now, nil
}

func (f *fakeMarker) MarkAllWithVolume(_ context.Context, chainID int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.order = append(f.order, "all")
	index := f.allCalls
	f.allCalls++
	if index < len(f.allResults) {
		return f.allResults[index].marked, f.allResults[index].err
	}
	return 0, nil
}

func (f *fakeMarker) MarkExpiringWindow(_ context.Context, chainID int64, from, to time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.order = append(f.order, "window")
	index := len(f.winCalls)
	f.winCalls = append(f.winCalls, fakeMarkCall{chainID: chainID, from: from, to: to})
	if index+1 == f.stopAfter {
		defer close(f.done)
	}
	if index < len(f.winResults) {
		return f.winResults[index].marked, f.winResults[index].err
	}
	return 0, nil
}

// runSweeper runs the sweeper until the fake has seen stopAfter windowed calls.
func runSweeper(t *testing.T, sweeper WindowSweeper, marker *fakeMarker) {
	t.Helper()
	marker.done = make(chan struct{})
	sweeper.Source = marker
	if sweeper.Interval == 0 {
		sweeper.Interval = time.Millisecond
	}
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

var sweepT0 = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func TestSweeperWindowBounds(t *testing.T) {
	marker := &fakeMarker{now: sweepT0, step: time.Minute, stopAfter: 2}
	runSweeper(t, WindowSweeper{ChainID: 7, Window: 24 * time.Hour, Overlap: 2 * time.Minute}, marker)
	// Tick 1 is the catch-up, which sets watermark = T0-24h. Tick 2 (database now = T0+1m) sweeps
	// [watermark-Overlap, now-Window).
	first, second := marker.winCalls[0], marker.winCalls[1]
	if first.chainID != 7 || !first.from.Equal(sweepT0.Add(-24*time.Hour-2*time.Minute)) || !first.to.Equal(sweepT0.Add(time.Minute-24*time.Hour)) {
		t.Fatalf("first windowed call = %+v", first)
	}
	if !second.from.Equal(first.to.Add(-2*time.Minute)) || !second.to.Equal(first.to.Add(time.Minute)) {
		t.Fatalf("second windowed call = %+v; want from = previous to - overlap", second)
	}
}

func TestSweeperWakesOnlyWhenMarked(t *testing.T) {
	marker := &fakeMarker{now: sweepT0, step: time.Minute, stopAfter: 3, allResults: []fakeMarkResult{{marked: 0}}, winResults: []fakeMarkResult{{marked: 0}, {marked: 3}, {marked: 0}}}
	var wakes int
	runSweeper(t, WindowSweeper{Wake: func() { wakes++ }}, marker)
	if wakes != 1 {
		t.Fatalf("wakes = %d; want exactly 1 (only the sweep that marked rows)", wakes)
	}
}

func TestSweeperWatermarkAdvancesOnlyOnSuccess(t *testing.T) {
	boom := errors.New("boom")
	marker := &fakeMarker{now: sweepT0, step: time.Minute, stopAfter: 3, winResults: []fakeMarkResult{{err: boom}, {marked: 1}, {}}}
	var errs []error
	runSweeper(t, WindowSweeper{OnError: func(err error) { errs = append(errs, err) }}, marker)
	if len(errs) != 1 || !errors.Is(errs[0], boom) {
		t.Fatalf("OnError calls = %v; want the single sweep error", errs)
	}
	failed, retry, next := marker.winCalls[0], marker.winCalls[1], marker.winCalls[2]
	// The failed call did not advance the watermark, so the retry reaches back to the same from
	// and covers the gap up to the newer now.
	if !retry.from.Equal(failed.from) || !retry.to.Equal(failed.to.Add(time.Minute)) {
		t.Fatalf("retry = %+v after failed %+v; want same from and a later to", retry, failed)
	}
	// The successful retry advanced the watermark to its to.
	if !next.from.Equal(retry.to.Add(-2 * time.Minute)) {
		t.Fatalf("next from = %s; want retry.to - overlap", next.from)
	}
}

func TestSweeperCatchUpRunsFirstAndIsRetried(t *testing.T) {
	boom := errors.New("boom")
	marker := &fakeMarker{now: sweepT0, step: time.Minute, stopAfter: 1, allResults: []fakeMarkResult{{err: boom}, {marked: 2}}}
	var errs []error
	var wakes int
	runSweeper(t, WindowSweeper{OnError: func(err error) { errs = append(errs, err) }, Wake: func() { wakes++ }}, marker)
	want := []string{"all", "all", "window"}
	if len(marker.order) < 3 || marker.order[0] != want[0] || marker.order[1] != want[1] || marker.order[2] != want[2] {
		t.Fatalf("call order = %v; want %v (no windowed sweep before the catch-up succeeds)", marker.order, want)
	}
	if len(errs) != 1 || wakes != 1 {
		t.Fatalf("errors = %v, wakes = %d; want one error and one wake from the retried catch-up", errs, wakes)
	}
	// The catch-up set the watermark from the database clock of the successful attempt (T0+1m).
	if got, wantFrom := marker.winCalls[0].from, sweepT0.Add(time.Minute-24*time.Hour-2*time.Minute); !got.Equal(wantFrom) {
		t.Fatalf("first windowed from = %s; want %s", got, wantFrom)
	}
}

func TestSweeperDatabaseClockErrorIsReported(t *testing.T) {
	boom := errors.New("clock down")
	marker := &fakeMarker{now: sweepT0, step: time.Minute, stopAfter: 1, nowErrs: []error{boom}}
	var errs []error
	runSweeper(t, WindowSweeper{OnError: func(err error) { errs = append(errs, err) }}, marker)
	if len(errs) != 1 || !errors.Is(errs[0], boom) {
		t.Fatalf("OnError calls = %v; want the clock error", errs)
	}
}
