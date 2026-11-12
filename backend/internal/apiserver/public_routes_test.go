package apiserver

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/candle"
	"github.com/Contictus/launchtap/backend/internal/holder"
	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/Contictus/launchtap/backend/internal/stats"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/Contictus/launchtap/backend/internal/trading"
	"github.com/ethereum/go-ethereum/common"
)

type publicTokenStub struct {
	detail token.Detail
	err    error
}

func (s publicTokenStub) List(context.Context, token.ListQuery) (token.Page, error) {
	return token.Page{}, s.err
}
func (s publicTokenStub) Get(context.Context, int64, common.Address) (token.Detail, error) {
	return s.detail, s.err
}
func (s publicTokenStub) ReadQuoteState(context.Context, int64, common.Address) (token.QuoteState, error) {
	return token.QuoteState{}, s.err
}

type publicMarketStub struct {
	trades  trading.Page
	holders holder.Page
	err     error
}

func (s publicMarketStub) ListTrades(context.Context, trading.Query) (trading.Page, error) {
	return s.trades, s.err
}
func (s publicMarketStub) ListHolders(context.Context, holder.Query) (holder.Page, error) {
	return s.holders, s.err
}

type protocolStub struct{ value stats.Protocol }

func (s protocolStub) ReadProtocol(context.Context, int64) (stats.Protocol, error) {
	return s.value, nil
}

type protocolDailyStub struct{ value stats.DailyPage }

func (s protocolDailyStub) ReadProtocolDaily(context.Context, int64, stats.DailyQuery) (stats.DailyPage, error) {
	return s.value, nil
}

func TestPublicReadWireContracts(t *testing.T) {
	snapshot := pagination.Snapshot{ChainID: 46630, BlockNumber: 42, BlockHash: [32]byte{31: 1}}
	tokenAddress := common.HexToAddress("0x0000000000000000000000000000000000000001")
	trader := common.HexToAddress("0x0000000000000000000000000000000000000002")
	now := time.Date(2026, 9, 9, 8, 0, 0, 0, time.UTC)
	tokens := publicTokenStub{detail: token.Detail{
		Summary: token.Summary{Address: tokenAddress, Name: "Alpha", Symbol: "ALP", Phase: "curve", LaunchTime: now, LaunchBlock: 1, TotalSupply: big.NewInt(1000), MarketCapETH: big.NewInt(20), Volume24hETH: big.NewInt(30), HolderCount: 1},
		Curve:   tokenAddress, Pair: tokenAddress, WETH: tokenAddress, Creator: tokenAddress, ProtocolTreasury: tokenAddress,
		InitialVirtualETH: big.NewInt(10), InitialVirtualToken: big.NewInt(11), CurveTokens: big.NewInt(12), LPTokens: big.NewInt(13), GraduationETH: big.NewInt(14),
		ETHReserve: big.NewInt(15), TokenReserve: big.NewInt(16), SpotPriceETH: big.NewInt(17), FDVETH: big.NewInt(18), LiquidityETH: big.NewInt(19), ATHPriceETH: big.NewInt(21), ATHAt: now,
		PriceChange24hBPS: 2_147_490_000,
		ReserveHash:       common.HexToHash("0x02"), Snapshot: snapshot, Finality: "safe",
	}}
	market := publicMarketStub{
		trades:  trading.Page{Items: []trading.Trade{{Source: "curve", Trader: &trader, Buy: true, ExecutionPrice: big.NewInt(5), SpotPrice: big.NewInt(6), ETHVolume: big.NewInt(7), TokenVolume: big.NewInt(8), BlockNumber: 42, TxHash: common.HexToHash("0x03"), Time: now, Finality: "safe"}}, Snapshot: snapshot, Finality: "safe", NextCursor: "next-trade"},
		holders: holder.Page{Items: []holder.Balance{{Address: trader, Balance: big.NewInt(9), FirstAcquiredBlock: 4}}, Snapshot: snapshot, Finality: "safe", NextCursor: "next-holder"},
	}
	protocol := protocolStub{value: stats.Protocol{Volume24hETH: big.NewInt(22), VolumeAllTimeETH: big.NewInt(23), TradesAllTime: 24, UpdatedAt: now, Snapshot: snapshot, Finality: "safe"}}
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterPublicRoutes(PublicRoutes{Tokens: tokens, Market: market, Protocol: protocol, ChainID: 46630})

	tests := []struct {
		path string
		want []string
	}{
		{"/v1/tokens/" + tokenAddress.Hex(), []string{`"total_supply":"1000"`, `"address":"` + tokenAddress.Hex() + `"`, `"finality":"safe"`, `"price_change_24h_bps":2147490000`, `"launch_block":1`}},
		{"/v1/tokens/" + tokenAddress.Hex() + "/trades", []string{`"execution_price":"5"`, `"trader":"` + trader.Hex() + `"`, `"next_cursor":"next-trade"`}},
		{"/v1/tokens/" + tokenAddress.Hex() + "/holders", []string{`"balance":"9"`, `"address":"` + trader.Hex() + `"`, `"next_cursor":"next-holder"`}},
		{"/v1/stats/protocol", []string{`"volume_24h_eth":"22"`, `"trades_all_time":24`, `"as_of_block":42`}},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("content-type=%q", w.Header().Get("Content-Type"))
			}
			for _, want := range tc.want {
				if !strings.Contains(w.Body.String(), want) {
					t.Fatalf("body missing %s: %s", want, w.Body.String())
				}
			}
		})
	}
}

func TestProtocolDailyRangeAndOrderingContract(t *testing.T) {
	snapshot := pagination.Snapshot{ChainID: 46630, BlockNumber: 42, BlockHash: [32]byte{31: 1}}
	now := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterPublicRoutes(PublicRoutes{
		Protocol:      protocolStub{},
		ProtocolDaily: protocolDailyStub{value: stats.DailyPage{Snapshot: snapshot, Finality: "safe", Items: []stats.Daily{{Day: now, VolumeETH: big.NewInt(2)}}}},
		ChainID:       46630,
	})
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/stats/protocol/daily?from=2026-09-01&to=2026-09-09&limit=10", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"day":"2026-09-09"`) || !strings.Contains(w.Body.String(), `"as_of_block":42`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/stats/protocol/daily?from=2026-09-10&to=2026-09-09", nil))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_range") {
		t.Fatalf("invalid range status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPublicReadProblemsAreTyped(t *testing.T) {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	tokens := publicTokenStub{err: token.ErrNotFound}
	s.RegisterPublicRoutes(PublicRoutes{Tokens: tokens, Market: publicMarketStub{err: pagination.ErrCursorInvalidated}, Protocol: protocolStub{}, ChainID: 46630})

	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/tokens/0x0000000000000000000000000000000000000001", nil))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "urn:launchpad:problem:token_not_found") {
		t.Fatalf("not-found status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/tokens/0x0000000000000000000000000000000000000001/trades", nil))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "urn:launchpad:problem:cursor_invalidated") {
		t.Fatalf("cursor status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/tokens/0x0000000000000000000000000000000000000001/holders?cursor=not-base64", nil))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "urn:launchpad:problem:invalid_cursor") {
		t.Fatalf("invalid cursor status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPublicReadReturnsUnavailableBeforeFirstSnapshot(t *testing.T) {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterPublicRoutes(PublicRoutes{
		Tokens: publicTokenStub{err: pagination.ErrSnapshotUnavailable},
		Market: publicMarketStub{}, Protocol: protocolStub{}, ChainID: 46630,
	})
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/tokens/0x0000000000000000000000000000000000000001", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "urn:launchpad:problem:snapshot_unavailable") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestInternalReadProblemExposesRequestIDWithoutCause(t *testing.T) {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterPublicRoutes(PublicRoutes{Tokens: publicTokenStub{err: errors.New("SELECT secret FROM hidden")}, Market: publicMarketStub{}, Protocol: protocolStub{}, ChainID: 46630})
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/tokens/0x0000000000000000000000000000000000000001", nil))
	requestID := w.Header().Get("X-Request-ID")
	if w.Code != http.StatusInternalServerError || requestID == "" || !strings.Contains(w.Body.String(), "urn:launchpad:request:"+requestID) {
		t.Fatalf("status=%d request-id=%q body=%s", w.Code, requestID, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "SELECT secret") {
		t.Fatalf("internal cause leaked: %s", w.Body.String())
	}
}

type candleCapture struct{ query candle.Query }

func (c *candleCapture) List(_ context.Context, q candle.Query) (candle.Page, error) {
	c.query = q
	return candle.Page{Snapshot: pagination.Snapshot{ChainID: q.ChainID}, Finality: "provisional"}, nil
}

func TestCandleIntervalsAndDefaultLimit(t *testing.T) {
	for _, interval := range []string{"1m", "5m", "1h", "1d", "6h", "all"} {
		capture := &candleCapture{}
		s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
		s.RegisterCandleRoutes(CandleRoutes{Reader: capture, ChainID: 46630})
		w := httptest.NewRecorder()
		path := "/v1/tokens/0x0000000000000000000000000000000000000001/candles?interval=" + interval
		s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || capture.query.Interval != interval || capture.query.Limit != 20 {
			t.Fatalf("interval=%s status=%d query=%+v body=%s", interval, w.Code, capture.query, w.Body.String())
		}
	}
}
