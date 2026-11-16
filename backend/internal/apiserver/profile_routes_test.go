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
	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/profile"
	"github.com/ethereum/go-ethereum/common"
)

type profileReaderStub struct {
	page profile.Page
	err  error
}

func (s profileReaderStub) List(context.Context, int64, []common.Address) (profile.Page, error) {
	return s.page, s.err
}

type profileVerifierStub struct {
	principal privyauth.Principal
	err       error
}

func (s profileVerifierStub) Verify(context.Context, string, string) (privyauth.Principal, error) {
	return s.principal, s.err
}

func TestProfileRouteRequiresCredentialsAndReturnsSnapshotBoundActions(t *testing.T) {
	wallet := common.HexToAddress("0x0000000000000000000000000000000000000001")
	token := common.HexToAddress("0x0000000000000000000000000000000000000002")
	page := profile.Page{Snapshot: pagination.Snapshot{ChainID: 46630, BlockNumber: 42, BlockHash: [32]byte{31: 1}}, Finality: "safe", Items: []profile.Action{{Token: token, Curve: token, Name: "Alpha", Symbol: "ALP", Phase: "curve", CreatorFees: big.NewInt(11), Refund: big.NewInt(7)}}}
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterProfileRoutes(ProfileRoutes{Reader: profileReaderStub{page: page}, Verifier: profileVerifierStub{principal: privyauth.Principal{PrivyDID: "did:privy:test", Wallets: []common.Address{wallet}}}, ChainID: 46630})

	unauthorized := httptest.NewRecorder()
	server.Handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/profile", nil))
	if unauthorized.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/profile", nil)
	request.Header.Set("Authorization", "Bearer access")
	request.Header.Set("privy-id-token", "identity")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", got)
	}
	for _, want := range []string{`"as_of_block":42`, `"token":"` + token.Hex() + `"`, `"creator_fees":"11"`, `"refund":"7"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body missing %s: %s", want, response.Body.String())
		}
	}
}

func TestProfileRouteMapsReaderErrors(t *testing.T) {
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterProfileRoutes(ProfileRoutes{Reader: profileReaderStub{err: errors.New("hidden")}, Verifier: profileVerifierStub{principal: privyauth.Principal{PrivyDID: "did:privy:test"}}, ChainID: 1})
	request := httptest.NewRequest(http.MethodGet, "/v1/profile", nil)
	request.Header.Set("Authorization", "Bearer access")
	request.Header.Set("privy-id-token", "identity")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "hidden") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
