package position

import (
	"errors"
	"math/big"
	"testing"
)

// pow10 returns n * 10^e.
func pow10(n int64, e int) *big.Int {
	v := big.NewInt(n)
	return v.Mul(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(e)), nil))
}

// leg builds legs with strictly ascending (Block, TxIndex) from a list of triples.
func legs(triples ...[3]*big.Int) []Leg {
	out := make([]Leg, len(triples))
	for i, t := range triples {
		out[i] = Leg{Block: int64(i + 1), TokenDelta: t[0], ETHPaid: t[1], ETHReceived: t[2]}
	}
	return out
}

func tr(d, p, r *big.Int) [3]*big.Int { return [3]*big.Int{d, p, r} }

func TestComputeVectors(t *testing.T) {
	z := big.NewInt(0)
	n := big.NewInt
	e18 := pow10(1, 18)
	tests := []struct {
		name                                   string
		legs                                   []Leg
		balance, remaining, realized, transfer *big.Int
	}{
		{"buy", legs(tr(n(1000), e18, z)), n(1000), e18, z, z},
		{"partial sell", legs(tr(n(1000), e18, z), tr(n(-400), z, pow10(6, 17))), n(600), pow10(6, 17), pow10(2, 17), z},
		{"full sell", legs(tr(n(1000), e18, z), tr(n(-1000), z, pow10(5, 17))), z, z, pow10(-5, 17), z},
		{"transfer in then sell", legs(tr(n(500), z, z), tr(n(-500), z, pow10(1, 17))), z, z, pow10(1, 17), n(500)},
		{"transfer out", legs(tr(n(1000), e18, z), tr(n(-250), z, z)), n(750), pow10(75, 16), z, z},
		{"rebuy after zero", legs(tr(n(10), n(100), z), tr(n(-10), z, n(50)), tr(n(5), n(20), z)), n(5), n(20), n(-50), z},
		{"round trip in tx", legs(tr(z, e18, pow10(9, 17))), z, z, pow10(-1, 17), z},
		{"bought for another", legs(tr(z, e18, z)), z, z, pow10(-1, 18), z},
		{"rounding floor", legs(tr(n(3), n(10), z), tr(n(-1), z, z), tr(n(-2), z, z)), z, z, z, z},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Compute(tc.legs)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range []struct {
				field     string
				got, want *big.Int
			}{
				{"Balance", p.Balance, tc.balance},
				{"RemainingCost", p.RemainingCost, tc.remaining},
				{"RealizedPnL", p.RealizedPnL, tc.realized},
				{"TransferredIn", p.TransferredIn, tc.transfer},
			} {
				if c.got.Cmp(c.want) != 0 {
					t.Errorf("%s = %s, want %s", c.field, c.got, c.want)
				}
			}
		})
	}
}

func TestComputeRoundingIntermediate(t *testing.T) {
	n := big.NewInt
	z := n(0)
	p, err := Compute(legs(tr(n(3), n(10), z), tr(n(-1), z, z)))
	if err != nil {
		t.Fatal(err)
	}
	if p.RemainingCost.Cmp(n(7)) != 0 {
		t.Fatalf("RemainingCost = %s, want 7", p.RemainingCost)
	}
}

func TestComputeAggregates(t *testing.T) {
	n := big.NewInt
	z := n(0)
	in := []Leg{
		{Block: 5, TxIndex: 1, TokenDelta: n(1000), ETHPaid: n(100), ETHReceived: z},
		{Block: 5, TxIndex: 2, TokenDelta: n(-100), ETHPaid: z, ETHReceived: n(30)},
		{Block: 9, TxIndex: 0, TokenDelta: n(-100), ETHPaid: z, ETHReceived: z},
	}
	p, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	if p.TradeCount != 2 || p.FirstBlock != 5 || p.LastBlock != 9 {
		t.Fatalf("got count=%d first=%d last=%d", p.TradeCount, p.FirstBlock, p.LastBlock)
	}
	if p.ETHPaid.Cmp(n(100)) != 0 || p.ETHReceived.Cmp(n(30)) != 0 {
		t.Fatalf("paid=%s received=%s", p.ETHPaid, p.ETHReceived)
	}
	// Inputs must not be mutated and result must not alias them.
	if in[0].TokenDelta.Cmp(n(1000)) != 0 || in[1].ETHReceived.Cmp(n(30)) != 0 {
		t.Fatal("input mutated")
	}
	p.Balance.SetInt64(1)
	if in[0].TokenDelta.Cmp(n(1000)) != 0 {
		t.Fatal("result aliases input")
	}
}

func TestComputeEmpty(t *testing.T) {
	p, err := Compute(nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Balance == nil || p.RemainingCost == nil || p.RealizedPnL == nil || p.TransferredIn == nil ||
		p.ETHPaid == nil || p.ETHReceived == nil {
		t.Fatal("nil field in empty position")
	}
	if p.Balance.Sign() != 0 || p.TradeCount != 0 || p.FirstBlock != 0 || p.LastBlock != 0 {
		t.Fatalf("unexpected %+v", p)
	}
}

func TestComputeErrors(t *testing.T) {
	n := big.NewInt
	z := n(0)
	neg := n(-1)
	tests := []struct {
		name string
		legs []Leg
		want error
	}{
		{"unordered block", []Leg{
			{Block: 2, TokenDelta: n(1), ETHPaid: z, ETHReceived: z},
			{Block: 1, TokenDelta: n(1), ETHPaid: z, ETHReceived: z},
		}, ErrUnordered},
		{"duplicate position", []Leg{
			{Block: 1, TxIndex: 1, TokenDelta: n(1), ETHPaid: z, ETHReceived: z},
			{Block: 1, TxIndex: 1, TokenDelta: n(1), ETHPaid: z, ETHReceived: z},
		}, ErrUnordered},
		{"negative balance", legs(tr(n(10), n(1), z), tr(n(-11), z, z)), ErrNegativeBalance},
		{"sell from empty", legs(tr(n(-1), z, z)), ErrNegativeBalance},
		{"nil delta", []Leg{{Block: 1, TokenDelta: nil, ETHPaid: z, ETHReceived: z}}, ErrInvalidLeg},
		{"nil paid", []Leg{{Block: 1, TokenDelta: z, ETHPaid: nil, ETHReceived: z}}, ErrInvalidLeg},
		{"nil received", []Leg{{Block: 1, TokenDelta: z, ETHPaid: z, ETHReceived: nil}}, ErrInvalidLeg},
		{"negative paid", []Leg{{Block: 1, TokenDelta: z, ETHPaid: neg, ETHReceived: z}}, ErrInvalidLeg},
		{"negative received", []Leg{{Block: 1, TokenDelta: z, ETHPaid: z, ETHReceived: neg}}, ErrInvalidLeg},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Compute(tc.legs); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestComputeLegRealized pins spec C3 AC1: LegRealized is aligned with the input legs and its sum
// equals RealizedPnL.
func TestComputeLegRealized(t *testing.T) {
	z := big.NewInt(0)
	n := big.NewInt
	e18 := pow10(1, 18)
	in := legs(
		tr(n(1000), e18, z),          // buy: no realized change
		tr(n(-400), z, pow10(6, 17)), // sell 40% for 0.6: +0.2
		tr(z, e18, pow10(9, 17)),     // round trip in one tx: -0.1
		tr(n(-600), z, pow10(5, 17)), // sell rest for 0.5 with 0.6 cost: -0.1
		tr(n(10), z, z),              // transfer in: no change
	)
	p, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []*big.Int{z, pow10(2, 17), pow10(-1, 17), pow10(-1, 17), z}
	if len(p.LegRealized) != len(in) {
		t.Fatalf("LegRealized has %d entries, want %d", len(p.LegRealized), len(in))
	}
	sum := new(big.Int)
	for i, got := range p.LegRealized {
		if got.Cmp(want[i]) != 0 {
			t.Errorf("leg %d realized = %s, want %s", i, got, want[i])
		}
		sum.Add(sum, got)
	}
	if sum.Cmp(p.RealizedPnL) != 0 {
		t.Errorf("sum of legs = %s, RealizedPnL = %s", sum, p.RealizedPnL)
	}
	// The returned values are not aliased to the running total (which is 0.2 - 0.1 - 0.1 = 0).
	p.LegRealized[1].SetInt64(42)
	if p.RealizedPnL.Sign() != 0 {
		t.Fatalf("RealizedPnL = %s after mutating a leg value, want 0", p.RealizedPnL)
	}
}
