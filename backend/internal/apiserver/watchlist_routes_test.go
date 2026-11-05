package apiserver

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/ethereum/go-ethereum/common"
)

const watchlistTestDID = "did:privy:cm-secret-did-12345"

type watchlistStoreStub struct {
	mu        sync.Mutex
	owners    []watchlist.Owner
	addErr    error
	removeErr error
	entries   []watchlist.Entry
	added     []common.Address
	removed   []common.Address
}

func (s *watchlistStoreStub) Add(_ context.Context, o watchlist.Owner, t common.Address) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owners = append(s.owners, o)
	s.added = append(s.added, t)
	return s.addErr
}

func (s *watchlistStoreStub) Remove(_ context.Context, o watchlist.Owner, t common.Address) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owners = append(s.owners, o)
	s.removed = append(s.removed, t)
	return s.removeErr
}

func (s *watchlistStoreStub) List(_ context.Context, o watchlist.Owner) ([]watchlist.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owners = append(s.owners, o)
	return s.entries, nil
}

func newWatchlistServer(store watchlist.Store, did string, logger *slog.Logger) *Server {
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), logger)
	server.RegisterWatchlistRoutes(WatchlistRoutes{
		Verifier: profileVerifierStub{principal: privyauth.Principal{PrivyDID: did}}, Store: store,
		ChainID: 46630, DeploymentID: "dep-1",
	})
	return server
}

func watchlistRequest(method, path string, authed bool) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	if authed {
		r.Header.Set("Authorization", "Bearer access")
		r.Header.Set("privy-id-token", "identity")
	}
	return r
}

const watchlistTokenHex = "0x0000000000000000000000000000000000000002"

func TestWatchlistRequiresAuth(t *testing.T) {
	server := newWatchlistServer(&watchlistStoreStub{}, watchlistTestDID, nil)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/v1/me/watchlist"},
		{http.MethodPut, "/v1/me/watchlist/" + watchlistTokenHex},
		{http.MethodDelete, "/v1/me/watchlist/" + watchlistTokenHex},
	} {
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, watchlistRequest(c.method, c.path, false))
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "authentication_failed") {
			t.Fatalf("%s %s status=%d body=%s", c.method, c.path, w.Code, w.Body.String())
		}
	}
}

func TestWatchlistRejectsOversizedDID(t *testing.T) {
	server := newWatchlistServer(&watchlistStoreStub{}, strings.Repeat("d", 256), nil)
	w := httptest.NewRecorder()
	server.Handler.ServeHTTP(w, watchlistRequest(http.MethodGet, "/v1/me/watchlist", true))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWatchlistPutIdempotent(t *testing.T) {
	store := &watchlistStoreStub{}
	server := newWatchlistServer(store, watchlistTestDID, nil)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, watchlistRequest(http.MethodPut, "/v1/me/watchlist/"+watchlistTokenHex, true))
		if w.Code != http.StatusNoContent {
			t.Fatalf("put %d status=%d body=%s", i, w.Code, w.Body.String())
		}
	}
	if len(store.added) != 2 || store.added[0] != common.HexToAddress(watchlistTokenHex) {
		t.Fatalf("added=%v", store.added)
	}
}

func TestWatchlistDeleteIdempotent(t *testing.T) {
	store := &watchlistStoreStub{}
	server := newWatchlistServer(store, watchlistTestDID, nil)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, watchlistRequest(http.MethodDelete, "/v1/me/watchlist/"+watchlistTokenHex, true))
		if w.Code != http.StatusNoContent {
			t.Fatalf("delete %d status=%d body=%s", i, w.Code, w.Body.String())
		}
	}
	if len(store.removed) != 2 {
		t.Fatalf("removed=%v", store.removed)
	}
}

func TestWatchlistFullReturns409(t *testing.T) {
	server := newWatchlistServer(&watchlistStoreStub{addErr: watchlist.ErrFull}, watchlistTestDID, nil)
	w := httptest.NewRecorder()
	server.Handler.ServeHTTP(w, watchlistRequest(http.MethodPut, "/v1/me/watchlist/"+watchlistTokenHex, true))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "watchlist_full") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWatchlistUnknownToken404(t *testing.T) {
	server := newWatchlistServer(&watchlistStoreStub{addErr: watchlist.ErrUnknownToken}, watchlistTestDID, nil)
	w := httptest.NewRecorder()
	server.Handler.ServeHTTP(w, watchlistRequest(http.MethodPut, "/v1/me/watchlist/"+watchlistTokenHex, true))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "token_not_found") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWatchlistInvalidAddress400(t *testing.T) {
	server := newWatchlistServer(&watchlistStoreStub{}, watchlistTestDID, nil)
	w := httptest.NewRecorder()
	server.Handler.ServeHTTP(w, watchlistRequest(http.MethodPut, "/v1/me/watchlist/not-an-address", true))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWatchlistListMapsEntries(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	gone := common.HexToAddress("0x0000000000000000000000000000000000000009")
	live := common.HexToAddress(watchlistTokenHex)
	store := &watchlistStoreStub{entries: []watchlist.Entry{
		{Token: live, AddedAt: at, Summary: &token.Summary{Address: live, Name: "Alpha", Symbol: "ALP", Phase: "curve", LaunchTime: at, LaunchBlock: 7}},
		{Token: gone, AddedAt: at, Summary: nil},
	}}
	server := newWatchlistServer(store, watchlistTestDID, nil)
	w := httptest.NewRecorder()
	server.Handler.ServeHTTP(w, watchlistRequest(http.MethodGet, "/v1/me/watchlist", true))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"address":"` + live.Hex() + `"`, `"added_at":"2026-01-02T03:04:05.000000Z"`, `"name":"Alpha"`, `"address":"` + gone.Hex() + `"`, `"token":null`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
}

func TestWatchlistNoStoreHeader(t *testing.T) {
	cases := []struct {
		name         string
		store        *watchlistStoreStub
		method, path string
		authed       bool
		status       int
	}{
		{"list ok", &watchlistStoreStub{}, http.MethodGet, "/v1/me/watchlist", true, http.StatusOK},
		{"put ok", &watchlistStoreStub{}, http.MethodPut, "/v1/me/watchlist/" + watchlistTokenHex, true, http.StatusNoContent},
		{"delete ok", &watchlistStoreStub{}, http.MethodDelete, "/v1/me/watchlist/" + watchlistTokenHex, true, http.StatusNoContent},
		{"401", &watchlistStoreStub{}, http.MethodGet, "/v1/me/watchlist", false, http.StatusUnauthorized},
		{"400", &watchlistStoreStub{}, http.MethodPut, "/v1/me/watchlist/zzz", true, http.StatusBadRequest},
		{"404", &watchlistStoreStub{addErr: watchlist.ErrUnknownToken}, http.MethodPut, "/v1/me/watchlist/" + watchlistTokenHex, true, http.StatusNotFound},
		{"409", &watchlistStoreStub{addErr: watchlist.ErrFull}, http.MethodPut, "/v1/me/watchlist/" + watchlistTokenHex, true, http.StatusConflict},
		{"500", &watchlistStoreStub{addErr: errors.New("boom")}, http.MethodPut, "/v1/me/watchlist/" + watchlistTokenHex, true, http.StatusInternalServerError},
	}
	for _, c := range cases {
		server := newWatchlistServer(c.store, watchlistTestDID, nil)
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, watchlistRequest(c.method, c.path, c.authed))
		if w.Code != c.status {
			t.Fatalf("%s: status=%d want %d body=%s", c.name, w.Code, c.status, w.Body.String())
		}
		if got := w.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("%s: Cache-Control=%q", c.name, got)
		}
	}
}

func TestWatchlistScopesByDID(t *testing.T) {
	store := &watchlistStoreStub{}
	server := newWatchlistServer(store, watchlistTestDID, nil)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/v1/me/watchlist"},
		{http.MethodPut, "/v1/me/watchlist/" + watchlistTokenHex},
		{http.MethodDelete, "/v1/me/watchlist/" + watchlistTokenHex},
	} {
		r := watchlistRequest(c.method, c.path, true)
		r.Header.Set("X-Privy-DID", "did:privy:attacker")
		server.Handler.ServeHTTP(httptest.NewRecorder(), r)
	}
	if len(store.owners) != 3 {
		t.Fatalf("owners=%v", store.owners)
	}
	for _, o := range store.owners {
		if o != (watchlist.Owner{ChainID: 46630, DeploymentID: "dep-1", PrivyDID: watchlistTestDID}) {
			t.Fatalf("owner=%+v", o)
		}
	}
}

func TestWatchlistLogsOmitDID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	for _, store := range []*watchlistStoreStub{{}, {addErr: errors.New("db failed for " + watchlistTestDID)}, {addErr: watchlist.ErrFull}} {
		server := newWatchlistServer(store, watchlistTestDID, logger)
		for _, c := range []struct{ method, path string }{
			{http.MethodGet, "/v1/me/watchlist"},
			{http.MethodPut, "/v1/me/watchlist/" + watchlistTokenHex},
			{http.MethodDelete, "/v1/me/watchlist/" + watchlistTokenHex},
		} {
			w := httptest.NewRecorder()
			server.Handler.ServeHTTP(w, watchlistRequest(c.method, c.path, true))
			if strings.Contains(w.Body.String(), watchlistTestDID) {
				t.Fatalf("response leaks DID: %s", w.Body.String())
			}
		}
	}
	if buf.Len() == 0 {
		t.Fatal("expected request logs")
	}
	if strings.Contains(buf.String(), watchlistTestDID) {
		t.Fatalf("logs leak DID: %s", buf.String())
	}
}
