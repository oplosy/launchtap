//go:build integration

package postgrestest

import (
	"context"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/feed"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

const feedChainID int64 = 48100

type feedFixture struct {
	reader feed.Reader
	token  common.Address
	pair   common.Address
	second common.Address
}

// newFeedFixture seeds one launch, one curve trade, one graduation, one pool swap with a NULL
// trader, and a later curve trade, all for a single token.
func newFeedFixture(t *testing.T, ctx context.Context, seed bool) feedFixture {
	t.Helper()
	database := NewMigrated(t)
	pool, err := pgxpool.New(ctx, database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fixture := feedFixture{reader: storepostgres.FeedReader{Pool: pool}}
	if !seed {
		return fixture
	}
	at := time.Date(2026, time.November, 6, 9, 0, 0, 0, time.UTC)
	hash10, hash11 := hashBytes(0x21), hashBytes(0x22)
	mustInsertBlock(t, ctx, database.DB, feedChainID, 10, hash10, hashBytes(0x20), at, "safe")
	mustInsertBlock(t, ctx, database.DB, feedChainID, 11, hash11, hash10, at.Add(time.Second), "safe")
	token, pair, weth := addressBytes(0x10), addressBytes(0x31), addressBytes(0x90)
	fixture.token, fixture.pair = common.BytesToAddress(token), common.BytesToAddress(pair)

	insertProjectionLaunch(t, ctx, database.DB, feedChainID, 10, hash10, at, hashBytes(0x30), projectionLaunchFixture{token: token, curve: addressBytes(0x41), pair: pair, weth: weth})
	insertProjectionTrade(t, ctx, database.DB, feedChainID, 10, hash10, at, hashBytes(0x32), 1, token, 100, 110, 900000)
	insertProjectionGraduation(t, ctx, database.DB, feedChainID, 10, hash10, at, hashBytes(0x31), 2, token, pair)
	swapTx := hashBytes(0x7f)
	insertPoolSync(t, ctx, database.DB, feedChainID, 10, hash10, at, swapTx, 3, pair, 200, 1200)
	insertPoolSwap(t, ctx, database.DB, feedChainID, 10, hash10, at, swapTx, 4, pair, 0, 100, 20, 0)
	// A curve trade cannot follow its token's graduation, so block 11 belongs to a second token.
	second := addressBytes(0x11)
	fixture.second = common.BytesToAddress(second)
	insertProjectionLaunch(t, ctx, database.DB, feedChainID, 11, hash11, at.Add(time.Second), hashBytes(0x34), projectionLaunchFixture{token: second, curve: addressBytes(0x42), pair: addressBytes(0x32), weth: weth})
	insertProjectionTrade(t, ctx, database.DB, feedChainID, 11, hash11, at.Add(time.Second), hashBytes(0x33), 1, second, 300, 410, 700000)
	// Nonzero transaction indexes so a cursor comparison that ignored them would fail.
	for _, update := range []struct {
		table   string
		txHash  []byte
		txIndex int
	}{{"trades", hashBytes(0x32), 5}, {"pool_swaps", swapTx, 3}, {"graduations", hashBytes(0x31), 7}} {
		if _, err := database.DB.ExecContext(ctx, `UPDATE `+update.table+` SET transaction_index = $1 WHERE chain_id = $2 AND tx_hash = $3`, update.txIndex, feedChainID, update.txHash); err != nil {
			t.Fatal(err)
		}
	}
	callRebuild(t, ctx, database.DB, feedChainID, token)
	callRebuild(t, ctx, database.DB, feedChainID, second)
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO sync_state(chain_id, deployment_id, observed_number, observed_hash, observed_at,
			safe_number, safe_hash, safe_at, finalized_number, finalized_hash, finalized_at)
		VALUES($1, 'feed-test', 11, $2, $3, 10, $4, $3, 8, $5, $3)`,
		feedChainID, hash11, at, hash10, hashBytes(0x19)); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestFeedReaderTradesAfterOrdersAscending(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newFeedFixture(t, ctx, true)

	all, err := f.reader.TradesAfter(ctx, feedChainID, feed.Cursor{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("trades = %d, want 3: %+v", len(all), all)
	}
	wantPositions := []feed.Cursor{{Block: 10, TxIndex: 3, LogIndex: 4}, {Block: 10, TxIndex: 5, LogIndex: 1}, {Block: 11, LogIndex: 1}}
	for i, trade := range all {
		got := feed.Cursor{Block: trade.Block, TxIndex: trade.TxIndex, LogIndex: trade.LogIndex}
		if got != wantPositions[i] {
			t.Fatalf("trade %d position = %+v, want %+v", i, got, wantPositions[i])
		}
		wantToken := f.token
		if i == 2 {
			wantToken = f.second
		}
		if trade.Token != wantToken || trade.Name != "Token" || trade.Symbol != "TKN" || trade.Finality != "safe" || trade.ETHVolume == nil || trade.TokenVolume == nil {
			t.Fatalf("trade %d = %+v", i, trade)
		}
	}
	if all[1].Source != "curve" || all[1].Trader == nil || !all[1].Buy || all[1].ETHVolume.String() != "100" {
		t.Fatalf("curve trade = %+v", all[1])
	}
	if all[0].Source != "dex" || all[0].Trader != nil || !all[0].Buy {
		t.Fatalf("pool swap = %+v", all[0])
	}

	after, err := f.reader.TradesAfter(ctx, feedChainID, feed.Cursor{Block: 10, TxIndex: 3, LogIndex: 4}, 10)
	if err != nil || len(after) != 2 || after[0].TxIndex != 5 || after[1].Block != 11 {
		t.Fatalf("after cursor = %+v err=%v", after, err)
	}
	limited, err := f.reader.TradesAfter(ctx, feedChainID, feed.Cursor{}, 1)
	if err != nil || len(limited) != 1 || limited[0].Source != "dex" {
		t.Fatalf("limited = %+v err=%v", limited, err)
	}
	if _, err := f.reader.TradesAfter(ctx, feedChainID, feed.Cursor{}, 0); err == nil {
		t.Fatal("zero limit accepted")
	}
}

func TestFeedReaderRecentTradesDescending(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newFeedFixture(t, ctx, true)

	recent, err := f.reader.RecentTrades(ctx, feedChainID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].Block != 11 || recent[1].TxIndex != 5 {
		t.Fatalf("recent = %+v", recent)
	}
}

func TestFeedReaderEmptyDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newFeedFixture(t, ctx, false)

	trade, graduation, launch, err := f.reader.Newest(ctx, feedChainID)
	if err != nil || trade != (feed.Cursor{}) || graduation != (feed.Cursor{}) || launch != (feed.Cursor{}) {
		t.Fatalf("newest = %+v %+v %+v err=%v", trade, graduation, launch, err)
	}
	trades, err := f.reader.TradesAfter(ctx, feedChainID, feed.Cursor{}, 10)
	if err != nil || len(trades) != 0 {
		t.Fatalf("trades = %+v err=%v", trades, err)
	}
	recent, err := f.reader.RecentTrades(ctx, feedChainID, 10)
	if err != nil || len(recent) != 0 {
		t.Fatalf("recent = %+v err=%v", recent, err)
	}
}

func TestFeedReaderNewestCursors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newFeedFixture(t, ctx, true)

	trade, graduation, launch, err := f.reader.Newest(ctx, feedChainID)
	if err != nil {
		t.Fatal(err)
	}
	if trade != (feed.Cursor{Block: 11, LogIndex: 1}) || graduation != (feed.Cursor{Block: 10, TxIndex: 7, LogIndex: 2}) || launch != (feed.Cursor{Block: 11}) {
		t.Fatalf("newest = %+v %+v %+v", trade, graduation, launch)
	}
}

func TestFeedReaderWatermarks(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newFeedFixture(t, ctx, true)

	got, err := f.reader.Watermarks(ctx, feedChainID, "feed-test")
	if err != nil || got != (feed.Watermarks{Safe: 10, Finalized: 8}) {
		t.Fatalf("watermarks = %+v err=%v", got, err)
	}
	missing, err := f.reader.Watermarks(ctx, feedChainID, "no-such-deployment")
	if err != nil || missing != (feed.Watermarks{}) {
		t.Fatalf("missing watermarks = %+v err=%v", missing, err)
	}
}

func TestFeedReaderGraduationsAfter(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newFeedFixture(t, ctx, true)

	rows, err := f.reader.GraduationsAfter(ctx, feedChainID, feed.Cursor{}, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("graduations = %+v err=%v", rows, err)
	}
	if rows[0].Token != f.token || rows[0].Pair != f.pair || rows[0].Name != "Token" || rows[0].Symbol != "TKN" || rows[0].Block != 10 || rows[0].TxIndex != 7 || rows[0].LogIndex != 2 {
		t.Fatalf("graduation = %+v", rows[0])
	}
	// Cursor at a later log of an earlier transaction: only the transaction index orders the graduation after it.
	later, err := f.reader.GraduationsAfter(ctx, feedChainID, feed.Cursor{Block: 10, TxIndex: 5, LogIndex: 9}, 10)
	if err != nil || len(later) != 1 {
		t.Fatalf("tuple cursor ignored transaction index: %+v err=%v", later, err)
	}
	none, err := f.reader.GraduationsAfter(ctx, feedChainID, feed.Cursor{Block: 10, TxIndex: 7, LogIndex: 2}, 10)
	if err != nil || len(none) != 0 {
		t.Fatalf("after newest = %+v err=%v", none, err)
	}
}

func TestFeedReaderLaunchesAfter(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newFeedFixture(t, ctx, true)

	rows, err := f.reader.LaunchesAfter(ctx, feedChainID, feed.Cursor{}, 10)
	if err != nil || len(rows) != 2 || rows[1].Token != f.second || rows[1].Block != 11 {
		t.Fatalf("launches = %+v err=%v", rows, err)
	}
	if rows[0].Token != f.token || rows[0].Name != "Token" || rows[0].Symbol != "TKN" || rows[0].Creator != common.BytesToAddress(addressBytes(0x55)) || rows[0].Block != 10 {
		t.Fatalf("launch = %+v", rows[0])
	}
}
