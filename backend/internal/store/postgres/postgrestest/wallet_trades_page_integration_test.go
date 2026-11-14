//go:build integration

package postgrestest

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/wallet"
	"github.com/ethereum/go-ethereum/common"
)

// TestWalletTradePagesMatchTheView pages one wallet's mixed curve and pair history through
// WalletReader.ListTrades, which bounds each page by a floor key from the base tables, and checks
// every page size against the market_trades view read in full. Another wallet's trades interleave
// with the wallet's, and two of the wallet's swaps have no Sync, so the view drops them while the
// floor query still counts them: the short-page fallback must keep the pages identical.
func TestWalletTradePagesMatchTheView(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	const chainID int64 = 46641
	const deployment = "wallet-trade-page-test"
	at := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	token, pair := addressBytes(0x72), addressBytes(0x75)
	trader, other := addressBytes(0x91), addressBytes(0x92)
	block := func(n int64) []byte { return hashBytes(byte(n)) }
	tx := func(n int64) []byte { return hashBytes(byte(100 + n)) }
	timeOf := func(n int64) time.Time { return at.Add(time.Duration(n) * time.Second) }
	const last int64 = 62
	for n := int64(1); n <= last; n++ {
		mustInsertBlock(t, ctx, database.DB, chainID, n, block(n), block(n-1), timeOf(n), "safe")
	}
	insertProjectionLaunch(t, ctx, database.DB, chainID, 1, block(1), timeOf(1), tx(1), projectionLaunchFixture{token: token, curve: addressBytes(0x74), pair: pair, weth: addressBytes(0x76)})
	for n := int64(2); n <= 21; n++ {
		insertProjectionTrade(t, ctx, database.DB, chainID, n, block(n), timeOf(n), tx(n), 1, token, 100+n, 110+n, 900000-n)
	}
	insertProjectionGraduation(t, ctx, database.DB, chainID, 22, block(22), timeOf(22), tx(22), 0, token, pair)
	for n := int64(23); n <= last; n++ {
		switch n {
		case 40, 41: // a swap whose Sync is missing: present in pool_swaps, absent from the view
			insertPoolSwap(t, ctx, database.DB, chainID, n, block(n), timeOf(n), tx(n), 1, pair, 0, 10, 5, 0)
		case 50: // two swaps in one transaction
			insertPoolSync(t, ctx, database.DB, chainID, n, block(n), timeOf(n), tx(n), 0, pair, 1000, 2000)
			insertPoolSwap(t, ctx, database.DB, chainID, n, block(n), timeOf(n), tx(n), 1, pair, 0, 10, 5, 0)
			insertPoolSync(t, ctx, database.DB, chainID, n, block(n), timeOf(n), tx(n), 2, pair, 1005, 1990)
			insertPoolSwap(t, ctx, database.DB, chainID, n, block(n), timeOf(n), tx(n), 3, pair, 5, 0, 0, 10)
		default:
			insertPoolSync(t, ctx, database.DB, chainID, n, block(n), timeOf(n), tx(n), 0, pair, 1000+n, 2000-n)
			insertPoolSwap(t, ctx, database.DB, chainID, n, block(n), timeOf(n), tx(n), 1, pair, 0, 10, 5, 0)
		}
	}
	// Every fifth curve trade and every seventh swap belong to the other wallet.
	for _, statement := range []string{
		`UPDATE trades SET trader = CASE WHEN block_number % 5 = 0 THEN $2::bytea ELSE $3::bytea END WHERE chain_id = $1`,
		`UPDATE pool_swaps SET tx_from = CASE WHEN block_number % 7 = 0 THEN $2::bytea ELSE $3::bytea END WHERE chain_id = $1`,
	} {
		if _, err := database.DB.ExecContext(ctx, statement, chainID, other, trader); err != nil {
			t.Fatal(err)
		}
	}
	callRebuild(t, ctx, database.DB, chainID, token)
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO sync_state(chain_id,deployment_id,observed_number,observed_hash,observed_at,safe_number,safe_hash,safe_at) VALUES($1,$2,$3,$4,$5,$3,$4,$5)`, chainID, deployment, last, block(last), timeOf(last)); err != nil {
		t.Fatal(err)
	}

	var want []string
	rows, err := database.DB.QueryContext(ctx, `SELECT source, block_number, transaction_index, log_index FROM market_trades WHERE chain_id = $1 AND trader = $2 ORDER BY block_number DESC, transaction_index DESC, log_index DESC`, chainID, trader)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var source string
		var b int64
		var ti, li int32
		if err := rows.Scan(&source, &b, &ti, &li); err != nil {
			t.Fatal(err)
		}
		want = append(want, fmt.Sprintf("%s:%d:%d:%d", source, b, ti, li))
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	// 16 of the 20 curve trades; of the 41 swaps, 2 have no Sync and blocks 28, 35, 42, 49, 56 belong
	// to the other wallet.
	if len(want) != 16+41-2-5 {
		t.Fatalf("view holds %d of the wallet's trades, want 50", len(want))
	}

	reader := storepostgres.WalletReader{Pool: pool, DeploymentID: deployment}
	for _, limit := range []int{1, 2, 3, 7, 19, 21, 25, 49, 50, 51, 100} {
		var got []string
		var cursor *pagination.Cursor
		for pages := 0; ; pages++ {
			if pages > len(want)+1 {
				t.Fatalf("limit %d: paging did not end", limit)
			}
			page, err := reader.ListTrades(ctx, wallet.TradesQuery{ChainID: chainID, Wallet: common.BytesToAddress(trader), Limit: limit, Cursor: cursor})
			if err != nil {
				t.Fatalf("limit %d page %d: %v", limit, pages, err)
			}
			for _, item := range page.Items {
				got = append(got, fmt.Sprintf("%s:%d:%d:%d", item.Source, item.BlockNumber, item.TransactionIndex, item.LogIndex))
			}
			if page.NextCursor == "" {
				break
			}
			decoded, err := pagination.Decode(page.NextCursor)
			if err != nil {
				t.Fatal(err)
			}
			cursor = &decoded
		}
		if !slices.Equal(got, want) {
			t.Fatalf("limit %d pages differ from the view:\n got %v\nwant %v", limit, got, want)
		}
	}
}
