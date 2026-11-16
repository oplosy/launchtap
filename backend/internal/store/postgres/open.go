package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Open creates a database handle backed by pgx's database/sql adapter.
// Callers must close the returned handle.
func Open(databaseURL string) (*sql.DB, error) {
	return sql.Open("pgx", databaseURL)
}

const defaultPoolMaxConns int32 = 8

// MinimumPoolMaxConns is the smallest pool OpenPool accepts. Single-purpose tools open their
// pool at exactly this size so the floor and their callers cannot drift apart.
const MinimumPoolMaxConns int32 = 4

// PoolOptions configures the application pool. Session ownership uses a separate
// connection, outside pool lifetime and idle policies; MaxConns remains fully
// available for aggregation, reads, and health work.
type PoolOptions struct{ MaxConns int32 }

// OpenPool opens the pgx pool used by application transactions, never migrations.
func OpenPool(ctx context.Context, databaseURL string, options PoolOptions) (*pgxpool.Pool, error) {
	maxConns := options.MaxConns
	if maxConns == 0 {
		maxConns = defaultPoolMaxConns
	}
	if maxConns < MinimumPoolMaxConns {
		return nil, fmt.Errorf("pool max connections %d is below minimum %d", maxConns, MinimumPoolMaxConns)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}
	config.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	return pool, nil
}
