package chain

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// concurrentSource serializes an inner fake source and records how many calls were in flight.
type concurrentSource struct {
	inner    LogSource
	delay    time.Duration
	mu       sync.Mutex
	inflight int
	peak     int
	calls    int
}

func (s *concurrentSource) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	s.mu.Lock()
	s.inflight++
	s.calls++
	s.peak = max(s.peak, s.inflight)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inflight--
		s.mu.Unlock()
	}()
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.FilterLogs(ctx, query)
}

func TestWithLogConcurrencyRejectsOutOfRangeLimits(t *testing.T) {
	t.Parallel()
	_, _, emitters := loadFixtureLogs(t)
	decoder, _ := NewDecoder(1)
	for _, limit := range []int{0, -1, MaxLogFetchConcurrency + 1} {
		discoverer, _ := NewDiscoverer(&fixtureLogSource{}, decoder, emitters.Factory, 1)
		if _, err := discoverer.WithLogConcurrency(limit); err == nil {
			t.Fatalf("WithLogConcurrency(%d) accepted", limit)
		}
	}
}

func TestParallelDiscoveryMatchesSequential(t *testing.T) {
	t.Parallel()
	_, logs, emitters := loadFixtureLogs(t)
	decoder, _ := NewDecoder(1)
	discover := func(limit int) (DiscoveryResult, *concurrentSource) {
		t.Helper()
		source := &concurrentSource{inner: &fixtureLogSource{logs: logs}, delay: 2 * time.Millisecond}
		discoverer, err := NewDiscoverer(source, decoder, emitters.Factory, 1)
		if err != nil {
			t.Fatal(err)
		}
		if discoverer, err = discoverer.WithLogConcurrency(limit); err != nil {
			t.Fatal(err)
		}
		result, err := discoverer.Discover(context.Background(), 0, maxBlock(logs), emitters)
		if err != nil {
			t.Fatalf("Discover(concurrency %d) error = %v", limit, err)
		}
		return result, source
	}
	sequential, sequentialSource := discover(1)
	parallel, parallelSource := discover(4)
	if len(sequential.Logs) == 0 {
		t.Fatal("fixture precondition: no logs discovered")
	}
	if !reflect.DeepEqual(sequential, parallel) {
		t.Fatal("parallel discovery differs from sequential discovery")
	}
	if sequentialSource.peak != 1 || sequentialSource.calls != parallelSource.calls {
		t.Fatalf("sequential peak %d calls %d, parallel calls %d", sequentialSource.peak, sequentialSource.calls, parallelSource.calls)
	}
	if parallelSource.peak < 2 || parallelSource.peak > 4 {
		t.Fatalf("parallel peak in-flight calls = %d, want 2..4", parallelSource.peak)
	}
}

func TestParallelDiscoveryV2MatchesSequential(t *testing.T) {
	t.Parallel()
	fixture := loadV2DiscoveryLogs(t)
	logs := []types.Log{
		at(fixture.launchLog, 10, 0, 0),
		at(fixture.initializeLog, 12, 1, 3),
		at(fixture.graduatedLog, 12, 1, 5),
		at(fixture.swapLog, 12, 2, 0),
		at(fixture.lockerLog, 12, 3, 0),
	}
	discover := func(limit int) (DiscoveryResult, *concurrentSource) {
		t.Helper()
		source := &concurrentSource{inner: &fixtureLogSource{logs: logs}, delay: 2 * time.Millisecond}
		discoverer, err := NewDiscovererV2(source, fixture.decoder, fixture.emitters.Factory, 10, fixture.config())
		if err != nil {
			t.Fatal(err)
		}
		if discoverer, err = discoverer.WithLogConcurrency(limit); err != nil {
			t.Fatal(err)
		}
		result, err := discoverer.Discover(context.Background(), 10, 12, emptyEmitters(fixture.emitters.Factory))
		if err != nil {
			t.Fatalf("Discover(concurrency %d) error = %v", limit, err)
		}
		return result, source
	}
	sequential, sequentialSource := discover(1)
	parallel, parallelSource := discover(4)
	if !reflect.DeepEqual(sequential, parallel) {
		t.Fatal("parallel engine-2 discovery differs from sequential discovery")
	}
	if !slices.ContainsFunc(parallel.Logs, func(log types.Log) bool { return sameLog(log, logs[3]) }) {
		t.Fatal("parallel discovery missed the swap on the newly graduated pool")
	}
	if sequentialSource.calls != parallelSource.calls || parallelSource.peak < 2 {
		t.Fatalf("calls %d/%d, parallel peak %d", sequentialSource.calls, parallelSource.calls, parallelSource.peak)
	}
}

// failingSource answers the factory query, fails every query for one address, and holds the
// others until they are canceled. The failure is returned only once another call is in flight,
// so the test always has a call to cancel instead of racing the scheduler.
type failingSource struct {
	factory  common.Address
	fail     common.Address
	err      error
	inFlight chan struct{}
	signal   sync.Once
	mu       sync.Mutex
	canceled int
}

func (s *failingSource) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	if slices.Equal(query.Addresses, []common.Address{s.factory}) {
		return nil, nil
	}
	if slices.Contains(query.Addresses, s.fail) {
		select {
		case <-s.inFlight:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return nil, s.err
	}
	s.signal.Do(func() { close(s.inFlight) })
	<-ctx.Done()
	s.mu.Lock()
	s.canceled++
	s.mu.Unlock()
	return nil, ctx.Err()
}

func TestParallelDiscoveryReturnsFirstFailureAndCancelsTheRest(t *testing.T) {
	t.Parallel()
	_, _, emitters := loadFixtureLogs(t)
	decoder, _ := NewDecoder(1)
	tokens := slices.Collect(func(yield func(common.Address) bool) {
		for address := range emitters.Tokens {
			if !yield(address) {
				return
			}
		}
	})
	if len(tokens) == 0 {
		t.Fatal("fixture precondition: no tokens")
	}
	failure := errors.New("provider down")
	source := &failingSource{
		factory: emitters.Factory, fail: tokens[0], err: failure, inFlight: make(chan struct{}),
	}
	discoverer, _ := NewDiscoverer(source, decoder, emitters.Factory, 1)
	discoverer, err := discoverer.WithLogConcurrency(4)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = discoverer.Discover(ctx, 0, 100, emitters)
	if !errors.Is(err, failure) {
		t.Fatalf("Discover() error = %v, want the provider failure", err)
	}
	if ctx.Err() != nil {
		t.Fatal("discovery waited for the outer deadline instead of canceling its other calls")
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.canceled == 0 {
		t.Fatal("no in-flight call was canceled after the failure")
	}
}

func maxBlock(logs []types.Log) uint64 {
	var highest uint64
	for _, log := range logs {
		highest = max(highest, log.BlockNumber)
	}
	return highest
}
