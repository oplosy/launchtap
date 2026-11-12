package curve

import (
	"math/big"
	"strings"
	"testing"
)

func TestLoadEmbeddedVectorsV2(t *testing.T) {
	artifact, err := LoadEmbeddedVectorsV2()
	if err != nil {
		t.Fatalf("LoadEmbeddedVectorsV2() error = %v", err)
	}
	if artifact.EngineVersion != 2 || len(artifact.Cases) < 10 {
		t.Fatalf("engineVersion = %d, cases = %d", artifact.EngineVersion, len(artifact.Cases))
	}
	if artifact.Parameters.AntiSnipeStartBPS != 5000 || artifact.Parameters.AntiSnipeDurationSeconds != 60 {
		t.Fatalf("anti-snipe parameters = %+v", artifact.Parameters)
	}
}

func TestLoadVectorsV2RejectsInvalidArtifacts(t *testing.T) {
	source := embeddedVectorsV2
	cases := map[string]func(map[string]any){
		"v1 engine version":  func(v map[string]any) { v["engineVersion"] = 1 },
		"unknown field":      func(v map[string]any) { v["extra"] = true },
		"missing snipe tax":  func(v map[string]any) { delete(firstCase(v)["initialState"].(map[string]any), "snipeTax") },
		"tax above cap":      func(v map[string]any) { v["parameters"].(map[string]any)["antiSnipeStartBps"] = 9001 },
		"duration above cap": func(v map[string]any) { v["parameters"].(map[string]any)["antiSnipeDurationSeconds"] = 301 },
		"negative seconds":   func(v map[string]any) { firstCase(v)["secondsSinceLaunch"] = -1 },
		"missing graduation": func(v map[string]any) { delete(firstCase(v), "graduation") },
		"bad graduation sqrt": func(v map[string]any) {
			caseByID(v, "buy_final_in_window")["graduation"].(map[string]any)["sqrtPriceX96"] = "-1"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadVectorsV2(strings.NewReader(mutateJSON(t, string(source), mutate))); err == nil {
				t.Fatal("LoadVectorsV2() error = nil")
			}
		})
	}
}

// Each case runs at launchTime 0 with `now = secondsSinceLaunch`, matching how the Solidity
// generator warps from the launch block. The graduation block is checked for shape only:
// the indexer reads pool state from the Graduated event and mirrors no graduation math.
func TestSolidityVectorsV2(t *testing.T) {
	artifact, err := LoadEmbeddedVectorsV2()
	if err != nil {
		t.Fatalf("LoadEmbeddedVectorsV2() error = %v", err)
	}
	parameters := parametersFromVector(t, artifact.Parameters.VectorParameters)
	profile, err := NewAntiSnipe(uint16(artifact.Parameters.AntiSnipeStartBPS), uint32(artifact.Parameters.AntiSnipeDurationSeconds))
	if err != nil {
		t.Fatalf("NewAntiSnipe() error = %v", err)
	}

	for _, vectorCase := range artifact.Cases {
		t.Run(vectorCase.ID, func(t *testing.T) {
			state := stateFromVector(t, vectorCase.InitialState.VectorState)
			assertDerivedVectorState(t, parameters, state, vectorCase.InitialState.VectorState)
			now := uint64(vectorCase.SecondsSinceLaunch)

			switch vectorCase.Operation {
			case "buy":
				quote, quoteErr := BuyV2(state, parameters, profile, 0, now, decimal(t, vectorCase.Input.ETHGross))
				if vectorCase.ExpectedRevert != nil {
					assertVectorError(t, quoteErr, vectorCase.ExpectedRevert.Name)
					return
				}
				if quoteErr != nil {
					t.Fatalf("BuyV2() error = %v", quoteErr)
				}
				expected := *vectorCase.Output
				assertAmount(t, "ethGross", quote.ETHGrossUsed(), expected.ETHGross)
				assertAmount(t, "ethRefund", quote.Refund(), expected.ETHRefund)
				assertAmount(t, "ethOut", new(big.Int), expected.ETHOut)
				assertAmount(t, "tokenAmount", quote.TokensOut(), expected.TokenAmount)
				assertAmount(t, "protocolFee", quote.ProtocolFee(), expected.ProtocolFee)
				assertAmount(t, "creatorFee", quote.CreatorFee(), expected.CreatorFee)
				assertAmount(t, "snipeTax", quote.SnipeTax(), expected.SnipeTax)
				if int(quote.TaxBPS()) != expected.TaxBPS || quote.Graduates() != expected.Graduates {
					t.Fatalf("TaxBPS() = %d, Graduates() = %t, want %d, %t", quote.TaxBPS(), quote.Graduates(), expected.TaxBPS, expected.Graduates)
				}
				if quote.Graduates() != (vectorCase.Graduation != nil) {
					t.Fatalf("graduation block present = %t, want %t", vectorCase.Graduation != nil, quote.Graduates())
				}
				assertVectorState(t, parameters, quote.NextState(), curveNextState(t, vectorCase))
				assertSnipeTaxAccrual(t, vectorCase, quote.SnipeTax())
			case "sell":
				quote, quoteErr := Sell(state, parameters, decimal(t, vectorCase.Input.TokensIn))
				if vectorCase.ExpectedRevert != nil {
					assertVectorError(t, quoteErr, vectorCase.ExpectedRevert.Name)
					return
				}
				if quoteErr != nil {
					t.Fatalf("Sell() error = %v", quoteErr)
				}
				expected := *vectorCase.Output
				assertSellVectorOutput(t, quote, vectorCase.Input.TokensIn, expected.VectorOutput)
				// Sells are never taxed; the vector still records the rate in force.
				assertAmount(t, "snipeTax", new(big.Int), expected.SnipeTax)
				if got := TaxBPS(profile, 0, now); int(got) != expected.TaxBPS {
					t.Fatalf("TaxBPS() = %d, want %d", got, expected.TaxBPS)
				}
				assertVectorState(t, parameters, quote.NextState(), vectorCase.NextState.VectorState)
				assertSnipeTaxAccrual(t, vectorCase, new(big.Int))
			default:
				t.Fatalf("unsupported operation %q", vectorCase.Operation)
			}
		})
	}
}

// curveNextState is the vector's next state without the graduation ETH remainder, which the
// curve books to the protocol inside _graduate (reported by Graduated.ethRemainder) rather
// than in the quote that this package mirrors.
func curveNextState(t *testing.T, vectorCase VectorCaseV2) VectorState {
	t.Helper()
	next := vectorCase.NextState.VectorState
	if vectorCase.Graduation == nil {
		return next
	}
	protocolFees := new(big.Int).Sub(decimal(t, next.ProtocolFees), decimal(t, vectorCase.Graduation.ETHRemainder))
	if protocolFees.Sign() < 0 {
		t.Fatalf("graduation remainder %s exceeds next protocol fees %s", vectorCase.Graduation.ETHRemainder, next.ProtocolFees)
	}
	next.ProtocolFees = protocolFees.String()
	return next
}

func assertSnipeTaxAccrual(t *testing.T, vectorCase VectorCaseV2, tax *big.Int) {
	t.Helper()
	next := new(big.Int).Add(decimal(t, vectorCase.InitialState.SnipeTax), tax)
	assertAmount(t, "nextState.snipeTax", next, vectorCase.NextState.SnipeTax)
}

// Guards against the v2 artifact being decoded by the v1 loader.
func TestLoadVectorsRejectsV2Artifact(t *testing.T) {
	if _, err := LoadVectors(strings.NewReader(string(embeddedVectorsV2))); err == nil {
		t.Fatal("LoadVectors() accepted the v2 artifact")
	}
}
