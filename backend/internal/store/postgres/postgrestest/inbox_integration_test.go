//go:build integration

package postgrestest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/inbox"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/ethereum/go-ethereum/common"
)

const inboxDeployment = "inbox-dep"

const halfETH int64 = 500_000_000_000_000_000

// newInboxFixture launches the position token (0x10, curve 0x41, pair 0x31) and an unwatched
// token (0x12) at block 10, whose time is `at`; later blocks follow one minute apart.
func newInboxFixture(t *testing.T, ctx context.Context, at time.Time) positionLedger {
	t.Helper()
	database := NewMigrated(t)
	pool := openPool(t, ctx, database.URL)
	hash := hashBytes(0x21)
	mustInsertBlock(t, ctx, database.DB, walletChainID, 10, hash, hashBytes(0x20), at, "safe")
	pair := addressBytes(0x31)
	insertProjectionLaunch(t, ctx, database.DB, walletChainID, 10, hash, at, hashBytes(0x30), projectionLaunchFixture{token: positionToken, curve: addressBytes(0x41), pair: pair, weth: addressBytes(0x90)})
	insertProjectionLaunch(t, ctx, database.DB, walletChainID, 10, hash, at, hashBytes(0x3a), projectionLaunchFixture{token: addressBytes(0x12), curve: addressBytes(0x42), pair: addressBytes(0x32), weth: addressBytes(0x90)})
	f := walletSchemaFixture{database: database, pool: pool, reader: storepostgres.FeedReader{Pool: pool}, pair: common.BytesToAddress(pair), at: at, hash: hash}
	return positionLedger{t: t, ctx: ctx, f: f}
}

func (l positionLedger) watch(deployment, did string, token []byte, createdAt time.Time) {
	l.t.Helper()
	l.exec(`INSERT INTO watchlist_entries (chain_id, deployment_id, privy_did, token_address, created_at) VALUES ($1, $2, $3, $4, $5)`,
		walletChainID, deployment, did, token, createdAt)
}

func (l positionLedger) tradeOn(token []byte, block int64, txIndex int, txHash byte, ethGross int64) {
	l.t.Helper()
	l.exec(`INSERT INTO trades (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
		token_address, trader, is_buy, eth_gross, eth_refund, token_amount, protocol_fee, creator_fee, new_eth_reserve, new_token_reserve)
		VALUES ($1, $2, $3, $4, $5, $6, 0, $7, $8, true, $9, 0, 10, 0, 0, 1000, 1000)`,
		walletChainID, block, l.blockHash(block), l.blockTime(block), txIndex, hashBytes(txHash), token, addressBytes(0xc1), ethGross)
}

func inboxIDs(page inbox.Page) []string {
	out := make([]string, len(page.Items))
	for i, e := range page.Items {
		out[i] = e.Kind + "@" + e.Position.String()
	}
	return out
}

// Spec P0-C4 AC1-AC4.
func TestInboxDerivedFromWatchlistAndLedger(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	l := newInboxFixture(t, ctx, time.Now().UTC().Truncate(time.Second).Add(-2*time.Hour))
	for block := int64(11); block <= 15; block++ {
		l.block(block, "safe")
	}
	me := watchlist.Owner{ChainID: walletChainID, DeploymentID: inboxDeployment, PrivyDID: "did:me"}
	l.watch(inboxDeployment, "did:me", positionToken, l.blockTime(11).Add(30*time.Second))
	// Another account and another deployment watch the second token: never part of my inbox.
	l.watch(inboxDeployment, "did:other", addressBytes(0x12), l.blockTime(10))
	l.watch("other-dep", "did:me", addressBytes(0x12), l.blockTime(10))

	l.tradeOn(positionToken, 11, 0, 0x81, 6*halfETH/5)    // before the watch started
	l.tradeOn(positionToken, 12, 0, 0x82, 4*halfETH/5)    // under 0.5 ETH
	l.tradeOn(positionToken, 12, 1, 0x83, halfETH)        // exactly 0.5 ETH: listed
	l.tradeOn(addressBytes(0x12), 12, 2, 0x84, 2*halfETH) // a token this account does not watch
	insertProjectionGraduation(t, ctx, l.f.database.DB, walletChainID, 13, l.blockHash(13), l.blockTime(13), hashBytes(0x8f), 0, positionToken, l.f.pair.Bytes())
	insertPoolSync(t, ctx, l.f.database.DB, walletChainID, 13, l.blockHash(13), l.blockTime(13), hashBytes(0x8f), 1, l.f.pair.Bytes(), 200, 1200)
	callRebuild(t, ctx, l.f.database.DB, walletChainID, positionToken)
	// Pool buy: 0.7 WETH (token1) in; pool sell: 0.8 WETH out, sender unknown.
	// market_trades prices a DEX swap from the Sync just before it in the same transaction.
	insertPoolSync(t, ctx, l.f.database.DB, walletChainID, 14, l.blockHash(14), l.blockTime(14), hashBytes(0x85), 0, l.f.pair.Bytes(), 190, 1300)
	l.swap(14, 0, 0x85, 1, 0, 7*halfETH/5, 10, 0, 0xc2, 0xc2)
	insertPoolSync(t, ctx, l.f.database.DB, walletChainID, 15, l.blockHash(15), l.blockTime(15), hashBytes(0x86), 0, l.f.pair.Bytes(), 200, 1200)
	l.swap(15, 0, 0x86, 1, 10, 0, 0, 8*halfETH/5, 0xc3, 0xc3)
	l.exec(`UPDATE pool_swaps SET tx_from = NULL WHERE chain_id = $1 AND block_number = 15`, walletChainID)

	store := storepostgres.InboxStore{Pool: l.f.pool}
	first, err := store.List(ctx, me, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"large_trade@15-0-1", "large_trade@14-0-1"}; !slices.Equal(inboxIDs(first), want) || first.Next == nil || first.Unread != 4 {
		t.Fatalf("first page = %v next=%v unread=%d, want %v unread 4", inboxIDs(first), first.Next, first.Unread, want)
	}
	sell, buy := first.Items[0], first.Items[1]
	if sell.Trader != nil || sell.Source != "dex" || sell.SideBuy == nil || *sell.SideBuy || sell.ETHWei.Int64() != 8*halfETH/5 {
		t.Fatalf("pool sell = %+v", sell)
	}
	if buy.Trader == nil || *buy.Trader != common.BytesToAddress(addressBytes(0xc2)) || buy.SideBuy == nil || !*buy.SideBuy || buy.Symbol == "" {
		t.Fatalf("pool buy = %+v", buy)
	}
	second, err := store.List(ctx, me, first.Next, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"graduation@13-0-0", "large_trade@12-1-0"}; !slices.Equal(inboxIDs(second), want) || second.Next != nil {
		t.Fatalf("second page = %v next=%v, want %v and no next", inboxIDs(second), second.Next, want)
	}
	if !second.Items[1].BlockTime.Equal(l.blockTime(12)) || second.Items[1].Source != "curve" {
		t.Fatalf("curve trade = %+v", second.Items[1])
	}

	// AC3: marking read moves forward only.
	if err := store.MarkRead(ctx, me, second.Items[0].Position); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRead(ctx, me, second.Items[1].Position); err != nil {
		t.Fatal(err)
	}
	if page, err := store.List(ctx, me, nil, 10); err != nil || page.Unread != 2 || len(page.Items) != 4 {
		t.Fatalf("after mark read: unread=%d items=%d err=%v", page.Unread, len(page.Items), err)
	}
	// Another account's inbox and read position are separate.
	other := watchlist.Owner{ChainID: walletChainID, DeploymentID: inboxDeployment, PrivyDID: "did:other"}
	if page, err := store.List(ctx, other, nil, 10); err != nil || page.Unread != 1 || len(page.Items) != 1 {
		t.Fatalf("other account: %v unread=%d err=%v", inboxIDs(page), page.Unread, err)
	}

	// AC4: a reorg that deletes block 15 removes its trade from the next read.
	if err := storepostgres.WithinTx(ctx, l.f.pool, func(ctx context.Context, a *storepostgres.Adapter) error {
		return a.DeleteCanonicalAbove(ctx, walletChainID, 14)
	}); err != nil {
		t.Fatal(err)
	}
	page, err := store.List(ctx, me, nil, 10)
	if want := []string{"large_trade@14-0-1", "graduation@13-0-0", "large_trade@12-1-0"}; err != nil || !slices.Equal(inboxIDs(page), want) || page.Unread != 1 {
		t.Fatalf("after reorg = %v unread=%d err=%v, want %v unread 1", inboxIDs(page), page.Unread, err, want)
	}
}

// Spec P0-C4 AC1: events older than 30 days are not listed even when the watch is older.
func TestInboxIgnoresEventsOlderThan30Days(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	l := newInboxFixture(t, ctx, time.Now().UTC().Truncate(time.Second).Add(-31*24*time.Hour))
	l.block(11, "safe")
	l.watch(inboxDeployment, "did:me", positionToken, l.blockTime(10).Add(-time.Hour))
	l.tradeOn(positionToken, 11, 0, 0x81, 4*halfETH)
	owner := watchlist.Owner{ChainID: walletChainID, DeploymentID: inboxDeployment, PrivyDID: "did:me"}
	page, err := storepostgres.InboxStore{Pool: l.f.pool}.List(ctx, owner, nil, 10)
	if err != nil || len(page.Items) != 0 || page.Unread != 0 {
		t.Fatalf("old events listed: %v unread=%d err=%v", inboxIDs(page), page.Unread, err)
	}
}
