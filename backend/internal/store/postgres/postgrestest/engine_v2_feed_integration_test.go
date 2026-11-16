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

// TestEngineV2ReadsCoverV4SwapsAndFeeRecipients pins that V4 swaps of a graduated engine-2
// token show up in the reads that union base tables directly instead of using market_trades,
// that the PoolManager and locker never get a position, and that profile curve claims follow the
// engine-2 per-account fee balance.
func TestEngineV2ReadsCoverV4SwapsAndFeeRecipients(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)

	const chainID int64 = 49021
	blockTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	block := ledger.IndexedBlock{ChainID: chainID, BlockNumber: 7, BlockHash: common.Hash{0xc7}, ParentHash: common.Hash{0xc6}, BlockTime: blockTime, FinalityStatus: "observed"}
	decoded := decodeV2Fixture(t, block, common.HexToAddress("0x5a5a"))
	// The indexer registers the reviewed PoolManager at startup, before any chunk.
	if err := storepostgres.NewAdapter(pool).RegisterV4PoolManager(ctx, chainID, v2FixturePoolManager); err != nil {
		t.Fatalf("register pool manager: %v", err)
	}
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

	// The PoolManager and locker take the other side of graduation and swap transfers; neither
	// may be queued as a wallet position.
	var managers, excludedQueued int
	if err := database.DB.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM v4_pool_managers WHERE chain_id=$1),
		(SELECT count(*) FROM position_dirty AS dirty WHERE dirty.chain_id=$1 AND (
			dirty.wallet IN (SELECT pool_manager FROM v4_pool_managers WHERE chain_id=$1)
			OR dirty.wallet IN (SELECT locker FROM tokens WHERE chain_id=$1 AND locker IS NOT NULL)))`,
		chainID).Scan(&managers, &excludedQueued); err != nil {
		t.Fatalf("read excluded position marks: %v", err)
	}
	if managers != 1 || excludedQueued != 0 {
		t.Fatalf("pool managers = %d, queued PoolManager/locker positions = %d; want 1 and 0", managers, excludedQueued)
	}

	// Profile curve claims follow the per-account engine-2 balance (the fee recipient can change),
	// not "creator gets every trade fee less every claim". The fixture claims everything, so
	// leave the accepted (non-creator) recipient something owed.
	if _, err := database.DB.ExecContext(ctx, `UPDATE creator_fee_balances AS balance SET curve_eth_owed = 7
		FROM tokens AS token WHERE balance.chain_id=$1 AND token.chain_id=balance.chain_id
		AND token.token_address=balance.token_address AND balance.account <> token.creator
		AND balance.account <> token.protocol_treasury`, chainID); err != nil {
		t.Fatalf("seed recipient balance: %v", err)
	}
	balances, err :=database.DB.QueryContext(ctx, "SELECT account, curve_eth_owed::text FROM creator_fee_balances WHERE chain_id=$1", chainID)
	if err != nil {
		t.Fatalf("read creator fee balances: %v", err)
	}
	owed := map[string]string{}
	for balances.Next() {
		var account []byte
		var amount string
		if err := balances.Scan(&account, &amount); err != nil {
			t.Fatalf("scan balance: %v", err)
		}
		owed[string(account)] = amount
	}
	if err := balances.Close(); err != nil {
		t.Fatal(err)
	}
	var positive int
	for account, want := range owed {
		rows, err := queries.ListProfileActions(ctx, sqlc.ListProfileActionsParams{ChainID: chainID, Wallets: [][]byte{[]byte(account)}})
		if err != nil {
			t.Fatalf("profile actions: %v", err)
		}
		got := "0"
		if len(rows) == 1 {
			value, err := rows[0].CreatorFeeAmount.Value()
			if err != nil {
				t.Fatal(err)
			}
			got = value.(string)
		}
		if want != "0" {
			positive++
		}
		if len(rows) > 1 || got != want {
			t.Fatalf("profile creator fee for %x = %s (%d rows), want curve balance %s", account, got, len(rows), want)
		}
	}
	if len(owed) < 2 || positive == 0 {
		t.Fatalf("creator fee balances = %v, want the creator and the accepted recipient with something owed", owed)
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
