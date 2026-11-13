package funding

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// fakeChain answers from per-block histories: values change at the listed blocks.
type fakeChain struct {
	code       bool
	nonceFrom  uint64 // nonce becomes 1 at this block (0 = never)
	balanceAt  map[uint64]int64
	topLevel   map[uint64][]Transfer
	traced     map[uint64][]Transfer
	calls      int
	traceError error
}

func (f *fakeChain) NonceAt(_ context.Context, _ common.Address, block uint64) (uint64, error) {
	f.calls++
	if f.nonceFrom != 0 && block >= f.nonceFrom {
		return 1, nil
	}
	return 0, nil
}

func (f *fakeChain) BalanceAt(_ context.Context, _ common.Address, block uint64) (*big.Int, error) {
	f.calls++
	var best int64
	var at uint64
	found := false
	for b, v := range f.balanceAt {
		if b <= block && (!found || b > at) {
			best, at, found = v, b, true
		}
	}
	return big.NewInt(best), nil
}

func (f *fakeChain) HasCode(context.Context, common.Address, uint64) (bool, error) {
	return f.code, nil
}

func (f *fakeChain) TopLevelTransfers(_ context.Context, block uint64) ([]Transfer, error) {
	return f.topLevel[block], nil
}

func (f *fakeChain) TracedTransfers(_ context.Context, block uint64) ([]Transfer, error) {
	if f.traceError != nil {
		return nil, f.traceError
	}
	return f.traced[block], nil
}

var (
	wallet = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	funder = common.HexToAddress("0x00000000000000000000000000000000000000f1")
	other  = common.HexToAddress("0x00000000000000000000000000000000000000f2")
)

func TestResolveFindsTheFirstFundingBeforeTheFirstSend(t *testing.T) {
	// Funded at 700 by funder (an internal call), topped up at 900, first send at 1000; later the
	// balance drops to zero and comes back, which must not confuse the search.
	chain := &fakeChain{
		nonceFrom: 1000,
		balanceAt: map[uint64]int64{700: 5, 900: 9, 1000: 0, 5000: 3},
		traced: map[uint64][]Transfer{700: {
			{TxHash: common.HexToHash("0x01"), From: other, To: funder, Value: big.NewInt(1)},
			{TxHash: common.HexToHash("0x02"), From: funder, To: wallet, Value: big.NewInt(5)},
		}},
	}
	got, err := Resolve(context.Background(), chain, wallet, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	want := Result{Status: StatusFunded, Funder: funder, Block: 700, TxHash: common.HexToHash("0x02"), CheckedBlock: 1_000_000}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if chain.calls > 80 {
		t.Fatalf("%d RPC calls; the search should be logarithmic", chain.calls)
	}
}

func TestResolveFallsBackToTopLevelTransfers(t *testing.T) {
	chain := &fakeChain{
		balanceAt: map[uint64]int64{42: 1},
		topLevel:  map[uint64][]Transfer{42: {{TxHash: common.HexToHash("0x03"), From: funder, To: wallet, Value: big.NewInt(1)}}},
	}
	got, err := Resolve(context.Background(), chain, wallet, 100)
	if err != nil || got.Status != StatusFunded || got.Funder != funder || got.Block != 42 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestResolveOtherStatuses(t *testing.T) {
	cases := []struct {
		name  string
		chain *fakeChain
		want  Result
	}{
		{"never funded", &fakeChain{}, Result{Status: StatusUnfunded, CheckedBlock: 100}},
		{"contract", &fakeChain{code: true, balanceAt: map[uint64]int64{1: 1}}, Result{Status: StatusContract, CheckedBlock: 100}},
		{"sent with no balance", &fakeChain{nonceFrom: 10}, Result{Status: StatusUnknown, CheckedBlock: 100}},
		{"genesis balance", &fakeChain{nonceFrom: 1, balanceAt: map[uint64]int64{0: 1}}, Result{Status: StatusUnknown, Block: 0, CheckedBlock: 100}},
		{"balance without a visible transfer", &fakeChain{balanceAt: map[uint64]int64{7: 1}}, Result{Status: StatusUnknown, Block: 7, CheckedBlock: 100}},
	}
	for _, c := range cases {
		got, err := Resolve(context.Background(), c.chain, wallet, 100)
		if err != nil || got != c.want {
			t.Fatalf("%s: got %+v, %v; want %+v", c.name, got, err, c.want)
		}
	}
}

func TestResolvePropagatesRPCErrors(t *testing.T) {
	boom := errors.New("trace unavailable")
	chain := &fakeChain{balanceAt: map[uint64]int64{7: 1}, traceError: boom}
	if _, err := Resolve(context.Background(), chain, wallet, 100); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
