package main

import (
	"crypto/sha256"
	"fmt"
	stdhash "hash"
	"math/big"
	"testing"
	"time"
)

// checkingSink replays the generated ledger and fails on any state a real chain cannot produce.
type checkingSink struct {
	t          *testing.T
	digest     stdhash.Hash
	lastBlock  block
	balances   map[[2]address]*big.Int
	graduated  map[address]int64
	launched   map[address]bool
	curveOf    map[address]address
	pairOwner  map[address]address
	curveTrade int
}

func newCheckingSink(t *testing.T) *checkingSink {
	return &checkingSink{t: t, digest: sha256.New(), balances: map[[2]address]*big.Int{}, graduated: map[address]int64{}, launched: map[address]bool{}, curveOf: map[address]address{}, pairOwner: map[address]address{}}
}

func (s *checkingSink) record(v any) { _, _ = fmt.Fprintf(s.digest, "%+v\n", v) }

func (s *checkingSink) block(b block) error {
	s.record(b)
	if b.Number != s.lastBlock.Number+1 {
		s.t.Fatalf("block %d follows %d", b.Number, s.lastBlock.Number)
	}
	if b.Time.Before(s.lastBlock.Time) {
		s.t.Fatalf("block %d time goes backwards", b.Number)
	}
	if b.ParentHash != derivedHash("block", b.Number-1) {
		s.t.Fatalf("block %d parent hash mismatch", b.Number)
	}
	s.lastBlock = b
	return nil
}

func (s *checkingSink) launch(r launchRow) error {
	s.record(r)
	if s.launched[r.Token] {
		s.t.Fatalf("token %x launched twice", r.Token)
	}
	s.launched[r.Token] = true
	s.curveOf[r.Token] = r.Curve
	s.pairOwner[r.Pair] = r.Token
	return nil
}

func (s *checkingSink) trade(r tradeRow) error {
	s.record(r)
	s.curveTrade++
	if !s.launched[r.Token] {
		s.t.Fatalf("trade before launch of %x", r.Token)
	}
	if at, ok := s.graduated[r.Token]; ok && r.BlockNumber > at {
		s.t.Fatalf("curve trade of %x in block %d after graduation in %d", r.Token, r.BlockNumber, at)
	}
	if new(big.Int).Mul(r.NewETHReserve, r.NewTokenReserve).Cmp(curveConstant) < 0 {
		s.t.Fatalf("curve trade in block %d breaks the constant product", r.BlockNumber)
	}
	if r.TokenAmount.Sign() <= 0 || r.ETHGross.Sign() < 0 {
		s.t.Fatalf("curve trade in block %d has amounts %s / %s", r.BlockNumber, r.TokenAmount, r.ETHGross)
	}
	return nil
}

func (s *checkingSink) transfer(r transferRow) error {
	s.record(r)
	if r.Value.Sign() < 0 {
		s.t.Fatalf("negative transfer in block %d", r.BlockNumber)
	}
	if r.From != zeroAddress {
		from := s.balance(r.Token, r.From)
		from.Sub(from, r.Value)
		if from.Sign() < 0 {
			s.t.Fatalf("transfer in block %d leaves %x with a negative balance", r.BlockNumber, r.From)
		}
	}
	to := s.balance(r.Token, r.To)
	to.Add(to, r.Value)
	return nil
}

func (s *checkingSink) balance(token, holder address) *big.Int {
	key := [2]address{token, holder}
	if s.balances[key] == nil {
		s.balances[key] = new(big.Int)
	}
	return s.balances[key]
}

func (s *checkingSink) graduation(r graduationRow) error {
	s.record(r)
	if _, ok := s.graduated[r.Token]; ok {
		s.t.Fatalf("token %x graduated twice", r.Token)
	}
	s.graduated[r.Token] = r.BlockNumber
	return nil
}

func (s *checkingSink) poolSync(r poolSyncRow) error {
	s.record(r)
	if r.Reserve0.Sign() <= 0 || r.Reserve1.Sign() <= 0 {
		s.t.Fatalf("pool sync in block %d has an empty reserve", r.BlockNumber)
	}
	return nil
}

func (s *checkingSink) poolMint(r poolMintRow) error { s.record(r); return nil }

func (s *checkingSink) poolSwap(r poolSwapRow) error {
	s.record(r)
	token := s.pairOwner[r.Pair]
	if _, ok := s.graduated[token]; !ok {
		s.t.Fatalf("pool swap in block %d before the pair's graduation", r.BlockNumber)
	}
	return nil
}

func testParams() Params {
	return Params{ChainID: 31337, Tokens: 40, Wallets: 300, Positions: 6000, Seed: 7, End: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), Span: 30 * 24 * time.Hour}
}

func TestGenerateProducesAConsistentLedger(t *testing.T) {
	sink := newCheckingSink(t)
	summary, err := generate(testParams(), sink)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Launches != 40 || summary.Graduations == 0 || summary.PoolSwaps == 0 || summary.CurveTrades == 0 {
		t.Fatalf("summary %+v: want launches, graduations, curve trades and pool swaps", summary)
	}
	if summary.Graduations == summary.Launches {
		t.Fatalf("every token graduated; want a mix of curve and graduated tokens")
	}
	if summary.Blocks != summary.LastBlock.Number {
		t.Fatalf("blocks %d, last block %d", summary.Blocks, summary.LastBlock.Number)
	}
	supply := map[address]*big.Int{}
	for key, balance := range sink.balances {
		if supply[key[0]] == nil {
			supply[key[0]] = new(big.Int)
		}
		supply[key[0]].Add(supply[key[0]], balance)
	}
	for token, total := range supply {
		if total.Cmp(totalSupply) != 0 {
			t.Fatalf("token %x holder balances sum to %s, want the total supply", token, total)
		}
	}
	if summary.QuietCurveToken == (address{}) {
		t.Fatal("no traded curve-phase token reported")
	}
	if _, ok := sink.graduated[summary.HottestToken]; !ok {
		t.Fatal("the most traded token did not graduate")
	}
}

func TestGenerateIsReproducible(t *testing.T) {
	first, second := newCheckingSink(t), newCheckingSink(t)
	if _, err := generate(testParams(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := generate(testParams(), second); err != nil {
		t.Fatal(err)
	}
	if string(first.digest.Sum(nil)) != string(second.digest.Sum(nil)) {
		t.Fatal("the same seed produced different ledgers")
	}
	other := testParams()
	other.Seed = 8
	third := newCheckingSink(t)
	if _, err := generate(other, third); err != nil {
		t.Fatal(err)
	}
	if string(first.digest.Sum(nil)) == string(third.digest.Sum(nil)) {
		t.Fatal("a different seed produced the same ledger")
	}
}

func TestGenerateRejectsInvalidParams(t *testing.T) {
	for _, mutate := range []func(*Params){
		func(p *Params) { p.ChainID = 0 },
		func(p *Params) { p.Tokens = 0 },
		func(p *Params) { p.Positions = 0 },
		func(p *Params) { p.Span = 0 },
	} {
		p := testParams()
		mutate(&p)
		if _, err := generate(p, newCheckingSink(t)); err == nil {
			t.Fatalf("params %+v accepted", p)
		}
	}
}
