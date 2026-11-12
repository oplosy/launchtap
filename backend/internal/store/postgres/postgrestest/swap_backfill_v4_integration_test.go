//go:build integration

package postgrestest

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/swapbackfill"
	"github.com/ethereum/go-ethereum/common"
)

// V4 swaps ingested without a sender are filled by the same backfill as v1 pool swaps, and the
// graduated token's aggregate is re-marked (the trader is market_trades.trader); a rerun is a
// no-op.
func TestBackfillFillsV4SwapSenders(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	adapter := storepostgres.NewAdapter(pool)

	const chainID int64 = 49031
	block := ledger.IndexedBlock{ChainID: chainID, BlockNumber: 9, BlockHash: common.Hash{0xc9}, ParentHash: common.Hash{0xc8}, BlockTime: time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC), FinalityStatus: "observed"}
	decoded := decodeV2Fixture(t, block, common.HexToAddress("0x0b0b"))
	if err := adapter.RegisterV4PoolManager(ctx, chainID, v2FixturePoolManager); err != nil {
		t.Fatalf("register pool manager: %v", err)
	}
	var token common.Address
	for _, log := range decoded {
		if launch, ok := log.Value.(chain.TokenLaunchedV2); ok {
			token = launch.Token
		}
	}
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
		if _, err := adapter.UpsertIndexedBlock(ctx, block); err != nil {
			return err
		}
		return indexer.LedgerRouter{ChainID: chainID}.Apply(ctx, adapter, decoded[:29], map[int64]ledger.IndexedBlock{block.BlockNumber: block}, nil)
	}); err != nil {
		t.Fatalf("ingest fixture logs: %v", err)
	}
	// The fixture knows every sender; drop them to model a missed live lookup.
	if _, err := database.DB.ExecContext(ctx, `UPDATE v4_swaps SET tx_from = NULL WHERE chain_id = $1`, chainID); err != nil {
		t.Fatal(err)
	}

	nullV4Hashes := func() []common.Hash {
		t.Helper()
		rows, err := database.DB.QueryContext(ctx, `SELECT DISTINCT tx_hash FROM v4_swaps WHERE chain_id = $1 AND tx_from IS NULL`, chainID)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var hashes []common.Hash
		for rows.Next() {
			var hash []byte
			if err := rows.Scan(&hash); err != nil {
				t.Fatal(err)
			}
			hashes = append(hashes, common.BytesToHash(hash))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return hashes
	}
	pending := nullV4Hashes()
	if len(pending) == 0 {
		t.Fatal("fixture has no V4 swap without a sender")
	}
	if _, err := database.DB.ExecContext(ctx, `DELETE FROM aggregation_dirty WHERE chain_id = $1`, chainID); err != nil {
		t.Fatal(err)
	}

	store := swapbackfill.PostgresStore{Pool: pool, ChainID: chainID, Factory: common.HexToAddress("0x0b0b")}
	if count, err := store.CountUnattributed(ctx, block.BlockNumber); err != nil || count < int64(len(pending)) {
		t.Fatalf("CountUnattributed = %d, %v; want at least %d V4 swaps", count, err, len(pending))
	}
	trader := common.HexToAddress("0x00000000000000000000000000000000000000d7")
	senders := mapSenders{}
	for _, hash := range pending {
		senders[hash] = trader
	}
	run := func() swapbackfill.Summary {
		t.Helper()
		summary, err := swapbackfill.Run(ctx, swapbackfill.Deps{
			Store:   store,
			Senders: senders,
			Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
			Sleep:   func(context.Context) error { return nil },
		}, swapbackfill.Options{Batch: 2})
		if err != nil {
			t.Fatalf("backfill: %v", err)
		}
		return summary
	}
	summary := run()
	if left := nullV4Hashes(); len(left) != 0 || summary.Updated == 0 {
		t.Fatalf("summary = %+v, V4 swaps still without a sender = %d", summary, len(left))
	}
	var traders int
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM market_trades WHERE chain_id = $1 AND token_address = $2 AND trader = $3`, chainID, token[:], trader[:]).Scan(&traders); err != nil || traders == 0 {
		t.Fatalf("market_trades attributed to the trader = %d, %v; want the V4 swaps", traders, err)
	}
	var generation int64
	if err := database.DB.QueryRowContext(ctx, `SELECT generation FROM aggregation_dirty WHERE chain_id = $1 AND token_address = $2`, chainID, token[:]).Scan(&generation); err != nil {
		t.Fatalf("token aggregate not re-marked after the V4 backfill: %v", err)
	}

	if again := run(); again.Listed != 0 || again.Updated != 0 {
		t.Fatalf("second run summary = %+v, want nothing", again)
	}
	var after int64
	if err := database.DB.QueryRowContext(ctx, `SELECT generation FROM aggregation_dirty WHERE chain_id = $1 AND token_address = $2`, chainID, token[:]).Scan(&after); err != nil || after != generation {
		t.Fatalf("second run re-marked the token: generation %d -> %d (%v)", generation, after, err)
	}
}
