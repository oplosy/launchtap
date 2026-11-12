package quote_test

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/curve"
	"github.com/Contictus/launchtap/backend/internal/quote"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/ethereum/go-ethereum/common"
)

type stateReader struct{ state token.QuoteState }

func (r stateReader) ReadQuoteState(context.Context, int64, common.Address) (token.QuoteState, error) {
	return r.state, nil
}

func amount(t *testing.T, value string) *big.Int {
	t.Helper()
	n, ok := new(big.Int).SetString(value, 10)
	if !ok {
		t.Fatalf("invalid amount %q", value)
	}
	return n
}

var launchTime = time.Date(2026, time.November, 12, 10, 0, 0, 0, time.UTC)

func quoteState(t *testing.T, p curve.VectorParameters, s curve.VectorState, engine uint16) token.QuoteState {
	t.Helper()
	phase := "curve"
	if s.Phase != "curve" {
		phase = "graduated"
	}
	return token.QuoteState{
		Detail: token.Detail{
			Summary:             token.Summary{TotalSupply: amount(t, p.TotalSupply), Phase: phase, LaunchTime: launchTime},
			EngineVersion:       engine,
			CurveTokens:         amount(t, p.CurveTokens),
			LPTokens:            amount(t, p.LPTokens),
			GraduationETH:       amount(t, p.GraduationETH),
			InitialVirtualETH:   amount(t, p.InitialVirtualETH),
			InitialVirtualToken: amount(t, p.InitialVirtualToken),
			TradeFeeBPS:         uint16(p.TradeFeeBPS),
			ProtocolShareBPS:    uint16(p.ProtocolShareBPS),
			ETHReserve:          amount(t, s.VirtualETH),
			TokenReserve:        amount(t, s.VirtualToken),
		},
		ProtocolFees: amount(t, s.ProtocolFees),
		CreatorFees:  amount(t, s.CreatorFees),
	}
}

// TestQuoteReplaysEngineV2Vectors quotes every v2 Solidity vector at the vector's clock, taken
// as the indexed head's block time.
func TestQuoteReplaysEngineV2Vectors(t *testing.T) {
	artifact, err := curve.LoadEmbeddedVectorsV2()
	if err != nil {
		t.Fatal(err)
	}
	p := artifact.Parameters
	for _, tc := range artifact.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			state := quoteState(t, p.VectorParameters, tc.InitialState.VectorState, 2)
			state.AntiSnipeStartBPS = uint16(p.AntiSnipeStartBPS)
			state.AntiSnipeDurationSeconds = uint32(p.AntiSnipeDurationSeconds)
			state.HeadTime = launchTime.Add(time.Duration(tc.SecondsSinceLaunch) * time.Second)
			buy := tc.Operation == "buy"
			input := tc.Input.ETHGross
			if !buy {
				input = tc.Input.TokensIn
			}
			result, err := quote.Service{Reader: stateReader{state}, ChainID: 1}.Quote(t.Context(), common.Address{1}, buy, amount(t, input))
			if tc.ExpectedRevert != nil {
				if err == nil {
					t.Fatalf("quote succeeded, want %s", tc.ExpectedRevert.Name)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := tc.Output
			wantOutput := want.TokenAmount
			if !buy {
				wantOutput = want.ETHOut
			}
			checks := []struct {
				name      string
				got, want *big.Int
			}{
				{"output", result.Output, amount(t, wantOutput)},
				{"protocol fee", result.ProtocolFee, amount(t, want.ProtocolFee)},
				{"creator fee", result.CreatorFee, amount(t, want.CreatorFee)},
				{"refund", result.Refund, amount(t, want.ETHRefund)},
				{"snipe tax", result.SnipeTax, amount(t, want.SnipeTax)},
			}
			for _, check := range checks {
				if check.got == nil || check.got.Cmp(check.want) != 0 {
					t.Fatalf("%s = %v, want %s", check.name, check.got, check.want)
				}
			}
			if result.TaxBPS != uint16(want.TaxBPS) || result.Graduates != want.Graduates {
				t.Fatalf("tax bps %d graduates %t, want %d and %t", result.TaxBPS, result.Graduates, want.TaxBPS, want.Graduates)
			}
			wantEnds := time.Time{}
			if want.TaxBPS > 0 {
				wantEnds = launchTime.Add(time.Duration(p.AntiSnipeDurationSeconds) * time.Second)
			}
			if !result.TaxEndsAt.Equal(wantEnds) {
				t.Fatalf("tax ends at %v, want %v", result.TaxEndsAt, wantEnds)
			}
		})
	}
}

func TestQuoteEngineV1HasNoSnipeTax(t *testing.T) {
	artifact, err := curve.LoadEmbeddedVectors()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range artifact.Cases {
		if tc.Operation != "buy" || tc.ExpectedRevert != nil {
			continue
		}
		state := quoteState(t, artifact.Parameters, tc.InitialState, 1)
		// A v1 token carries no profile; even a launch-second head time must not tax it.
		state.HeadTime = launchTime
		result, err := quote.Service{Reader: stateReader{state}, ChainID: 1}.Quote(t.Context(), common.Address{1}, true, amount(t, tc.Input.ETHGross))
		if err != nil {
			t.Fatal(err)
		}
		if result.Output.Cmp(amount(t, tc.Output.TokenAmount)) != 0 {
			t.Fatalf("%s: output %v, want %s", tc.ID, result.Output, tc.Output.TokenAmount)
		}
		if result.SnipeTax == nil || result.SnipeTax.Sign() != 0 || result.TaxBPS != 0 || !result.TaxEndsAt.IsZero() {
			t.Fatalf("%s: v1 tax %v / %d bps / ends %v, want none", tc.ID, result.SnipeTax, result.TaxBPS, result.TaxEndsAt)
		}
		return
	}
	t.Fatal("no v1 buy vector")
}

func TestQuoteGraduatedEngineV2IsWrongPhase(t *testing.T) {
	artifact, err := curve.LoadEmbeddedVectorsV2()
	if err != nil {
		t.Fatal(err)
	}
	p := artifact.Parameters
	initial := artifact.Cases[0].InitialState.VectorState
	initial.Phase = "graduated"
	state := quoteState(t, p.VectorParameters, initial, 2)
	state.AntiSnipeStartBPS = uint16(p.AntiSnipeStartBPS)
	state.AntiSnipeDurationSeconds = uint32(p.AntiSnipeDurationSeconds)
	state.HeadTime = launchTime
	_, err = quote.Service{Reader: stateReader{state}, ChainID: 1}.Quote(t.Context(), common.Address{1}, true, big.NewInt(1e18))
	var phase curve.ErrWrongPhase
	if !errors.As(err, &phase) {
		t.Fatalf("graduated v2 quote error = %v, want ErrWrongPhase", err)
	}
}
