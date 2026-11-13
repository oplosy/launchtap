package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/telegram"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
)

type telegramStoreStub struct {
	owner    watchlist.Owner
	digest   [32]byte
	status   telegram.Status
	settings *telegram.Settings
	unlinked bool
	err      error
}

func (s *telegramStoreStub) CreateLinkCode(_ context.Context, o watchlist.Owner, d [32]byte) (time.Time, error) {
	s.owner, s.digest = o, d
	return time.Date(2026, 11, 12, 10, 15, 0, 0, time.UTC), s.err
}

func (s *telegramStoreStub) Status(_ context.Context, o watchlist.Owner) (telegram.Status, error) {
	s.owner = o
	return s.status, s.err
}

func (s *telegramStoreStub) UpdateSettings(_ context.Context, o watchlist.Owner, settings telegram.Settings) error {
	s.owner, s.settings = o, &settings
	if s.err == nil && settings.MinTradeWei != nil {
		s.status.Settings = settings
	}
	return s.err
}

func (s *telegramStoreStub) Unlink(_ context.Context, o watchlist.Owner) error {
	s.owner, s.unlinked = o, true
	return s.err
}

func newTelegramServer(store telegram.AccountStore, bot string) *Server {
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterTelegramRoutes(TelegramRoutes{
		Verifier: profileVerifierStub{principal: privyauth.Principal{PrivyDID: watchlistTestDID}}, Store: store,
		ChainID: 46630, DeploymentID: "dep-1", BotUsername: bot,
		NewCode: func() (string, [32]byte, error) { return "the-code", [32]byte{7}, nil },
	})
	return server
}

func TestTelegramRoutesRequireAuthConfigAndNoStore(t *testing.T) {
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/v1/me/telegram", ""},
		{http.MethodPut, "/v1/me/telegram", `{"graduations":true,"large_trades":true,"min_trade_wei":"500000000000000000"}`},
		{http.MethodDelete, "/v1/me/telegram", ""},
		{http.MethodPost, "/v1/me/telegram/link", ""},
	}
	for _, c := range routes {
		w := inboxDo(newTelegramServer(&telegramStoreStub{}, "LaunchBot"), c.method, c.path, c.body, false)
		if w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s %s unauthenticated: %d %q", c.method, c.path, w.Code, w.Header().Get("Cache-Control"))
		}
		w = inboxDo(newTelegramServer(&telegramStoreStub{}, ""), c.method, c.path, c.body, true)
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "telegram_unavailable") {
			t.Fatalf("%s %s without a bot: %d %s", c.method, c.path, w.Code, w.Body.String())
		}
	}
}

func TestTelegramLinkReturnsDeepLink(t *testing.T) {
	store := &telegramStoreStub{}
	w := inboxDo(newTelegramServer(store, "LaunchBot"), http.MethodPost, "/v1/me/telegram/link", "", true)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body telegramLinkBody
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.URL != "https://t.me/LaunchBot?start=the-code" || body.ExpiresAt != "2026-11-12T10:15:00.000000Z" {
		t.Fatalf("body = %+v", body)
	}
	if store.digest != [32]byte{7} || store.owner.PrivyDID != watchlistTestDID || store.owner.DeploymentID != "dep-1" {
		t.Fatalf("stored %x for %+v", store.digest, store.owner)
	}
}

func TestTelegramStatusAndSettings(t *testing.T) {
	store := &telegramStoreStub{status: telegram.Status{Settings: telegram.DefaultSettings()}}
	server := newTelegramServer(store, "LaunchBot")
	w := inboxDo(server, http.MethodGet, "/v1/me/telegram", "", true)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"linked":false`) || !strings.Contains(w.Body.String(), `"linked_at":null`) ||
		!strings.Contains(w.Body.String(), `"min_trade_wei":"500000000000000000"`) {
		t.Fatalf("status = %d %s", w.Code, w.Body.String())
	}

	store.err = telegram.ErrNotLinked
	w = inboxDo(server, http.MethodPut, "/v1/me/telegram", `{"graduations":false,"large_trades":true,"min_trade_wei":"1000000000000000000"}`, true)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "telegram_not_linked") {
		t.Fatalf("not linked = %d %s", w.Code, w.Body.String())
	}

	store.err = nil
	store.status = telegram.Status{Linked: true, LinkedAt: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC), Settings: telegram.DefaultSettings()}
	w = inboxDo(server, http.MethodPut, "/v1/me/telegram", `{"graduations":false,"large_trades":true,"min_trade_wei":"1000000000000000000"}`, true)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"graduations":false`) || !strings.Contains(w.Body.String(), `"linked_at":"2026-11-12T09:00:00.000000Z"`) {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
	if store.settings == nil || store.settings.MinTradeWei.Cmp(big.NewInt(1e18)) != 0 || store.settings.Graduations {
		t.Fatalf("stored settings = %+v", store.settings)
	}

	for _, threshold := range []string{"", "abc", "0500000000000000000", "499999999999999999", "10000000000000000000001", "-1", "1e18"} {
		store.settings = nil
		w = inboxDo(server, http.MethodPut, "/v1/me/telegram", `{"graduations":true,"large_trades":true,"min_trade_wei":"`+threshold+`"}`, true)
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "invalid_threshold") || store.settings != nil {
			t.Fatalf("threshold %q = %d %s", threshold, w.Code, w.Body.String())
		}
	}
}

func TestTelegramUnlinkAndStoreFailure(t *testing.T) {
	store := &telegramStoreStub{}
	server := newTelegramServer(store, "LaunchBot")
	if w := inboxDo(server, http.MethodDelete, "/v1/me/telegram", "", true); w.Code != http.StatusNoContent || !store.unlinked {
		t.Fatalf("unlink = %d", w.Code)
	}
	store.err = errors.New("db down " + watchlistTestDID)
	w := inboxDo(server, http.MethodGet, "/v1/me/telegram", "", true)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), watchlistTestDID) {
		t.Fatalf("failure = %d %s", w.Code, w.Body.String())
	}
}
