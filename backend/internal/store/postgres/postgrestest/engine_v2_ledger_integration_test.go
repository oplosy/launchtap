//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/migrations"
	"github.com/ethereum/go-ethereum/common"
)

// column is one (name, value) pair of a ledger row; a slice keeps the column order stable.
type column struct {
	name  string
	value any
}

func insertLedgerRow(ctx context.Context, execer sqlExecer, table string, coordinates eventCoordinates, payload ...column) error {
	columns := []column{
		{"chain_id", coordinates.chainID}, {"block_number", coordinates.blockNumber},
		{"block_hash", coordinates.blockHash}, {"block_time", coordinates.blockTime},
		{"transaction_index", coordinates.transactionIndex}, {"tx_hash", coordinates.txHash},
		{"log_index", coordinates.logIndex},
	}
	columns = append(columns, payload...)
	names := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	values := make([]any, len(columns))
	for index, entry := range columns {
		names[index] = entry.name
		placeholders[index] = fmt.Sprintf("$%d", index+1)
		values[index] = entry.value
	}
	_, err := execer.ExecContext(ctx, "INSERT INTO "+table+" ("+strings.Join(names, ", ")+") VALUES ("+strings.Join(placeholders, ", ")+")", values...)
	return err
}

// launchPayload is a v1 launch's payload; v2Launch replaces the pair and adds the v2 profile.
func launchPayload(token []byte, engine int, pair, weth []byte) []column {
	return []column{
		{"token_address", token}, {"curve_address", addressBytes(0xb1)}, {"creator", addressBytes(0xb2)},
		{"lp_pair", pair}, {"weth", weth}, {"protocol_treasury", addressBytes(0xb5)},
		{"engine_version", engine}, {"name", "Token"}, {"symbol", "TKN"}, {"total_supply", 1_000_000},
		{"virtual_eth", 10}, {"virtual_token", 1_000_000}, {"curve_tokens", 800_000}, {"lp_tokens", 200_000},
		{"graduation_eth", 100}, {"launch_fee_paid", 1}, {"trade_fee_bps", 100}, {"protocol_share_bps", 5000},
	}
}

func v2Profile() []column {
	return []column{
		{"hook", addressBytes(0xe1)}, {"locker", addressBytes(0xe2)}, {"anti_snipe_start_bps", 9000},
		{"anti_snipe_duration_seconds", int64(300)}, {"lp_creator_share_bps", 5000},
	}
}

func v2Launch(token []byte) []column {
	return append(launchPayload(token, 2, nil, nil), v2Profile()...)
}

func v2Graduation(token, poolID []byte) []column {
	return []column{
		{"token_address", token}, {"eth_to_pool", 100}, {"tokens_to_pool", 200_000},
		{"pool_id", poolID}, {"position_token_id", 7}, {"liquidity", 1_000}, {"sqrt_price_x96", 79_228_162},
		{"eth_remainder", 3}, {"token_remainder", 0},
	}
}

func v4Swap(poolID []byte) []column {
	return []column{
		{"pool_id", poolID}, {"sender", addressBytes(0xf1)}, {"amount0_delta", -500}, {"amount1_delta", 4_000},
		{"sqrt_price_x96", 79_228_000}, {"liquidity", 1_000}, {"tick", -887_000}, {"fee", 10_000},
		{"tx_from", addressBytes(0xf2)},
	}
}

func TestEngineV2LedgerConstraints(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	const chainID int64 = 52001
	base := time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	mustInsertBlock(t, ctx, database.DB, chainID, 1, hashBytes(0x01), hashBytes(0x00), base, "observed")
	at := func(tx byte, log int) eventCoordinates {
		return eventCoordinates{chainID, 1, hashBytes(0x01), base, 0, hashBytes(tx), log}
	}

	if err := insertLedgerRow(ctx, database.DB, "token_launches", at(0x21, 0), v2Launch(addressBytes(0x21))...); err != nil {
		t.Fatalf("insert v2 launch without a pair: %v", err)
	}

	err := insertLedgerRow(ctx, database.DB, "token_launches", at(0x22, 0), launchPayload(addressBytes(0x22), 1, nil, addressBytes(0xb4))...)
	assertPostgresConstraint(t, err, "23514", "token_launches_engine_pair")
	err = insertLedgerRow(ctx, database.DB, "token_launches", at(0x23, 0), append(launchPayload(addressBytes(0x23), 2, addressBytes(0xb3), addressBytes(0xb4)), v2Profile()...)...)
	assertPostgresConstraint(t, err, "23514", "token_launches_engine_pair")
	err = insertLedgerRow(ctx, database.DB, "token_launches", at(0x24, 0), launchPayload(addressBytes(0x24), 2, nil, nil)...)
	assertPostgresConstraint(t, err, "23514", "token_launches_engine_v2_fields")
	err = insertLedgerRow(ctx, database.DB, "token_launches", at(0x25, 0), append(launchPayload(addressBytes(0x25), 1, addressBytes(0xb3), addressBytes(0xb4)), v2Profile()...)...)
	assertPostgresConstraint(t, err, "23514", "token_launches_engine_v2_fields")

	err = insertLedgerRow(ctx, database.DB, "graduations", at(0x26, 0), append(v2Graduation(addressBytes(0x21), hashBytes(0x26)), column{"lp_pair", addressBytes(0xb3)})...)
	assertPostgresConstraint(t, err, "23514", "graduations_engine_shape")

	err = insertLedgerRow(ctx, database.DB, "trades", at(0x27, 0),
		column{"token_address", addressBytes(0x21)}, column{"trader", addressBytes(0xc1)}, column{"is_buy", false},
		column{"eth_gross", 100}, column{"eth_refund", 0}, column{"token_amount", 1_000}, column{"protocol_fee", 1},
		column{"creator_fee", 1}, column{"new_eth_reserve", 10}, column{"new_token_reserve", 999_000}, column{"snipe_tax", 5})
	assertPostgresConstraint(t, err, "23514", "trades_snipe_tax_buy_only")

	err = insertLedgerRow(ctx, database.DB, "v4_swaps", at(0x28, 0), append(v4Swap(hashBytes(0x28))[:6], column{"tick", 8_388_608}, column{"fee", 10_000})...)
	assertPostgresConstraint(t, err, "23514", "v4_swaps_tick_int24")

	// A graduation's shape must match its launch's engine; the deferred trigger checks at commit.
	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertLedgerRow(ctx, tx, "token_launches", at(0x29, 0), launchPayload(addressBytes(0x29), 1, addressBytes(0xb3), addressBytes(0xb4))...); err != nil {
		rollbackAfterFailure(t, tx)
		t.Fatalf("insert v1 launch: %v", err)
	}
	if err := insertLedgerRow(ctx, tx, "graduations", at(0x29, 1), v2Graduation(addressBytes(0x29), hashBytes(0x29))...); err != nil {
		rollbackAfterFailure(t, tx)
		t.Fatalf("insert v2-shaped graduation: %v", err)
	}
	assertTriggerViolation(t, tx.Commit(), "does not match its launch engine")

	// Native ETH is currency0 in every v2 pool, so a v2 token is never token0.
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO tokens (
			chain_id, token_address, curve_address, creator, protocol_treasury, engine_version, name, symbol,
			total_supply, initial_virtual_eth, initial_virtual_token, curve_tokens, lp_tokens, graduation_eth,
			trade_fee_bps, protocol_share_bps, launch_block_number, launch_block_hash, launch_block_time,
			launch_tx_hash, launch_log_index, hook, locker, anti_snipe_start_bps, anti_snipe_duration_seconds,
			lp_creator_share_bps
		) VALUES ($1, $2, $3, $3, $3, 2, 'Token', 'TKN', 1000000, 10, 1000000, 800000, 200000, 100, 100, 5000,
			1, $4, $5, $6, 0, $3, $3, 9000, 300, 5000)
	`, chainID, addressBytes(0x21), addressBytes(0xb1), hashBytes(0x01), base, hashBytes(0x21)); err != nil {
		t.Fatalf("insert v2 token projection: %v", err)
	}
	var tokenIsToken0 bool
	if err := database.DB.QueryRowContext(ctx, `SELECT token_is_token0 FROM tokens WHERE chain_id=$1 AND token_address=$2`, chainID, addressBytes(0x21)).Scan(&tokenIsToken0); err != nil || tokenIsToken0 {
		t.Fatalf("v2 token_is_token0 = %v, %v; want false", tokenIsToken0, err)
	}
}

var engineV2LedgerTables = []string{
	"snipe_tax_claims", "fee_recipient_proposals", "fee_recipient_acceptances", "lp_fee_collections",
	"lp_fee_claims", "v4_pool_initializations", "v4_liquidity_changes", "v4_swaps",
}

func TestReorgDeletesEngineV2LedgerAboveAncestor(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	const chainID int64 = 52002
	base := time.Date(2026, time.November, 11, 13, 0, 0, 0, time.UTC)
	for number := int64(1); number <= 2; number++ {
		mustInsertBlock(t, ctx, database.DB, chainID, number, hashBytes(byte(number)), hashBytes(byte(number-1)), base.Add(time.Duration(number)*time.Minute), "observed")
	}
	at := func(block int64, tx byte, log int) eventCoordinates {
		return eventCoordinates{chainID, block, hashBytes(byte(block)), base.Add(time.Duration(block) * time.Minute), int(tx), hashBytes(tx), log}
	}
	tokenA, tokenB, tokenC := addressBytes(0x31), addressBytes(0x32), addressBytes(0x33)
	poolA, poolC := hashBytes(0x3a), hashBytes(0x3c)

	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	inserts := []struct {
		table       string
		coordinates eventCoordinates
		payload     []column
	}{
		{"token_launches", at(1, 0x01, 0), v2Launch(tokenA)},
		{"token_launches", at(1, 0x02, 0), v2Launch(tokenB)},
		{"token_launches", at(1, 0x03, 0), v2Launch(tokenC)},
		// Token C graduates at the ancestor and only trades on its pool above it.
		{"graduations", at(1, 0x04, 0), v2Graduation(tokenC, poolC)},
		{"trades", at(2, 0x10, 0), []column{{"token_address", tokenA}, {"trader", addressBytes(0xc1)}, {"is_buy", true}, {"eth_gross", 100}, {"eth_refund", 0}, {"token_amount", 1_000}, {"protocol_fee", 1}, {"creator_fee", 1}, {"new_eth_reserve", 90}, {"new_token_reserve", 999_000}, {"snipe_tax", 8}}},
		{"v4_pool_initializations", at(2, 0x11, 0), []column{{"pool_id", poolA}, {"currency0", addressBytes(0x00)}, {"currency1", tokenA}, {"fee", 10_000}, {"tick_spacing", 200}, {"hooks", addressBytes(0xe1)}, {"sqrt_price_x96", 79_228_162}, {"tick", -887_200}}},
		{"v4_liquidity_changes", at(2, 0x11, 1), []column{{"pool_id", poolA}, {"sender", addressBytes(0xe3)}, {"tick_lower", -887_200}, {"tick_upper", 887_200}, {"liquidity_delta", 1_000}, {"salt", hashBytes(0x00)}}},
		{"graduations", at(2, 0x11, 2), v2Graduation(tokenA, poolA)},
		{"v4_swaps", at(2, 0x12, 0), v4Swap(poolA)},
		{"v4_swaps", at(2, 0x13, 0), v4Swap(poolC)},
		{"snipe_tax_claims", at(2, 0x14, 0), []column{{"token_address", tokenA}, {"treasury", addressBytes(0xb5)}, {"amount", 8}}},
		{"fee_recipient_proposals", at(2, 0x15, 0), []column{{"token_address", tokenA}, {"current_recipient", addressBytes(0xb2)}, {"next_recipient", addressBytes(0xd2)}}},
		{"fee_recipient_acceptances", at(2, 0x16, 0), []column{{"token_address", tokenA}, {"previous_recipient", addressBytes(0xb2)}, {"next_recipient", addressBytes(0xd2)}}},
		{"lp_fee_collections", at(2, 0x17, 0), []column{{"token_address", tokenB}, {"eth_amount", 5}, {"token_amount", 50}}},
		{"lp_fee_claims", at(2, 0x18, 0), []column{{"token_address", tokenA}, {"recipient", addressBytes(0xd2)}, {"eth_amount", 2}, {"token_amount", 20}}},
	}
	for _, insert := range inserts {
		if err := insertLedgerRow(ctx, tx, insert.table, insert.coordinates, insert.payload...); err != nil {
			rollbackAfterFailure(t, tx)
			t.Fatalf("insert %s: %v", insert.table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit v2 ledger: %v", err)
	}

	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, tx *storepostgres.Adapter) error {
		affected, err := tx.AffectedTokensAbove(ctx, chainID, 1)
		if err != nil {
			return err
		}
		want := []common.Address{common.Address(tokenA), common.Address(tokenB), common.Address(tokenC)}
		if fmt.Sprint(affected) != fmt.Sprint(want) {
			t.Errorf("affected tokens = %v, want %v", affected, want)
		}
		return tx.DeleteCanonicalAbove(ctx, chainID, 1)
	}); err != nil {
		t.Fatalf("delete canonical v2 ledger above ancestor: %v", err)
	}

	for _, table := range append([]string{"trades"}, engineV2LedgerTables...) {
		assertRowCount(t, ctx, database.DB, table, chainID, 0)
	}
	assertRowCount(t, ctx, database.DB, "graduations", chainID, 1)
	assertRowCount(t, ctx, database.DB, "token_launches", chainID, 3)
}

func TestEngineV2LedgerDownRefusesV2Rows(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	const chainID int64 = 52003
	base := time.Date(2026, time.November, 11, 14, 0, 0, 0, time.UTC)
	mustInsertBlock(t, ctx, database.DB, chainID, 1, hashBytes(0x01), hashBytes(0x00), base, "observed")
	if err := insertLedgerRow(ctx, database.DB, "token_launches", eventCoordinates{chainID, 1, hashBytes(0x01), base, 0, hashBytes(0x41), 0}, v2Launch(addressBytes(0x41))...); err != nil {
		t.Fatal(err)
	}
	// The V4 swap sender index rolls back first; the v2 projections migration holds no projection
	// rows here, so it rolls back next.
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back the leaderboard windows: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back the tokens locker index: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back the incremental protocol aggregates: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back the wallet funding: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back the Telegram links: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back the comment votes: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back the V4 swap sender index: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back engine v2 projections: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err == nil || !strings.Contains(err.Error(), "engine v2 ledger rows exist") {
		t.Fatalf("down with a v2 launch error = %v, want refusal", err)
	}
	if _, err := database.DB.ExecContext(ctx, `DELETE FROM token_launches WHERE chain_id=$1`, chainID); err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("down without v2 rows: %v", err)
	}
	for _, table := range engineV2LedgerTables {
		assertTableExists(t, ctx, database.DB, table, false)
	}
	var nullable string
	if err := database.DB.QueryRowContext(ctx, `SELECT is_nullable FROM information_schema.columns WHERE table_name='token_launches' AND column_name='lp_pair'`).Scan(&nullable); err != nil || nullable != "NO" {
		t.Fatalf("token_launches.lp_pair nullable = %q, %v; want NO", nullable, err)
	}
}

func assertRowCount(t testing.TB, ctx context.Context, database *sql.DB, table string, chainID int64, want int) {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE chain_id=$1", chainID).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s rows = %d, want %d", table, count, want)
	}
}
