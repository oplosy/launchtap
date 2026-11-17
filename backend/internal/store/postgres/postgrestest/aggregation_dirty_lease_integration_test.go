//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/stats"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
)

// TestAggregationDirtyLeaseGuardsStaleCompletion validates the fixed 30s lease
// without sleeping for its full duration. Run with:
// go test -tags=integration ./internal/store/postgres/postgrestest -run '^TestAggregationDirtyLeaseGuardsStaleCompletion$' -count=1 -v
func TestAggregationDirtyLeaseGuardsStaleCompletion(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URLAs(t, IndexerRole))
	const chainID int64 = 48010
	now := time.Now().UTC().Truncate(time.Second)
	blockHash := hashBytes(0x61)
	token, curve := addressBytes(0x62), addressBytes(0x63)
	pair, weth := addressBytes(0x64), addressBytes(0x65)
	mustInsertBlock(t, ctx, database.DB, chainID, 1, blockHash, hashBytes(0x60), now, "observed")
	insertProjectionLaunch(t, ctx, database.DB, chainID, 1, blockHash, now, hashBytes(0x66), projectionLaunchFixture{
		token: token, curve: curve, pair: pair, weth: weth,
	})
	callRebuild(t, ctx, database.DB, chainID, token)
	markAggregationDirtyGeneration(t, ctx, database.DB, chainID, token)

	source := storepostgres.AggregationSource{Adapter: storepostgres.NewAdapter(pool)}
	initialClaims, err := source.Claim(ctx, "initial-owner", 1)
	if err != nil || len(initialClaims) != 1 {
		t.Fatalf("claim seeded dirty row: claims=%d err=%v", len(initialClaims), err)
	}
	assertAggregationDirtyLeaseBehavior(t, ctx, source, database.DB, chainID, initialClaims)
}

func assertAggregationDirtyLeaseBehavior(t *testing.T, ctx context.Context, source storepostgres.AggregationSource, database *sql.DB, chainID int64, tokens []stats.Claim) {
	t.Helper()

	// The worker does not renew the fixed 30s lease. Expiry may duplicate
	// recomputation, but completion must not delete work claimed by a newer owner
	// or work whose generation advanced while the prior computation was running.
	if _, err := database.ExecContext(ctx, `UPDATE aggregation_dirty SET claimed_at = now() - interval '31 seconds' WHERE chain_id = $1 AND claimed_generation IS NOT NULL`, chainID); err != nil {
		t.Fatalf("expire fixture lease: %v", err)
	}
	claimedA, err := source.Claim(ctx, "lease-worker-a", int32(len(tokens)))
	if err != nil || len(claimedA) != len(tokens) {
		t.Fatalf("reclaim dirty tokens for lease test: claims=%d err=%v", len(claimedA), err)
	}
	first := claimedA[0]
	if _, err := database.ExecContext(ctx, `UPDATE aggregation_dirty SET claimed_at = now() - interval '31 seconds' WHERE chain_id = $1 AND token_address = $2`, chainID, first.Token[:]); err != nil {
		t.Fatalf("expire first lease: %v", err)
	}
	claimedB, err := source.Claim(ctx, "lease-worker-b", 1)
	if err != nil || len(claimedB) != 1 || claimedB[0].Token != first.Token {
		t.Fatalf("take over expired claim: claims=%#v err=%v", claimedB, err)
	}
	completed, err := source.Complete(ctx, first, "lease-worker-a")
	if err != nil || completed {
		t.Fatalf("stale owner completion = %t, %v; want false", completed, err)
	}
	assertDirtyRowPresent(t, ctx, database, chainID, first.Token[:])

	// Mirror UpsertAggregationDirty's conflict path to model newer dirty work.
	markAggregationDirtyGeneration(t, ctx, database, chainID, first.Token[:])
	claimedC, err := source.Claim(ctx, "lease-worker-c", 1)
	if err != nil || len(claimedC) != 1 || claimedC[0].Token != first.Token || claimedC[0].Generation <= claimedB[0].Generation {
		t.Fatalf("claim advanced dirty generation: claims=%#v err=%v", claimedC, err)
	}
	completed, err = source.Complete(ctx, claimedB[0], "lease-worker-b")
	if err != nil || completed {
		t.Fatalf("stale generation completion = %t, %v; want false", completed, err)
	}
	assertDirtyRowPresent(t, ctx, database, chainID, first.Token[:])
	completed, err = source.Complete(ctx, claimedC[0], "lease-worker-c")
	if err != nil || !completed {
		t.Fatalf("current generation completion = %t, %v; want true", completed, err)
	}
	t.Log("lease_check=30s expired owner rejected; newer generation survived stale completion and was removed only by current owner")
}

func markAggregationDirtyGeneration(t *testing.T, ctx context.Context, database *sql.DB, chainID int64, token []byte) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO aggregation_dirty (chain_id, token_address, generation)
		VALUES ($1, $2, nextval('aggregation_dirty_generation_seq'))
		ON CONFLICT (chain_id, token_address)
		DO UPDATE SET generation = nextval('aggregation_dirty_generation_seq')
	`, chainID, token); err != nil {
		t.Fatalf("advance dirty generation: %v", err)
	}
}

func assertDirtyRowPresent(t *testing.T, ctx context.Context, database *sql.DB, chainID int64, token []byte) {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM aggregation_dirty WHERE chain_id = $1 AND token_address = $2)`, chainID, token).Scan(&exists); err != nil {
		t.Fatalf("read dirty row after stale completion: %v", err)
	}
	if !exists {
		t.Fatal("stale completion deleted dirty work")
	}
}
