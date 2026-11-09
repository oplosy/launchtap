//go:build integration

package postgrestest

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

var eventPayloadColumns = map[string][]string{
	"token_launches":                 {"token_address", "curve_address", "creator", "lp_pair", "weth", "protocol_treasury", "engine_version", "name", "symbol", "total_supply", "virtual_eth", "virtual_token", "curve_tokens", "lp_tokens", "graduation_eth", "launch_fee_paid", "trade_fee_bps", "protocol_share_bps"},
	"trades":                         {"token_address", "trader", "is_buy", "eth_gross", "eth_refund", "token_amount", "protocol_fee", "creator_fee", "new_eth_reserve", "new_token_reserve"},
	"graduations":                    {"token_address", "lp_pair", "eth_to_pool", "tokens_to_pool", "lp_liquidity_burned"},
	"creator_fee_claims":             {"token_address", "creator", "amount"},
	"protocol_fee_claims":            {"token_address", "treasury", "amount"},
	"launch_fee_claims":              {"treasury", "amount"},
	"refund_credits":                 {"token_address", "account", "amount"},
	"refund_claims":                  {"token_address", "account", "amount"},
	"transfers":                      {"token_address", "from_address", "to_address", "value"},
	"pool_mints":                     {"pair_address", "sender", "amount0", "amount1"},
	"pool_burns":                     {"pair_address", "sender", "amount0", "amount1", "to_address"},
	"pool_swaps":                     {"pair_address", "sender", "amount0_in", "amount1_in", "amount0_out", "amount1_out", "to_address", "tx_from"},
	"pool_syncs":                     {"pair_address", "reserve0", "reserve1"},
	"launch_pause_events":            {"paused"},
	"trading_pause_events":           {"paused"},
	"engine_configurations":          {"engine_version", "implementation", "enabled"},
	"future_defaults_configurations": {"config_hash"},
	"future_treasury_configurations": {"previous_treasury", "new_treasury"},
}

var sharedEventColumns = []string{
	"chain_id",
	"block_number",
	"block_hash",
	"block_time",
	"transaction_index",
	"tx_hash",
	"log_index",
}

func TestEventLedgerSchema(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	t.Run("exact event tables and columns", func(t *testing.T) {
		testExactEventTablesAndColumns(t, ctx, database.DB)
	})
	t.Run("shared constraints and indexes", func(t *testing.T) {
		testSharedEventConstraintsAndIndexes(t, ctx, database.DB)
	})
	t.Run("duplicate and scalar constraints", func(t *testing.T) {
		testDuplicateAndScalarConstraints(t, ctx, database.DB)
	})
	t.Run("deferred block and token links", func(t *testing.T) {
		testDeferredEventLinks(t, ctx, database.DB)
	})
	t.Run("graduation ordering", func(t *testing.T) {
		testGraduationOrdering(t, ctx, database.DB)
	})
	t.Run("rollback order", func(t *testing.T) {
		testRollbackOrder(t, ctx, database.DB)
	})
}

func testExactEventTablesAndColumns(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for table, payload := range eventPayloadColumns {
		assertTableExists(t, ctx, database, table, true)

		rows, err := database.QueryContext(ctx, `
			SELECT column_name
			FROM information_schema.columns
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
				if closeErr := rows.Close(); closeErr != nil {
					t.Errorf("close %s columns after scan failure: %v", table, closeErr)
				}
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

		want := append(append([]string{}, sharedEventColumns...), payload...)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s columns = %v, want %v", table, got, want)
		}
	}
}

func testSharedEventConstraintsAndIndexes(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for table := range eventPayloadColumns {
		for _, suffix := range []string{"pkey", "chain_id_positive", "block_number_nonnegative", "block_hash_length", "transaction_index_nonnegative", "tx_hash_length", "log_index_nonnegative", "block_fk"} {
			assertConstraintExists(t, ctx, database, table, table+"_"+suffix)
		}
	}

	assertConstraintExists(t, ctx, database, "indexed_blocks", "indexed_blocks_event_coordinates_key")
	assertConstraintExists(t, ctx, database, "token_launches", "token_launches_chain_token_key")

	var blockFKs int
	if err := database.QueryRowContext(ctx, `
		SELECT count(*)
		FROM pg_catalog.pg_constraint
		WHERE contype = 'f'
		  AND conname LIKE '%_block_fk'
		  AND condeferrable
		  AND condeferred
		  AND confdeltype = 'a'
	`).Scan(&blockFKs); err != nil {
		t.Fatalf("count deferred block foreign keys: %v", err)
	}
	wantBlockFKs := len(eventPayloadColumns) + 2 // tokens graduation and token_reserves source coordinates.
	if blockFKs != wantBlockFKs {
		t.Fatalf("deferred no-action block foreign keys = %d, want %d", blockFKs, wantBlockFKs)
	}

	var tokenFKs int
	if err := database.QueryRowContext(ctx, `
		SELECT count(*)
		FROM pg_catalog.pg_constraint
		WHERE contype = 'f'
		  AND conname LIKE '%_token_launch_fk'
		  AND condeferrable
		  AND condeferred
		  AND confdeltype = 'a'
	`).Scan(&tokenFKs); err != nil {
		t.Fatalf("count deferred token foreign keys: %v", err)
	}
	if tokenFKs != 8 { // Seven event links plus tokens; metadata survives orphan launches.
		t.Fatalf("deferred no-action token foreign keys = %d, want 8", tokenFKs)
	}

	for _, index := range []string{"pool_swaps_reserve_lookup_idx", "pool_syncs_reserve_lookup_idx"} {
		var definition string
		if err := database.QueryRowContext(ctx, `
			SELECT indexdef
			FROM pg_catalog.pg_indexes
			WHERE schemaname = 'public' AND indexname = $1
		`, index).Scan(&definition); err != nil {
			t.Fatalf("read index %s: %v", index, err)
		}
		if !strings.Contains(definition, "(chain_id, pair_address, tx_hash, log_index)") {
			t.Fatalf("index %s definition = %q", index, definition)
		}
	}
}

func testDuplicateAndScalarConstraints(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	const chainID int64 = 47001
	blockTime := time.Date(2026, time.September, 4, 15, 0, 0, 0, time.UTC)
	blockHash := hashBytes(0x51)
	mustInsertBlock(t, ctx, database, chainID, 1, blockHash, hashBytes(0x50), blockTime, "observed")

	insertPause := `
		INSERT INTO launch_pause_events (
			chain_id, block_number, block_hash, block_time,
			transaction_index, tx_hash, log_index, paused
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	args := []any{chainID, 1, blockHash, blockTime, 0, hashBytes(0x52), 0, true}
	if _, err := database.ExecContext(ctx, insertPause, args...); err != nil {
		t.Fatalf("insert launch pause event: %v", err)
	}
	_, err := database.ExecContext(ctx, insertPause, args...)
	assertPostgresConstraint(t, err, "23505", "launch_pause_events_pkey")

	_, err = database.ExecContext(ctx, `
		INSERT INTO launch_fee_claims (
			chain_id, block_number, block_hash, block_time,
			transaction_index, tx_hash, log_index, treasury, amount
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, chainID, 1, blockHash, blockTime, 0, hashBytes(0x53), 0, addressBytes(0x53), -1)
	assertPostgresConstraint(t, err, "23514", "launch_fee_claims_amount_nonnegative")

	_, err = database.ExecContext(ctx, `
		INSERT INTO engine_configurations (
			chain_id, block_number, block_hash, block_time,
			transaction_index, tx_hash, log_index, engine_version, implementation, enabled
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, chainID, 1, blockHash, blockTime, 0, hashBytes(0x54), 0, 65536, addressBytes(0x54), true)
	assertPostgresConstraint(t, err, "23514", "engine_configurations_engine_version_uint16")
}

func testDeferredEventLinks(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	const chainID int64 = 47002
	blockTime := time.Date(2026, time.September, 4, 16, 0, 0, 0, time.UTC)
	blockHash := hashBytes(0x61)
	mustInsertBlock(t, ctx, database, chainID, 10, blockHash, hashBytes(0x60), blockTime, "observed")
	token := addressBytes(0x61)

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin deferred token-link transaction: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO transfers (
			chain_id, block_number, block_hash, block_time,
			transaction_index, tx_hash, log_index,
			token_address, from_address, to_address, value
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, chainID, 10, blockHash, blockTime, 0, hashBytes(0x62), 0, token, addressBytes(0), addressBytes(0x62), 1000); err != nil {
		rollbackAfterFailure(t, tx)
		t.Fatalf("insert transfer before token launch: %v", err)
	}
	if err := insertTokenLaunch(ctx, tx, eventCoordinates{chainID, 10, blockHash, blockTime, 0, hashBytes(0x63), 1}, token); err != nil {
		rollbackAfterFailure(t, tx)
		t.Fatalf("insert token launch after transfer: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit deferred token-link transaction: %v", err)
	}

	unknownBlockHash := hashBytes(0x64)
	err = insertTokenLaunch(ctx, database, eventCoordinates{chainID, 11, unknownBlockHash, blockTime.Add(time.Second), 0, hashBytes(0x64), 0}, addressBytes(0x64))
	assertPostgresConstraint(t, err, "23503", "token_launches_block_fk")

	err = insertTrade(ctx, database, eventCoordinates{chainID, 10, blockHash, blockTime, 0, hashBytes(0x65), 0}, addressBytes(0x65))
	assertPostgresConstraint(t, err, "23503", "trades_token_launch_fk")

	const eventFirstChainID int64 = 47005
	eventFirstTime := blockTime.Add(2 * time.Second)
	eventFirstHash := hashBytes(0x66)
	tx, err = database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin deferred block-link transaction: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO trading_pause_events (
			chain_id, block_number, block_hash, block_time,
			transaction_index, tx_hash, log_index, paused
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, eventFirstChainID, 12, eventFirstHash, eventFirstTime, 0, hashBytes(0x67), 0, false); err != nil {
		rollbackAfterFailure(t, tx)
		t.Fatalf("insert event before block: %v", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		insertIndexedBlockSQL,
		eventFirstChainID,
		12,
		eventFirstHash,
		hashBytes(0x65),
		eventFirstTime,
		"observed",
	); err != nil {
		rollbackAfterFailure(t, tx)
		t.Fatalf("insert block after event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit deferred block-link transaction: %v", err)
	}
}

func testGraduationOrdering(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	const chainID int64 = 47003
	baseTime := time.Date(2026, time.September, 4, 17, 0, 0, 0, time.UTC)
	for blockNumber := int64(20); blockNumber <= 22; blockNumber++ {
		mustInsertBlock(
			t,
			ctx,
			database,
			chainID,
			blockNumber,
			hashBytes(byte(0x70+blockNumber-20)),
			hashBytes(byte(0x6f+blockNumber-20)),
			baseTime.Add(time.Duration(blockNumber-20)*time.Second),
			"observed",
		)
	}

	tests := []struct {
		name            string
		tokenByte       byte
		tradeFirst      bool
		tradeBlock      int64
		graduationBlock int64
		wantError       string
	}{
		{"trade then earlier graduation", 0x71, true, 22, 21, "occurs after graduation"},
		{"graduation then later trade", 0x72, false, 22, 21, "precedes an existing later trade"},
		{"same-block graduating trade", 0x73, true, 21, 21, ""},
	}

	for testIndex, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token := addressBytes(test.tokenByte)
			launchCoordinates := coordinatesForBlock(chainID, 20, baseTime, byte(0x80+testIndex*4))
			if err := insertTokenLaunch(ctx, database, launchCoordinates, token); err != nil {
				t.Fatalf("insert token launch: %v", err)
			}

			tx, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin ordering transaction: %v", err)
			}
			tradeCoordinates := coordinatesForBlock(chainID, test.tradeBlock, baseTime, byte(0x81+testIndex*4))
			graduationCoordinates := coordinatesForBlock(chainID, test.graduationBlock, baseTime, byte(0x82+testIndex*4))
			if test.tradeFirst {
				err = insertTrade(ctx, tx, tradeCoordinates, token)
				if err == nil {
					err = insertGraduation(ctx, tx, graduationCoordinates, token)
				}
			} else {
				err = insertGraduation(ctx, tx, graduationCoordinates, token)
				if err == nil {
					err = insertTrade(ctx, tx, tradeCoordinates, token)
				}
			}
			if err != nil {
				rollbackAfterFailure(t, tx)
				t.Fatalf("insert ordering fixture: %v", err)
			}

			err = tx.Commit()
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("commit same-block events: %v", err)
				}
				return
			}
			assertTriggerViolation(t, err, test.wantError)
		})
	}

	token := addressBytes(0x74)
	if err := insertTokenLaunch(ctx, database, coordinatesForBlock(chainID, 20, baseTime, 0x90), token); err != nil {
		t.Fatalf("insert committed-graduation token launch: %v", err)
	}
	if err := insertGraduation(ctx, database, coordinatesForBlock(chainID, 21, baseTime, 0x91), token); err != nil {
		t.Fatalf("insert committed graduation: %v", err)
	}
	err := insertTrade(ctx, database, coordinatesForBlock(chainID, 22, baseTime, 0x92), token)
	assertTriggerViolation(t, err, "occurs after graduation")
}

func testRollbackOrder(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	const chainID int64 = 47004
	blockTime := time.Date(2026, time.September, 4, 18, 0, 0, 0, time.UTC)
	blockHash := hashBytes(0xa1)
	mustInsertBlock(t, ctx, database, chainID, 30, blockHash, hashBytes(0xa0), blockTime, "observed")
	if err := insertTokenLaunch(ctx, database, eventCoordinates{chainID, 30, blockHash, blockTime, 0, hashBytes(0xa2), 0}, addressBytes(0xa2)); err != nil {
		t.Fatalf("insert rollback token launch: %v", err)
	}

	_, err := database.ExecContext(ctx, `DELETE FROM indexed_blocks WHERE chain_id = $1 AND block_number = $2`, chainID, 30)
	assertPostgresConstraint(t, err, "23503", "token_launches_block_fk")

	if _, err := database.ExecContext(ctx, `DELETE FROM token_launches WHERE chain_id = $1`, chainID); err != nil {
		t.Fatalf("delete event before block: %v", err)
	}
	result, err := database.ExecContext(ctx, `DELETE FROM indexed_blocks WHERE chain_id = $1 AND block_number = $2`, chainID, 30)
	if err != nil {
		t.Fatalf("delete block after event: %v", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("deleted block rows = %d, err = %v; want 1", affected, err)
	}
}

type eventCoordinates struct {
	chainID          int64
	blockNumber      int64
	blockHash        []byte
	blockTime        time.Time
	transactionIndex int
	txHash           []byte
	logIndex         int
}

type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertTokenLaunch(ctx context.Context, execer sqlExecer, coordinates eventCoordinates, token []byte) error {
	_, err := execer.ExecContext(ctx, `
		INSERT INTO token_launches (
			chain_id, block_number, block_hash, block_time,
			transaction_index, tx_hash, log_index,
			token_address, curve_address, creator, lp_pair, weth, protocol_treasury,
			engine_version, name, symbol, total_supply, virtual_eth, virtual_token,
			curve_tokens, lp_tokens, graduation_eth, launch_fee_paid,
			trade_fee_bps, protocol_share_bps
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25
		)
	`,
		coordinates.chainID, coordinates.blockNumber, coordinates.blockHash, coordinates.blockTime,
		coordinates.transactionIndex, coordinates.txHash, coordinates.logIndex,
		token, addressBytes(0xb1), addressBytes(0xb2), addressBytes(0xb3), addressBytes(0xb4), addressBytes(0xb5),
		1, "Token", "TKN", 1_000_000, 10, 1_000_000, 800_000, 200_000, 100, 1, 100, 50,
	)
	return err
}

func insertTrade(ctx context.Context, execer sqlExecer, coordinates eventCoordinates, token []byte) error {
	_, err := execer.ExecContext(ctx, `
		INSERT INTO trades (
			chain_id, block_number, block_hash, block_time,
			transaction_index, tx_hash, log_index,
			token_address, trader, is_buy, eth_gross, eth_refund, token_amount,
			protocol_fee, creator_fee, new_eth_reserve, new_token_reserve
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14, $15, $16, $17
		)
	`,
		coordinates.chainID, coordinates.blockNumber, coordinates.blockHash, coordinates.blockTime,
		coordinates.transactionIndex, coordinates.txHash, coordinates.logIndex,
		token, addressBytes(0xc1), true, 100, 0, 1000, 1, 1, 110, 999_000,
	)
	return err
}

func insertGraduation(ctx context.Context, execer sqlExecer, coordinates eventCoordinates, token []byte) error {
	_, err := execer.ExecContext(ctx, `
		INSERT INTO graduations (
			chain_id, block_number, block_hash, block_time,
			transaction_index, tx_hash, log_index,
			token_address, lp_pair, eth_to_pool, tokens_to_pool, lp_liquidity_burned
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		coordinates.chainID, coordinates.blockNumber, coordinates.blockHash, coordinates.blockTime,
		coordinates.transactionIndex, coordinates.txHash, coordinates.logIndex,
		token, addressBytes(0xd1), 100, 200_000, 1000,
	)
	return err
}

func coordinatesForBlock(chainID, blockNumber int64, baseTime time.Time, txByte byte) eventCoordinates {
	return eventCoordinates{
		chainID:          chainID,
		blockNumber:      blockNumber,
		blockHash:        hashBytes(byte(0x70 + blockNumber - 20)),
		blockTime:        baseTime.Add(time.Duration(blockNumber-20) * time.Second),
		transactionIndex: 0,
		txHash:           hashBytes(txByte),
		logIndex:         0,
	}
}

func assertConstraintExists(t testing.TB, ctx context.Context, database *sql.DB, table, constraint string) {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_constraint AS constraint_record
			JOIN pg_catalog.pg_class AS relation ON relation.oid = constraint_record.conrelid
			JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = relation.relnamespace
			WHERE namespace.nspname = 'public'
			  AND relation.relname = $1
			  AND constraint_record.conname = $2
		)
	`, table, constraint).Scan(&exists); err != nil {
		t.Fatalf("check constraint %s on %s: %v", constraint, table, err)
	}
	if !exists {
		t.Fatalf("constraint %s does not exist on %s", constraint, table)
	}
}

func assertPostgresConstraint(t testing.TB, err error, code, constraint string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected constraint %q to reject statement", constraint)
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatalf("error type = %T, want *pgconn.PgError: %v", err, err)
	}
	if postgresError.Code != code || postgresError.ConstraintName != constraint {
		t.Fatalf("constraint error = code %s constraint %q, want code %s constraint %q: %v", postgresError.Code, postgresError.ConstraintName, code, constraint, err)
	}
}

func assertTriggerViolation(t testing.TB, err error, message string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected trigger violation containing %q", message)
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatalf("error type = %T, want *pgconn.PgError: %v", err, err)
	}
	if postgresError.Code != "P0001" || !strings.Contains(postgresError.Message, message) {
		t.Fatalf("trigger error = code %s message %q, want P0001 containing %q", postgresError.Code, postgresError.Message, message)
	}
}

func rollbackAfterFailure(t testing.TB, tx *sql.Tx) {
	t.Helper()
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("rollback failed transaction: %v", err)
	}
}

func addressBytes(value byte) []byte {
	return bytes.Repeat([]byte{value}, 20)
}
