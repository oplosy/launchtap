package swapbackfill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type safeHead struct {
	mu    sync.Mutex
	block int64
	ok    bool
	err   error
	calls int
}

func (h *safeHead) read(context.Context) (int64, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	return h.block, h.ok, h.err
}

func TestSchedulerDisabledReturnsWithoutWork(t *testing.T) {
	t.Parallel()
	store, senders, d := newDeps([]SwapTx{{hash(1), 1}}, map[common.Hash]common.Address{hash(1): {1}})
	head := &safeHead{block: 10, ok: true}
	done := make(chan error, 1)
	go func() {
		done <- (&Scheduler{Deps: d, Interval: 0, Batch: 10, MaxTransactions: 10, SafeBlock: head.read}).Run(t.Context())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("disabled Run() = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("disabled scheduler did not return")
	}
	if store.listings != 0 || senders.calls != 0 || head.calls != 0 {
		t.Fatalf("disabled scheduler did work: listings=%d calls=%d heads=%d", store.listings, senders.calls, head.calls)
	}
}

func TestSchedulerPassBoundsBySafeHeadAndReportsFilled(t *testing.T) {
	t.Parallel()
	rows := []SwapTx{{hash(1), 5}, {hash(2), 7}, {hash(3), 8}}
	known := map[common.Hash]common.Address{hash(1): {1}, hash(2): {2}, hash(3): {3}}
	store, _, d := newDeps(rows, known)
	var filled []int
	s := &Scheduler{Deps: d, Interval: time.Minute, Batch: 10, MaxTransactions: 100, SafeBlock: (&safeHead{block: 7, ok: true}).read,
		OnFilled: func(n int) { filled = append(filled, n) }}
	s.Pass(t.Context())
	if len(store.filled) != 2 || len(filled) != 1 || filled[0] != 2 {
		t.Fatalf("filled=%v reported=%v, want the two swaps at or below safe block 7", store.filled, filled)
	}
	for _, got := range store.through {
		if got != 7 {
			t.Fatalf("throughBlock = %v, want the safe head 7", store.through)
		}
	}
}

func TestSchedulerPassSkipsWithoutSafeHead(t *testing.T) {
	t.Parallel()
	store, senders, d := newDeps([]SwapTx{{hash(1), 1}}, map[common.Hash]common.Address{hash(1): {1}})
	s := &Scheduler{Deps: d, Interval: time.Minute, Batch: 10, MaxTransactions: 10, SafeBlock: (&safeHead{}).read}
	s.Pass(t.Context())
	if store.listings != 0 || senders.calls != 0 {
		t.Fatalf("pass without a safe head did work: listings=%d calls=%d", store.listings, senders.calls)
	}
}

func TestSchedulerPassCountsAndRedactsErrors(t *testing.T) {
	t.Parallel()
	const rpcURL = "https://rpc.example/v2/SECRETKEY"
	var logs bytes.Buffer
	_, senders, d := newDeps([]SwapTx{{hash(1), 1}}, nil)
	d.Log = slog.New(slog.NewTextHandler(&logs, nil))
	senders.err = fmt.Errorf("eth_getTransactionByHash: %w", &url.Error{Op: "Post", URL: rpcURL, Err: errors.New("connection reset")})
	errorsSeen := 0
	s := &Scheduler{Deps: d, Interval: time.Minute, Batch: 10, MaxTransactions: 10, SafeBlock: (&safeHead{block: 5, ok: true}).read,
		OnError: func() { errorsSeen++ }, Secrets: []string{rpcURL}}
	s.Pass(t.Context())
	headErr := &Scheduler{Deps: d, Interval: time.Minute, Batch: 10, MaxTransactions: 10, SafeBlock: (&safeHead{err: errors.New("db down")}).read,
		OnError: func() { errorsSeen++ }}
	headErr.Pass(t.Context())
	if errorsSeen != 2 {
		t.Fatalf("errors counted = %d, want 2", errorsSeen)
	}
	out := logs.String()
	if strings.Contains(out, "SECRETKEY") || strings.Contains(out, "rpc.example") || !strings.Contains(out, "connection reset") || !strings.Contains(out, "db down") {
		t.Fatalf("log = %q, want redacted errors", out)
	}
}

func TestSchedulerUnresolvedIsNotAnError(t *testing.T) {
	t.Parallel()
	_, _, d := newDeps([]SwapTx{{hash(1), 1}}, nil)
	errorsSeen := 0
	s := &Scheduler{Deps: d, Interval: time.Minute, Batch: 10, MaxTransactions: 10, SafeBlock: (&safeHead{block: 5, ok: true}).read,
		OnError: func() { errorsSeen++ }}
	s.Pass(t.Context())
	if errorsSeen != 0 {
		t.Fatalf("an unresolved sender counted as a pass error")
	}
}

// countingSenders signals every lookup so the test can await passes.
type countingSenders struct{ calls chan struct{} }

func (c countingSenders) TransactionSenders(_ context.Context, _ []common.Hash) (map[common.Hash]common.Address, error) {
	c.calls <- struct{}{}
	return map[common.Hash]common.Address{}, nil
}

func TestSchedulerRunsPassesAtIntervalUntilCancelled(t *testing.T) {
	t.Parallel()
	_, _, d := newDeps([]SwapTx{{hash(1), 1}}, nil)
	calls := make(chan struct{}, 16)
	d.Senders = countingSenders{calls: calls}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- (&Scheduler{Deps: d, Interval: 10 * time.Millisecond, Batch: 10, MaxTransactions: 10, SafeBlock: (&safeHead{block: 5, ok: true}).read}).Run(ctx)
	}()
	for range 3 {
		select {
		case <-calls:
		case <-time.After(5 * time.Second):
			t.Fatal("scheduler did not run a pass")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler ignored cancellation")
	}
}

func TestSchedulerFirstPassWaitsOneInterval(t *testing.T) {
	t.Parallel()
	_, _, d := newDeps([]SwapTx{{hash(1), 1}}, nil)
	calls := make(chan struct{}, 1)
	d.Senders = countingSenders{calls: calls}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_ = (&Scheduler{Deps: d, Interval: time.Hour, Batch: 10, MaxTransactions: 10, SafeBlock: (&safeHead{block: 5, ok: true}).read}).Run(ctx)
	if len(calls) != 0 {
		t.Fatal("scheduler ran a pass before the first interval elapsed")
	}
}

func TestSchedulerCancelledDuringPassIsNotAnError(t *testing.T) {
	t.Parallel()
	_, _, d := newDeps([]SwapTx{{hash(1), 1}, {hash(2), 2}}, map[common.Hash]common.Address{hash(1): {1}, hash(2): {2}})
	ctx, cancel := context.WithCancel(t.Context())
	d.Sleep = func(ctx context.Context) error { cancel(); <-ctx.Done(); return ctx.Err() }
	errorsSeen := 0
	s := &Scheduler{Deps: d, Interval: time.Minute, Batch: 1, MaxTransactions: 10, SafeBlock: (&safeHead{block: 5, ok: true}).read,
		OnError: func() { errorsSeen++ }}
	s.Pass(ctx)
	if errorsSeen != 0 {
		t.Fatal("a shutdown cancellation counted as a pass error")
	}
}

// More unresolvable transactions than one pass may list must not starve a resolvable one
// behind them: the next pass resumes after the cursor.
func TestSchedulerResumesPastUnresolvableBacklog(t *testing.T) {
	t.Parallel()
	var rows []SwapTx
	for i := byte(1); i <= 5; i++ {
		rows = append(rows, SwapTx{hash(i), int64(i)})
	}
	rows = append(rows, SwapTx{hash(9), 9})
	store, _, d := newDeps(rows, map[common.Hash]common.Address{hash(9): {9}})
	s := &Scheduler{Deps: d, Interval: time.Minute, Batch: 2, MaxTransactions: 4, SafeBlock: (&safeHead{block: 100, ok: true}).read}
	s.Pass(t.Context())
	if len(store.filled) != 0 {
		t.Fatalf("first pass filled %v, want only the 4 unresolvable rows listed", store.filled)
	}
	s.Pass(t.Context())
	if _, filled := store.filled[hash(9)]; !filled {
		t.Fatalf("resolvable row not filled within 2 passes: filled=%v", store.filled)
	}
}

// A pass that reaches the end wraps the next pass to the oldest row again.
func TestSchedulerWrapsToStartAfterReachingEnd(t *testing.T) {
	t.Parallel()
	rows := []SwapTx{{hash(1), 1}, {hash(2), 2}, {hash(3), 3}}
	senders := map[common.Hash]common.Address{}
	store, fake, d := newDeps(rows, senders)
	s := &Scheduler{Deps: d, Interval: time.Minute, Batch: 10, MaxTransactions: 2, SafeBlock: (&safeHead{block: 100, ok: true}).read}
	s.Pass(t.Context()) // rows 1-2, capped
	s.Pass(t.Context()) // row 3, end reached
	fake.known = map[common.Hash]common.Address{hash(1): {1}}
	s.Pass(t.Context()) // wrapped: row 1 again
	if _, filled := store.filled[hash(1)]; !filled {
		t.Fatalf("third pass did not wrap to the oldest row: filled=%v", store.filled)
	}
}

func TestSchedulerPassReportsSummaryAndBacklog(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	rows := []SwapTx{{hash(1), 1}, {hash(2), 2}, {hash(3), 3}}
	_, _, d := newDeps(rows, map[common.Hash]common.Address{hash(1): {1}})
	d.Log = slog.New(slog.NewTextHandler(&logs, nil))
	var backlog []int64
	var throughs []int64
	s := &Scheduler{Deps: d, Interval: time.Minute, Batch: 10, MaxTransactions: 2, SafeBlock: (&safeHead{block: 7, ok: true}).read,
		Backlog: func(_ context.Context, through int64) (int64, error) {
			throughs = append(throughs, through)
			return 2, nil
		},
		OnBacklog: func(n int64) { backlog = append(backlog, n) }}
	s.Pass(t.Context())
	out := logs.String()
	if strings.Count(out, "sender backfill pass finished") != 1 || !strings.Contains(out, "capped=true") || !strings.Contains(out, "transactions_unresolved=1") || strings.Contains(out, "level=WARN") {
		t.Fatalf("log = %q, want one summary line with capped=true and transactions_unresolved=1 and no per-tx warning", out)
	}
	if len(backlog) != 1 || backlog[0] != 2 || len(throughs) != 1 || throughs[0] != 7 {
		t.Fatalf("backlog = %v through = %v, want one report of 2 at safe block 7", backlog, throughs)
	}
}
