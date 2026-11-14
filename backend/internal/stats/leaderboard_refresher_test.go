package stats

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeLeaderboardSource struct {
	mu    sync.Mutex
	calls []int
	fail  map[int]error
	done  chan struct{}
	stop  int
}

func (f *fakeLeaderboardSource) RefreshLeaderboardWindow(_ context.Context, windowDays int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, windowDays)
	if len(f.calls) == f.stop {
		close(f.done)
	}
	if err := f.fail[windowDays]; err != nil {
		return 0, err
	}
	return int64(windowDays), nil
}

func TestLeaderboardRefresherRefreshesEveryWindowOnStartAndEachTick(t *testing.T) {
	failure := errors.New("rank failed")
	source := &fakeLeaderboardSource{fail: map[int]error{7: failure}, done: make(chan struct{}), stop: 4}
	var mu sync.Mutex
	var refreshed, failed []int
	refresher := LeaderboardRefresher{
		Source: source, Windows: []int{7, 30}, Interval: 10 * time.Millisecond,
		OnRefreshed: func(windowDays int, ranked int64) {
			mu.Lock()
			defer mu.Unlock()
			if ranked != int64(windowDays) {
				t.Errorf("window %d reported %d ranked", windowDays, ranked)
			}
			refreshed = append(refreshed, windowDays)
		},
		OnError: func(windowDays int, err error) {
			mu.Lock()
			defer mu.Unlock()
			if !errors.Is(err, failure) {
				t.Errorf("window %d error = %v", windowDays, err)
			}
			failed = append(failed, windowDays)
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- refresher.Run(ctx) }()
	select {
	case <-source.done:
	case <-time.After(5 * time.Second):
		t.Fatal("refresher did not run a second tick")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want context.Canceled", err)
	}
	source.mu.Lock()
	calls := append([]int(nil), source.calls[:4]...)
	source.mu.Unlock()
	// A failing 7d window neither stops the loop nor skips the 30d window.
	if want := []int{7, 30, 7, 30}; !equalInts(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(failed) < 2 || failed[0] != 7 || failed[1] != 7 {
		t.Fatalf("errors reported for %v, want the 7d window on both ticks", failed)
	}
	if len(refreshed) < 2 || refreshed[0] != 30 || refreshed[1] != 30 {
		t.Fatalf("refreshes reported for %v, want the 30d window on both ticks", refreshed)
	}
}

func TestLeaderboardRefresherStopsBeforeRefreshingWhenCancelled(t *testing.T) {
	source := &fakeLeaderboardSource{done: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := (LeaderboardRefresher{Source: source, Windows: []int{7, 30}}).Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want context.Canceled", err)
	}
	if len(source.calls) != 0 {
		t.Fatalf("cancelled refresher refreshed %v", source.calls)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
