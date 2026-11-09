//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/stats"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
)

// The stats SQL reads the database clock, so these tests seed trades relative to real time and
// emulate time passing by shifting mutable candle rows (trades and blocks are immutable).
func TestVolume24hDecaysWithoutNewTrades(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	fixture := newProjectionDifferentialFixture()
	ingestProjectionChunk(t, ctx, pool, fixture.chunks[0])

	// The trade is 24h 1m old. Its candles are shifted 3 minutes forward to reproduce the moment
	// it was still 23h 58m old, comfortably inside the window.
	ingestProjectionChunk(t, ctx, pool, tradeChunk(fixture, 2, time.Now().UTC().Add(-24*time.Hour-time.Minute), 50))
	shiftCandles(t, ctx, database.DB, fixture, "3 minutes")

	source := storepostgres.AggregationSource{Adapter: storepostgres.NewAdapter(pool), ChainID: fixture.chainID}
	drainAggregationWorker(t, ctx, database.DB, source)
	if volume, sortVolume := readVolumes(t, ctx, database.DB, fixture); volume != "50" || sortVolume != "50" {
		t.Fatalf("volume_24h/sort before decay = %s/%s; want 50/50", volume, sortVolume)
	}

	// Time passes with no new trade: the candle is back at its real, expired bucket. Nothing is
	// dirty, so without a sweep the stale value stays: this is the bug.
	shiftCandles(t, ctx, database.DB, fixture, "-3 minutes")
	drainAggregationWorker(t, ctx, database.DB, source)
	if volume, _ := readVolumes(t, ctx, database.DB, fixture); volume != "50" {
		t.Fatalf("volume_24h = %s before sweep; want the stale 50", volume)
	}

	runSweeperUntilVolume(t, ctx, database.DB, source, fixture, "0")
	if _, sortVolume := readVolumes(t, ctx, database.DB, fixture); sortVolume != "0" {
		t.Fatalf("tokens.sort_volume_24h after decay = %s; want 0", sortVolume)
	}
}

// A token whose last trade is long outside the sweep window (for example expired while the
// indexer was down) is only reached by the startup catch-up.
func TestVolume24hStartupCatchUpDecaysMissedExpiry(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	fixture := newProjectionDifferentialFixture()
	ingestProjectionChunk(t, ctx, pool, fixture.chunks[0])
	ingestProjectionChunk(t, ctx, pool, tradeChunk(fixture, 2, time.Now().UTC().Add(-time.Hour), 50))

	source := storepostgres.AggregationSource{Adapter: storepostgres.NewAdapter(pool), ChainID: fixture.chainID}
	drainAggregationWorker(t, ctx, database.DB, source)
	if volume, sortVolume := readVolumes(t, ctx, database.DB, fixture); volume != "50" || sortVolume != "50" {
		t.Fatalf("volume_24h/sort before expiry = %s/%s; want 50/50", volume, sortVolume)
	}
	// The sweeper was down while the candle expired: it is now 30h old, far outside the sweep window.
	shiftCandles(t, ctx, database.DB, fixture, "-29 hours")
	drainAggregationWorker(t, ctx, database.DB, source)
	if volume, _ := readVolumes(t, ctx, database.DB, fixture); volume != "50" {
		t.Fatalf("volume_24h = %s before catch-up; want the stale 50", volume)
	}

	runSweeperUntilVolume(t, ctx, database.DB, source, fixture, "0")
	if _, sortVolume := readVolumes(t, ctx, database.DB, fixture); sortVolume != "0" {
		t.Fatalf("tokens.sort_volume_24h after catch-up = %s; want 0", sortVolume)
	}
}

// runSweeperUntilVolume runs the real WindowSweeper and stats worker until volume_24h reaches want.
func runSweeperUntilVolume(t *testing.T, ctx context.Context, db *sql.DB, source storepostgres.AggregationSource, fixture projectionDifferentialFixture, want string) {
	t.Helper()
	runCtx, stop := context.WithCancel(ctx)
	wake := make(chan struct{}, 1)
	var wakes atomic.Int64
	sweeper := stats.WindowSweeper{Source: source, ChainID: fixture.chainID, Interval: 50 * time.Millisecond, Wake: func() {
		wakes.Add(1)
		select {
		case wake <- struct{}{}:
		default:
		}
	}, OnError: func(err error) { t.Errorf("sweeper: %v", err) }}
	worker := stats.Worker{Source: source, WorkerID: "decay-test", PollInterval: 20 * time.Millisecond, BatchSize: 8, Wake: wake}
	finished := make(chan error, 2)
	go func() { finished <- sweeper.Run(runCtx) }()
	go func() { finished <- worker.Run(runCtx) }()
	defer func() {
		stop()
		for range 2 {
			if err := <-finished; err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("background run: %v", err)
			}
		}
	}()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if volume, _ := readVolumes(t, ctx, db, fixture); volume == want {
			if wakes.Load() == 0 {
				t.Fatal("sweeper marked tokens but never called Wake")
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	volume, _ := readVolumes(t, ctx, db, fixture)
	t.Fatalf("volume_24h = %s after sweeping; want %s", volume, want)
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
