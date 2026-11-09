package swapbackfill

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

type fakeStore struct {
	rows     []SwapTx // ordered by (block, hash)
	filled   map[common.Hash]common.Address
	listings int
	through  []int64 // throughBlock of every List call
	limits   []int   // limit of every List call
}

func (s *fakeStore) List(_ context.Context, throughBlock, afterBlock int64, afterHash common.Hash, limit int) ([]SwapTx, error) {
	s.listings++
	s.through = append(s.through, throughBlock)
	s.limits = append(s.limits, limit)
	var out []SwapTx
	for _, row := range s.rows {
		if _, done := s.filled[row.Hash]; done {
			continue
		}
		if row.Block > throughBlock {
			continue
		}
		if row.Block < afterBlock || (row.Block == afterBlock && string(row.Hash[:]) <= string(afterHash[:])) {
			continue
		}
		out = append(out, row)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (s *fakeStore) Attribute(_ context.Context, senders map[common.Hash]common.Address) (int, error) {
	for hash, sender := range senders {
		s.filled[hash] = sender
	}
	return len(senders), nil
}

type fakeSenders struct {
	known map[common.Hash]common.Address
	calls int
	err   error
}

func (f *fakeSenders) TransactionSenders(_ context.Context, hashes []common.Hash) (map[common.Hash]common.Address, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := map[common.Hash]common.Address{}
	for _, hash := range hashes {
		if sender, ok := f.known[hash]; ok {
			out[hash] = sender
		}
	}
	return out, nil
}

func hash(b byte) common.Hash { return common.Hash{31: b} }

func newDeps(rows []SwapTx, known map[common.Hash]common.Address) (*fakeStore, *fakeSenders, Deps) {
	store := &fakeStore{rows: rows, filled: map[common.Hash]common.Address{}}
	senders := &fakeSenders{known: known}
	return store, senders, Deps{Store: store, Senders: senders, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Sleep: func(context.Context) error { return nil }}
}

func TestRunFillsAllBatches(t *testing.T) {
	t.Parallel()
	rows := []SwapTx{{hash(1), 5}, {hash(2), 5}, {hash(3), 6}}
	known := map[common.Hash]common.Address{hash(1): {1}, hash(2): {2}, hash(3): {3}}
	store, senders, d := newDeps(rows, known)
	summary, err := Run(t.Context(), d, Options{Batch: 2})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary.Updated != 3 || summary.Skipped != 0 || len(store.filled) != 3 || senders.calls != 2 {
		t.Fatalf("summary=%+v filled=%d calls=%d", summary, len(store.filled), senders.calls)
	}
}

func TestRunSkipsUnresolvedAndTerminates(t *testing.T) {
	t.Parallel()
	rows := []SwapTx{{hash(1), 5}, {hash(2), 5}, {hash(3), 6}}
	store, _, d := newDeps(rows, map[common.Hash]common.Address{hash(1): {1}})
	summary, err := Run(t.Context(), d, Options{Batch: 1})
	if !errors.Is(err, ErrSkipped) {
		t.Fatalf("err = %v, want ErrSkipped", err)
	}
	if summary.Updated != 1 || summary.Skipped != 2 || len(store.filled) != 1 {
		t.Fatalf("summary=%+v filled=%d", summary, len(store.filled))
	}
}

func TestRunDryRunWritesNothingAndTerminates(t *testing.T) {
	t.Parallel()
	rows := []SwapTx{{hash(1), 5}, {hash(2), 6}, {hash(3), 7}}
	known := map[common.Hash]common.Address{hash(1): {1}, hash(2): {2}, hash(3): {3}}
	store, _, d := newDeps(rows, known)
	summary, err := Run(t.Context(), d, Options{Batch: 2, DryRun: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(store.filled) != 0 || summary.WouldUpdate != 3 || summary.Updated != 0 {
		t.Fatalf("summary=%+v filled=%d", summary, len(store.filled))
	}
}

func TestRunPropagatesRPCFailure(t *testing.T) {
	t.Parallel()
	_, senders, d := newDeps([]SwapTx{{hash(1), 5}}, nil)
	senders.err = errors.New("rpc down")
	if _, err := Run(t.Context(), d, Options{Batch: 10}); err == nil || errors.Is(err, ErrSkipped) {
		t.Fatalf("err = %v, want RPC failure", err)
	}
}

func TestRunWithoutBoundListsEverything(t *testing.T) {
	t.Parallel()
	store, _, d := newDeps([]SwapTx{{hash(1), 5}}, map[common.Hash]common.Address{hash(1): {1}})
	if _, err := Run(t.Context(), d, Options{Batch: 10}); err != nil {
		t.Fatal(err)
	}
	if len(store.through) == 0 || store.through[0] != math.MaxInt64 {
		t.Fatalf("throughBlock = %v, want MaxInt64 without a bound", store.through)
	}
}

func TestRunHonorsThroughBlock(t *testing.T) {
	t.Parallel()
	rows := []SwapTx{{hash(1), 5}, {hash(2), 6}, {hash(3), 9}}
	known := map[common.Hash]common.Address{hash(1): {1}, hash(2): {2}, hash(3): {3}}
	store, _, d := newDeps(rows, known)
	through := int64(6)
	summary, err := Run(t.Context(), d, Options{Batch: 10, ThroughBlock: &through})
	if err != nil {
		t.Fatal(err)
	}
	if _, filled := store.filled[hash(3)]; filled || len(store.filled) != 2 || summary.Attributed != 2 {
		t.Fatalf("summary=%+v filled=%v, want only blocks <= 6", summary, store.filled)
	}
	for _, got := range store.through {
		if got != 6 {
			t.Fatalf("throughBlock = %v, want 6 on every listing", store.through)
		}
	}
}

func TestRunCapsTransactionsPerPass(t *testing.T) {
	t.Parallel()
	rows := []SwapTx{{hash(1), 5}, {hash(2), 6}, {hash(3), 7}, {hash(4), 8}, {hash(5), 9}}
	known := map[common.Hash]common.Address{hash(1): {1}, hash(2): {2}, hash(3): {3}, hash(4): {4}, hash(5): {5}}
	store, _, d := newDeps(rows, known)
	summary, err := Run(t.Context(), d, Options{Batch: 2, MaxTransactions: 3})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Listed != 3 || summary.Attributed != 3 || len(store.filled) != 3 {
		t.Fatalf("summary=%+v filled=%d, want 3 transactions", summary, len(store.filled))
	}
	if want := []int{2, 1}; len(store.limits) != 2 || store.limits[0] != want[0] || store.limits[1] != want[1] {
		t.Fatalf("limits = %v, want %v", store.limits, want)
	}
}
