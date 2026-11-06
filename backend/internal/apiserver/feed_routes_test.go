package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/feed"
	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/ethereum/go-ethereum/common"
)

type recentReaderStub struct {
	limit int
	err   error
}

func (s *recentReaderStub) RecentTradesSnapshot(_ context.Context, _ int64, limit int) (feed.RecentPage, error) {
	s.limit = limit
	if s.err != nil {
		return feed.RecentPage{}, s.err
	}
	// Newest first, as the store returns them.
	items := []feed.Trade{
		{Token: common.HexToAddress("0xaa"), Name: "Alpha", Symbol: "ALP", Source: "dex", Buy: false, ETHVolume: big.NewInt(9), TokenVolume: big.NewInt(8), Block: 30, TxIndex: 1, LogIndex: 1, Time: time.Unix(1700000300, 0), Finality: "safe"},
		{Token: common.HexToAddress("0xbb"), Name: "Beta", Symbol: "BET", Source: "curve", Buy: true, ETHVolume: big.NewInt(7), TokenVolume: big.NewInt(6), Block: 20, TxIndex: 0, LogIndex: 4, Time: time.Unix(1700000200, 0), Finality: "provisional"},
	}
	return feed.RecentPage{Snapshot: pagination.Snapshot{ChainID: 46630, BlockNumber: 31, BlockHash: [32]byte{1}}, Finality: "provisional", Items: items}, nil
}

func recentServer(reader *recentReaderStub) *Server {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterFeedRoutes(FeedRoutes{Reader: reader, ChainID: 46630, DeploymentID: "testnet"})
	return s
}

func getRecent(t *testing.T, s *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/trades/recent"+query, nil))
	return rec
}

func TestRecentTradesLimitBounds(t *testing.T) {
	reader := &recentReaderStub{}
	s := recentServer(reader)
	for _, tc := range []struct {
		query string
		code  int
		limit int
	}{
		{"", http.StatusOK, 50},
		{"?limit=1", http.StatusOK, 1},
		{"?limit=100", http.StatusOK, 100},
		{"?limit=0", http.StatusUnprocessableEntity, 0},
		{"?limit=101", http.StatusUnprocessableEntity, 0},
	} {
		reader.limit = 0
		rec := getRecent(t, s, tc.query)
		if rec.Code != tc.code || reader.limit != tc.limit {
			t.Fatalf("%q: code=%d limit=%d, want %d/%d body=%s", tc.query, rec.Code, reader.limit, tc.code, tc.limit, rec.Body)
		}
	}
}

func TestRecentTradesBodyNewestFirstWithSnapshot(t *testing.T) {
	rec := getRecent(t, recentServer(&recentReaderStub{}), "")
	var body struct {
		Snapshot struct {
			ChainID  int64  `json:"chain_id"`
			AsOf     int64  `json:"as_of_block"`
			Finality string `json:"finality"`
		} `json:"snapshot"`
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Snapshot.ChainID != 46630 || body.Snapshot.AsOf != 31 || body.Snapshot.Finality != "provisional" || len(body.Items) != 2 {
		t.Fatalf("body=%s", rec.Body)
	}
	first, second := body.Items[0], body.Items[1]
	if first["block_number"].(float64) != 30 || second["block_number"].(float64) != 20 {
		t.Fatalf("not newest first: %s", rec.Body)
	}
	if first["source"] != "dex" || first["side"] != "sell" || first["eth_volume"] != "9" || first["token_volume"] != "8" || first["deployment_id"] != "testnet" || first["trader"] != nil {
		t.Fatalf("first=%v", first)
	}
	if second["source"] != "curve" || second["side"] != "buy" {
		t.Fatalf("second=%v", second)
	}
}

func TestRecentTradesErrors(t *testing.T) {
	if rec := getRecent(t, recentServer(&recentReaderStub{err: pagination.ErrSnapshotUnavailable}), ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if rec := getRecent(t, recentServer(&recentReaderStub{err: errors.New("boom")}), ""); rec.Code < 500 {
		t.Fatalf("code=%d", rec.Code)
	}
}
