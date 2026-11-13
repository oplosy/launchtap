//go:build integration

package postgrestest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

// TestRecomputeTokensStatsMatchesPerTokenRecompute requires one batch statement over several
// tokens of a chain to write the same token_stats rows as recomputing each token alone, both
// from scratch and over existing rows (pre-flight 2026-11-13-batch-token-stats-recompute).
func TestRecomputeTokensStatsMatchesPerTokenRecompute(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	const chainID int64 = 48021
	tokens := insertStatsBatchFixture(t, ctx, database, chainID)
	adapter := storepostgres.NewAdapter(pool)

	perToken := func() {
		t.Helper()
		for _, token := range tokens {
			if err := adapter.RecomputeTokenStats(ctx, chainID, token); err != nil {
				t.Fatalf("recompute %s: %v", token.Hex(), err)
			}
		}
	}
	batch := func() {
		t.Helper()
		// An unknown token and a duplicate must not change or break the batch.
		request := append([]common.Address{common.Address(addressBytes(0xee)), tokens[0]}, tokens...)
		if err := adapter.RecomputeTokensStats(ctx, chainID, request); err != nil {
			t.Fatalf("recompute batch: %v", err)
		}
	}

	perToken()
	wantFresh := readTokenStatsRows(t, ctx, database, chainID)
	perToken()
	wantAgain := readTokenStatsRows(t, ctx, database, chainID)
	clearTokenStats(t, ctx, database, chainID)

	batch()
	if got := readTokenStatsRows(t, ctx, database, chainID); got != wantFresh {
		t.Fatalf("fresh batch differs from per-token recompute\nbatch=%s\nper-token=%s", got, wantFresh)
	}
	batch()
	if got := readTokenStatsRows(t, ctx, database, chainID); got != wantAgain {
		t.Fatalf("repeated batch differs from per-token recompute\nbatch=%s\nper-token=%s", got, wantAgain)
	}
	if strings.Count(wantFresh, "\n") != len(tokens) {
		t.Fatalf("fixture precondition: %d stats rows, want %d\n%s", strings.Count(wantFresh, "\n"), len(tokens), wantFresh)
	}
}

// insertStatsBatchFixture launches four tokens on one chain with different shapes: full history,
// no trades, recent trades only, and a curve that holds most of the supply.
func insertStatsBatchFixture(t *testing.T, ctx context.Context, database *Database, chainID int64) []common.Address {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Minute)
	launchedAt := now.Add(-30 * time.Hour)
	blockHash := hashBytes(0x61)
	mustInsertBlock(t, ctx, database.DB, chainID, 1, blockHash, hashBytes(0x60), launchedAt, "observed")
	type candle struct {
		start               time.Time
		high, close, volume string
	}
	shapes := []struct {
		reserve  bool
		holders  []int64
		candles  []candle
		curveCut int64
	}{
		{reserve: true, holders: []int64{10, 20, 30}, candles: []candle{
			{now.Add(-26 * time.Hour), "3000000000000000", "1000000000000000", "5"},
			{now.Add(-25 * time.Hour), "1500000000000000", "1200000000000000", "6"},
			{now.Add(-2 * time.Hour), "2000000000000000", "1800000000000000", "7"},
		}},
		{},
		{reserve: true, holders: []int64{5}, candles: []candle{
			{now.Add(-3 * time.Hour), "40000000000000", "30000000000000", "2"},
			{now.Add(-time.Hour), "50000000000000", "45000000000000", "9"},
		}},
		{reserve: true, holders: []int64{1, 1}, curveCut: 900000, candles: []candle{
			{now.Add(-28 * time.Hour), "90000000000000", "20000000000000", "1"},
		}},
	}
	tokens := make([]common.Address, 0, len(shapes))
	for i, shape := range shapes {
		base := byte(0x70 + 0x10*i)
		token, curve, pair, weth := addressBytes(base), addressBytes(base+1), addressBytes(base+2), addressBytes(base+3)
		insertProjectionLaunch(t, ctx, database.DB, chainID, 1, blockHash, launchedAt, hashBytes(base+4), projectionLaunchFixture{token: token, curve: curve, pair: pair, weth: weth})
		callRebuild(t, ctx, database.DB, chainID, token)
		if shape.reserve {
			if _, err := database.DB.ExecContext(ctx, `
				INSERT INTO token_reserves (
					chain_id, token_address, reserve_source, eth_reserve, token_reserve,
					source_block_number, source_block_hash, source_block_time, source_tx_hash, source_log_index
				) VALUES ($1, $2, 'curve', $3, 100, 1, $4, $5, $6, 1)
			`, chainID, token, 200+i, blockHash, launchedAt, hashBytes(base+5)); err != nil {
				t.Fatalf("insert reserve: %v", err)
			}
		}
		holders := map[string]int64{}
		for n, balance := range shape.holders {
			holders[string(addressBytes(base+6+byte(n)))] = balance
		}
		if shape.curveCut > 0 {
			holders[string(curve)] = shape.curveCut
		}
		for holder, balance := range holders {
			if _, err := database.DB.ExecContext(ctx, `
				INSERT INTO holder_balances (chain_id, token_address, holder_address, balance, first_acquired_block_number)
				VALUES ($1, $2, $3, $4, 1)
			`, chainID, token, []byte(holder), balance); err != nil {
				t.Fatalf("insert holder: %v", err)
			}
		}
		for _, c := range shape.candles {
			if _, err := database.DB.ExecContext(ctx, `
				INSERT INTO candles (
					chain_id, token_address, interval, bucket_start_time,
					open_price_wad, high_price_wad, low_price_wad, close_price_wad, gross_eth_volume
				) VALUES ($1, $2, '1m', $3, $4, $4, $4, $5, $6)
			`, chainID, token, c.start, c.high, c.close, c.volume); err != nil {
				t.Fatalf("insert candle: %v", err)
			}
		}
		tokens = append(tokens, common.Address(token))
	}
	return tokens
}

func readTokenStatsRows(t testing.TB, ctx context.Context, database *Database, chainID int64) string {
	t.Helper()
	rows, err := database.DB.QueryContext(ctx, `
		SELECT encode(token_address, 'hex'), spot_price_eth_wad::TEXT, market_cap_eth_wad::TEXT, fdv_eth_wad::TEXT,
		       COALESCE(liquidity_eth_wad::TEXT, 'null'), ath_price_eth_wad::TEXT, ath_at, volume_24h_eth_wad::TEXT,
		       price_change_24h_bps, holder_count
		FROM token_stats WHERE chain_id = $1 ORDER BY token_address`, chainID)
	if err != nil {
		t.Fatalf("read token stats: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out strings.Builder
	for rows.Next() {
		var token, spot, marketCap, fdv, liquidity, ath, volume string
		var athAt time.Time
		var change int64
		var holders int
		if err := rows.Scan(&token, &spot, &marketCap, &fdv, &liquidity, &ath, &athAt, &volume, &change, &holders); err != nil {
			t.Fatalf("scan token stats: %v", err)
		}
		fmt.Fprintf(&out, "%s spot=%s cap=%s fdv=%s liq=%s ath=%s@%s vol=%s change=%d holders=%d\n",
			token, spot, marketCap, fdv, liquidity, ath, athAt.UTC().Format(time.RFC3339), volume, change, holders)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate token stats: %v", err)
	}
	return out.String()
}

func clearTokenStats(t testing.TB, ctx context.Context, database *Database, chainID int64) {
	t.Helper()
	if _, err := database.DB.ExecContext(ctx, `DELETE FROM token_stats WHERE chain_id = $1`, chainID); err != nil {
		t.Fatalf("clear token stats: %v", err)
	}
}
