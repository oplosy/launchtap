//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"math/big"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

const walletChainID int64 = 48200

type walletSchemaFixture struct {
	database *Database
	pool     *pgxpool.Pool
	reader   storepostgres.FeedReader
	pair     common.Address
	at       time.Time
	hash     []byte
}

// newWalletSchemaFixture seeds one launched token with an opening pool sync so that ingested
// pool swaps appear in market_trades.
func newWalletSchemaFixture(t *testing.T, ctx context.Context) walletSchemaFixture {
	t.Helper()
	database := NewMigrated(t)
	pool := openPool(t, ctx, database.URL)
	at := time.Date(2026, time.November, 6, 9, 0, 0, 0, time.UTC)
	hash := hashBytes(0x21)
	mustInsertBlock(t, ctx, database.DB, walletChainID, 10, hash, hashBytes(0x20), at, "safe")
	token, pair, weth := addressBytes(0x10), addressBytes(0x31), addressBytes(0x90)
	insertProjectionLaunch(t, ctx, database.DB, walletChainID, 10, hash, at, hashBytes(0x30), projectionLaunchFixture{token: token, curve: addressBytes(0x41), pair: pair, weth: weth})
	insertProjectionGraduation(t, ctx, database.DB, walletChainID, 10, hash, at, hashBytes(0x31), 2, token, pair)
	insertPoolSync(t, ctx, database.DB, walletChainID, 10, hash, at, hashBytes(0x7f), 3, pair, 200, 1200)
	callRebuild(t, ctx, database.DB, walletChainID, token)
	return walletSchemaFixture{database: database, pool: pool, reader: storepostgres.FeedReader{Pool: pool}, pair: common.BytesToAddress(pair), at: at, hash: hash}
}

func (f walletSchemaFixture) swap(txByte byte, txFrom *common.Address) ledger.PoolSwap {
	return ledger.PoolSwap{
		EventCoordinates: ledger.EventCoordinates{ChainID: walletChainID, BlockNumber: 10, BlockHash: common.BytesToHash(f.hash), BlockTime: f.at, TxHash: common.BytesToHash(hashBytes(txByte)), LogIndex: 4},
		Pair:             f.pair, Sender: common.Address{19: 0xa1}, To: common.Address{19: 0xa2},
		Amount0In: big.NewInt(0), Amount1In: big.NewInt(100), Amount0Out: big.NewInt(20), Amount1Out: big.NewInt(0),
		TxFrom: txFrom,
	}
}

func (f walletSchemaFixture) ingest(t *testing.T, ctx context.Context, swap ledger.PoolSwap) error {
	t.Helper()
	return storepostgres.WithinTx(ctx, f.pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
		_, err := adapter.IngestPoolSwap(ctx, swap)
		return err
	})
}

func (f walletSchemaFixture) trader(t *testing.T, ctx context.Context, txByte byte) sql.RawBytes {
	t.Helper()
	var trader []byte
	if err := f.database.DB.QueryRowContext(ctx, `SELECT trader FROM market_trades WHERE chain_id = $1 AND tx_hash = $2 AND source = 'dex'`, walletChainID, hashBytes(txByte)).Scan(&trader); err != nil {
		t.Fatalf("read market_trades trader: %v", err)
	}
	return trader
}

func TestPoolSwapStoresTxFrom(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	sender := common.Address{19: 0xb7}
	if err := f.ingest(t, ctx, f.swap(0x7f, &sender)); err != nil {
		t.Fatal(err)
	}
	if got := f.trader(t, ctx, 0x7f); string(got) != string(sender.Bytes()) {
		t.Fatalf("trader = %x, want %x", []byte(got), sender.Bytes())
	}
}

func TestPoolSwapNullTxFrom(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	if err := f.ingest(t, ctx, f.swap(0x7f, nil)); err != nil {
		t.Fatal(err)
	}
	if got := f.trader(t, ctx, 0x7f); got != nil {
		t.Fatalf("trader = %x, want NULL", []byte(got))
	}
}

func TestPoolSwapReplayIgnoresTxFrom(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	sender := common.Address{19: 0xb7}
	if err := f.ingest(t, ctx, f.swap(0x7f, &sender)); err != nil {
		t.Fatal(err)
	}
	if err := f.ingest(t, ctx, f.swap(0x7f, nil)); err != nil {
		t.Fatalf("replay with unknown sender: %v", err)
	}
	if got := f.trader(t, ctx, 0x7f); string(got) != string(sender.Bytes()) {
		t.Fatalf("trader after replay = %x, want %x", []byte(got), sender.Bytes())
	}
}

func TestRecentTradesCarryPoolTrader(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	sender := common.Address{19: 0xb7}
	if err := f.ingest(t, ctx, f.swap(0x7f, &sender)); err != nil {
		t.Fatal(err)
	}
	recent, err := f.reader.RecentTrades(ctx, walletChainID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].Source != "dex" || recent[0].Trader == nil || *recent[0].Trader != sender {
		t.Fatalf("recent = %+v", recent)
	}
}
