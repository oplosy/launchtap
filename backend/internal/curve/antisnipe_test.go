package curve

import (
	"errors"
	"math/big"
	"testing"
)

func defaultAntiSnipe(t *testing.T) AntiSnipe {
	t.Helper()
	profile, err := NewAntiSnipe(5000, 60)
	if err != nil {
		t.Fatalf("NewAntiSnipe() error = %v", err)
	}
	return profile
}

func TestTaxBPSDecaysLinearlyAndRoundsDown(t *testing.T) {
	profile := defaultAntiSnipe(t)
	const launch = uint64(1_000)
	cases := []struct {
		name string
		now  uint64
		want uint16
	}{
		{"before launch is treated as launch", launch - 5, 5000},
		{"launch second", launch, 5000},
		{"mid window", launch + 30, 2500},
		{"last taxed second rounds down", launch + 59, 83},
		{"window end", launch + 60, 0},
		{"after window", launch + 600, 0},
	}
	for _, tc := range cases {
		if got := TaxBPS(profile, launch, tc.now); got != tc.want {
			t.Errorf("%s: TaxBPS() = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestTaxBPSIsZeroWhenDisabled(t *testing.T) {
	for _, profile := range []struct{ start, duration int }{{5000, 0}, {0, 60}} {
		disabled, err := NewAntiSnipe(uint16(profile.start), uint32(profile.duration))
		if err != nil {
			t.Fatalf("NewAntiSnipe(%d, %d) error = %v", profile.start, profile.duration, err)
		}
		if got := TaxBPS(disabled, 100, 100); got != 0 {
			t.Errorf("TaxBPS(%d, %d) = %d, want 0", profile.start, profile.duration, got)
		}
	}
}

func TestNewAntiSnipeEnforcesFactoryCaps(t *testing.T) {
	if _, err := NewAntiSnipe(9001, 60); !errors.As(err, new(ErrInvalidAntiSnipeProfile)) {
		t.Errorf("start 9001: error = %v, want ErrInvalidAntiSnipeProfile", err)
	}
	if _, err := NewAntiSnipe(5000, 301); !errors.As(err, new(ErrInvalidAntiSnipeProfile)) {
		t.Errorf("duration 301: error = %v, want ErrInvalidAntiSnipeProfile", err)
	}
	if _, err := NewAntiSnipe(9000, 300); err != nil {
		t.Errorf("caps themselves: error = %v", err)
	}
}

func TestTaxEndsAt(t *testing.T) {
	if got := TaxEndsAt(defaultAntiSnipe(t), 1_000); got != 1_060 {
		t.Errorf("TaxEndsAt() = %d, want 1060", got)
	}
}

func TestBuyV2TaxesOnlyConsumedETHOnFinalBuy(t *testing.T) {
	parameters := vectorParameters(t)
	state := initialState(t, parameters)
	profile := defaultAntiSnipe(t)
	supplied := new(big.Int).Mul(big.NewInt(20), big.NewInt(1e18))

	quote, err := BuyV2(state, parameters, profile, 0, 30, supplied)
	if err != nil {
		t.Fatalf("BuyV2() error = %v", err)
	}
	if !quote.Graduates() || quote.TaxBPS() != 2500 {
		t.Fatalf("Graduates() = %t, TaxBPS() = %d, want true, 2500", quote.Graduates(), quote.TaxBPS())
	}
	curveGross := new(big.Int).Sub(quote.ETHGrossUsed(), quote.SnipeTax())
	// ethGrossUsed is the smallest gross whose post-tax net covers the curve's gross.
	if _, net, _ := splitSnipeTax(quote.ETHGrossUsed(), 2500); net.Cmp(curveGross) < 0 {
		t.Fatalf("net of ethGrossUsed %s is below curve gross %s", net, curveGross)
	}
	smaller := new(big.Int).Sub(quote.ETHGrossUsed(), big.NewInt(1))
	if _, net, _ := splitSnipeTax(smaller, 2500); net.Cmp(curveGross) >= 0 {
		t.Fatalf("ethGrossUsed %s is not minimal", quote.ETHGrossUsed())
	}
	refund := new(big.Int).Sub(supplied, quote.ETHGrossUsed())
	if quote.Refund().Cmp(refund) != 0 {
		t.Fatalf("Refund() = %s, want untaxed %s", quote.Refund(), refund)
	}
}

func TestBuyV2RejectsZeroInputAndWrongPhase(t *testing.T) {
	parameters := vectorParameters(t)
	state := initialState(t, parameters)
	if _, err := BuyV2(state, parameters, defaultAntiSnipe(t), 0, 0, new(big.Int)); !errors.Is(err, ErrZeroInput) {
		t.Errorf("zero input: error = %v, want ErrZeroInput", err)
	}
	graduated, err := NewState(PhaseGraduated, state.VirtualETH(), state.VirtualToken(), new(big.Int), new(big.Int))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuyV2(graduated, parameters, defaultAntiSnipe(t), 0, 0, big.NewInt(1)); !errors.As(err, new(ErrWrongPhase)) {
		t.Errorf("graduated: error = %v, want ErrWrongPhase", err)
	}
}
