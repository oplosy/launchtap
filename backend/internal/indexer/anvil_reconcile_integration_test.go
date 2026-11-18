//go:build integration

package indexer_test

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/reconcile"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

// reconcileOnce runs one reconciliation pass over an indexed Anvil database, as the indexer's
// worker does (docs/specs/2026-11-14-state-reconciliation-preflight.md).
func reconcileOnce(t *testing.T, ctx context.Context, pool *pgxpool.Pool, source *chain.Client, chainID int64, deploymentID string) reconcile.Result {
	t.Helper()
	result, err := reconcile.Pass(ctx, storepostgres.ReconcileSource{Pool: pool, ChainID: chainID, DeploymentID: deploymentID}, chain.StateReader{Client: source}, reconcile.Limits{})
	if err != nil {
		t.Fatalf("reconcile pass: %v", err)
	}
	return result
}

// assertReconciled expects the indexed state to match the chain, with token among the sampled.
func assertReconciled(t *testing.T, ctx context.Context, pool *pgxpool.Pool, source *chain.Client, chainID int64, deploymentID string) reconcile.Result {
	t.Helper()
	result := reconcileOnce(t, ctx, pool, source, chainID, deploymentID)
	if result.Outcome != reconcile.OutcomeOK || len(result.Mismatches) != 0 || result.Checks == 0 {
		t.Fatalf("reconciliation = %s with %d checks, mismatches %v; want ok", result.Outcome, result.Checks, result.Mismatches)
	}
	return result
}

// assertPoolDriftDetected shifts the graduated token's indexed pool state by one unit (the pair's
// WETH reserve for CheckPairETH, the v4 price for CheckV4Price), expects exactly that mismatch, and
// restores the row (docs/specs/2026-11-18-pool-reconciliation-preflight.md).
func assertPoolDriftDetected(t *testing.T, ctx context.Context, db *sql.DB, pool *pgxpool.Pool, source *chain.Client, chainID int64, deploymentID string, token common.Address, check string) {
	t.Helper()
	statements := map[string]string{
		reconcile.CheckPairETH: `UPDATE token_reserves SET eth_reserve = eth_reserve + $3 WHERE chain_id=$1 AND token_address=$2 AND reserve_source='pair'`,
		reconcile.CheckV4Price: `UPDATE token_reserves SET sqrt_price_x96 = sqrt_price_x96 + $3 WHERE chain_id=$1 AND token_address=$2 AND reserve_source='v4'`,
	}
	statement, ok := statements[check]
	if !ok {
		t.Fatalf("no pool corruption for check %s", check)
	}
	shift := func(delta int) {
		t.Helper()
		changed, err := db.ExecContext(ctx, statement, chainID, token.Bytes(), delta)
		if err != nil {
			t.Fatalf("shift indexed pool state: %v", err)
		}
		if rows, err := changed.RowsAffected(); err != nil || rows != 1 {
			t.Fatalf("shift indexed pool state: rows=%d err=%v; want 1", rows, err)
		}
	}
	shift(1)
	result := reconcileOnce(t, ctx, pool, source, chainID, deploymentID)
	shift(-1)
	if result.Outcome != reconcile.OutcomeDrift || len(result.Mismatches) != 1 || result.Mismatches[0].Check != check || result.Mismatches[0].Token != token {
		t.Fatalf("corrupted pool state: outcome %s, mismatches %v; want exactly %s on %s", result.Outcome, result.Mismatches, check, token.Hex())
	}
}

// assertDriftDetected corrupts one indexed holder balance, expects the pass to report exactly the
// holder and the supply-sum mismatch for it, and restores the row.
func assertDriftDetected(t *testing.T, ctx context.Context, db *sql.DB, pool *pgxpool.Pool, source *chain.Client, chainID int64, deploymentID string, token common.Address) {
	t.Helper()
	var holderBytes []byte
	if err := db.QueryRowContext(ctx, `SELECT holder_address FROM holder_balances WHERE chain_id=$1 AND token_address=$2 ORDER BY balance DESC, holder_address LIMIT 1`, chainID, token.Bytes()).Scan(&holderBytes); err != nil {
		t.Fatalf("read top holder: %v", err)
	}
	shift := func(delta int) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `UPDATE holder_balances SET balance = balance + $4 WHERE chain_id=$1 AND token_address=$2 AND holder_address=$3`, chainID, token.Bytes(), holderBytes, delta); err != nil {
			t.Fatalf("shift holder balance: %v", err)
		}
	}
	shift(1)
	result := reconcileOnce(t, ctx, pool, source, chainID, deploymentID)
	shift(-1)
	var checks []string
	for _, mismatch := range result.Mismatches {
		if mismatch.Token != token {
			t.Fatalf("unexpected mismatch on another token: %s", mismatch)
		}
		checks = append(checks, mismatch.Check)
	}
	slices.Sort(checks)
	if result.Outcome != reconcile.OutcomeDrift || !slices.Equal(checks, []string{reconcile.CheckHolder, reconcile.CheckSupply}) {
		t.Fatalf("corrupted holder: outcome %s, mismatches %v; want drift on %s and %s", result.Outcome, result.Mismatches, reconcile.CheckHolder, reconcile.CheckSupply)
	}
}
