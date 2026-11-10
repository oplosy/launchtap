package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/inbox"
	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/ethereum/go-ethereum/common"
)

type inboxStoreStub struct {
	owner   watchlist.Owner
	before  *inbox.Position
	limit   int
	through *inbox.Position
	page    inbox.Page
	err     error
}

func (s *inboxStoreStub) List(_ context.Context, o watchlist.Owner, before *inbox.Position, limit int) (inbox.Page, error) {
	s.owner, s.before, s.limit = o, before, limit
	return s.page, s.err
}

func (s *inboxStoreStub) MarkRead(_ context.Context, o watchlist.Owner, through inbox.Position) error {
	s.owner, s.through = o, &through
	return s.err
}

func newInboxServer(store inbox.Store) *Server {
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterInboxRoutes(InboxRoutes{
		Verifier: profileVerifierStub{principal: privyauth.Principal{PrivyDID: watchlistTestDID}}, Store: store,
		ChainID: 46630, DeploymentID: "dep-1",
	})
	return server
}

func inboxDo(server *Server, method, path, body string, authed bool) *httptest.ResponseRecorder {
	r := watchlistRequest(method, path, authed)
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if authed {
			r.Header.Set("Authorization", "Bearer access")
			r.Header.Set("privy-id-token", "identity")
		}
	}
	w := httptest.NewRecorder()
	server.Handler.ServeHTTP(w, r)
	return w
}

func TestInboxRequiresAuthAndNoStore(t *testing.T) {
	server := newInboxServer(&inboxStoreStub{})
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/me/notifications", ""},
		{http.MethodPut, "/v1/me/notifications/read", `{"through":"1-0-0"}`},
	} {
		w := inboxDo(server, c.method, c.path, c.body, false)
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "authentication_failed") {
			t.Fatalf("%s %s status=%d body=%s", c.method, c.path, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("%s Cache-Control=%q", c.path, got)
		}
	}
}

func TestInboxListMapsPage(t *testing.T) {
	buy := true
	trader := common.HexToAddress("0xc1")
	stub := &inboxStoreStub{page: inbox.Page{
		Items: []inbox.Event{
			{Position: inbox.Position{Block: 12, TxIndex: 1, LogIndex: 3}, Kind: inbox.KindLargeTrade, Token: common.HexToAddress("0xaa"),
				Name: "A", Symbol: "AAA", BlockTime: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC), TxHash: common.HexToHash("0x01"),
				ETHWei: big.NewInt(600), SideBuy: &buy, Trader: &trader, Source: "curve"},
			{Position: inbox.Position{Block: 11, TxIndex: 0, LogIndex: 0}, Kind: inbox.KindGraduation, Token: common.HexToAddress("0xbb"),
				Name: "B", Symbol: "BBB", BlockTime: time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC), TxHash: common.HexToHash("0x02"), ETHWei: big.NewInt(7)},
		},
		Next:   &inbox.Position{Block: 11, TxIndex: 0, LogIndex: 0},
		Unread: 2,
	}}
	server := newInboxServer(stub)
	w := inboxDo(server, http.MethodGet, "/v1/me/notifications?cursor=13-0-0&limit=2", "", true)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if stub.owner != (watchlist.Owner{ChainID: 46630, DeploymentID: "dep-1", PrivyDID: watchlistTestDID}) || stub.limit != 2 ||
		stub.before == nil || *stub.before != (inbox.Position{Block: 13}) {
		t.Fatalf("store called with %+v %+v %d", stub.owner, stub.before, stub.limit)
	}
	var body struct {
		Items       []map[string]any `json:"items"`
		NextCursor  string           `json:"next_cursor"`
		UnreadCount int              `json:"unread_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.NextCursor != "11-0-0" || body.UnreadCount != 2 || len(body.Items) != 2 {
		t.Fatalf("body = %s", w.Body.String())
	}
	trade, grad := body.Items[0], body.Items[1]
	if trade["id"] != "12-1-3" || trade["kind"] != "large_trade" || trade["eth_wei"] != "600" || trade["side_buy"] != true ||
		trade["trader"] != trader.Hex() || trade["source"] != "curve" || trade["block_time"] != "2026-11-10T09:00:00.000000Z" {
		t.Fatalf("trade = %v", trade)
	}
	if grad["kind"] != "graduation" || grad["side_buy"] != nil || grad["trader"] != nil || grad["source"] != nil {
		t.Fatalf("graduation = %v", grad)
	}

	// Default limit and no cursor.
	inboxDo(server, http.MethodGet, "/v1/me/notifications", "", true)
	if stub.limit != inbox.DefaultPageSize || stub.before != nil {
		t.Fatalf("defaults: limit=%d before=%v", stub.limit, stub.before)
	}
}

func TestInboxValidation(t *testing.T) {
	server := newInboxServer(&inboxStoreStub{})
	for _, c := range []struct{ method, path, body, code string }{
		{http.MethodGet, "/v1/me/notifications?cursor=abc", "", "invalid_cursor"},
		{http.MethodGet, "/v1/me/notifications?limit=0", "", "invalid_limit"},
		{http.MethodGet, "/v1/me/notifications?limit=51", "", "invalid_limit"},
		{http.MethodPut, "/v1/me/notifications/read", `{"through":"1-2"}`, "invalid_notification_id"},
		{http.MethodPut, "/v1/me/notifications/read", `{"through":""}`, "invalid_notification_id"},
	} {
		w := inboxDo(server, c.method, c.path, c.body, true)
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), c.code) {
			t.Fatalf("%s %s %s: status=%d body=%s", c.method, c.path, c.body, w.Code, w.Body.String())
		}
	}
}

func TestInboxMarkRead(t *testing.T) {
	stub := &inboxStoreStub{}
	server := newInboxServer(stub)
	w := inboxDo(server, http.MethodPut, "/v1/me/notifications/read", `{"through":"12-1-3"}`, true)
	if w.Code != http.StatusNoContent || stub.through == nil || *stub.through != (inbox.Position{Block: 12, TxIndex: 1, LogIndex: 3}) {
		t.Fatalf("status=%d through=%v body=%s", w.Code, stub.through, w.Body.String())
	}
	failing := newInboxServer(&inboxStoreStub{err: errors.New("did:privy:leak")})
	w = inboxDo(failing, http.MethodPut, "/v1/me/notifications/read", `{"through":"12-1-3"}`, true)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "did:privy") {
		t.Fatalf("failure status=%d body=%s", w.Code, w.Body.String())
	}
}
