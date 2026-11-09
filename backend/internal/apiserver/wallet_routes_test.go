package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/Contictus/launchtap/backend/internal/wallet"
	"github.com/ethereum/go-ethereum/common"
)

type walletReaderStub struct {
	err         error
	truncated   bool
	positionsQ  wallet.PositionsQuery
	leaderLimit int
}

func (s *walletReaderStub) stamp() wallet.Stamp {
	return wallet.Stamp{Snapshot: pagination.Snapshot{ChainID: 46630, BlockNumber: 31, BlockHash: [32]byte{1}}, Finality: "safe"}
}

func (s *walletReaderStub) GetWallet(_ context.Context, _ int64, w common.Address) (wallet.Overview, error) {
	if s.err != nil {
		return wallet.Overview{}, s.err
	}
	first := int64(5)
	return wallet.Overview{Stamp: s.stamp(), Address: w, RealizedPnL: big.NewInt(-7), UnrealizedPnL: big.NewInt(3), PortfolioValue: big.NewInt(9), HeldCount: 1, CreatedCount: 2, TokensTraded: 3, FirstBlock: &first, AttributionComplete: true}, nil
}

func (s *walletReaderStub) ListPositions(_ context.Context, q wallet.PositionsQuery) (wallet.PositionsPage, error) {
	s.positionsQ = q
	if s.err != nil {
		return wallet.PositionsPage{}, s.err
	}
	return wallet.PositionsPage{Stamp: s.stamp(), Truncated: s.truncated, Items: []wallet.Position{
		{Token: common.HexToAddress("0xaa"), Name: "A", Symbol: "A", Phase: "curve", Balance: big.NewInt(100), RemainingCost: big.NewInt(50), RealizedPnL: big.NewInt(-1), CurrentValue: big.NewInt(80), TransferredIn: big.NewInt(0), TradeCount: 2, LastBlock: 9},
		{Token: common.HexToAddress("0xbb"), Name: "B", Symbol: "B", Phase: "graduated", Balance: big.NewInt(0), RemainingCost: big.NewInt(0), RealizedPnL: big.NewInt(4), CurrentValue: big.NewInt(0), TransferredIn: big.NewInt(0), TradeCount: 1, LastBlock: 8},
	}}, nil
}

func (s *walletReaderStub) ListTrades(context.Context, wallet.TradesQuery) (wallet.TradesPage, error) {
	if s.err != nil {
		return wallet.TradesPage{}, s.err
	}
	return wallet.TradesPage{Stamp: s.stamp()}, nil
}

func (s *walletReaderStub) ListCreated(context.Context, wallet.CreatedQuery) (wallet.CreatedPage, error) {
	if s.err != nil {
		return wallet.CreatedPage{}, s.err
	}
	return wallet.CreatedPage{Stamp: s.stamp()}, nil
}

func (s *walletReaderStub) Leaderboard(_ context.Context, _ int64, limit int) (wallet.LeaderboardPage, error) {
	s.leaderLimit = limit
	if s.err != nil {
		return wallet.LeaderboardPage{}, s.err
	}
	return wallet.LeaderboardPage{Stamp: s.stamp(), Items: []wallet.Ranked{{Rank: 1, Address: common.HexToAddress("0xcc"), RealizedPnL: big.NewInt(5), TokensTraded: 2}}}, nil
}

func walletServer(reader wallet.Reader) *Server {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterWalletRoutes(WalletRoutes{Reader: reader, ChainID: 46630})
	return s
}

func walletGet(s *Server, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1"+path, nil))
	return rec
}

const walletAddr = "0x00000000000000000000000000000000000000a0"

func TestWalletInvalidAddress422(t *testing.T) {
	s := walletServer(&walletReaderStub{})
	for _, suffix := range []string{"", "/positions", "/trades", "/created"} {
		if rec := walletGet(s, "/wallets/not-an-address"+suffix); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%q: code=%d body=%s", suffix, rec.Code, rec.Body)
		}
	}
}

func TestWalletBodyShape(t *testing.T) {
	rec := walletGet(walletServer(&walletReaderStub{}), "/wallets/"+walletAddr)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["realized_pnl_wei"] != "-7" || body["unrealized_pnl_wei"] != "3" || body["portfolio_value_wei"] != "9" ||
		body["attribution_complete"] != true || body["first_block"].(float64) != 5 || body["last_block"] != nil {
		t.Fatalf("body=%s", rec.Body)
	}
	snap := body["snapshot"].(map[string]any)
	if snap["as_of_block"].(float64) != 31 || snap["finality"] != "safe" {
		t.Fatalf("snapshot=%v", snap)
	}
}

func TestWalletPositionsModeAndAverageCost(t *testing.T) {
	stub := &walletReaderStub{}
	s := walletServer(stub)
	rec := walletGet(s, "/wallets/"+walletAddr+"/positions?closed=true&limit=7")
	if rec.Code != http.StatusOK || !stub.positionsQ.Closed || stub.positionsQ.Limit != 7 {
		t.Fatalf("code=%d q=%+v body=%s", rec.Code, stub.positionsQ, rec.Body)
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// 50e18 / 100, floor; the zero-balance position has no average cost.
	if body.Items[0]["average_cost_wad"] != "500000000000000000" || body.Items[1]["average_cost_wad"] != nil {
		t.Fatalf("items=%v", body.Items)
	}
	if body.Items[0]["unrealized_pnl_wei"] != "30" || body.Items[0]["realized_pnl_wei"] != "-1" {
		t.Fatalf("item=%v", body.Items[0])
	}
	if rec := walletGet(s, "/wallets/"+walletAddr+"/positions"); rec.Code != http.StatusOK || stub.positionsQ.Closed || stub.positionsQ.Limit != 50 {
		t.Fatalf("defaults: q=%+v", stub.positionsQ)
	}
}

func TestWalletCursorModeMismatchMaps422(t *testing.T) {
	s := walletServer(&walletReaderStub{err: wallet.ErrCursorMode})
	if rec := walletGet(s, "/wallets/"+walletAddr+"/positions"); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
}

func TestWalletLimitsAndErrors(t *testing.T) {
	stub := &walletReaderStub{}
	s := walletServer(stub)
	for _, p := range []string{"/wallets/" + walletAddr + "/positions?limit=101", "/wallets/" + walletAddr + "/trades?limit=0", "/leaderboard?limit=101", "/leaderboard?limit=0"} {
		if rec := walletGet(s, p); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: code=%d", p, rec.Code)
		}
	}
	if rec := walletGet(s, "/leaderboard"); rec.Code != http.StatusOK || stub.leaderLimit != 100 {
		t.Fatalf("default leaderboard limit=%d", stub.leaderLimit)
	}
	if rec := walletGet(walletServer(&walletReaderStub{err: pagination.ErrSnapshotUnavailable}), "/leaderboard"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d", rec.Code)
	}
	if rec := walletGet(walletServer(&walletReaderStub{err: errors.New("boom")}), "/wallets/"+walletAddr); rec.Code < 500 {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestWalletOpenPositionsRejectCursorAndReportTruncated(t *testing.T) {
	stub := &walletReaderStub{}
	s := walletServer(stub)
	cur := "c3RhbGU" // any non-empty cursor: open mode is not paginated
	if rec := walletGet(s, "/wallets/"+walletAddr+"/positions?cursor="+cur); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("open + cursor: code=%d body=%s", rec.Code, rec.Body)
	}
	for _, truncated := range []bool{false, true} {
		stub.truncated = truncated
		rec := walletGet(s, "/wallets/"+walletAddr+"/positions")
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK || body["truncated"] != truncated || body["next_cursor"] != nil {
			t.Fatalf("truncated=%t: code=%d body=%s", truncated, rec.Code, rec.Body)
		}
	}
}

func TestWalletAndLeaderboardUsernameField(t *testing.T) {
	s := walletServer(&walletReaderStub{})
	var body map[string]any
	if err := json.Unmarshal(walletGet(s, "/wallets/"+walletAddr).Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if v, present := body["username"]; !present || v != nil {
		t.Fatalf("wallet username = %v (present=%v); want explicit null", v, present)
	}

	name := "alice"
	s = walletServer(&namedLeaderboardStub{name: &name})
	var board struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(walletGet(s, "/leaderboard").Body.Bytes(), &board); err != nil {
		t.Fatal(err)
	}
	if len(board.Items) != 1 || board.Items[0]["username"] != "alice" {
		t.Fatalf("leaderboard items = %v", board.Items)
	}
}

type namedLeaderboardStub struct {
	walletReaderStub
	name *string
}

func (s *namedLeaderboardStub) Leaderboard(ctx context.Context, chainID int64, limit int) (wallet.LeaderboardPage, error) {
	page, err := s.walletReaderStub.Leaderboard(ctx, chainID, limit)
	for i := range page.Items {
		page.Items[i].Username = s.name
	}
	return page, err
}
