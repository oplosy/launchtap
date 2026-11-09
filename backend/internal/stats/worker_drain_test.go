package stats

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// queueSource hands out its pending claims in batches and records each claim call's size.
type queueSource struct {
	mu       sync.Mutex
	pending  []Claim
	sizes    []int
	claimed  chan struct{}
	onClaim  func(call int)
	computed int
}

func newQueueSource(n int) *queueSource {
	s := &queueSource{claimed: make(chan struct{}, 64)}
	for i := range n {
		s.pending = append(s.pending, Claim{ChainID: 1, Token: [20]byte{byte(i + 1)}, Generation: int64(i + 1)})
	}
	return s
}

func (s *queueSource) Claim(_ context.Context, _ string, batch int32) ([]Claim, error) {
	s.mu.Lock()
	take := min(int(batch), len(s.pending))
	out := append([]Claim(nil), s.pending[:take]...)
	s.pending = s.pending[take:]
	s.sizes = append(s.sizes, take)
	call := len(s.sizes)
	s.mu.Unlock()
	if s.onClaim != nil {
		s.onClaim(call)
	}
	s.claimed <- struct{}{}
	return out, nil
}

func (s *queueSource) Compute(context.Context, Claim) error {
	s.mu.Lock()
	s.computed++
	s.mu.Unlock()
	return nil
}

func (s *queueSource) Complete(context.Context, Claim, string) (bool, error) { return true, nil }

func (s *queueSource) snapshot() ([]int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.sizes...), s.computed
}

// awaitClaims waits for n claim calls, then gives the worker time to make an unexpected one.
func awaitClaims(t *testing.T, s *queueSource, n int) {
	t.Helper()
	for range n {
		select {
		case <-s.claimed:
		case <-time.After(5 * time.Second):
			sizes, _ := s.snapshot()
			t.Fatalf("worker made %d claim calls, want %d", len(sizes), n)
		}
	}
	select {
	case <-s.claimed:
		sizes, _ := s.snapshot()
		t.Fatalf("worker made an extra claim call: sizes %v", sizes)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWorkerDrainsFullBatchesUntilShort(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source := newQueueSource(5)
	worker := Worker{Source: source, WorkerID: "drain", PollInterval: time.Hour, BatchSize: 2, DrainFullBatches: true}
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	awaitClaims(t, source, 3)
	sizes, computed := source.snapshot()
	if want := []int{2, 2, 1}; len(sizes) != 3 || sizes[0] != want[0] || sizes[1] != want[1] || sizes[2] != want[2] || computed != 5 {
		t.Fatalf("claim sizes = %v computed = %d, want %v and 5", sizes, computed, want)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() = %v, want context.Canceled", err)
	}
}

func TestWorkerDrainStopsOnEmptyFullBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source := newQueueSource(4)
	worker := Worker{Source: source, WorkerID: "drain", PollInterval: time.Hour, BatchSize: 2, DrainFullBatches: true}
	go func() { _ = worker.Run(ctx) }()
	awaitClaims(t, source, 3) // 2, 2, then an empty claim ends the drain
	if sizes, _ := source.snapshot(); sizes[2] != 0 {
		t.Fatalf("claim sizes = %v, want a final empty claim", sizes)
	}
}

func TestWorkerWithoutDrainClaimsOncePerWake(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source := newQueueSource(5)
	wake := make(chan struct{}, 1)
	worker := Worker{Source: source, WorkerID: "aggregation", PollInterval: time.Hour, BatchSize: 2, Wake: wake}
	go func() { _ = worker.Run(ctx) }()
	awaitClaims(t, source, 1)
	wake <- struct{}{}
	awaitClaims(t, source, 1)
	if sizes, computed := source.snapshot(); len(sizes) != 2 || computed != 4 {
		t.Fatalf("claim sizes = %v computed = %d, want one full batch per wake", sizes, computed)
	}
}

func TestWorkerDrainHonorsCancellationBetweenBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source := newQueueSource(10)
	source.onClaim = func(call int) {
		if call == 2 {
			cancel()
		}
	}
	worker := Worker{Source: source, WorkerID: "drain", PollInterval: time.Hour, BatchSize: 2, DrainFullBatches: true}
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("draining worker ignored cancellation")
	}
	if sizes, _ := source.snapshot(); len(sizes) != 2 {
		t.Fatalf("claim sizes = %v, want no claim after cancellation", sizes)
	}
}
