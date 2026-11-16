//go:build integration

package postgrestest

import (
	"context"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestEngineV2SwapsReachFeedTrendingAndAttribution pins that V4 swaps of a graduated engine-2
// token show up in the reads that union base tables directly instead of using market_trades.
func TestEngineV2SwapsReachFeedTrendingAndAttribution(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)

	const chainID int64 = 49021
	blockTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	block := ledger.IndexedBlock{ChainID: chainID, BlockNumber: 7, BlockHash: common.Hash{0xc7}, ParentHash: common.Hash{0xc6}, BlockTime: blockTime, FinalityStatus: "observed"}
	decoded := decodeV2Fixture(t, block, common.HexToAddress("0x5a5a"))
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
		if _, err := adapter.UpsertIndexedBlock(ctx, block); err != nil {
			return err
		}
		return indexer.LedgerRouter{ChainID: chainID}.Apply(ctx, adapter, decoded, map[int64]ledger.IndexedBlock{block.BlockNumber: block}, nil)
	}); err != nil {
		t.Fatalf("ingest v2 fixture: %v", err)
	}
	queries := sqlc.New(pool)

	recent, err := queries.ListRecentMarketTrades(ctx, sqlc.ListRecentMarketTradesParams{ChainID: chainID, PageSize: 50})
	if err != nil {
		t.Fatalf("recent trades: %v", err)
	}
	after, err := queries.ListMarketTradesAfter(ctx, sqlc.ListMarketTradesAfterParams{ChainID: chainID, AfterBlock: -1, PageSize: 50})
	if err != nil {
		t.Fatalf("trades after: %v", err)
	}
	for name, sources := range map[string][]string{"recent": sourcesOf(recent), "after": sourcesOfAfter(after)} {
		if dex := countOf(sources, "dex"); len(sources) != 6 || dex != 2 {
			t.Fatalf("%s feed sources = %v, want 4 curve trades and 2 V4 swaps", name, sources)
		}
	}

	var lastSwapTxIndex, lastSwapLogIndex int32
	if err := database.DB.QueryRowContext(ctx, "SELECT transaction_index, log_index FROM v4_swaps WHERE chain_id=$1 ORDER BY transaction_index DESC, log_index DESC LIMIT 1", chainID).Scan(&lastSwapTxIndex, &lastSwapLogIndex); err != nil {
		t.Fatalf("read last swap: %v", err)
	}
	cursors, err := queries.GetNewestFeedCursors(ctx, chainID)
	if err != nil {
		t.Fatalf("newest cursors: %v", err)
	}
	if cursors.TradeTransactionIndex != lastSwapTxIndex || cursors.TradeLogIndex != lastSwapLogIndex {
		t.Fatalf("newest trade cursor = (%d,%d), want the last V4 swap (%d,%d)", cursors.TradeTransactionIndex, cursors.TradeLogIndex, lastSwapTxIndex, lastSwapLogIndex)
	}

	var wantVolume string
	if err := database.DB.QueryRowContext(ctx, `SELECT ((SELECT sum(eth_gross) FROM trades WHERE chain_id=$1)
		+ (SELECT sum(abs(amount0_delta)) FROM v4_swaps WHERE chain_id=$1))::text`, chainID).Scan(&wantVolume); err != nil {
		t.Fatalf("read expected volume: %v", err)
	}
	trending, err := queries.ListTrendingTokens(ctx, sqlc.ListTrendingTokensParams{ChainID: chainID, RowLimit: 10, WindowStart: pgtype.Timestamptz{Time: blockTime.Add(-time.Minute), Valid: true}})
	if err != nil {
		t.Fatalf("trending: %v", err)
	}
	if len(trending) != 1 || trending[0].WindowVolumeEthWad.BigInt().String() != wantVolume {
		t.Fatalf("trending = %+v, want one token with curve + V4 volume %s", trending, wantVolume)
	}

	if complete, err := queries.WalletAttributionComplete(ctx, chainID); err != nil || !complete {
		t.Fatalf("attribution complete = %t, %v; want true with every V4 sender known", complete, err)
	}
	if _, err := database.DB.ExecContext(ctx, "UPDATE v4_swaps SET tx_from = NULL WHERE chain_id=$1", chainID); err != nil {
		t.Fatalf("clear v4 sender: %v", err)
	}
	if complete, err := queries.WalletAttributionComplete(ctx, chainID); err != nil || complete {
		t.Fatalf("attribution complete = %t, %v; want false while a V4 sender is unknown", complete, err)
	}
}

func sourcesOf(rows []sqlc.ListRecentMarketTradesRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Source)
	}
	return out
}

func sourcesOfAfter(rows []sqlc.ListMarketTradesAfterRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Source)
	}
	return out
}

func countOf(values []string, want string) int {
	n := 0
	for _, value := range values {
		if value == want {
			n++
		}
	}
	return n
}
