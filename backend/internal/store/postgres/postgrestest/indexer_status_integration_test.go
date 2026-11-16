//go:build integration

package postgrestest

import (
	"context"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

// Audit pass 3, L-5: every saved indexer state moves the heartbeat with the database clock, and
// the API role reads it back as the indexer status.
func TestIndexerHeartbeatFollowsSavedState(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	database := NewMigrated(t)
	writer := openPool(t, ctx, database.URLAs(t, IndexerRole))
	reader := storepostgres.StatusReader{Pool: openPool(t, ctx, database.URLAs(t, APIRole))}
	const chainID, deployment = int64(46630), "status-dep"

	before, err := reader.Indexer(ctx, chainID, deployment)
	if err != nil || before.HeartbeatAt != nil || before.ObservedBlock != nil || before.CheckedAt.IsZero() || !before.Stale() {
		t.Fatalf("before the first step = %+v, %v; want no heartbeat, stale", before, err)
	}

	at := time.Now().UTC().Truncate(time.Second)
	hash := hashBytes(0x31)
	mustInsertBlock(t, ctx, database.DB, chainID, 7, hash, hashBytes(0x30), at, "safe")
	block := ledger.IndexedBlock{ChainID: chainID, BlockNumber: 7, BlockHash: common.BytesToHash(hash), BlockTime: at}
	save := func() {
		t.Helper()
		if err := storepostgres.WithinTx(ctx, writer, func(ctx context.Context, a *storepostgres.Adapter) error {
			return a.WriteState(ctx, indexer.State{ChainID: chainID, DeploymentID: deployment, Observed: &block, Safe: &block})
		}); err != nil {
			t.Fatal(err)
		}
	}
	save()
	first, err := reader.Indexer(ctx, chainID, deployment)
	if err != nil || first.HeartbeatAt == nil || first.Stale() || first.ObservedBlock == nil || *first.ObservedBlock != 7 ||
		first.SafeBlock == nil || *first.SafeBlock != 7 || first.FinalizedBlock != nil {
		t.Fatalf("after a step = %+v, %v", first, err)
	}

	// An idle step at the tip saves the same blocks and still moves the heartbeat.
	if _, err := database.DB.ExecContext(ctx, `UPDATE indexer_heartbeats SET beat_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if stalled, err := reader.Indexer(ctx, chainID, deployment); err != nil || !stalled.Stale() {
		t.Fatalf("an hour-old heartbeat = %+v, %v; want stale", stalled, err)
	}
	save()
	if again, err := reader.Indexer(ctx, chainID, deployment); err != nil || again.Stale() || !again.HeartbeatAt.After(first.HeartbeatAt.Add(-time.Second)) {
		t.Fatalf("after an idle step = %+v, %v; want a fresh heartbeat", again, err)
	}

	// Another deployment's heartbeat is not this one's.
	if other, err := reader.Indexer(ctx, chainID, "other-dep"); err != nil || other.HeartbeatAt != nil {
		t.Fatalf("other deployment = %+v, %v", other, err)
	}
}
