package swapbackfill

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

type fakeStore struct {
	rows     []SwapTx // ordered by (block, hash)
	filled   map[common.Hash]common.Address
	listings int
}

func (s *fakeStore) List(_ context.Context, afterBlock int64, afterHash common.Hash, limit int) ([]SwapTx, error) {
	s.listings++
	var out []SwapTx
	for _, row := range s.rows {
		if _, done := s.filled[row.Hash]; done {
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
