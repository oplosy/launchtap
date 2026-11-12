package funding

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

type fakeStore struct {
	finalized uint64
	known     bool
	waiting   []common.Address
	limit     int
	saved     map[common.Address]Result
}

func (s *fakeStore) FinalizedHead(context.Context) (uint64, bool, error) {
	return s.finalized, s.known, nil
}

func (s *fakeStore) AwaitingWallets(_ context.Context, finalized uint64, limit int) ([]common.Address, error) {
	s.limit = limit
	if finalized != s.finalized {
		return nil, errors.New("wrong head")
	}
	return s.waiting, nil
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
