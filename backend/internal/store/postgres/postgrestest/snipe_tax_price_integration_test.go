//go:build integration

package postgrestest

import (
	"context"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/migrations"
)

// MED-11: an engine-2 taxed buy prices the market at the ETH that reached the curve, not at the
// gross the buyer paid; migration 00038 rebuilds the candles of tokens already holding taxed
// trades and drops their ratcheted all-time high.
func TestTaxedCurveBuyPricesWithoutSnipeTax(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back the indexer heartbeat: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back the service roles: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back the audit follow-ups: %v", err)
	}
	const chainID int64 = 52077
	base := time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC)
	mustInsertBlock(t, ctx, database.DB, chainID, 1, hashBytes(0x01), hashBytes(0x00), base, "safe")
	at := func(tx byte) eventCoordinates {
		return eventCoordinates{chainID, 1, hashBytes(0x01), base, 0, hashBytes(tx), 0}
	}
	token := addressBytes(0x51)
	if err := insertLedgerRow(ctx, database.DB, "token_launches", at(0x51), v2Launch(token)...); err != nil {
		t.Fatal(err)
	}
	// 100 wei paid for 1,000 token units, 90 of it anti-snipe tax: 10 wei reached the curve.
	if err := insertLedgerRow(ctx, database.DB, "trades", at(0x52),
		column{"token_address", token}, column{"trader", addressBytes(0xc1)}, column{"is_buy", true},
		column{"eth_gross", 100}, column{"eth_refund", 0}, column{"token_amount", 1_000}, column{"protocol_fee", 0},
		column{"creator_fee", 0}, column{"new_eth_reserve", 10}, column{"new_token_reserve", 999_000}, column{"snipe_tax", 90}); err != nil {
		t.Fatal(err)
	}
	callRebuild(t, ctx, database.DB, chainID, token)
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO token_stats (chain_id, token_address, spot_price_eth_wad, market_cap_eth_wad, fdv_eth_wad, ath_price_eth_wad, ath_at, volume_24h_eth_wad, price_change_24h_bps, holder_count, updated_at)
		VALUES ($1, $2, 0, 0, 0, 100000000000000000, $3, 0, 0, 0, $3)`, chainID, token, base); err != nil {
		t.Fatalf("seed the inflated stats row: %v", err)
	}
	const taxedHigh, curveHigh = "100000000000000000", "10000000000000000" // 100 or 10 wei per 1,000 units, in wad
	if got := candleHigh(t, ctx, database, chainID, token); got != taxedHigh {
		t.Fatalf("candle high before the migration = %s, want the taxed %s", got, taxedHigh)
	}

	if _, err := migrations.Run(ctx, database.DB, migrations.CommandUp); err != nil {
		t.Fatalf("apply the audit follow-ups: %v", err)
	}
	if got := candleHigh(t, ctx, database, chainID, token); got != curveHigh {
		t.Fatalf("candle high after the migration = %s, want %s", got, curveHigh)
	}
	var price string
	if err := database.DB.QueryRowContext(ctx, `SELECT execution_price_wad::text FROM market_trades WHERE chain_id = $1 AND token_address = $2`, chainID, token).Scan(&price); err != nil || price != curveHigh {
		t.Fatalf("market_trades price = %q, %v; want %s", price, err, curveHigh)
	}
	var stats, dirty int
	if err := database.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM token_stats WHERE chain_id = $1 AND token_address = $2), (SELECT count(*) FROM aggregation_dirty WHERE chain_id = $1 AND token_address = $2)`, chainID, token).Scan(&stats, &dirty); err != nil {
		t.Fatal(err)
	}
	if stats != 0 || dirty != 1 {
		t.Fatalf("stats rows=%d dirty=%d; want the inflated stats dropped and the token queued", stats, dirty)
	}
}

func candleHigh(t *testing.T, ctx context.Context, database *Database, chainID int64, token []byte) string {
	t.Helper()
	var high string
	if err := database.DB.QueryRowContext(ctx, `SELECT high_price_wad::text FROM candles WHERE chain_id = $1 AND token_address = $2 AND interval = '1m'`, chainID, token).Scan(&high); err != nil {
		t.Fatalf("read 1m candle: %v", err)
	}
	return high
}
