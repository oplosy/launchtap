//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/stats"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
)

// The stats SQL reads the database clock, so this test seeds trades relative to
// real time and moves the data back in time instead of injecting a clock.
func TestVolume24hDecaysWithoutNewTrades(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	fixture := newProjectionDifferentialFixture()
	ingestProjectionChunk(t, ctx, pool, fixture.chunks[0])

	now := time.Now().UTC()
	ingestProjectionChunk(t, ctx, pool, tradeChunk(fixture, 2, now.Add(-24*time.Hour-time.Minute), 50))
	// The trade is already 24h 1m old, so ingest put its candles outside the window. Shift them
	// two minutes forward to reproduce the moment the trade was still 23h 59m old.
	shiftCandles(t, ctx, database.DB, fixture, "2 minutes")

	source := storepostgres.AggregationSource{Adapter: storepostgres.NewAdapter(pool), ChainID: fixture.chainID}
	drainAggregationWorker(t, ctx, database.DB, source)
	if volume, sortVolume := readVolumes(t, ctx, database.DB, fixture); volume != "50" || sortVolume != "50" {
		t.Fatalf("volume_24h/sort before decay = %s/%s; want 50/50", volume, sortVolume)
	}

	// Two minutes pass with no new trade: the candle is back at its real, expired bucket.
	shiftCandles(t, ctx, database.DB, fixture, "-2 minutes")
	// Without a sweep nothing is dirty, so the stale value stays: this is the bug.
	drainAggregationWorker(t, ctx, database.DB, source)
	if volume, _ := readVolumes(t, ctx, database.DB, fixture); volume != "50" {
		t.Fatalf("volume_24h = %s before sweep; want the stale 50", volume)
	}

	marked, err := source.MarkExpiringWindow(ctx, fixture.chainID, time.Now().Add(-stats.DefaultSweepWindow-stats.DefaultSweepOverlap), time.Now().Add(-stats.DefaultSweepWindow))
	if err != nil || marked != 1 {
		t.Fatalf("MarkExpiringWindow = %d, %v; want 1, nil", marked, err)
	}
	drainAggregationWorker(t, ctx, database.DB, source)
	if volume, sortVolume := readVolumes(t, ctx, database.DB, fixture); volume != "0" || sortVolume != "0" {
		t.Fatalf("volume_24h/sort after decay = %s/%s; want 0/0", volume, sortVolume)
	}
}

func shiftCandles(t *testing.T, ctx context.Context, db *sql.DB, fixture projectionDifferentialFixture, by string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `UPDATE candles SET bucket_start_time = bucket_start_time + $2::interval WHERE chain_id = $1`, fixture.chainID, by); err != nil {
		t.Fatalf("shift candles by %s: %v", by, err)
	}
}

func readVolumes(t *testing.T, ctx context.Context, db *sql.DB, fixture projectionDifferentialFixture) (volume, sortVolume string) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `
		SELECT s.volume_24h_eth_wad::TEXT, k.sort_volume_24h_eth_wad::TEXT
		FROM token_stats s JOIN tokens k USING (chain_id, token_address)
		WHERE s.chain_id = $1 AND s.token_address = $2
	`, fixture.chainID, fixture.token[:]).Scan(&volume, &sortVolume); err != nil {
		t.Fatalf("read volumes: %v", err)
	}
	return volume, sortVolume
}

// drainAggregationWorker runs the real stats worker until the dirty table is empty.
func drainAggregationWorker(t *testing.T, ctx context.Context, db *sql.DB, source storepostgres.AggregationSource) {
	t.Helper()
	workerCtx, stop := context.WithCancel(ctx)
	finished := make(chan error, 1)
	go func() {
		finished <- stats.Worker{Source: source, WorkerID: "decay-test", PollInterval: 20 * time.Millisecond, BatchSize: 8}.Run(workerCtx)
	}()
	defer func() {
		stop()
		if err := <-finished; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("worker: %v", err)
		}
	}()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var pending int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM aggregation_dirty`).Scan(&pending); err != nil {
			t.Fatalf("count dirty: %v", err)
		}
		if pending == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("aggregation worker did not drain the dirty table")
}
