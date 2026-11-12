package apiserver

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Contictus/launchtap/backend/internal/earlybuyer"
	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/ethereum/go-ethereum/common"
)

type earlyBuyersStub struct {
	report earlybuyer.Report
	err    error
	query  *earlybuyer.Query
}

func (s earlyBuyersStub) ReadEarlyBuyers(_ context.Context, q earlybuyer.Query) (earlybuyer.Report, error) {
	if s.query != nil {
		*s.query = q
	}
	return s.report, s.err
}

func TestEarlyBuyersWireContract(t *testing.T) {
	tokenAddress := common.HexToAddress("0x0000000000000000000000000000000000000001")
	walletA := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	walletB := common.HexToAddress("0x00000000000000000000000000000000000000bb")
	var seen earlybuyer.Query
	stub := earlyBuyersStub{query: &seen, report: earlybuyer.Report{
		Result: earlybuyer.Result{
			EarlyWalletCount: 3,
			EarlyBalance:     big.NewInt(90),
			ClusterCount:     1,
			Clusters: []earlybuyer.Cluster{{
				WalletCount:     2,
				Balance:         big.NewInt(80),
				Links:           []string{earlybuyer.LinkSameTransaction},
				IncludesCreator: true,
				Wallets:         []earlybuyer.Wallet{{Address: walletA, Balance: big.NewInt(50)}, {Address: walletB, Balance: big.NewInt(30)}},
			}},
		},
		WindowOpen: true,
		Snapshot:   pagination.Snapshot{ChainID: 46630, BlockNumber: 42},
		Finality:   "safe",
	}}
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterPublicRoutes(PublicRoutes{EarlyBuyers: stub, ChainID: 46630})

	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/tokens/"+tokenAddress.Hex()+"/early-buyers", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if seen.ChainID != 46630 || seen.Token != tokenAddress {
		t.Fatalf("query = %+v", seen)
	}
	for _, want := range []string{
		`"window_seconds":60`, `"window_open":true`, `"early_wallet_count":3`, `"early_balance":"90"`,
		`"cluster_count":1`, `"wallet_count":2`, `"balance":"80"`, `"links":["same_transaction"]`,
		`"includes_creator":true`, `"address":"` + walletA.Hex() + `","balance":"50"`, `"as_of_block":42`,
	} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("body missing %s: %s", want, w.Body.String())
		}
	}
}

func TestEarlyBuyersEmptyClustersAreAnArray(t *testing.T) {
	stub := earlyBuyersStub{report: earlybuyer.Report{Result: earlybuyer.Result{EarlyBalance: new(big.Int), Clusters: []earlybuyer.Cluster{}}}}
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterPublicRoutes(PublicRoutes{EarlyBuyers: stub, ChainID: 46630})
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/tokens/0x0000000000000000000000000000000000000001/early-buyers", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"clusters":[]`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestEarlyBuyersProblems(t *testing.T) {
	tests := []struct {
		name   string
		routes PublicRoutes
		path   string
		status int
		code   string
	}{
		{"unknown token", PublicRoutes{EarlyBuyers: earlyBuyersStub{err: token.ErrNotFound}}, "/v1/tokens/0x0000000000000000000000000000000000000001/early-buyers", http.StatusNotFound, "token_not_found"},
		{"not configured", PublicRoutes{}, "/v1/tokens/0x0000000000000000000000000000000000000001/early-buyers", http.StatusServiceUnavailable, "early_buyers_unavailable"},
		{"bad address", PublicRoutes{EarlyBuyers: earlyBuyersStub{}}, "/v1/tokens/not-an-address/early-buyers", http.StatusBadRequest, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
			s.RegisterPublicRoutes(tc.routes)
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
