//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

func TestTokenSortMetricsMirrorTokenStats(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	const chainID int64 = 48021
	at := time.Now().UTC().Truncate(time.Second)
	blockHash := hashBytes(0x91)
	tokenBytes := addressBytes(0x92)
	mustInsertBlock(t, ctx, database.DB, chainID, 1, blockHash, hashBytes(0x90), at, "observed")
	insertProjectionLaunch(t, ctx, database.DB, chainID, 1, blockHash, at, hashBytes(0x93), projectionLaunchFixture{
		token: tokenBytes, curve: addressBytes(0x94), pair: addressBytes(0x95), weth: addressBytes(0x96),
	})
	callRebuild(t, ctx, database.DB, chainID, tokenBytes)

	sortMetrics := func(t *testing.T) (string, string) {
		t.Helper()
		var marketCap, volume string
		if err := database.DB.QueryRowContext(ctx, `SELECT sort_market_cap_eth_wad::text, sort_volume_24h_eth_wad::text FROM tokens WHERE chain_id=$1 AND token_address=$2`, chainID, tokenBytes).Scan(&marketCap, &volume); err != nil {
			t.Fatal(err)
		}
		return marketCap, volume
	}
	if marketCap, volume := sortMetrics(t); marketCap != "0" || volume != "0" {
		t.Fatalf("token without stats sorts as %s/%s, want 0/0", marketCap, volume)
	}
	if err := storepostgres.NewAdapter(pool).RecomputeTokenStats(ctx, chainID, common.Address(tokenBytes)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.ExecContext(ctx, `UPDATE token_stats SET market_cap_eth_wad=123, volume_24h_eth_wad=45 WHERE chain_id=$1 AND token_address=$2`, chainID, tokenBytes); err != nil {
		t.Fatal(err)
	}
	if marketCap, volume := sortMetrics(t); marketCap != "123" || volume != "45" {
		t.Fatalf("sort metrics did not follow stats update: %s/%s", marketCap, volume)
	}

	// A token row re-inserted while its stats row survives (deferred FK) loads the stats.
	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range []string{`SET CONSTRAINTS ALL DEFERRED`, `DELETE FROM tokens WHERE chain_id=$1 AND token_address=$2`, `SELECT rebuild_token_projections($1, $2)`} {
		args := []any{chainID, tokenBytes}
		if !strings.Contains(statement, "$1") {
			args = nil
		}
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	var reloaded string
	if err := tx.QueryRowContext(ctx, `SELECT sort_market_cap_eth_wad::text FROM tokens WHERE chain_id=$1 AND token_address=$2`, chainID, tokenBytes).Scan(&reloaded); err != nil || reloaded != "123" {
		t.Fatalf("re-inserted token sort metric=%q err=%v, want 123", reloaded, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if _, err := database.DB.ExecContext(ctx, `DELETE FROM token_stats WHERE chain_id=$1 AND token_address=$2`, chainID, tokenBytes); err != nil {
		t.Fatal(err)
	}
	if marketCap, volume := sortMetrics(t); marketCap != "0" || volume != "0" {
		t.Fatalf("deleted stats left sort metrics %s/%s", marketCap, volume)
	}
}

func TestMetricSortedTokenListCanUsePhaseIndex(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	connection, err := database.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := connection.ExecContext(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	for sort, index := range map[string]string{"sort_market_cap_eth_wad": "tokens_phase_market_cap_cursor_idx", "sort_volume_24h_eth_wad": "tokens_phase_volume_cursor_idx"} {
		plan := explain(t, ctx, connection, `SELECT token_address FROM tokens WHERE chain_id = 1 AND phase = 'curve' ORDER BY `+sort+` DESC, token_address DESC LIMIT 20`)
		if !strings.Contains(plan, index) || strings.Contains(plan, "Sort  (") {
			t.Fatalf("%s plan does not walk %s without sorting:\n%s", sort, index, plan)
		}
	}
}

func explain(t *testing.T, ctx context.Context, connection *sql.Conn, query string) string {
	t.Helper()
	rows, err := connection.QueryContext(ctx, `EXPLAIN `+query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	return plan.String()
}
