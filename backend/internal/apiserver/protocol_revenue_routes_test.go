package apiserver

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/Contictus/launchtap/backend/internal/stats"
)

type revenueStub struct {
	value stats.Revenue
	err   error
}

func (s revenueStub) ReadProtocolRevenue(context.Context, int64) (stats.Revenue, error) {
	return s.value, s.err
}

func revenueSource(e1, e2 int64) stats.RevenueSource {
	return stats.RevenueSource{Engine1: big.NewInt(e1), Engine2: big.NewInt(e2), Total: big.NewInt(e1 + e2)}
}

func TestProtocolRevenueWireContract(t *testing.T) {
	stub := revenueStub{value: stats.Revenue{
		LaunchFees: revenueSource(1, 2), TradeFees: revenueSource(3, 4), SnipeTax: revenueSource(0, 5),
		GraduationRemainder: revenueSource(0, 6), LPFees: revenueSource(0, 7), Total: big.NewInt(28),
		Snapshot: pagination.Snapshot{ChainID: 46630, BlockNumber: 42}, Finality: "safe",
	}}
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterPublicRoutes(PublicRoutes{Revenue: stub, ChainID: 46630})

	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/stats/protocol/revenue", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	for _, want := range []string{
		`"total":"28"`,
		`"launch_fees":{"engine_1":"1","engine_2":"2","total":"3"}`,
		`"trade_fees":{"engine_1":"3","engine_2":"4","total":"7"}`,
		`"snipe_tax":{"engine_1":"0","engine_2":"5","total":"5"}`,
		`"graduation_remainder":{"engine_1":"0","engine_2":"6","total":"6"}`,
		`"lp_fees":{"engine_1":"0","engine_2":"7","total":"7"}`,
		`"as_of_block":42`,
	} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("body missing %s: %s", want, w.Body.String())
		}
	}
}

func TestProtocolRevenueProblems(t *testing.T) {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterPublicRoutes(PublicRoutes{ChainID: 46630})
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/stats/protocol/revenue", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "stats_unavailable") {
		t.Fatalf("nil reader: status=%d body=%s", w.Code, w.Body.String())
	}

	s = New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterPublicRoutes(PublicRoutes{Revenue: revenueStub{err: errors.New("SELECT secret")}, ChainID: 46630})
	w = httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/stats/protocol/revenue", nil))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("read error: status=%d body=%s", w.Code, w.Body.String())
	}
}
