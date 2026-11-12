package funding

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type fakeStore struct {
	finalized uint64
	known     bool
	waiting   []common.Address
	limit     int
	skipped   []common.Address
	saved     map[common.Address]Result
}

func (s *fakeStore) FinalizedHead(context.Context) (uint64, bool, error) {
	return s.finalized, s.known, nil
}

func (s *fakeStore) AwaitingWallets(_ context.Context, finalized uint64, limit int, skip []common.Address) ([]common.Address, error) {
	s.limit, s.skipped = limit, skip
	if finalized != s.finalized {
		return nil, errors.New("wrong head")
	}
	var out []common.Address
	for _, w := range s.waiting {
		if !slices.Contains(skip, w) && len(out) < limit {
			out = append(out, w)
		}
	}
	return out, nil
}

func (s *fakeStore) Save(_ context.Context, wallet common.Address, r Result) error {
	if s.saved == nil {
		s.saved = map[common.Address]Result{}
	}
	s.saved[wallet] = r
	return nil
}

// failingChain fails every call for one wallet and answers "never funded" for the rest.
type failingChain struct{ bad common.Address }

func (c failingChain) NonceAt(_ context.Context, a common.Address, _ uint64) (uint64, error) {
	if a == c.bad {
		return 0, errors.New("rpc down")
	}
	return 0, nil
}
func (failingChain) BalanceAt(context.Context, common.Address, uint64) (*big.Int, error) {
	return new(big.Int), nil
}
func (failingChain) HasCode(context.Context, common.Address, uint64) (bool, error) { return false, nil }
func (failingChain) TopLevelTransfers(context.Context, uint64) ([]Transfer, error) { return nil, nil }
func (failingChain) TracedTransfers(context.Context, uint64) ([]Transfer, error)   { return nil, nil }

func TestWorkerPassResolvesUpToTheFinalizedHeadAndSkipsFailures(t *testing.T) {
	good, bad := common.HexToAddress("0x01"), common.HexToAddress("0x02")
	store := &fakeStore{finalized: 500, known: true, waiting: []common.Address{bad, good}}
	w := Worker{Store: store, Chain: failingChain{bad: bad}, BatchSize: 20, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	resolved, err := w.Pass(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resolved != 1 || store.limit != 20 {
		t.Fatalf("resolved %d with limit %d", resolved, store.limit)
	}
	if got := store.saved[good]; got != (Result{Status: StatusUnfunded, CheckedBlock: 500}) {
		t.Fatalf("saved %+v", got)
	}
	if _, ok := store.saved[bad]; ok {
		t.Fatal("a failed wallet must stay waiting")
	}
}

func TestWorkerPassWaitsForFinality(t *testing.T) {
	store := &fakeStore{waiting: []common.Address{common.HexToAddress("0x01")}}
	w := Worker{Store: store, Chain: failingChain{}, BatchSize: 20, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if resolved, err := w.Pass(context.Background()); err != nil || resolved != 0 || store.saved != nil {
		t.Fatalf("resolved %d, err %v, saved %v", resolved, err, store.saved)
	}
}

type urlFailingChain struct{ failingChain }

func (urlFailingChain) NonceAt(context.Context, common.Address, uint64) (uint64, error) {
	return 0, errors.New(`Post "https://archive.example/v1/secret-key": dial tcp: refused`)
}

func TestWorkerLogsNeverCarryTheRPCURL(t *testing.T) {
	var logs strings.Builder
	store := &fakeStore{finalized: 1, known: true, waiting: []common.Address{common.HexToAddress("0x01")}}
	w := Worker{Store: store, Chain: urlFailingChain{}, BatchSize: 1, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	if _, err := w.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "secret-key") || !strings.Contains(logs.String(), "funding resolution failed") {
		t.Fatalf("logs = %s", logs.String())
	}
}

// flakyChain fails for bad until healed.
type flakyChain struct {
	failingChain
	healed *bool
}

func (c flakyChain) NonceAt(ctx context.Context, a common.Address, b uint64) (uint64, error) {
	if *c.healed {
		return 0, nil
	}
	return c.failingChain.NonceAt(ctx, a, b)
}

func TestWorkerBacksOffAFailingWalletSoItFreesItsSlot(t *testing.T) {
	good, bad := common.HexToAddress("0x01"), common.HexToAddress("0x02")
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	healed := false
	store := &fakeStore{finalized: 500, known: true, waiting: []common.Address{bad}}
	w := &Worker{
		Store: store, Chain: flakyChain{failingChain{bad: bad}, &healed}, BatchSize: 1,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return now },
	}
	pass := func() int {
		t.Helper()
		n, err := w.Pass(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if pass() != 0 {
		t.Fatal("bad wallet resolved")
	}
	// The failing wallet is held back, so the next waiting wallet gets the one slot.
	store.waiting = []common.Address{bad, good}
	if pass() != 1 || !reflect.DeepEqual(store.skipped, []common.Address{bad}) {
		t.Fatalf("skip %v saved %v", store.skipped, store.saved)
	}
	// It stays held back until its backoff (1 minute after the first failure) ends.
	now = now.Add(59 * time.Second)
	pass()
	if len(store.skipped) != 1 {
		t.Fatalf("released early: skip %v", store.skipped)
	}
	// Second failure doubles the wait.
	now = now.Add(2 * time.Second)
	pass()
	now = now.Add(119 * time.Second)
	pass()
	if len(store.skipped) != 1 {
		t.Fatalf("second backoff too short: skip %v", store.skipped)
	}
	now = now.Add(2 * time.Second)
	healed = true
	if pass() != 1 || len(store.skipped) != 0 {
		t.Fatalf("after healing: skip %v saved %v", store.skipped, store.saved)
	}
}

func TestBackoffIsCapped(t *testing.T) {
	if got := backoff(40); got != MaxBackoff {
		t.Fatalf("backoff(40) = %s", got)
	}
	if got := backoff(1); got != time.Minute {
		t.Fatalf("backoff(1) = %s", got)
	}
}
