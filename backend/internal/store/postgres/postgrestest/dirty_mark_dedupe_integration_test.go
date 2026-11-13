//go:build integration

package postgrestest

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// countingDB counts the statements an adapter sends inside one transaction.
type countingDB struct {
	tx         pgx.Tx
	statements int
}

func (db *countingDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	db.statements++
	return db.tx.Exec(ctx, sql, args...)
}

func (db *countingDB) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	db.statements++
	return db.tx.Query(ctx, sql, args...)
}

func (db *countingDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	db.statements++
	return db.tx.QueryRow(ctx, sql, args...)
}

// TestDirtyMarkDedupeMarksTheSameRows ingests the projection fixture, plus pool swaps with a known
// sender, once with a plain adapter and once with de-duplication, and requires identical dirty
// rows and projections from fewer statements (pre-flight 2026-11-13-indexer-dirty-mark-dedupe).
func TestDirtyMarkDedupeMarksTheSameRows(t *testing.T) {
	plain, plainStatements := ingestDirtyMarkFixture(t, false)
	deduped, dedupedStatements := ingestDirtyMarkFixture(t, true)
	if plain != deduped {
		t.Fatalf("de-duplicated ingest marked different rows\nplain=%s\ndeduped=%s", plain, deduped)
	}
	if dedupedStatements >= plainStatements {
		t.Fatalf("de-duplicated ingest sent %d statements, plain %d; want fewer", dedupedStatements, plainStatements)
	}
	t.Logf("statements: plain %d, de-duplicated %d", plainStatements, dedupedStatements)
}

func ingestDirtyMarkFixture(t *testing.T, dedupe bool) (string, int) {
	t.Helper()
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	fixture := newProjectionDifferentialFixture()
	fixture.chunks[3].events = append(fixture.chunks[3].events, knownSenderPoolSwaps(fixture.chainID)...)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	counter := &countingDB{tx: tx}
	adapter := storepostgres.NewAdapter(counter)
	if dedupe {
		adapter = adapter.WithDirtyMarkDedupe()
	}
	for _, chunk := range fixture.chunks {
		if _, err := adapter.UpsertIndexedBlock(ctx, chunk.block); err != nil {
			t.Fatalf("upsert block: %v", err)
		}
		for _, ingest := range chunk.events {
			if _, err := ingest(ctx, adapter); err != nil {
				t.Fatalf("ingest: %v", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	snapshot := readProjectionSnapshot(t, ctx, database, fixture.chainID, fixture.token)
	return fmt.Sprintf("aggregation=%s positions=%s projections=%+v",
		readDirtyKeys(t, ctx, database, `SELECT chain_id, encode(token_address, 'hex') FROM aggregation_dirty ORDER BY 1, 2`),
		readDirtyKeys(t, ctx, database, `SELECT chain_id, encode(wallet, 'hex') || ':' || encode(token_address, 'hex') FROM position_dirty ORDER BY 1, 2`),
		snapshot), counter.statements
}

// knownSenderPoolSwaps adds two post-graduation swaps sent by the same wallet in block 4.
func knownSenderPoolSwaps(chainID int64) []func(context.Context, *storepostgres.Adapter) (ledger.InsertResult, error) {
	sender := common.Address{19: 13}
	pair := common.Address{19: 3}
	at := time.Date(2026, 9, 6, 12, 1, 0, 0, time.UTC)
	var events []func(context.Context, *storepostgres.Adapter) (ledger.InsertResult, error)
	for i := range 2 {
		swap := ledger.PoolSwap{
			EventCoordinates: ledger.EventCoordinates{ChainID: chainID, BlockNumber: 4, BlockHash: common.Hash{31: 4}, BlockTime: at, TransactionIndex: int32(6 + i), TxHash: common.Hash{30: byte(6 + i)}, LogIndex: int32(10 + i)},
			Pair:             pair, Sender: common.Address{19: 9}, TxFrom: &sender,
			Amount0In: big.NewInt(1), Amount1In: big.NewInt(0), Amount0Out: big.NewInt(0), Amount1Out: big.NewInt(1), To: sender,
		}
		events = append(events, func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestPoolSwap(ctx, swap)
		})
	}
	return events
}

func readDirtyKeys(t testing.TB, ctx context.Context, database *Database, query string) string {
	t.Helper()
	rows, err := database.DB.QueryContext(ctx, query)
	if err != nil {
		t.Fatalf("read dirty keys: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var chainID int64
		var key string
		if err := rows.Scan(&chainID, &key); err != nil {
			t.Fatalf("scan dirty key: %v", err)
		}
		keys = append(keys, fmt.Sprintf("%d/%s", chainID, key))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate dirty keys: %v", err)
	}
	return strings.Join(keys, ",")
}
