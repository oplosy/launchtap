//go:build integration

package postgrestest

import (
	"errors"
	"testing"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

// The indexer records the engines it ingests and refuses an engine listed after its cursor
// passed that engine's start block (docs/specs/2026-11-20-dual-engine-preflight.md, decision 5).
func TestRecordEngineDeploymentsRefusesAnEngineAddedLate(t *testing.T) {
	database := NewMigrated(t)
	ctx := t.Context()
	const chainID, deploymentID = 49030, "dual-engine-site"
	owner, err := storepostgres.AcquireOwnership(ctx, database.URL, chainID, deploymentID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	v1 := storepostgres.EngineDeployment{DeploymentID: deploymentID, EngineVersion: 1, Factory: common.HexToAddress("0xf1"), StartBlock: 100}
	v2 := storepostgres.EngineDeployment{DeploymentID: "dual-engine-v2", EngineVersion: 2, Factory: common.HexToAddress("0xf2"), StartBlock: 500}

	if err := owner.RecordEngineDeployments(ctx, chainID, deploymentID, []storepostgres.EngineDeployment{v1}); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO sync_state (chain_id, deployment_id, observed_number, observed_hash, observed_at) VALUES ($1, $2, 600, decode(repeat('ab', 32), 'hex'), now())`, chainID, deploymentID); err != nil {
		t.Fatal(err)
	}
	if err := owner.RecordEngineDeployments(ctx, chainID, deploymentID, []storepostgres.EngineDeployment{v1}); err != nil {
		t.Fatalf("restart with the same engines: %v", err)
	}
	var late *storepostgres.EngineAddedLateError
	if err := owner.RecordEngineDeployments(ctx, chainID, deploymentID, []storepostgres.EngineDeployment{v1, v2}); !errors.As(err, &late) {
		t.Fatalf("engine added after the cursor: error = %v, want EngineAddedLateError", err)
	}
	var count int
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM indexer_engine_deployments WHERE chain_id = $1 AND deployment_id = $2`, chainID, deploymentID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("recorded engines = %d, %v; want only engine 1", count, err)
	}
}
