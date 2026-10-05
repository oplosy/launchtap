//go:build integration

package postgrestest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/migrations"
)

func TestMigrationsUpDownUp(t *testing.T) {
	t.Parallel()

	database := New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	firstUp, err := migrations.Run(ctx, database.DB, migrations.CommandUp)
	if err != nil {
		t.Fatalf("first migration up: %v", err)
	}
	assertMigrationResults(t, firstUp, []migrationResultWant{{version: 1, direction: "up"}, {version: 2, direction: "up"}, {version: 3, direction: "up"}, {version: 4, direction: "up"}, {version: 5, direction: "up"}, {version: 6, direction: "up"}, {version: 7, direction: "up"}, {version: 8, direction: "up"}, {version: 9, direction: "up"}, {version: 10, direction: "up"}, {version: 11, direction: "up"}, {version: 12, direction: "up"}, {version: 13, direction: "up"}, {version: 14, direction: "up"}})
	assertMigrationStates(t, ctx, database.DB, map[int64]string{1: "applied", 2: "applied", 3: "applied", 4: "applied", 5: "applied", 6: "applied", 7: "applied", 8: "applied", 9: "applied", 10: "applied", 11: "applied", 12: "applied", 13: "applied", 14: "applied"})
	assertTableExists(t, ctx, database.DB, "sync_state", true)
	assertTableExists(t, ctx, database.DB, "indexed_blocks", true)
	assertTableExists(t, ctx, database.DB, "token_launches", true)
	assertTableExists(t, ctx, database.DB, "tokens", true)

	down, err := migrations.Run(ctx, database.DB, migrations.CommandDown)
	if err != nil {
		t.Fatalf("migration down: %v", err)
	}
	assertMigrationResults(t, down, []migrationResultWant{{version: 14, direction: "down"}})
	assertMigrationStates(t, ctx, database.DB, map[int64]string{1: "applied", 2: "applied", 3: "applied", 4: "applied", 5: "applied", 6: "applied", 7: "applied", 8: "applied", 9: "applied", 10: "applied", 11: "applied", 12: "applied", 13: "applied", 14: "pending"})
	assertTableExists(t, ctx, database.DB, "sync_state", true)
	assertTableExists(t, ctx, database.DB, "indexed_blocks", true)
	assertTableExists(t, ctx, database.DB, "token_launches", true)
	assertTableExists(t, ctx, database.DB, "tokens", true)

	secondUp, err := migrations.Run(ctx, database.DB, migrations.CommandUp)
	if err != nil {
		t.Fatalf("second migration up: %v", err)
	}
	assertMigrationResults(t, secondUp, []migrationResultWant{{version: 14, direction: "up"}})
	assertMigrationStates(t, ctx, database.DB, map[int64]string{1: "applied", 2: "applied", 3: "applied", 4: "applied", 5: "applied", 6: "applied", 7: "applied", 8: "applied", 9: "applied", 10: "applied", 11: "applied", 12: "applied", 13: "applied", 14: "applied"})
	assertTableExists(t, ctx, database.DB, "sync_state", true)
	assertTableExists(t, ctx, database.DB, "indexed_blocks", true)
	assertTableExists(t, ctx, database.DB, "token_launches", true)
	assertTableExists(t, ctx, database.DB, "tokens", true)
}

func TestMetadataLaunchIdentityMigrationQuarantinesLegacyRowsAndProtectsDown(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("rollback UTC rebuild migration: %v", err)
	}

	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("rollback price-change migration: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("rollback launch identity migration: %v", err)
	}
	const chainID int64 = 46630
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	blockHash, txHash, token := hashBytes(0x91), hashBytes(0x92), addressBytes(0x93)
	mustInsertBlock(t, ctx, database.DB, chainID, 21, blockHash, hashBytes(0x90), at, "safe")
	insertProjectionLaunch(t, ctx, database.DB, chainID, 21, blockHash, at, txHash, projectionLaunchFixture{token: token, curve: addressBytes(0x94), pair: addressBytes(0x95), weth: addressBytes(0x96)})
	imageContent := []byte("legacy-image")
	imageHash := sha256.Sum256(imageContent)
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO token_metadata (chain_id, token_address, description, updated_at)
		VALUES ($1, $2, 'legacy metadata', $3)
	`, chainID, token, at); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO token_images (chain_id, token_address, content_type, content, byte_size, sha256, updated_at)
		VALUES ($1, $2, 'image/png', $3, $4, $5, $6)
	`, chainID, token, imageContent, len(imageContent), imageHash[:], at); err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandUp); err != nil {
		t.Fatalf("backfill launch identity: %v", err)
	}
	var legacyHash []byte
	var legacyLogIndex sql.NullInt64
	if err := database.DB.QueryRowContext(ctx, `SELECT launch_tx_hash, launch_log_index FROM token_metadata WHERE chain_id=$1 AND token_address=$2`, chainID, token).Scan(&legacyHash, &legacyLogIndex); err != nil {
		t.Fatal(err)
	}
	if legacyHash != nil || legacyLogIndex.Valid {
		t.Fatalf("legacy metadata was guessed onto a current launch: hash=%x log=%+v", legacyHash, legacyLogIndex)
	}
	if err := database.DB.QueryRowContext(ctx, `SELECT launch_tx_hash, launch_log_index FROM token_images WHERE chain_id=$1 AND token_address=$2`, chainID, token).Scan(&legacyHash, &legacyLogIndex); err != nil {
		t.Fatal(err)
	}
	if legacyHash != nil || legacyLogIndex.Valid {
		t.Fatalf("legacy image was guessed onto a current launch: hash=%x log=%+v", legacyHash, legacyLogIndex)
	}
	var description string
	if err := database.DB.QueryRowContext(ctx, `SELECT description FROM token_metadata WHERE chain_id=$1 AND token_address=$2`, chainID, token).Scan(&description); err != nil || description != "legacy metadata" {
		t.Fatalf("migration changed quarantined metadata: description=%q error=%v", description, err)
	}
	var gotImage []byte
	if err := database.DB.QueryRowContext(ctx, `SELECT content FROM token_images WHERE chain_id=$1 AND token_address=$2`, chainID, token).Scan(&gotImage); err != nil || !bytes.Equal(gotImage, imageContent) {
		t.Fatalf("migration changed quarantined image: content=%q error=%v", gotImage, err)
	}

	// The new launch-scoped row coexists with preserved legacy content. Down
	// refuses to collapse the two identities into the vulnerable address key.
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO token_metadata (chain_id, token_address, launch_tx_hash, launch_log_index, description, revision, updated_at)
		VALUES ($1, $2, $3, 0, 'launch-scoped metadata', 1, $4)
	`, chainID, token, txHash, at); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO token_images (chain_id, token_address, launch_tx_hash, launch_log_index, content_type, content, byte_size, sha256, revision, updated_at)
		VALUES ($1, $2, $3, 0, 'image/png', $4, $5, $6, 1, $7)
	`, chainID, token, txHash, imageContent, len(imageContent), imageHash[:], at); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
			t.Fatalf("rollback sort/index/UTC/price-change migrations before testing launch-identity guard: %v", err)
		}
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err == nil || !strings.Contains(err.Error(), "multiple metadata or image rows exist") {
		t.Fatalf("down migration with legacy and scoped rows error=%v", err)
	}
}

type migrationResultWant struct {
	version   int64
	direction string
}

func assertMigrationResults(t testing.TB, results []migrations.Result, want []migrationResultWant) {
	t.Helper()
	if len(results) != len(want) {
		t.Fatalf("migration result count = %d, want %d: %+v", len(results), len(want), results)
	}
	for index := range want {
		if results[index].Version != want[index].version ||
			results[index].Direction != want[index].direction ||
			results[index].State != "complete" {
			t.Fatalf("migration result %d = %+v, want version=%d direction=%s state=complete", index, results[index], want[index].version, want[index].direction)
		}
	}
}

func assertMigrationStates(t testing.TB, ctx context.Context, database *sql.DB, want map[int64]string) {
	t.Helper()
	statuses, err := migrations.Run(ctx, database, migrations.CommandStatus)
	if err != nil {
		t.Fatalf("migration status: %v", err)
	}
	if len(statuses) != len(want) {
		t.Fatalf("migration status count = %d, want %d: %+v", len(statuses), len(want), statuses)
	}
	for _, status := range statuses {
		if wantState, ok := want[status.Version]; !ok || status.State != wantState {
			t.Fatalf("unexpected migration status: %+v; want %+v", status, want)
		}
	}
}

func assertTableExists(t testing.TB, ctx context.Context, database *sql.DB, table string, want bool) {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_class AS relation
			JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = relation.relnamespace
			WHERE namespace.nspname = 'public'
			  AND relation.relname = $1
			  AND relation.relkind = 'r'
		)
	`, table).Scan(&exists); err != nil {
		t.Fatalf("check table %q existence: %v", table, err)
	}
	if exists != want {
		t.Fatalf("table %q exists = %t, want %t", table, exists, want)
	}
}
