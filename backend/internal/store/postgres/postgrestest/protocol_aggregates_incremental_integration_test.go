//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"encoding/binary"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/stats"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
)

// incrementalFixture is one chain with a launched token whose trades are added block by block.
type incrementalFixture struct {
	t        *testing.T
	ctx      context.Context
	database *Database
	chainID  int64
	token    []byte
	parent   []byte
}

func newIncrementalFixture(t *testing.T, ctx context.Context, database *Database, chainID int64, launchAt time.Time) *incrementalFixture {
	t.Helper()
	fixture := &incrementalFixture{t: t, ctx: ctx, database: database, chainID: chainID, token: addressBytes(0x72), parent: hashBytes(0x70)}
	blockHash := fixture.addBlock(1, launchAt)
	insertProjectionLaunch(t, ctx, database.DB, chainID, 1, blockHash, launchAt, hashBytes(0x76), projectionLaunchFixture{
		token: fixture.token, curve: addressBytes(0x73), pair: addressBytes(0x74), weth: addressBytes(0x75),
	})
	callRebuild(t, ctx, database.DB, chainID, fixture.token)
	return fixture
}

func (fixture *incrementalFixture) addBlock(number int64, at time.Time) []byte {
	fixture.t.Helper()
	hash := numberedHash(0xb1, number)
	mustInsertBlock(fixture.t, fixture.ctx, fixture.database.DB, fixture.chainID, number, hash, fixture.parent, at, "observed")
	fixture.parent = hash
	return hash
}

// addTrade indexes block number at the given time with one curve trade of ethGross.
func (fixture *incrementalFixture) addTrade(number int64, at time.Time, ethGross int64) {
	fixture.t.Helper()
	hash := fixture.addBlock(number, at)
	insertProjectionTrade(fixture.t, fixture.ctx, fixture.database.DB, fixture.chainID, number, hash, at, numberedHash(0x7c, number), 0, fixture.token, ethGross, 1_000_000, 1_000_000)
}

// deleteAbove mirrors reorg recovery's DeleteCanonicalAbove for the rows this fixture writes.
func (fixture *incrementalFixture) deleteAbove(block int64) {
	fixture.t.Helper()
	for _, statement := range []string{
		`DELETE FROM trades WHERE chain_id = $1 AND block_number > $2`,
		`DELETE FROM indexed_blocks WHERE chain_id = $1 AND block_number > $2`,
	} {
		if _, err := fixture.database.DB.ExecContext(fixture.ctx, statement, fixture.chainID, block); err != nil {
			fixture.t.Fatalf("delete above %d: %v", block, err)
		}
	}
}

// numberedHash is a 32-byte hash unique per (tag, number), so fixtures never collide across
// block numbers.
func numberedHash(tag byte, number int64) []byte {
	hash := hashBytes(tag)
	binary.BigEndian.PutUint64(hash[24:], uint64(number))
	return hash
}

func readDailyThroughBlock(t testing.TB, ctx context.Context, database *sql.DB, chainID int64) sql.NullInt64 {
	t.Helper()
	var through sql.NullInt64
	if err := database.QueryRowContext(ctx, `SELECT daily_through_block FROM protocol_stats WHERE chain_id = $1`, chainID).Scan(&through); err != nil {
		t.Fatalf("read daily watermark: %v", err)
	}
	return through
}

func setDailyVolume(t testing.TB, ctx context.Context, database *sql.DB, chainID int64, day time.Time, volume int64) {
	t.Helper()
	result, err := database.ExecContext(ctx, `UPDATE protocol_daily SET volume_eth_wad = $3 WHERE chain_id = $1 AND day = $2::date`, chainID, day.Format(time.DateOnly), volume)
	if err != nil {
		t.Fatalf("set daily volume: %v", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		t.Fatalf("set daily volume for %s updated %d rows; want 1", day.Format(time.DateOnly), rows)
	}
}

func readDailyVolume(t testing.TB, ctx context.Context, database *sql.DB, chainID int64, day time.Time) string {
	t.Helper()
	var volume string
	if err := database.QueryRowContext(ctx, `SELECT volume_eth_wad::text FROM protocol_daily WHERE chain_id = $1 AND day = $2::date`, chainID, day.Format(time.DateOnly)).Scan(&volume); err != nil {
		t.Fatalf("read daily volume for %s: %v", day.Format(time.DateOnly), err)
	}
	return volume
}

// A catching-up indexer commits blocks on past days; the incremental refresh must rebuild those
// days, not only today, and end equal to a full rebuild.
func TestRefreshProtocolAggregatesMatchesFullRebuildWhileCatchingUp(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	adapter := storepostgres.NewAdapter(pool)
	const chainID int64 = 48010
	now := time.Now().UTC().Truncate(time.Second)

	fixture := newIncrementalFixture(t, ctx, database, chainID, now.AddDate(0, 0, -5))
	fixture.addTrade(2, now.AddDate(0, 0, -5).Add(time.Minute), 10)
	if err := adapter.RefreshProtocolAggregates(ctx, chainID); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if through := readDailyThroughBlock(t, ctx, database.DB, chainID); !through.Valid || through.Int64 != 2 {
		t.Fatalf("watermark after first refresh = %+v; want 2", through)
	}

	fixture.addTrade(3, now.AddDate(0, 0, -3), 20)
	fixture.addTrade(4, now.AddDate(0, 0, -1), 30)
	fixture.addTrade(5, now.Add(-time.Hour), 40)
	if err := adapter.RefreshProtocolAggregates(ctx, chainID); err != nil {
		t.Fatalf("incremental refresh: %v", err)
	}
	incremental := snapshotProtocolAggregates(t, ctx, database.DB, chainID)
	if through := readDailyThroughBlock(t, ctx, database.DB, chainID); !through.Valid || through.Int64 != 5 {
		t.Fatalf("watermark after incremental refresh = %+v; want 5", through)
	}

	var volumeAllTime string
	var tradesAllTime, trades24h, launchesAllTime int64
	if err := database.DB.QueryRowContext(ctx, `
		SELECT volume_all_time_eth_wad::text, trades_all_time, trades_24h, launches_all_time
		FROM protocol_stats WHERE chain_id = $1
	`, chainID).Scan(&volumeAllTime, &tradesAllTime, &trades24h, &launchesAllTime); err != nil {
		t.Fatalf("read protocol summary: %v", err)
	}
	if volumeAllTime != "100" || tradesAllTime != 4 || trades24h != 1 || launchesAllTime != 1 {
		t.Fatalf("summary volume/trades/trades24h/launches = %s/%d/%d/%d; want 100/4/1/1", volumeAllTime, tradesAllTime, trades24h, launchesAllTime)
	}

	if err := adapter.RecomputeProtocolAggregates(ctx, chainID); err != nil {
		t.Fatalf("full rebuild: %v", err)
	}
	if full := snapshotProtocolAggregates(t, ctx, database.DB, chainID); full != incremental {
		t.Fatalf("incremental refresh differs from full rebuild:\nincremental: %#v\nfull:        %#v", incremental, full)
	}
}

// Days before the earliest new block are not rewritten, and the all-time columns are the sums of
// the daily rows. A refresh with no new block rewrites no daily row.
func TestRefreshProtocolAggregatesRewritesOnlyDaysOfNewBlocks(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	adapter := storepostgres.NewAdapter(pool)
	const chainID int64 = 48011
	now := time.Now().UTC().Truncate(time.Second)
	oldDay, today := now.AddDate(0, 0, -4), now.Add(-time.Hour)

	fixture := newIncrementalFixture(t, ctx, database, chainID, oldDay)
	fixture.addTrade(2, oldDay, 10)
	fixture.addTrade(3, today, 20)
	if err := adapter.RefreshProtocolAggregates(ctx, chainID); err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	// Sentinels: a rewritten day would lose them.
	setDailyVolume(t, ctx, database.DB, chainID, oldDay, 999)
	setDailyVolume(t, ctx, database.DB, chainID, today, 777)
	if err := adapter.RefreshProtocolAggregates(ctx, chainID); err != nil {
		t.Fatalf("refresh without new blocks: %v", err)
	}
	if got := readDailyVolume(t, ctx, database.DB, chainID, today); got != "777" {
		t.Fatalf("today's volume after a refresh without new blocks = %s; want untouched 777", got)
	}

	fixture.addTrade(4, today, 5)
	if err := adapter.RefreshProtocolAggregates(ctx, chainID); err != nil {
		t.Fatalf("refresh with a new block today: %v", err)
	}
	if got := readDailyVolume(t, ctx, database.DB, chainID, oldDay); got != "999" {
		t.Fatalf("old day's volume = %s; want untouched 999", got)
	}
	if got := readDailyVolume(t, ctx, database.DB, chainID, today); got != "25" {
		t.Fatalf("today's volume = %s; want rebuilt 25", got)
	}
	var volumeAllTime string
	if err := database.DB.QueryRowContext(ctx, `SELECT volume_all_time_eth_wad::text FROM protocol_stats WHERE chain_id = $1`, chainID).Scan(&volumeAllTime); err != nil {
		t.Fatalf("read all-time volume: %v", err)
	}
	if want := "1024"; volumeAllTime != want {
		t.Fatalf("all-time volume = %s; want %s, the sum of the daily rows", volumeAllTime, want)
	}

	if err := adapter.RecomputeProtocolAggregates(ctx, chainID); err != nil {
		t.Fatalf("full rebuild: %v", err)
	}
	if got := readDailyVolume(t, ctx, database.DB, chainID, oldDay); got != "10" {
		t.Fatalf("old day's volume after full rebuild = %s; want 10", got)
	}
}

// After reorg recovery deletes canonical rows, the full rebuild lowers the watermark, and an
// incremental refresh that finds the watermark above the head falls back to a full rebuild.
func TestProtocolAggregatesFollowReorgDeletes(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	adapter := storepostgres.NewAdapter(pool)
	now := time.Now().UTC().Truncate(time.Second)

	for _, recovery := range []struct {
		name    string
		chainID int64
		rebuild func(context.Context, int64) error
	}{
		{"full rebuild", 48012, adapter.RecomputeProtocolAggregates},
		{"refresh above head", 48013, adapter.RefreshProtocolAggregates},
	} {
		t.Run(recovery.name, func(t *testing.T) {
			fixture := newIncrementalFixture(t, ctx, database, recovery.chainID, now.AddDate(0, 0, -3))
			fixture.addTrade(2, now.AddDate(0, 0, -3), 10)
			fixture.addTrade(3, now.AddDate(0, 0, -2), 20)
			fixture.addTrade(4, now.Add(-time.Hour), 30)
			if err := adapter.RefreshProtocolAggregates(ctx, recovery.chainID); err != nil {
				t.Fatalf("refresh before reorg: %v", err)
			}

			fixture.parent = numberedHash(0xb1, 2)
			fixture.deleteAbove(2)
			if err := recovery.rebuild(ctx, recovery.chainID); err != nil {
				t.Fatalf("rebuild after reorg delete: %v", err)
			}
			if through := readDailyThroughBlock(t, ctx, database.DB, recovery.chainID); !through.Valid || through.Int64 != 2 {
				t.Fatalf("watermark after reorg = %+v; want 2", through)
			}
			var days int64
			var volumeAllTime string
			if err := database.DB.QueryRowContext(ctx, `
				SELECT (SELECT count(*) FROM protocol_daily WHERE chain_id = $1), volume_all_time_eth_wad::text
				FROM protocol_stats WHERE chain_id = $1
			`, recovery.chainID).Scan(&days, &volumeAllTime); err != nil {
				t.Fatalf("read aggregates after reorg: %v", err)
			}
			if days != 1 || volumeAllTime != "10" {
				t.Fatalf("daily rows/all-time volume after reorg = %d/%s; want 1/10", days, volumeAllTime)
			}

			// The replacement fork lands on an earlier day than the removed tip.
			fixture.addTrade(3, now.AddDate(0, 0, -2).Add(time.Hour), 7)
			if err := adapter.RefreshProtocolAggregates(ctx, recovery.chainID); err != nil {
				t.Fatalf("refresh after replacement block: %v", err)
			}
			incremental := snapshotProtocolAggregates(t, ctx, database.DB, recovery.chainID)
			if err := adapter.RecomputeProtocolAggregates(ctx, recovery.chainID); err != nil {
				t.Fatalf("full rebuild after replacement block: %v", err)
			}
			if full := snapshotProtocolAggregates(t, ctx, database.DB, recovery.chainID); full != incremental {
				t.Fatalf("refresh after reorg differs from full rebuild:\nincremental: %#v\nfull:        %#v", incremental, full)
			}
		})
	}
}

// A refresh waits for a concurrent holder of the chain's protocol aggregate lock.
func TestRefreshProtocolAggregatesWaitsForConcurrentRefresh(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	const chainID int64 = 48014
	newIncrementalFixture(t, ctx, database, chainID, time.Now().UTC().Add(-time.Hour))

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('protocol_aggregates:' || $1::bigint::text, 0))`, chainID); err != nil {
		t.Fatalf("take protocol aggregate lock: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- storepostgres.NewAdapter(pool).RefreshProtocolAggregates(ctx, chainID) }()
	select {
	case err := <-done:
		t.Fatalf("refresh finished while another transaction held the lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("refresh after lock release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("refresh did not finish after the lock was released")
	}
}

// The stats worker's batch path rewrites the days of new blocks and ends equal to a full rebuild.
func TestAggregationSourceBatchRefreshMatchesFullRebuild(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	adapter := storepostgres.NewAdapter(pool)
	source := storepostgres.AggregationSource{Adapter: adapter}
	const chainID int64 = 48016
	now := time.Now().UTC().Truncate(time.Second)

	fixture := newIncrementalFixture(t, ctx, database, chainID, now.AddDate(0, 0, -6))
	fixture.addTrade(2, now.AddDate(0, 0, -6), 10)
	claims := []stats.Claim{{ChainID: chainID, Token: [20]byte(fixture.token), Generation: 1}}
	for i, err := range source.ComputeBatch(ctx, claims) {
		if err != nil {
			t.Fatalf("first batch claim %d: %v", i, err)
		}
	}

	fixture.addTrade(3, now.AddDate(0, 0, -2), 20)
	fixture.addTrade(4, now.Add(-time.Hour), 30)
	for i, err := range source.ComputeBatch(ctx, claims) {
		if err != nil {
			t.Fatalf("second batch claim %d: %v", i, err)
		}
	}
	batched := snapshotProtocolAggregates(t, ctx, database.DB, chainID)
	if err := adapter.RecomputeProtocolAggregates(ctx, chainID); err != nil {
		t.Fatalf("full rebuild: %v", err)
	}
	if full := snapshotProtocolAggregates(t, ctx, database.DB, chainID); full != batched {
		t.Fatalf("batched refresh differs from full rebuild:\nbatched: %#v\nfull:    %#v", batched, full)
	}
}
