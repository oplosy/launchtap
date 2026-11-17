package reconcile

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

var (
	tokenA  = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	curveA  = common.HexToAddress("0x00000000000000000000000000000000000000c1")
	holder1 = common.HexToAddress("0x00000000000000000000000000000000000000b1")
	holder2 = common.HexToAddress("0x00000000000000000000000000000000000000b2")
	pinned  = common.HexToHash("0x01")
)

type fakeStore struct {
	snapshot Snapshot
	ok       bool
	err      error
}

func (s fakeStore) Snapshot(context.Context, int, int) (Snapshot, bool, error) {
	return s.snapshot, s.ok, s.err
}

type fakeChain struct {
	phase      uint8
	eth, token *big.Int
	supply     *big.Int
	balances   map[common.Address]*big.Int
	hash       common.Hash
	failOn     string
	blocks     []uint64
}

func (c *fakeChain) fail(method string, block uint64) error {
	c.blocks = append(c.blocks, block)
	if c.failOn == method {
		return errors.New("rpc unavailable")
	}
	return nil
}
func (c *fakeChain) Phase(_ context.Context, _ common.Address, block uint64) (uint8, error) {
	return c.phase, c.fail("phase", block)
}
func (c *fakeChain) VirtualReserves(_ context.Context, _ common.Address, block uint64) (*big.Int, *big.Int, error) {
	return c.eth, c.token, c.fail("reserves", block)
}
func (c *fakeChain) TotalSupply(_ context.Context, _ common.Address, block uint64) (*big.Int, error) {
	return c.supply, c.fail("supply", block)
}
func (c *fakeChain) BalanceOf(_ context.Context, _, holder common.Address, block uint64) (*big.Int, error) {
	return c.balances[holder], c.fail("balance", block)
}
func (c *fakeChain) BlockHash(_ context.Context, block uint64) (common.Hash, error) {
	return c.hash, c.fail("hash", block)
}

func indexed() Snapshot {
	return Snapshot{Block: 42, BlockHash: pinned, Tokens: []Token{{
		Address: tokenA, Curve: curveA, Phase: PhaseCurve,
		CurveReserves: &Reserves{ETH: big.NewInt(1000), Token: big.NewInt(2000)},
		HolderSum:     big.NewInt(100),
		TopHolders:    []Holder{{holder1, big.NewInt(60)}, {holder2, big.NewInt(40)}},
	}}}
}

func agreeing() *fakeChain {
	return &fakeChain{
		phase: 0, eth: big.NewInt(1000), token: big.NewInt(2000), supply: big.NewInt(100),
		balances: map[common.Address]*big.Int{holder1: big.NewInt(60), holder2: big.NewInt(40)},
		hash:     pinned,
	}
}

func TestAgreementIsOK(t *testing.T) {
	chain := agreeing()
	result, err := Pass(t.Context(), fakeStore{snapshot: indexed(), ok: true}, chain, Limits{})
	if err != nil || result.Outcome != OutcomeOK || len(result.Mismatches) != 0 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if result.Checks != 6 || result.Block != 42 || result.Tokens != 1 {
		t.Fatalf("checks = %d block = %d tokens = %d, want 6, 42, 1", result.Checks, result.Block, result.Tokens)
	}
	for _, block := range chain.blocks {
		if block != 42 {
			t.Fatalf("chain read at block %d, want every read pinned at 42", block)
		}
	}
}

func TestEachMismatchIsReported(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*fakeChain)
		check  string
	}{
		{"phase", func(c *fakeChain) { c.phase = 1 }, CheckPhase},
		{"eth reserve", func(c *fakeChain) { c.eth = big.NewInt(999) }, CheckReserveETH},
		{"token reserve", func(c *fakeChain) { c.token = big.NewInt(1) }, CheckReserveToken},
		{"supply", func(c *fakeChain) { c.supply = big.NewInt(101) }, CheckSupply},
		{"holder", func(c *fakeChain) { c.balances[holder2] = big.NewInt(41) }, CheckHolder},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chain := agreeing()
			tc.mutate(chain)
			result, err := Pass(t.Context(), fakeStore{snapshot: indexed(), ok: true}, chain, Limits{})
			if err != nil || result.Outcome != OutcomeDrift || len(result.Mismatches) != 1 || result.Mismatches[0].Check != tc.check {
				t.Fatalf("result = %+v, err = %v; want one %s mismatch", result, err, tc.check)
			}
		})
	}
}

func TestGraduatedTokenSkipsCurveReserves(t *testing.T) {
	snapshot := indexed()
	snapshot.Tokens[0].Phase = PhaseGraduated
	chain := agreeing()
	chain.phase, chain.eth = 1, big.NewInt(0)
	result, err := Pass(t.Context(), fakeStore{snapshot: snapshot, ok: true}, chain, Limits{})
	if err != nil || result.Outcome != OutcomeOK || result.Checks != 4 {
		t.Fatalf("result = %+v, err = %v; want ok with 4 checks", result, err)
	}
}

func TestReorgedPinnedBlockDiscardsThePass(t *testing.T) {
	chain := agreeing()
	chain.supply = big.NewInt(7) // would be drift on the pinned chain
	chain.hash = common.HexToHash("0x02")
	result, err := Pass(t.Context(), fakeStore{snapshot: indexed(), ok: true}, chain, Limits{})
	if err != nil || result.Outcome != OutcomeReorg || len(result.Mismatches) != 0 {
		t.Fatalf("result = %+v, err = %v; want a discarded reorg pass", result, err)
	}
}

func TestReadFailuresAreErrorsNotDrift(t *testing.T) {
	for _, method := range []string{"phase", "reserves", "supply", "balance", "hash"} {
		chain := agreeing()
		chain.failOn = method
		result, err := Pass(t.Context(), fakeStore{snapshot: indexed(), ok: true}, chain, Limits{})
		if err == nil || result.Outcome != OutcomeError || len(result.Mismatches) != 0 {
			t.Fatalf("%s: result = %+v, err = %v; want an error outcome", method, result, err)
		}
	}
	result, err := Pass(t.Context(), fakeStore{err: errors.New("db down")}, agreeing(), Limits{})
	if err == nil || result.Outcome != OutcomeError {
		t.Fatalf("store failure: result = %+v, err = %v", result, err)
	}
}

func TestNothingIndexedIsIdle(t *testing.T) {
	result, err := Pass(t.Context(), fakeStore{}, agreeing(), Limits{})
	if err != nil || result.Outcome != OutcomeIdle {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestMismatchString(t *testing.T) {
	m := Mismatch{Check: CheckHolder, Token: tokenA, Subject: holder1, Indexed: "1", Chain: "2"}
	if s := m.String(); !strings.Contains(s, "holder_balance") || !strings.Contains(s, "indexed=1 chain=2") {
		t.Fatalf("String() = %q", s)
	}
}

func TestWorkerRunsPassesUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	results := make(chan Result, 4)
	worker := Worker{
		Store: fakeStore{snapshot: indexed(), ok: true}, Chain: agreeing(),
		Interval: time.Millisecond, InitialDelay: time.Millisecond,
		OnResult: func(r Result) { results <- r },
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	for range 2 {
		select {
		case r := <-results:
			if r.Outcome != OutcomeOK {
				t.Fatalf("outcome = %s", r.Outcome)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not run a pass")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want context.Canceled", err)
	}
}
