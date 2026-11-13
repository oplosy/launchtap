package indexer

import (
	"testing"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/ethereum/go-ethereum/common"
)

func TestEngineCachesIdentitiesAcrossSteps(t *testing.T) {
	e, store, _ := newTestEngine(t)
	loads := 0
	store.unit.identityLoads = &loads
	// Chunks 1-2, 3-4 and 5, then an idle poll at the head.
	for step := range 4 {
		if _, err := e.Step(t.Context()); err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
	}
	if store.unit.state.Observed == nil || store.unit.state.Observed.BlockNumber != 5 {
		t.Fatalf("observed = %+v, want block 5", store.unit.state.Observed)
	}
	if loads != 1 {
		t.Fatalf("identity loads = %d over four steps without launches; want 1", loads)
	}
}

func TestEngineReloadsIdentitiesAfterCommittedLaunchAndGraduation(t *testing.T) {
	e, store, _, discovery, _ := newV2Engine(t)
	loads := 0
	store.unit.identityLoads = &loads
	// The fixture chunk launches and graduates a token.
	if _, err := e.Step(t.Context()); err != nil {
		t.Fatalf("launch step: %v", err)
	}
	// The memory store has no projections; stand in for the tokens row the launch wrote.
	launched, curve := common.HexToAddress("0x1001"), common.HexToAddress("0x2001")
	store.unit.identities = []TokenIdentity{{Token: launched, Curve: curve, EngineVersion: 2}}
	discovery.logs = nil
	if _, err := e.Step(t.Context()); err != nil {
		t.Fatalf("step after launch: %v", err)
	}
	if loads != 2 {
		t.Fatalf("identity loads = %d; want a reload after the launch chunk", loads)
	}
	if _, ok := discovery.known[1].Curves[curve]; !ok {
		t.Fatalf("known curves after launch = %v; want the launched curve %s", discovery.known[1].Curves, curve.Hex())
	}
	if _, err := e.Step(t.Context()); err != nil {
		t.Fatalf("step without launches: %v", err)
	}
	if loads != 2 {
		t.Fatalf("identity loads = %d; want the cache reused after a chunk without launches", loads)
	}
}

func TestEngineReloadsIdentitiesAfterFailedStep(t *testing.T) {
	e, store, _ := newTestEngine(t)
	loads := 0
	store.unit.identityLoads = &loads
	if _, err := e.Step(t.Context()); err != nil {
		t.Fatalf("first step: %v", err)
	}
	store.unit.fail = true
	if _, err := e.Step(t.Context()); err == nil {
		t.Fatal("expected the failing commit to fail the step")
	}
	store.unit.fail = false
	if _, err := e.Step(t.Context()); err != nil {
		t.Fatalf("step after failure: %v", err)
	}
	if loads != 2 {
		t.Fatalf("identity loads = %d; want a reload after the failed step", loads)
	}
}

func TestReorgRecoveryDropsIdentityCache(t *testing.T) {
	t.Run("completed recovery", func(t *testing.T) {
		e, _, _, state, tip := newDeepReorgFixture(t, 200, true, 140, 130, nil)
		e.identities = []TokenIdentity{}
		if err := e.recoverReorg(t.Context(), state, tip); err != nil {
			t.Fatalf("recoverReorg() error = %v", err)
		}
		if e.identities != nil {
			t.Fatal("identity cache survived a completed reorg recovery")
		}
	})
	t.Run("failed recovery", func(t *testing.T) {
		e, _, _ := newTestEngine(t)
		e.identities = []TokenIdentity{}
		if err := e.recoverReorg(t.Context(), State{}, ledger.IndexedBlock{}); err == nil {
			t.Fatal("expected recovery without an observed tip to fail")
		}
		if e.identities != nil {
			t.Fatal("identity cache survived a failed reorg recovery")
		}
	})
}

func TestChangesIdentitiesCoversLaunchesAndGraduations(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
		want  bool
	}{
		{"engine-1 launch", chain.TokenLaunched{}, true},
		{"engine-2 launch", chain.TokenLaunchedV2{}, true},
		{"engine-1 graduation", chain.Graduated{}, true},
		{"engine-2 graduation", chain.GraduatedV2{}, true},
		{"curve trade", chain.Trade{}, false},
		{"V4 swap", chain.V4Swap{}, false},
	} {
		if got := changesIdentities([]chain.DecodedLog{{Value: test.value}}); got != test.want {
			t.Errorf("%s: changesIdentities = %t, want %t", test.name, got, test.want)
		}
	}
}
