//go:build integration

package postgrestest

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/apiserver"
	"github.com/Contictus/launchtap/backend/internal/pagination"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/wallet"
	"github.com/ethereum/go-ethereum/common"
)

const walletReaderDeployment = "wallet-reader-test"

// Fixture tokens: A (0x10, launched block 10), B (0x11, block 11), C (0x12, block 12); all created by 0x55.
// Wallets: W = 0xa0 holds all three, W2 = 0xa2 has only closed positions.
var (
	walletTokenA, walletTokenB, walletTokenC = byte(0x10), byte(0x11), byte(0x12)
	walletW, walletW2                        = byte(0xa0), byte(0xa2)
)

type walletReaderFixture struct {
	walletSchemaFixture
	ledger positionLedger
	reader storepostgres.WalletReader
}

func newWalletReaderFixture(t *testing.T, ctx context.Context) walletReaderFixture {
	t.Helper()
	f := newWalletSchemaFixture(t, ctx)
	l := positionLedger{t: t, ctx: ctx, f: f}
	l.block(11, "safe")
	l.block(12, "safe")
	weth := addressBytes(0x90)
	insertProjectionLaunch(t, ctx, f.database.DB, walletChainID, 11, l.blockHash(11), l.blockTime(11), hashBytes(0x35), projectionLaunchFixture{token: addressBytes(walletTokenB), curve: addressBytes(0x42), pair: addressBytes(0x32), weth: weth})
	insertProjectionLaunch(t, ctx, f.database.DB, walletChainID, 12, l.blockHash(12), l.blockTime(12), hashBytes(0x36), projectionLaunchFixture{token: addressBytes(walletTokenC), curve: addressBytes(0x43), pair: addressBytes(0x33), weth: weth})
	callRebuild(t, ctx, f.database.DB, walletChainID, addressBytes(walletTokenB))
	callRebuild(t, ctx, f.database.DB, walletChainID, addressBytes(walletTokenC))
	l.exec(`INSERT INTO sync_state (chain_id, deployment_id, observed_number, observed_hash, observed_at, safe_number, safe_hash, safe_at, finalized_number, finalized_hash, finalized_at)
		VALUES ($1, $2, 12, $3, $4, 12, $3, $4, 10, $5, $4)`, walletChainID, walletReaderDeployment, l.blockHash(12), l.blockTime(12), f.hash)
	fixture := walletReaderFixture{walletSchemaFixture: f, ledger: l, reader: storepostgres.WalletReader{Pool: f.pool, DeploymentID: walletReaderDeployment}}
	// Controlled reserves: A 1000/300 (value of 100 = 333, floored), B 1000/250 (400), C has no row.
	l.exec(`DELETE FROM token_reserves WHERE chain_id = $1`, walletChainID)
	fixture.reserves(walletTokenA, 1000, 300)
	fixture.reserves(walletTokenB, 1000, 250)
	return fixture
}

func (f walletReaderFixture) reserves(token byte, eth, tokens int64) {
	f.ledger.exec(`DELETE FROM token_reserves WHERE chain_id = $1 AND token_address = $2`, walletChainID, addressBytes(token))
	f.ledger.exec(`INSERT INTO token_reserves (chain_id, token_address, reserve_source, eth_reserve, token_reserve, source_block_number, source_block_hash, source_block_time, source_tx_hash, source_log_index)
		VALUES ($1, $2, 'curve', $3, $4, 10, $5, $6, $7, 0)`, walletChainID, addressBytes(token), eth, tokens, f.hash, f.at, hashBytes(0x7e))
}

func (f walletReaderFixture) position(wallet, token byte, balance, cost, realized, tradeCount, lastBlock int64) {
	f.ledger.exec(`INSERT INTO wallet_positions (chain_id, wallet, token_address, balance, remaining_cost, realized_pnl, transferred_in, eth_paid, eth_received, trade_count, first_block_number, last_block_number)
		VALUES ($1, $2, $3, $4, $5, $6, 0, 0, 0, $7, 10, $8)`, walletChainID, addressBytes(wallet), addressBytes(token), balance, cost, realized, tradeCount, lastBlock)
}

func (f walletReaderFixture) totals(wallet byte, realized, tokensTraded, held int64) {
	f.ledger.exec(`INSERT INTO wallet_totals (chain_id, wallet, realized_pnl, tokens_traded, held_count, first_block_number, last_block_number)
		VALUES ($1, $2, $3, $4, $5, 10, 12)`, walletChainID, addressBytes(wallet), realized, tokensTraded, held)
}

// seedWallets gives W three open positions and W2 three closed ones.
func (f walletReaderFixture) seedWallets() {
	f.position(walletW, walletTokenA, 100, 50, 10, 2, 12)
	f.position(walletW, walletTokenB, 100, 400, -310, 3, 11)
	f.position(walletW, walletTokenC, 50, 20, 0, 1, 12)
	f.totals(walletW, -300, 3, 3)
	f.position(walletW2, walletTokenA, 0, 0, 5, 2, 12)
	f.position(walletW2, walletTokenB, 0, 0, -1, 2, 11)
	f.position(walletW2, walletTokenC, 0, 0, 0, 2, 11)
	f.totals(walletW2, 4, 3, 0)
}

func wantAddr(b byte) common.Address { return common.BytesToAddress(addressBytes(b)) }

func walletCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(t.Context(), 90*time.Second)
}

func TestWalletEmptyReturnsZeros(t *testing.T) {
	ctx, cancel := walletCtx(t)
	defer cancel()
	f := newWalletReaderFixture(t, ctx)
	nobody := wantAddr(0xee)
	got, err := f.reader.GetWallet(ctx, walletChainID, nobody)
	if err != nil {
		t.Fatal(err)
	}
	if got.Address != nobody || got.RealizedPnL.Sign() != 0 || got.UnrealizedPnL.Sign() != 0 || got.PortfolioValue.Sign() != 0 ||
		got.HeldCount != 0 || got.CreatedCount != 0 || got.TokensTraded != 0 || got.FirstBlock != nil || got.LastBlock != nil || !got.AttributionComplete {
		t.Fatalf("overview = %+v", got)
	}
	for _, closed := range []bool{false, true} {
		page, err := f.reader.ListPositions(ctx, wallet.PositionsQuery{ChainID: walletChainID, Wallet: nobody, Closed: closed, Limit: 10})
		if err != nil || len(page.Items) != 0 || page.NextCursor != "" {
			t.Fatalf("positions closed=%t: %+v %v", closed, page, err)
		}
	}
	if page, err := f.reader.ListTrades(ctx, wallet.TradesQuery{ChainID: walletChainID, Wallet: nobody, Limit: 10}); err != nil || len(page.Items) != 0 {
		t.Fatalf("trades: %+v %v", page, err)
	}
	if page, err := f.reader.ListCreated(ctx, wallet.CreatedQuery{ChainID: walletChainID, Wallet: nobody, Limit: 10}); err != nil || len(page.Items) != 0 {
		t.Fatalf("created: %+v %v", page, err)
	}
}

func TestWalletPositionsOrderAndClosedMode(t *testing.T) {
	ctx, cancel := walletCtx(t)
	defer cancel()
	f := newWalletReaderFixture(t, ctx)
	f.seedWallets()
	open, err := f.reader.ListPositions(ctx, wallet.PositionsQuery{ChainID: walletChainID, Wallet: wantAddr(walletW), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	// B 400, A floor(100*1000/300) = 333, C has no reserves.
	wantOpen := []struct {
		token byte
		value int64
	}{{walletTokenB, 400}, {walletTokenA, 333}, {walletTokenC, 0}}
	if len(open.Items) != len(wantOpen) {
		t.Fatalf("open items = %+v", open.Items)
	}
	for i, want := range wantOpen {
		got := open.Items[i]
		if got.Token != wantAddr(want.token) || got.CurrentValue.Int64() != want.value || got.Name != "Token" || got.Phase == "" {
			t.Fatalf("open[%d] = %+v, want token %x value %d", i, got, want.token, want.value)
		}
	}
	if open.Items[1].RealizedPnL.Int64() != 10 || open.Items[0].RealizedPnL.Int64() != -310 || open.Items[0].TradeCount != 3 || open.Items[0].LastBlock != 11 {
		t.Fatalf("open fields = %+v", open.Items)
	}

	// Paging one at a time visits the same order.
	var paged []common.Address
	var cursor *pagination.Cursor
	for range 4 {
		page, err := f.reader.ListPositions(ctx, wallet.PositionsQuery{ChainID: walletChainID, Wallet: wantAddr(walletW), Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			paged = append(paged, item.Token)
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
	if len(paged) != 3 || paged[0] != open.Items[0].Token || paged[1] != open.Items[1].Token || paged[2] != open.Items[2].Token {
		t.Fatalf("paged = %v", paged)
	}

	closed, err := f.reader.ListPositions(ctx, wallet.PositionsQuery{ChainID: walletChainID, Wallet: wantAddr(walletW2), Closed: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	// last block DESC, then token address ASC: A (12), B (11), C (11).
	if len(closed.Items) != 3 || closed.Items[0].Token != wantAddr(walletTokenA) || closed.Items[1].Token != wantAddr(walletTokenB) || closed.Items[2].Token != wantAddr(walletTokenC) {
		t.Fatalf("closed = %+v", closed.Items)
	}
	// W2 has no open positions and W has no closed ones.
	if page, err := f.reader.ListPositions(ctx, wallet.PositionsQuery{ChainID: walletChainID, Wallet: wantAddr(walletW2), Limit: 10}); err != nil || len(page.Items) != 0 {
		t.Fatalf("W2 open = %+v %v", page.Items, err)
	}
	if page, err := f.reader.ListPositions(ctx, wallet.PositionsQuery{ChainID: walletChainID, Wallet: wantAddr(walletW), Closed: true, Limit: 10}); err != nil || len(page.Items) != 0 {
		t.Fatalf("W closed = %+v %v", page.Items, err)
	}

	// Overview aggregates agree with the positions.
	overview, err := f.reader.GetWallet(ctx, walletChainID, wantAddr(walletW))
	if err != nil {
		t.Fatal(err)
	}
	// value 733 = 400 + 333 + 0; unrealized = 733 - (50 + 400 + 20) = 263.
	if overview.PortfolioValue.Int64() != 733 || overview.UnrealizedPnL.Int64() != 263 || overview.RealizedPnL.Int64() != -300 ||
		overview.HeldCount != 3 || overview.TokensTraded != 3 || overview.CreatedCount != 0 {
		t.Fatalf("overview = %+v", overview)
	}
	if overview.FirstBlock == nil || *overview.FirstBlock != 10 || overview.LastBlock == nil || *overview.LastBlock != 12 {
		t.Fatalf("blocks = %v %v", overview.FirstBlock, overview.LastBlock)
	}
}

func TestWalletPositionsCursorModeMismatch422(t *testing.T) {
	ctx, cancel := walletCtx(t)
	defer cancel()
	f := newWalletReaderFixture(t, ctx)
	f.seedWallets()
	page, err := f.reader.ListPositions(ctx, wallet.PositionsQuery{ChainID: walletChainID, Wallet: wantAddr(walletW), Limit: 1})
	if err != nil || page.NextCursor == "" {
		t.Fatalf("first page: %+v %v", page, err)
	}
	decoded, err := pagination.Decode(page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.reader.ListPositions(ctx, wallet.PositionsQuery{ChainID: walletChainID, Wallet: wantAddr(walletW), Closed: true, Limit: 1, Cursor: &decoded}); !errors.Is(err, wallet.ErrCursorMode) {
		t.Fatalf("err = %v, want ErrCursorMode", err)
	}

	server := apiserver.New(apiserver.DefaultConfig(), apiserver.ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterWalletRoutes(apiserver.WalletRoutes{Reader: f.reader, ChainID: walletChainID})
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	base := "/v1/wallets/" + wantAddr(walletW).Hex() + "/positions"
	if rec := get(base + "?limit=1&closed=true&cursor=" + page.NextCursor); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body)
	}
	if rec := get(base + "?limit=1&cursor=" + page.NextCursor); rec.Code != http.StatusOK {
		t.Fatalf("same-mode cursor: code = %d body = %s", rec.Code, rec.Body)
	}
	// Review Focus 4: no closed rows is still a 200 with an empty list.
	rec := get(base + "?closed=true")
	var body struct {
		Items []any `json:"items"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Items == nil || len(body.Items) != 0 {
		t.Fatalf("empty closed: code = %d body = %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/v1/wallets/zzz", "/v1/wallets/zzz/positions", "/v1/wallets/zzz/trades", "/v1/wallets/zzz/created", "/v1/leaderboard?limit=101"} {
		if rec := get(path); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: code = %d", path, rec.Code)
		}
	}
}

func TestWalletPositionsMissingReserves(t *testing.T) {
	ctx, cancel := walletCtx(t)
	defer cancel()
	f := newWalletReaderFixture(t, ctx)
	f.seedWallets()
	// C has no token_reserves row; A gets a zero token_reserve. Neither may divide by zero.
	f.reserves(walletTokenA, 1000, 0)
	page, err := f.reader.ListPositions(ctx, wallet.PositionsQuery{ChainID: walletChainID, Wallet: wantAddr(walletW), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	values := map[common.Address]string{}
	for _, item := range page.Items {
		values[item.Token] = item.CurrentValue.String()
	}
	if values[wantAddr(walletTokenA)] != "0" || values[wantAddr(walletTokenC)] != "0" || values[wantAddr(walletTokenB)] != "400" {
		t.Fatalf("values = %v", values)
	}
	overview, err := f.reader.GetWallet(ctx, walletChainID, wantAddr(walletW))
	if err != nil || overview.PortfolioValue.Int64() != 400 {
		t.Fatalf("overview = %+v %v", overview, err)
	}
}

func TestWalletTradesIncludePoolSwapsBySender(t *testing.T) {
	ctx, cancel := walletCtx(t)
	defer cancel()
	f := newWalletReaderFixture(t, ctx)
	// Swap 0x7f (block 10) is sent by W and received by 0xa9; the swap's own sender column is a router.
	f.ledger.swap(10, 0, 0x7f, 4, 0, 100, 20, 0, 0xa9, walletW)
	// A second swap by someone else must not appear.
	f.ledger.swap(10, 0, 0x7f, 5, 0, 100, 20, 0, 0xa9, 0xb7)
	f.curveTrade(12, 0x71, walletW)
	f.curveTrade(12, 0x72, 0xb7)
	page, err := f.reader.ListTrades(ctx, wallet.TradesQuery{ChainID: walletChainID, Wallet: wantAddr(walletW), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %+v", page.Items)
	}
	curve, dex := page.Items[0], page.Items[1]
	if curve.Source != "curve" || curve.BlockNumber != 12 || dex.Source != "dex" || dex.BlockNumber != 10 || dex.LogIndex != 4 {
		t.Fatalf("order/source: %+v", page.Items)
	}
	for _, item := range page.Items {
		if item.Trader == nil || *item.Trader != wantAddr(walletW) || item.Symbol != "TKN" || item.Name != "Token" {
			t.Fatalf("item = %+v", item)
		}
	}
	// Paging with limit 1 and the cursor returns the dex swap next.
	first, err := f.reader.ListTrades(ctx, wallet.TradesQuery{ChainID: walletChainID, Wallet: wantAddr(walletW), Limit: 1})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first = %+v %v", first, err)
	}
	decoded, err := pagination.Decode(first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.reader.ListTrades(ctx, wallet.TradesQuery{ChainID: walletChainID, Wallet: wantAddr(walletW), Limit: 1, Cursor: &decoded})
	if err != nil || len(second.Items) != 1 || second.Items[0].Source != "dex" {
		t.Fatalf("second = %+v %v", second, err)
	}
}

func TestWalletAttributionIncomplete(t *testing.T) {
	ctx, cancel := walletCtx(t)
	defer cancel()
	f := newWalletReaderFixture(t, ctx)
	before, err := f.reader.GetWallet(ctx, walletChainID, wantAddr(walletW))
	if err != nil || !before.AttributionComplete {
		t.Fatalf("before = %+v %v", before, err)
	}
	f.ledger.exec(`INSERT INTO pool_swaps (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
		pair_address, sender, amount0_in, amount1_in, amount0_out, amount1_out, to_address, tx_from)
		VALUES ($1, 10, $2, $3, 0, $4, 4, $5, $6, 0, 100, 20, 0, $7, NULL)`,
		walletChainID, f.hash, f.at, hashBytes(0x7f), f.pair.Bytes(), addressBytes(0xa1), addressBytes(0xa2))
	after, err := f.reader.GetWallet(ctx, walletChainID, wantAddr(0xee))
	if err != nil || after.AttributionComplete {
		t.Fatalf("after = %+v %v", after, err)
	}
}

func TestWalletCreatedAndSnapshotFields(t *testing.T) {
	ctx, cancel := walletCtx(t)
	defer cancel()
	f := newWalletReaderFixture(t, ctx)
	creator := wantAddr(0x55)
	overview, err := f.reader.GetWallet(ctx, walletChainID, creator)
	if err != nil {
		t.Fatal(err)
	}
	if overview.CreatedCount != 3 {
		t.Fatalf("created count = %d", overview.CreatedCount)
	}
	// TestWalletSnapshotFields: block 12 is the observed head and the safe watermark.
	if overview.Snapshot.ChainID != walletChainID || overview.Snapshot.BlockNumber != 12 || common.Hash(overview.Snapshot.BlockHash) != common.BytesToHash(f.ledger.blockHash(12)) || overview.Finality != "safe" {
		t.Fatalf("snapshot = %+v %q", overview.Snapshot, overview.Finality)
	}
	page, err := f.reader.ListCreated(ctx, wallet.CreatedQuery{ChainID: walletChainID, Wallet: creator, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Token != wantAddr(walletTokenC) || page.Items[0].LaunchBlock != 12 || page.Items[1].Token != wantAddr(walletTokenB) ||
		page.Items[0].MarketCap == nil || page.Items[0].Phase != "curve" || page.NextCursor == "" || page.Snapshot.BlockNumber != 12 || page.Finality != "safe" {
		t.Fatalf("created page = %+v", page)
	}
	decoded, err := pagination.Decode(page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	rest, err := f.reader.ListCreated(ctx, wallet.CreatedQuery{ChainID: walletChainID, Wallet: creator, Limit: 2, Cursor: &decoded})
	if err != nil || len(rest.Items) != 1 || rest.Items[0].Token != wantAddr(walletTokenA) || rest.Items[0].Phase != "graduated" {
		t.Fatalf("rest = %+v %v", rest, err)
	}
	// A cursor from another wallet is rejected, not misread.
	if _, err := f.reader.ListCreated(ctx, wallet.CreatedQuery{ChainID: walletChainID, Wallet: wantAddr(0xee), Limit: 2, Cursor: &decoded}); !errors.Is(err, pagination.ErrInvalidCursor) {
		t.Fatalf("foreign cursor err = %v", err)
	}
}

func TestLeaderboardRanksAndExcludesProtocol(t *testing.T) {
	ctx, cancel := walletCtx(t)
	defer cancel()
	f := newWalletReaderFixture(t, ctx)
	f.seedWallets() // W = -300, W2 = 4
	f.totals(0xb1, 500, 2, 0)
	f.totals(0xb2, 500, 1, 0)
	// Protocol-owned addresses with huge totals must never rank: pair of A, curve of A, curve of B, zero address.
	f.totals(0x31, 9999, 1, 0)
	f.totals(0x41, 8888, 1, 0)
	f.totals(0x42, 8887, 1, 0)
	f.ledger.exec(`INSERT INTO wallet_totals (chain_id, wallet, realized_pnl, tokens_traded, held_count, first_block_number, last_block_number)
		VALUES ($1, decode(repeat('00', 20), 'hex'), 7777, 1, 0, 10, 12)`, walletChainID)

	page, err := f.reader.Leaderboard(ctx, walletChainID, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		addr byte
		pnl  int64
	}{{0xb1, 500}, {0xb2, 500}, {walletW2, 4}, {walletW, -300}}
	if len(page.Items) != len(want) {
		t.Fatalf("items = %+v", page.Items)
	}
	for i, w := range want {
		got := page.Items[i]
		if got.Rank != i+1 || got.Address != wantAddr(w.addr) || got.RealizedPnL.Cmp(big.NewInt(w.pnl)) != 0 {
			t.Fatalf("rank %d = %+v, want %x %d", i+1, got, w.addr, w.pnl)
		}
	}
	if page.Items[0].TokensTraded != 2 {
		t.Fatalf("tokens traded = %d", page.Items[0].TokensTraded)
	}
	limited, err := f.reader.Leaderboard(ctx, walletChainID, 2)
	if err != nil || len(limited.Items) != 2 || limited.Snapshot.BlockNumber != 12 {
		t.Fatalf("limited = %+v %v", limited, err)
	}
	if _, err := f.reader.Leaderboard(ctx, walletChainID, 101); err == nil {
		t.Fatal("limit 101 accepted by reader")
	}
	server := apiserver.New(apiserver.DefaultConfig(), apiserver.ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterWalletRoutes(apiserver.WalletRoutes{Reader: f.reader, ChainID: walletChainID})
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/leaderboard?limit=101", nil))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("limit 101: code = %d", rec.Code)
	}
}

// curveTrade inserts a buy of token C (still on its curve) by trader at the given block.
func (f walletReaderFixture) curveTrade(block int64, txHash, trader byte) {
	f.ledger.exec(`INSERT INTO trades (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
		token_address, trader, is_buy, eth_gross, eth_refund, token_amount, protocol_fee, creator_fee, new_eth_reserve, new_token_reserve)
		VALUES ($1, $2, $3, $4, 0, $5, 1, $6, $7, true, 100, 0, 50, 1, 1, 1000, 1000)`,
		walletChainID, block, f.ledger.blockHash(block), f.ledger.blockTime(block), hashBytes(txHash), addressBytes(walletTokenC), addressBytes(trader))
}
