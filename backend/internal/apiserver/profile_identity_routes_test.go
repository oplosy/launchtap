package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/social"
	"github.com/ethereum/go-ethereum/common"
)

type profileStoreStub struct {
	mu        sync.Mutex
	putErr    error
	deleteErr error
	profile   social.PublicProfile
	usernames []*string
	wallets   [][]common.Address
	deletes   int
}

func (s *profileStoreStub) Get(context.Context, social.Actor) (social.PublicProfile, error) {
	return s.profile, nil
}

func (s *profileStoreStub) Put(_ context.Context, _ social.Actor, username *string, wallets []common.Address) (social.PublicProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usernames = append(s.usernames, username)
	s.wallets = append(s.wallets, wallets)
	return s.profile, s.putErr
}

func (s *profileStoreStub) Delete(context.Context, social.Actor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes++
	return s.deleteErr
}

func newProfileServer(store social.ProfileStore) *Server {
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterPublicProfileRoutes(PublicProfileRoutes{
		Verifier: profileVerifierStub{principal: privyauth.Principal{PrivyDID: commentTestDID, Wallets: []common.Address{common.HexToAddress(commentWalletHex)}}},
		Store:    store, ChainID: 46630, DeploymentID: "dep-1",
	})
	return server
}

func TestPublicProfileStatusTable(t *testing.T) {
	const path = "/v1/me/public-profile"
	putBody := func(name string) string {
		return `{"username":"` + name + `","public_wallets":["` + commentWalletHex + `"]}`
	}
	cases := []struct {
		name   string
		err    error
		body   string
		status int
		code   string
	}{
		{"taken", social.ErrUsernameTaken, putBody("alice"), 409, "username_taken"},
		{"invalid username", nil, putBody("a"), 422, "invalid_username"},
		{"reserved username", nil, putBody("Admin_1"), 422, "username_reserved"},
		{"too many wallets", social.ErrTooManyPublicWallets, putBody("alice"), 422, "too_many_public_wallets"},
		{"malformed wallet", nil, `{"public_wallets":["0x12"]}`, 422, "invalid_wallet"},
		{"wallet not linked", social.ErrWalletNotLinked, putBody("alice"), 403, "wallet_not_linked"},
		{"banned", social.ErrBanned, putBody("alice"), 403, "banned"},
		{"rate limited", &social.RateLimitedError{RetryAfter: 1500 * time.Millisecond}, putBody("alice"), 429, "rate_limited"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &profileStoreStub{putErr: tc.err}
			w := serveComment(newProfileServer(store), commentRequest("PUT", path, tc.body, true))
			if w.Code != tc.status || problemCode(t, w) != tc.code {
				t.Fatalf("status=%d code=%q body=%s; want %d %q", w.Code, problemCode(t, w), w.Body.String(), tc.status, tc.code)
			}
			if got := w.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Fatalf("Cache-Control = %q", got)
			}
			if tc.status == 429 && w.Header().Get("Retry-After") != "2" {
				t.Fatalf("Retry-After = %q; want 2", w.Header().Get("Retry-After"))
			}
		})
	}

	t.Run("unauthenticated", func(t *testing.T) {
		w := serveComment(newProfileServer(&profileStoreStub{}), commentRequest("GET", path, "", false))
		if w.Code != 401 {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestPublicProfileRoundTrip(t *testing.T) {
	name := "alice"
	changed := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	next := changed.Add(social.UsernameChangeInterval)
	store := &profileStoreStub{profile: social.PublicProfile{
		Username: &name, UsernameChangedAt: &changed, NextChangeAt: &next,
		PublicWallets: []common.Address{common.HexToAddress(commentWalletHex)},
	}}
	server := newProfileServer(store)

	w := serveComment(server, commentRequest("PUT", "/v1/me/public-profile", `{"username":"  Alice ","public_wallets":["`+commentWalletHex+`"]}`, true))
	if w.Code != http.StatusOK {
		t.Fatalf("put status %d: %s", w.Code, w.Body.String())
	}
	if len(store.usernames) != 1 || store.usernames[0] == nil || *store.usernames[0] != "alice" {
		t.Fatalf("store got username %v; want normalized alice", store.usernames)
	}
	var dto map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatal(err)
	}
	if dto["username"] != "alice" || dto["next_username_change_at"] != "2026-11-16T12:00:00.000000Z" || dto["banned"] != false {
		t.Fatalf("dto = %v", dto)
	}

	w = serveComment(server, commentRequest("PUT", "/v1/me/public-profile", `{"public_wallets":[]}`, true))
	if w.Code != http.StatusOK || store.usernames[1] != nil {
		t.Fatalf("wallets-only put: status %d username %v", w.Code, store.usernames[1])
	}

	w = serveComment(server, commentRequest("DELETE", "/v1/me/public-profile", "", true))
	if w.Code != http.StatusNoContent || store.deletes != 1 {
		t.Fatalf("delete status %d deletes %d", w.Code, store.deletes)
	}

	store.profile = social.PublicProfile{PublicWallets: []common.Address{}}
	w = serveComment(server, commentRequest("GET", "/v1/me/public-profile", "", true))
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || dto["username"] != nil || dto["next_username_change_at"] != nil {
		t.Fatalf("empty profile: %d %v", w.Code, dto)
	}
	if wallets, ok := dto["public_wallets"].([]any); !ok || len(wallets) != 0 {
		t.Fatalf("public_wallets = %v; want []", dto["public_wallets"])
	}
}
