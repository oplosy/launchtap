//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"testing"
	"time"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// OpenPool's StatementTimeout reaches every pool connection: Postgres cancels a statement that
// runs longer (SQLSTATE 57014), and a pool without it does not.
func TestOpenPoolStatementTimeoutCancelsLongStatements(t *testing.T) {
	database := New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	bounded, err := storepostgres.OpenPool(ctx, database.URL, storepostgres.PoolOptions{StatementTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer bounded.Close()
	_, err = bounded.Exec(ctx, `SELECT pg_sleep(1)`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" {
		t.Fatalf("long statement on a bounded pool = %v, want SQLSTATE 57014", err)
	}

	unbounded, err := storepostgres.OpenPool(ctx, database.URL, storepostgres.PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer unbounded.Close()
	if _, err := unbounded.Exec(ctx, `SELECT pg_sleep(0.3)`); err != nil {
		t.Fatalf("statement on an unbounded pool = %v", err)
	}
}
