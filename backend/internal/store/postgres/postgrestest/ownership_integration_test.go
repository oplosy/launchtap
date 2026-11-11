//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"testing"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
)

func TestOwnershipFencesTerminatedSession(t *testing.T) {
	database := NewMigrated(t)
	ctx := t.Context()
	first, err := storepostgres.AcquireOwnership(ctx, database.URL, 49020, "test-owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if second, err := storepostgres.AcquireOwnership(ctx, database.URL, 49020, "test-owner"); !errors.Is(err, storepostgres.ErrOwnershipBusy) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("second owner: %v", err)
	}
	var terminated bool
	if err := database.DB.QueryRowContext(ctx, `SELECT pg_terminate_backend($1)`, first.BackendPID()).Scan(&terminated); err != nil || !terminated {
		t.Fatalf("terminate owner: %v", err)
	}
	second, err := storepostgres.AcquireOwnership(ctx, database.URL, 49020, "test-owner")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	called := false
	if err := first.WithinTx(ctx, func(context.Context, *storepostgres.Adapter) error { called = true; return nil }); err == nil || called {
		t.Fatalf("dead owner wrote before probe: called=%t err=%v", called, err)
	}
	if err := first.Probe(ctx); !errors.Is(err, storepostgres.ErrOwnershipLost) {
		t.Fatalf("probe error: %v", err)
	}
	if err := second.WithinTx(ctx, func(context.Context, *storepostgres.Adapter) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

// Writer ownership is chain-scoped: two deployments must never index one chain at once,
// because the ledger and reorg deletes are keyed by chain only (pre-flight B2).
func TestOwnershipIsExclusivePerChain(t *testing.T) {
	database := NewMigrated(t)
	ctx := t.Context()
	first, err := storepostgres.AcquireOwnership(ctx, database.URL, 49021, "deployment-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if second, err := storepostgres.AcquireOwnership(ctx, database.URL, 49021, "deployment-b"); !errors.Is(err, storepostgres.ErrOwnershipBusy) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("second deployment on the same chain: %v", err)
	}
	other, err := storepostgres.AcquireOwnership(ctx, database.URL, 49022, "deployment-b")
	if err != nil {
		t.Fatalf("other chain: %v", err)
	}
	_ = other.Close()
}

func TestForeignDeploymentsListsOtherDeploymentsOnTheChain(t *testing.T) {
	database := NewMigrated(t)
	ctx := t.Context()
	for _, row := range []struct {
		chainID      int64
		deploymentID string
	}{{49023, "mine"}, {49023, "theirs"}, {49024, "elsewhere"}} {
		if _, err := database.DB.ExecContext(ctx, `INSERT INTO sync_state (chain_id, deployment_id) VALUES ($1, $2)`, row.chainID, row.deploymentID); err != nil {
			t.Fatal(err)
		}
	}
	adapter := storepostgres.NewAdapter(openPool(t, ctx, database.URL))
	others, err := adapter.ForeignDeployments(ctx, 49023, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if len(others) != 1 || others[0] != "theirs" {
		t.Fatalf("ForeignDeployments() = %v, want [theirs]", others)
	}
	none, err := adapter.ForeignDeployments(ctx, 49024, "elsewhere")
	if err != nil || len(none) != 0 {
		t.Fatalf("ForeignDeployments(single) = %v, %v", none, err)
	}
}
