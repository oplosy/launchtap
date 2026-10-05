//go:build integration

package postgrestest

import (
	"bytes"
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestChainProjectionsSchema(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	for _, table := range []string{
		"tokens", "token_reserves", "holder_balances", "aggregation_dirty", "token_metadata", "token_images",
		"candles", "token_stats", "protocol_daily", "protocol_stats",
	} {
		assertTableExists(t, ctx, database.DB, table, true)
	}
	assertProjectionColumns(t, ctx, database.DB)

	for table, constraints := range map[string][]string{
		"tokens": {
			"tokens_pkey", "tokens_token_address_length", "tokens_total_supply_nonnegative",
			"tokens_graduation_coordinates_with_phase", "tokens_token_launch_fk",
			"tokens_graduation_block_fk", "tokens_graduation_event_fk",
		},
		"token_reserves": {
			"token_reserves_pkey", "token_reserves_source_valid", "token_reserves_eth_reserve_nonnegative",
			"token_reserves_token_fk", "token_reserves_source_block_fk",
		},
		"holder_balances": {
			"holder_balances_pkey", "holder_balances_balance_nonnegative",
			"holder_balances_first_acquired_with_balance", "holder_balances_token_fk",
		},
		"aggregation_dirty": {
			"aggregation_dirty_pkey", "aggregation_dirty_claim_together",
			"aggregation_dirty_claim_not_ahead", "aggregation_dirty_token_fk",
		},
		"token_metadata": {
			"token_metadata_pkey", "token_metadata_revision_nonnegative",
			"token_metadata_launch_tx_hash_length", "token_metadata_launch_log_index_nonnegative",
			"token_metadata_launch_identity_pair",
		},
		"token_images": {
			"token_images_pkey", "token_images_chain_id_positive", "token_images_token_address_length",
			"token_images_content_type_valid", "token_images_byte_size_valid", "token_images_sha256_length",
			"token_images_revision_nonnegative", "token_images_launch_tx_hash_length",
			"token_images_launch_log_index_nonnegative", "token_images_launch_identity_pair",
		},
		"candles":        {"candles_pkey", "candles_interval_valid", "candles_token_fk"},
		"token_stats":    {"token_stats_pkey", "token_stats_token_fk"},
		"protocol_daily": {"protocol_daily_pkey", "protocol_daily_volume_eth_nonnegative"},
		"protocol_stats": {"protocol_stats_pkey", "protocol_stats_trades_all_time_nonnegative"},
	} {
		for _, constraint := range constraints {
			assertConstraintExists(t, ctx, database.DB, table, constraint)
		}
	}

	var generated string
	if err := database.DB.QueryRowContext(ctx, `
		SELECT generation_expression
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'tokens' AND column_name = 'token_is_token0'
	`).Scan(&generated); err != nil {
		t.Fatalf("read generated token ordering column: %v", err)
	}
	if generated == "" {
		t.Fatal("tokens.token_is_token0 is not generated")
	}

	var relationKind string
	if err := database.DB.QueryRowContext(ctx, `
		SELECT relkind::TEXT FROM pg_catalog.pg_class
		WHERE oid = 'public.market_trades'::regclass
	`).Scan(&relationKind); err != nil {
		t.Fatalf("read market_trades relation kind: %v", err)
	}
	if relationKind != "v" {
		t.Fatalf("market_trades relation kind = %q, want plain view", relationKind)
	}

	for _, column := range []struct {
		table, name, dataType string
		numericPrecision      int
		numericScale          int
	}{
		{"protocol_daily", "day", "date", 0, 0},
		{"protocol_stats", "trades_all_time", "bigint", 64, 0},
		{"token_stats", "spot_price_usd", "numeric", 38, 18},
		{"candles", "open_price_wad", "numeric", 78, 0},
	} {
		var dataType string
		var precision, scale sql.NullInt64
		if err := database.DB.QueryRowContext(ctx, `
			SELECT data_type, numeric_precision, numeric_scale
			FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		`, column.table, column.name).Scan(&dataType, &precision, &scale); err != nil {
			t.Fatalf("read %s.%s type: %v", column.table, column.name, err)
		}
		if dataType != column.dataType || (column.numericPrecision != 0 && (!precision.Valid || int(precision.Int64) != column.numericPrecision || !scale.Valid || int(scale.Int64) != column.numericScale)) {
			t.Fatalf("%s.%s type = %s(%v,%v), want %s(%d,%d)", column.table, column.name, dataType, precision, scale, column.dataType, column.numericPrecision, column.numericScale)
		}
	}

	testProjectionConstraintRejections(t, ctx, database.DB)
}

func assertProjectionColumns(t testing.TB, ctx context.Context, database *sql.DB) {
	t.Helper()
	wantByTable := map[string][]string{
		"tokens": {
			"chain_id", "token_address", "curve_address", "lp_pair", "weth", "creator", "protocol_treasury",
			"engine_version", "name", "symbol", "total_supply", "initial_virtual_eth", "initial_virtual_token",
			"curve_tokens", "lp_tokens", "graduation_eth", "trade_fee_bps", "protocol_share_bps",
			"launch_block_number", "launch_block_hash", "launch_block_time", "launch_tx_hash", "launch_log_index",
			"phase", "graduation_block_number", "graduation_block_hash", "graduation_block_time",
			"graduation_tx_hash", "graduation_log_index", "token_is_token0",
			"sort_market_cap_eth_wad", "sort_volume_24h_eth_wad",
		},
		"token_reserves":    {"chain_id", "token_address", "reserve_source", "eth_reserve", "token_reserve", "source_block_number", "source_block_hash", "source_block_time", "source_tx_hash", "source_log_index"},
		"holder_balances":   {"chain_id", "token_address", "holder_address", "balance", "first_acquired_block_number"},
		"aggregation_dirty": {"chain_id", "token_address", "generation", "claimed_generation", "claimed_at", "claimed_by"},
		"token_metadata":    {"chain_id", "token_address", "description", "image_url", "x_url", "telegram_url", "updated_at", "revision", "content_id", "launch_tx_hash", "launch_log_index"},
		"token_images":      {"chain_id", "token_address", "content_type", "content", "byte_size", "sha256", "revision", "updated_at", "content_id", "launch_tx_hash", "launch_log_index"},
		"candles":           {"chain_id", "token_address", "interval", "bucket_start_time", "open_price_wad", "high_price_wad", "low_price_wad", "close_price_wad", "gross_eth_volume", "token_volume", "trade_count"},
		"token_stats":       {"chain_id", "token_address", "spot_price_eth_wad", "market_cap_eth_wad", "fdv_eth_wad", "liquidity_eth_wad", "ath_price_eth_wad", "ath_at", "volume_24h_eth_wad", "price_change_24h_bps", "holder_count", "spot_price_usd", "market_cap_usd", "fdv_usd", "liquidity_usd", "ath_usd", "volume_24h_usd", "updated_at"},
		"protocol_daily":    {"chain_id", "day", "volume_eth_wad", "volume_usd", "launches_count", "trades_count", "graduations_count"},
		"protocol_stats":    {"chain_id", "volume_24h_eth_wad", "volume_24h_usd", "volume_all_time_eth_wad", "volume_all_time_usd", "launches_24h", "launches_all_time", "trades_24h", "trades_all_time", "graduations_24h", "graduations_all_time", "updated_at"},
	}
	for table, want := range wantByTable {
		rows, err := database.QueryContext(ctx, `
			SELECT column_name FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1
			ORDER BY ordinal_position
		`, table)
		if err != nil {
			t.Fatalf("list %s columns: %v", table, err)
		}
		var got []string
		for rows.Next() {
			var column string
			if err := rows.Scan(&column); err != nil {
				_ = rows.Close()
				t.Fatalf("scan %s column: %v", table, err)
			}
			got = append(got, column)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close %s columns: %v", table, err)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate %s columns: %v", table, err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s columns = %v, want %v", table, got, want)
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("%s columns = %v, want %v", table, got, want)
			}
		}
	}
}

func testProjectionConstraintRejections(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	const chainID int64 = 48001
	blockTime := time.Date(2026, time.September, 4, 10, 0, 0, 0, time.UTC)
	blockHash := hashBytes(0x11)
	mustInsertBlock(t, ctx, database, chainID, 1, blockHash, hashBytes(0x10), blockTime, "observed")
	token := addressBytes(0x20)
	launch := projectionLaunchFixture{token: token, curve: addressBytes(0x30), pair: addressBytes(0x40), weth: addressBytes(0x50)}
	insertProjectionLaunch(t, ctx, database, chainID, 1, blockHash, blockTime, hashBytes(0x12), launch)
	callRebuild(t, ctx, database, chainID, token)

	_, err := database.ExecContext(ctx, `
		INSERT INTO tokens (
			chain_id, token_address, curve_address, lp_pair, weth, creator, protocol_treasury,
			engine_version, name, symbol, total_supply, initial_virtual_eth, initial_virtual_token,
			curve_tokens, lp_tokens, graduation_eth, trade_fee_bps, protocol_share_bps,
			launch_block_number, launch_block_hash, launch_block_time, launch_tx_hash, launch_log_index
		) SELECT chain_id, token_address, curve_address, lp_pair, weth, creator, protocol_treasury,
			engine_version, name, symbol, total_supply, initial_virtual_eth, initial_virtual_token,
			curve_tokens, lp_tokens, graduation_eth, trade_fee_bps, protocol_share_bps,
			launch_block_number, launch_block_hash, launch_block_time, launch_tx_hash, launch_log_index
		FROM tokens WHERE chain_id = $1 AND token_address = $2
	`, chainID, token)
	assertPostgresConstraint(t, err, "23505", "tokens_pkey")

	_, err = database.ExecContext(ctx, `UPDATE tokens SET total_supply = -1 WHERE chain_id = $1 AND token_address = $2`, chainID, token)
	assertPostgresConstraint(t, err, "23514", "tokens_total_supply_nonnegative")

	_, err = database.ExecContext(ctx, `
		INSERT INTO token_reserves (
			chain_id, token_address, reserve_source, eth_reserve, token_reserve,
			source_block_number, source_block_hash, source_block_time, source_tx_hash, source_log_index
		) VALUES ($1, $2, 'curve', -1, 1, 1, $3, $4, $5, 0)
	`, chainID, token, blockHash, blockTime, hashBytes(0x13))
	assertPostgresConstraint(t, err, "23514", "token_reserves_eth_reserve_nonnegative")

	_, err = database.ExecContext(ctx, `
		INSERT INTO holder_balances (chain_id, token_address, holder_address, balance, first_acquired_block_number)
		VALUES ($1, $2, $3, 0, 1)
	`, chainID, token, addressBytes(0x60))
	assertPostgresConstraint(t, err, "23514", "holder_balances_first_acquired_with_balance")
	_, err = database.ExecContext(ctx, `
		INSERT INTO holder_balances (chain_id, token_address, holder_address, balance, first_acquired_block_number)
		VALUES ($1, $2, $3, -1, 1)
	`, chainID, token, addressBytes(0x62))
	assertPostgresConstraint(t, err, "23514", "holder_balances_balance_nonnegative")

	_, err = database.ExecContext(ctx, `
		INSERT INTO aggregation_dirty (
			chain_id, token_address, generation, claimed_generation, claimed_at, claimed_by
		) VALUES ($1, $2, 1, 2, now(), 'worker')
		ON CONFLICT (chain_id, token_address) DO UPDATE SET
			generation = 1, claimed_generation = 2, claimed_at = now(), claimed_by = 'worker'
	`, chainID, token)
	assertPostgresConstraint(t, err, "23514", "aggregation_dirty_claim_not_ahead")
	_, err = database.ExecContext(ctx, `UPDATE aggregation_dirty SET generation = -1 WHERE chain_id = $1 AND token_address = $2`, chainID, token)
	assertPostgresConstraint(t, err, "23514", "aggregation_dirty_generation_nonnegative")

	_, err = database.ExecContext(ctx, `
		INSERT INTO token_metadata (chain_id, token_address, updated_at)
		VALUES ($1, $2, now())
	`, chainID, addressBytes(0x61))
	if err != nil {
		t.Fatalf("metadata without canonical launch: %v", err)
	}

	_, err = database.ExecContext(ctx, `
		INSERT INTO candles (
			chain_id, token_address, interval, bucket_start_time,
			open_price_wad, high_price_wad, low_price_wad, close_price_wad
		) VALUES ($1, $2, '6h', now(), 1, 1, 1, 1)
	`, chainID, token)
	assertPostgresConstraint(t, err, "23514", "candles_interval_valid")
	_, err = database.ExecContext(ctx, `
		INSERT INTO candles (
			chain_id, token_address, interval, bucket_start_time,
			open_price_wad, high_price_wad, low_price_wad, close_price_wad
		) VALUES ($1, $2, '1m', now(), -1, 1, 1, 1)
	`, chainID, token)
	assertPostgresConstraint(t, err, "23514", "candles_open_price_nonnegative")

	_, err = database.ExecContext(ctx, `
		INSERT INTO token_stats (
			chain_id, token_address, spot_price_eth_wad, market_cap_eth_wad, fdv_eth_wad,
			liquidity_eth_wad, ath_price_eth_wad, ath_at, price_change_24h_bps, updated_at
		) VALUES ($1, $2, 1, 1, 1, 1, 1, now(), -250, now())
	`, chainID, token)
	if err != nil {
		t.Fatalf("insert signed price change: %v", err)
	}
	_, err = database.ExecContext(ctx, `UPDATE token_stats SET spot_price_eth_wad = -1 WHERE chain_id = $1 AND token_address = $2`, chainID, token)
	assertPostgresConstraint(t, err, "23514", "token_stats_spot_price_nonnegative")

	_, err = database.ExecContext(ctx, `INSERT INTO protocol_daily (chain_id, day, volume_eth_wad) VALUES ($1, CURRENT_DATE, -1)`, chainID)
	assertPostgresConstraint(t, err, "23514", "protocol_daily_volume_eth_nonnegative")

	_, err = database.ExecContext(ctx, `INSERT INTO protocol_stats (chain_id, trades_all_time, updated_at) VALUES ($1, -1, now())`, chainID)
	assertPostgresConstraint(t, err, "23514", "protocol_stats_trades_all_time_nonnegative")

	testUnknownProjectionAddresses(t, ctx, database, chainID, token, blockHash, blockTime)
}

func testUnknownProjectionAddresses(t testing.TB, ctx context.Context, database *sql.DB, chainID int64, knownToken, blockHash []byte, blockTime time.Time) {
	t.Helper()
	unknown := addressBytes(0xfe)

	_, err := database.ExecContext(ctx, `
		INSERT INTO tokens (
			chain_id, token_address, curve_address, lp_pair, weth, creator, protocol_treasury,
			engine_version, name, symbol, total_supply, initial_virtual_eth, initial_virtual_token,
			curve_tokens, lp_tokens, graduation_eth, trade_fee_bps, protocol_share_bps,
			launch_block_number, launch_block_hash, launch_block_time, launch_tx_hash, launch_log_index
		) SELECT chain_id, $3, curve_address, lp_pair, weth, creator, protocol_treasury,
			engine_version, name, symbol, total_supply, initial_virtual_eth, initial_virtual_token,
			curve_tokens, lp_tokens, graduation_eth, trade_fee_bps, protocol_share_bps,
			launch_block_number, launch_block_hash, launch_block_time, launch_tx_hash, launch_log_index
		FROM tokens WHERE chain_id = $1 AND token_address = $2
	`, chainID, knownToken, unknown)
	assertPostgresConstraint(t, err, "23503", "tokens_token_launch_fk")

	_, err = database.ExecContext(ctx, `
		INSERT INTO token_reserves (
			chain_id, token_address, reserve_source, eth_reserve, token_reserve,
			source_block_number, source_block_hash, source_block_time, source_tx_hash, source_log_index
		) VALUES ($1, $2, 'curve', 1, 1, 1, $3, $4, $5, 0)
	`, chainID, unknown, blockHash, blockTime, hashBytes(0xf1))
	assertPostgresConstraint(t, err, "23503", "token_reserves_token_fk")

	_, err = database.ExecContext(ctx, `
		INSERT INTO holder_balances (chain_id, token_address, holder_address, balance, first_acquired_block_number)
		VALUES ($1, $2, $3, 1, 1)
	`, chainID, unknown, addressBytes(0xf2))
	assertPostgresConstraint(t, err, "23503", "holder_balances_token_fk")

	_, err = database.ExecContext(ctx, `INSERT INTO aggregation_dirty (chain_id, token_address, generation) VALUES ($1, $2, 1)`, chainID, unknown)
	assertPostgresConstraint(t, err, "23503", "aggregation_dirty_token_fk")

	_, err = database.ExecContext(ctx, `INSERT INTO token_metadata (chain_id, token_address, updated_at) VALUES ($1, $2, now())`, chainID, unknown)
	if err != nil {
		t.Fatalf("orphan metadata: %v", err)
	}

	_, err = database.ExecContext(ctx, `
		INSERT INTO candles (
			chain_id, token_address, interval, bucket_start_time,
			open_price_wad, high_price_wad, low_price_wad, close_price_wad
		) VALUES ($1, $2, '1m', now(), 1, 1, 1, 1)
	`, chainID, unknown)
	assertPostgresConstraint(t, err, "23503", "candles_token_fk")

	_, err = database.ExecContext(ctx, `
		INSERT INTO token_stats (
			chain_id, token_address, spot_price_eth_wad, market_cap_eth_wad, fdv_eth_wad,
			liquidity_eth_wad, ath_price_eth_wad, ath_at, updated_at
		) VALUES ($1, $2, 1, 1, 1, 1, 1, now(), now())
	`, chainID, unknown)
	assertPostgresConstraint(t, err, "23503", "token_stats_token_fk")
}

func TestMarketTradesPairOrderingAndSyncSelection(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	const chainID int64 = 48002
	blockTime := time.Date(2026, time.September, 4, 11, 0, 0, 0, time.UTC)
	blockHash := hashBytes(0x21)
	mustInsertBlock(t, ctx, database.DB, chainID, 10, blockHash, hashBytes(0x20), blockTime, "safe")

	tests := []struct {
		name          string
		token         []byte
		weth          []byte
		pair          []byte
		amount0In     int64
		amount1In     int64
		amount0Out    int64
		amount1Out    int64
		reserve0      int64
		reserve1      int64
		wantSideBuy   bool
		wantExecution string
		wantSpot      string
	}{
		{"token0 buy", addressBytes(0x10), addressBytes(0x90), addressBytes(0x31), 0, 100, 20, 0, 200, 1200, true, "5000000000000000000", "6000000000000000000"},
		{"token1 sell", addressBytes(0x90), addressBytes(0x10), addressBytes(0x32), 0, 50, 10, 0, 510, 2500, false, "200000000000000000", "204000000000000000"},
	}

	sharedSwapTx := hashBytes(0x7f)
	for index, test := range tests {
		launchTx := hashBytes(byte(0x30 + index*8))
		launch := projectionLaunchFixture{token: test.token, curve: addressBytes(byte(0x41 + index)), pair: test.pair, weth: test.weth}
		insertProjectionLaunch(t, ctx, database.DB, chainID, 10, blockHash, blockTime, launchTx, launch)
		graduationTx := hashBytes(byte(0x31 + index*8))
		insertProjectionGraduation(t, ctx, database.DB, chainID, 10, blockHash, blockTime, graduationTx, 0, test.token, test.pair)
		callRebuild(t, ctx, database.DB, chainID, test.token)

		firstSyncLog := 1 + index
		selectedSyncLog := 3 + index*2
		swapLog := selectedSyncLog + 1
		orphanSyncLog := 7 + index*2
		insertPoolSync(t, ctx, database.DB, chainID, 10, blockHash, blockTime, sharedSwapTx, firstSyncLog, test.pair, 1, 1)
		insertPoolSync(t, ctx, database.DB, chainID, 10, blockHash, blockTime, sharedSwapTx, selectedSyncLog, test.pair, test.reserve0, test.reserve1)
		insertPoolSwap(t, ctx, database.DB, chainID, 10, blockHash, blockTime, sharedSwapTx, swapLog, test.pair,
			test.amount0In, test.amount1In, test.amount0Out, test.amount1Out)
		// A Mint/Burn Sync without a following Swap must not create a market row.
		insertPoolSync(t, ctx, database.DB, chainID, 10, blockHash, blockTime, sharedSwapTx, orphanSyncLog, test.pair, test.reserve0+1, test.reserve1+1)
		insertPoolMint(t, ctx, database.DB, chainID, 10, blockHash, blockTime, sharedSwapTx, orphanSyncLog+1, test.pair)

		var source, execution, spot, finality string
		var sideBuy bool
		var trader []byte
		if err := database.DB.QueryRowContext(ctx, `
			SELECT source, side_buy, trader, execution_price_wad::TEXT, spot_price_wad::TEXT, finality
			FROM market_trades
			WHERE chain_id = $1 AND token_address = $2 AND source = 'dex'
		`, chainID, test.token).Scan(&source, &sideBuy, &trader, &execution, &spot, &finality); err != nil {
			t.Fatalf("%s: read DEX market row: %v", test.name, err)
		}
		if source != "dex" || sideBuy != test.wantSideBuy || trader != nil || execution != test.wantExecution || spot != test.wantSpot || finality != "safe" {
			t.Fatalf("%s: market row = source=%s buy=%t trader=%x execution=%s spot=%s finality=%s", test.name, source, sideBuy, trader, execution, spot, finality)
		}
	}
}

func TestRebuildTokenProjectionsAfterGraduationReorg(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	const chainID int64 = 48003
	baseTime := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC)
	for block := int64(100); block <= 102; block++ {
		blockTime := baseTime
		if block > 100 {
			blockTime = baseTime.Add(time.Minute)
		}
		mustInsertBlock(t, ctx, database.DB, chainID, block, hashBytes(byte(block)), hashBytes(byte(block-1)), blockTime, "observed")
	}

	token := addressBytes(0x51)
	curve := addressBytes(0x61)
	pair := addressBytes(0x71)
	weth := addressBytes(0x81)
	launch := projectionLaunchFixture{token: token, curve: curve, pair: pair, weth: weth}
	insertProjectionLaunch(t, ctx, database.DB, chainID, 100, hashBytes(100), baseTime, hashBytes(0xa0), launch)
	insertTransfer(t, ctx, database.DB, chainID, 100, hashBytes(100), baseTime, hashBytes(0xa1), 1, token, addressBytes(0), curve, 1_000_000)

	tradeTime := baseTime.Add(time.Minute)
	insertProjectionTrade(t, ctx, database.DB, chainID, 101, hashBytes(101), tradeTime, hashBytes(0xb0), 0, token, 100, 25, 800_000)
	insertTransfer(t, ctx, database.DB, chainID, 101, hashBytes(101), tradeTime, hashBytes(0xb1), 1, token, curve, addressBytes(0x91), 800_000)

	graduationTime := tradeTime // Equal timestamps must still order by the deterministic chain cursor.
	graduationTx := hashBytes(0xc0)
	insertProjectionGraduation(t, ctx, database.DB, chainID, 102, hashBytes(102), graduationTime, graduationTx, 0, token, pair)
	insertTransfer(t, ctx, database.DB, chainID, 102, hashBytes(102), graduationTime, hashBytes(0xc1), 1, token, curve, pair, 200_000)
	insertPoolSync(t, ctx, database.DB, chainID, 102, hashBytes(102), graduationTime, hashBytes(0xc2), 2, pair, 200_000, 100)
	insertPoolSwap(t, ctx, database.DB, chainID, 102, hashBytes(102), graduationTime, hashBytes(0xc2), 3, pair, 0, 10, 10_000, 0)

	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO token_metadata (chain_id, token_address, description, updated_at)
		VALUES ($1, $2, 'survives-reorg', now())
	`, chainID, token); err != nil {
		t.Fatalf("insert token metadata: %v", err)
	}
	callRebuild(t, ctx, database.DB, chainID, token)

	assertProjectionState(t, ctx, database.DB, chainID, token, "graduated", "pair", "1000000", 2)
	assertOneMinuteCandle(t, ctx, database.DB, chainID, token, "125000000000000", "1000000000000000", "110", 2)

	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin reorg rollback: %v", err)
	}
	for _, statement := range []string{
		`DELETE FROM pool_swaps WHERE chain_id = $1 AND block_number > 101`,
		`DELETE FROM pool_syncs WHERE chain_id = $1 AND block_number > 101`,
		`DELETE FROM transfers WHERE chain_id = $1 AND block_number > 101`,
		`DELETE FROM graduations WHERE chain_id = $1 AND block_number > 101`,
		`DELETE FROM indexed_blocks WHERE chain_id = $1 AND block_number > 101`,
	} {
		if _, err := tx.ExecContext(ctx, statement, chainID); err != nil {
			rollbackAfterFailure(t, tx)
			t.Fatalf("delete losing branch events: %v", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `SELECT rebuild_token_projections($1, $2)`, chainID, token); err != nil {
		rollbackAfterFailure(t, tx)
		t.Fatalf("rebuild after losing-branch delete: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit reorg rebuild: %v", err)
	}

	assertProjectionState(t, ctx, database.DB, chainID, token, "curve", "curve", "800000", 1)
	assertOneMinuteCandle(t, ctx, database.DB, chainID, token, "125000000000000", "125000000000000", "100", 1)

	var description string
	if err := database.DB.QueryRowContext(ctx, `SELECT description FROM token_metadata WHERE chain_id = $1 AND token_address = $2`, chainID, token).Scan(&description); err != nil {
		t.Fatalf("read metadata after rebuild: %v", err)
	}
	if description != "survives-reorg" {
		t.Fatalf("metadata description = %q, want survives-reorg", description)
	}

	var realCurveETH string
	if err := database.DB.QueryRowContext(ctx, `
		SELECT (reserve.eth_reserve - token.initial_virtual_eth)::TEXT
		FROM token_reserves AS reserve
		JOIN tokens AS token USING (chain_id, token_address)
		WHERE reserve.chain_id = $1 AND reserve.token_address = $2
	`, chainID, token).Scan(&realCurveETH); err != nil {
		t.Fatalf("derive fixture realCurveEth: %v", err)
	}
	if realCurveETH != "15" { // Fixture: post-trade virtual reserve 25 minus launch x0 10.
		t.Fatalf("realCurveEth = %s, want 15", realCurveETH)
	}
}

func assertOneMinuteCandle(t testing.TB, ctx context.Context, database *sql.DB, chainID int64, token []byte, wantOpen, wantClose, wantGross string, wantTrades int) {
	t.Helper()
	var open, close, gross string
	var trades int
	if err := database.QueryRowContext(ctx, `
		SELECT open_price_wad::TEXT, close_price_wad::TEXT, gross_eth_volume::TEXT, trade_count
		FROM candles
		WHERE chain_id = $1 AND token_address = $2 AND interval = '1m'
	`, chainID, token).Scan(&open, &close, &gross, &trades); err != nil {
		t.Fatalf("read one-minute candle: %v", err)
	}
	if open != wantOpen || close != wantClose || gross != wantGross || trades != wantTrades {
		t.Fatalf("one-minute candle = open=%s close=%s gross=%s trades=%d; want open=%s close=%s gross=%s trades=%d", open, close, gross, trades, wantOpen, wantClose, wantGross, wantTrades)
	}
}

func assertProjectionState(t testing.TB, ctx context.Context, database *sql.DB, chainID int64, token []byte, wantPhase, wantReserveSource, wantCirculating string, wantTrades int) {
	t.Helper()
	var phase, reserveSource, circulating string
	if err := database.QueryRowContext(ctx, `
		SELECT token.phase, reserve.reserve_source,
		       (token.total_supply
		        - COALESCE(curve_balance.balance, 0)
		        - COALESCE(zero_balance.balance, 0)
		        - COALESCE(dead_balance.balance, 0))::TEXT
		FROM tokens AS token
		JOIN token_reserves AS reserve USING (chain_id, token_address)
		LEFT JOIN holder_balances AS curve_balance
		  ON curve_balance.chain_id = token.chain_id
		 AND curve_balance.token_address = token.token_address
		 AND curve_balance.holder_address = token.curve_address
		LEFT JOIN holder_balances AS zero_balance
		  ON zero_balance.chain_id = token.chain_id
		 AND zero_balance.token_address = token.token_address
		 AND zero_balance.holder_address = decode(repeat('00', 20), 'hex')
		LEFT JOIN holder_balances AS dead_balance
		  ON dead_balance.chain_id = token.chain_id
		 AND dead_balance.token_address = token.token_address
		 AND dead_balance.holder_address = decode(lpad('dead', 40, '0'), 'hex')
		WHERE token.chain_id = $1 AND token.token_address = $2
	`, chainID, token).Scan(&phase, &reserveSource, &circulating); err != nil {
		t.Fatalf("read projection state: %v", err)
	}
	if phase != wantPhase || reserveSource != wantReserveSource || circulating != wantCirculating {
		t.Fatalf("projection state = phase=%s reserve=%s circulating=%s; want phase=%s reserve=%s circulating=%s", phase, reserveSource, circulating, wantPhase, wantReserveSource, wantCirculating)
	}

	var tradeCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM market_trades WHERE chain_id = $1 AND token_address = $2`, chainID, token).Scan(&tradeCount); err != nil {
		t.Fatalf("count market trades: %v", err)
	}
	if tradeCount != wantTrades {
		t.Fatalf("market trade count = %d, want %d", tradeCount, wantTrades)
	}

	var holderCount int
	if err := database.QueryRowContext(ctx, `
		SELECT count(*)
		FROM holder_balances AS balance
		JOIN tokens AS token USING (chain_id, token_address)
		WHERE balance.chain_id = $1 AND balance.token_address = $2
		  AND balance.balance > 0
		  AND balance.holder_address NOT IN (
		      decode(repeat('00', 20), 'hex'), decode(lpad('dead', 40, '0'), 'hex'),
		      token.curve_address, token.lp_pair
		  )
	`, chainID, token).Scan(&holderCount); err != nil {
		t.Fatalf("count eligible holders: %v", err)
	}
	if holderCount != 1 {
		t.Fatalf("eligible holder count = %d, want 1", holderCount)
	}
}

type projectionLaunchFixture struct {
	token []byte
	curve []byte
	pair  []byte
	weth  []byte
}

func insertProjectionLaunch(t testing.TB, ctx context.Context, database *sql.DB, chainID, blockNumber int64, blockHash []byte, blockTime time.Time, txHash []byte, launch projectionLaunchFixture) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
		INSERT INTO token_launches (
			chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
			token_address, curve_address, creator, lp_pair, weth, protocol_treasury,
			engine_version, name, symbol, total_supply, virtual_eth, virtual_token,
			curve_tokens, lp_tokens, graduation_eth, launch_fee_paid, trade_fee_bps, protocol_share_bps
		) VALUES ($1, $2, $3, $4, 0, $5, 0, $6, $7, $8, $9, $10, $11,
			1, 'Token', 'TKN', 1000000, 10, 1000000, 800000, 200000, 100, 1, 100, 50)
	`, chainID, blockNumber, blockHash, blockTime, txHash, launch.token, launch.curve,
		addressBytes(0x55), launch.pair, launch.weth, addressBytes(0x56))
	if err != nil {
		t.Fatalf("insert projection token launch: %v", err)
	}
}

func insertProjectionGraduation(t testing.TB, ctx context.Context, database *sql.DB, chainID, blockNumber int64, blockHash []byte, blockTime time.Time, txHash []byte, logIndex int, token, pair []byte) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
		INSERT INTO graduations (
			chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
			token_address, lp_pair, eth_to_pool, tokens_to_pool, lp_liquidity_burned
		) VALUES ($1, $2, $3, $4, 0, $5, $6, $7, $8, 100, 200000, 1)
	`, chainID, blockNumber, blockHash, blockTime, txHash, logIndex, token, pair)
	if err != nil {
		t.Fatalf("insert projection graduation: %v", err)
	}
}

func insertProjectionTrade(t testing.TB, ctx context.Context, database *sql.DB, chainID, blockNumber int64, blockHash []byte, blockTime time.Time, txHash []byte, logIndex int, token []byte, ethGross, newETHReserve, newTokenReserve int64) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
		INSERT INTO trades (
			chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
			token_address, trader, is_buy, eth_gross, eth_refund, token_amount,
			protocol_fee, creator_fee, new_eth_reserve, new_token_reserve
		) VALUES ($1, $2, $3, $4, 0, $5, $6, $7, $8, true, $9, 7, 800000, 1, 1, $10, $11)
	`, chainID, blockNumber, blockHash, blockTime, txHash, logIndex, token, addressBytes(0x91), ethGross, newETHReserve, newTokenReserve)
	if err != nil {
		t.Fatalf("insert projection trade: %v", err)
	}
}

func insertTransfer(t testing.TB, ctx context.Context, database sqlExecer, chainID, blockNumber int64, blockHash []byte, blockTime time.Time, txHash []byte, logIndex int, token, from, to []byte, value int64) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
		INSERT INTO transfers (
			chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
			token_address, from_address, to_address, value
		) VALUES ($1, $2, $3, $4, 0, $5, $6, $7, $8, $9, $10)
	`, chainID, blockNumber, blockHash, blockTime, txHash, logIndex, token, from, to, value)
	if err != nil {
		t.Fatalf("insert transfer: %v", err)
	}
}

func insertPoolSync(t testing.TB, ctx context.Context, database *sql.DB, chainID, blockNumber int64, blockHash []byte, blockTime time.Time, txHash []byte, logIndex int, pair []byte, reserve0, reserve1 int64) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
		INSERT INTO pool_syncs (
			chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
			pair_address, reserve0, reserve1
		) VALUES ($1, $2, $3, $4, 0, $5, $6, $7, $8, $9)
	`, chainID, blockNumber, blockHash, blockTime, txHash, logIndex, pair, reserve0, reserve1)
	if err != nil {
		t.Fatalf("insert pool sync: %v", err)
	}
}

func insertPoolSwap(t testing.TB, ctx context.Context, database *sql.DB, chainID, blockNumber int64, blockHash []byte, blockTime time.Time, txHash []byte, logIndex int, pair []byte, amount0In, amount1In, amount0Out, amount1Out int64) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
		INSERT INTO pool_swaps (
			chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
			pair_address, sender, amount0_in, amount1_in, amount0_out, amount1_out, to_address
		) VALUES ($1, $2, $3, $4, 0, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, chainID, blockNumber, blockHash, blockTime, txHash, logIndex, pair, addressBytes(0xa1), amount0In, amount1In, amount0Out, amount1Out, addressBytes(0xa2))
	if err != nil {
		t.Fatalf("insert pool swap: %v", err)
	}
}

func insertPoolMint(t testing.TB, ctx context.Context, database *sql.DB, chainID, blockNumber int64, blockHash []byte, blockTime time.Time, txHash []byte, logIndex int, pair []byte) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
		INSERT INTO pool_mints (
			chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
			pair_address, sender, amount0, amount1
		) VALUES ($1, $2, $3, $4, 0, $5, $6, $7, $8, 1, 1)
	`, chainID, blockNumber, blockHash, blockTime, txHash, logIndex, pair, addressBytes(0xa3))
	if err != nil {
		t.Fatalf("insert pool mint: %v", err)
	}
}

func callRebuild(t testing.TB, ctx context.Context, database *sql.DB, chainID int64, token []byte) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `SELECT rebuild_token_projections($1, $2)`, chainID, token); err != nil {
		t.Fatalf("rebuild token projections: %v", err)
	}
}

func TestHolderFirstAcquiredResetsAfterZero(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	const chainID int64 = 48004
	baseTime := time.Date(2026, time.September, 4, 13, 0, 0, 0, time.UTC)
	for block := int64(1); block <= 4; block++ {
		mustInsertBlock(t, ctx, database.DB, chainID, block, hashBytes(byte(0xd0+block)), hashBytes(byte(0xcf+block)), baseTime.Add(time.Duration(block)*time.Second), "observed")
	}
	token, curve, holder := addressBytes(0x22), addressBytes(0x33), addressBytes(0x44)
	insertProjectionLaunch(t, ctx, database.DB, chainID, 1, hashBytes(0xd1), baseTime.Add(time.Second), hashBytes(0xe1), projectionLaunchFixture{token: token, curve: curve, pair: addressBytes(0x55), weth: addressBytes(0x66)})
	insertTransfer(t, ctx, database.DB, chainID, 1, hashBytes(0xd1), baseTime.Add(time.Second), hashBytes(0xe2), 1, token, addressBytes(0), curve, 100)
	insertTransfer(t, ctx, database.DB, chainID, 2, hashBytes(0xd2), baseTime.Add(2*time.Second), hashBytes(0xe3), 0, token, curve, holder, 50)
	insertTransfer(t, ctx, database.DB, chainID, 3, hashBytes(0xd3), baseTime.Add(3*time.Second), hashBytes(0xe4), 0, token, holder, curve, 50)
	insertTransfer(t, ctx, database.DB, chainID, 4, hashBytes(0xd4), baseTime.Add(4*time.Second), hashBytes(0xe5), 0, token, curve, holder, 25)
	callRebuild(t, ctx, database.DB, chainID, token)

	var balance string
	var firstAcquired int64
	if err := database.DB.QueryRowContext(ctx, `
		SELECT balance::TEXT, first_acquired_block_number
		FROM holder_balances
		WHERE chain_id = $1 AND token_address = $2 AND holder_address = $3
	`, chainID, token, holder).Scan(&balance, &firstAcquired); err != nil {
		t.Fatalf("read reset holder balance: %v", err)
	}
	if balance != "25" || firstAcquired != 4 {
		t.Fatalf("holder balance = %s first acquired = %d, want 25 and 4", balance, firstAcquired)
	}

	var tokenIsToken0 bool
	if err := database.DB.QueryRowContext(ctx, `SELECT token_is_token0 FROM tokens WHERE chain_id = $1 AND token_address = $2`, chainID, token).Scan(&tokenIsToken0); err != nil {
		t.Fatalf("read generated token ordering: %v", err)
	}
	if !tokenIsToken0 || bytes.Compare(token, addressBytes(0x66)) >= 0 {
		t.Fatal("generated token ordering does not match bytewise address ordering")
	}
}
