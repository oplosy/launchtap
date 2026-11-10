package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/social"
	"github.com/ethereum/go-ethereum/common"
)

const (
	commentTestDID   = "did:privy:cm-secret-did-98765"
	commentTokenHex  = "0x0000000000000000000000000000000000000002"
	commentWalletHex = "0x00000000000000000000000000000000000000a1"
)

type commentStoreStub struct {
	mu        sync.Mutex
	listErr   error
	eligErr   error
	postErr   error
	reportErr error
	page      social.CommentPage
	elig      social.Eligibility
	posted    social.Comment
	actors    []social.Actor
	bodies    []string
	listArgs  []string
	reports   []social.ReportReason
}

func (s *commentStoreStub) List(_ context.Context, _ social.Scope, _ common.Address, cursor string, _ int) (social.CommentPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listArgs = append(s.listArgs, cursor)
	return s.page, s.listErr
}

func (s *commentStoreStub) Eligibility(_ context.Context, a social.Actor, _ common.Address) (social.Eligibility, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actors = append(s.actors, a)
	return s.elig, s.eligErr
}

// Post mirrors the store contract's order: the ban check comes before body validation.
func (s *commentStoreStub) Post(_ context.Context, a social.Actor, _, _ common.Address, body string) (social.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actors = append(s.actors, a)
	s.bodies = append(s.bodies, body)
	if errors.Is(s.postErr, social.ErrBanned) {
		return social.Comment{}, s.postErr
	}
	if _, err := social.NormalizeBody(body); err != nil {
		return social.Comment{}, err
	}
	return s.posted, s.postErr
}

func (s *commentStoreStub) Report(_ context.Context, a social.Actor, _ int64, reason social.ReportReason) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actors = append(s.actors, a)
	s.reports = append(s.reports, reason)
	return s.reportErr
}

func newCommentServer(store social.CommentStore, logger *slog.Logger) *Server {
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), logger)
	wallet := common.HexToAddress(commentWalletHex)
	server.RegisterCommentRoutes(CommentRoutes{
		Verifier: profileVerifierStub{principal: privyauth.Principal{PrivyDID: commentTestDID, Wallets: []common.Address{wallet, wallet}}},
		Store:    store, ChainID: 46630, DeploymentID: "dep-1",
	})
	return server
}

func commentRequest(method, path, body string, authed bool) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if authed {
		r.Header.Set("Authorization", "Bearer access")
		r.Header.Set("privy-id-token", "identity")
	}
	return r
}

func serveComment(server *Server, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	server.Handler.ServeHTTP(w, r)
	return w
}

func problemCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var p struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return strings.TrimPrefix(p.Type, "urn:launchpad:problem:")
}

const postBody = `{"body":"gm","wallet":"` + commentWalletHex + `"}`

func TestCommentRoutesStatusTable(t *testing.T) {
	tokenPath := "/v1/tokens/" + commentTokenHex + "/comments"
	reportReq := func(id, reason string) func() *http.Request {
		return func() *http.Request {
			return commentRequest("PUT", "/v1/comments/"+id+"/report", `{"reason":"`+reason+`"}`, true)
		}
	}
	postReq := func() *http.Request { return commentRequest("POST", tokenPath, postBody, true) }
	cases := []struct {
		name   string
		mutate func(*commentStoreStub)
		req    func() *http.Request
		status int
		code   string
	}{
		{"list unknown token", func(s *commentStoreStub) { s.listErr = social.ErrUnknownToken }, func() *http.Request { return commentRequest("GET", tokenPath, "", false) }, 404, "token_not_found"},
		{"list invalid cursor from store", func(s *commentStoreStub) { s.listErr = social.ErrInvalidCursor }, func() *http.Request { return commentRequest("GET", tokenPath+"?cursor=zz", "", false) }, 422, "invalid_cursor"},
		{"list limit 0", nil, func() *http.Request { return commentRequest("GET", tokenPath+"?limit=0", "", false) }, 422, "invalid_limit"},
		{"list limit 51", nil, func() *http.Request { return commentRequest("GET", tokenPath+"?limit=51", "", false) }, 422, "invalid_limit"},
		{"list bad address", nil, func() *http.Request { return commentRequest("GET", "/v1/tokens/0x12/comments", "", false) }, 422, ""},
		{"eligibility unauthenticated", nil, func() *http.Request { return commentRequest("GET", tokenPath+"/eligibility", "", false) }, 401, "authentication_failed"},
		{"eligibility unknown token", func(s *commentStoreStub) { s.eligErr = social.ErrUnknownToken }, func() *http.Request { return commentRequest("GET", tokenPath+"/eligibility", "", true) }, 404, "token_not_found"},
		{"post unauthenticated", nil, func() *http.Request { return commentRequest("POST", tokenPath, postBody, false) }, 401, "authentication_failed"},
		{"post bad wallet", nil, func() *http.Request {
			return commentRequest("POST", tokenPath, `{"body":"gm","wallet":"0x12"}`, true)
		}, 422, ""},
		{"post invalid body", nil, func() *http.Request {
			return commentRequest("POST", tokenPath, `{"body":"   ","wallet":"`+commentWalletHex+`"}`, true)
		}, 422, "invalid_comment"},
		{"post unknown token", func(s *commentStoreStub) { s.postErr = social.ErrUnknownToken }, postReq, 404, "token_not_found"},
		{"post wallet not linked", func(s *commentStoreStub) { s.postErr = social.ErrWalletNotLinked }, postReq, 403, "wallet_not_linked"},
		{"post not holder", func(s *commentStoreStub) { s.postErr = social.ErrNotHolder }, postReq, 403, "not_holder"},
		{"post banned", func(s *commentStoreStub) { s.postErr = social.ErrBanned }, postReq, 403, "banned"},
		{"post banned with invalid body", func(s *commentStoreStub) { s.postErr = social.ErrBanned }, func() *http.Request {
			return commentRequest("POST", tokenPath, `{"body":"   ","wallet":"`+commentWalletHex+`"}`, true)
		}, 403, "banned"},
		{"post rate limited", func(s *commentStoreStub) { s.postErr = &social.RateLimitedError{RetryAfter: time.Second} }, postReq, 429, "rate_limited"},
		{"post internal", func(s *commentStoreStub) { s.postErr = errors.New("boom " + commentTestDID) }, postReq, 500, "internal_error"},
		{"report unauthenticated", nil, func() *http.Request {
			return commentRequest("PUT", "/v1/comments/7/report", `{"reason":"spam"}`, false)
		}, 401, "authentication_failed"},
		{"report invalid reason", nil, reportReq("7", "rude"), 422, "invalid_report"},
		{"report not found", func(s *commentStoreStub) { s.reportErr = social.ErrCommentNotFound }, reportReq("7", "spam"), 404, "comment_not_found"},
		{"report own", func(s *commentStoreStub) { s.reportErr = social.ErrOwnComment }, reportReq("7", "spam"), 422, "own_comment"},
		{"report banned", func(s *commentStoreStub) { s.reportErr = social.ErrBanned }, reportReq("7", "spam"), 403, "banned"},
		{"report rate limited", func(s *commentStoreStub) { s.reportErr = &social.RateLimitedError{RetryAfter: 2 * time.Second} }, reportReq("7", "spam"), 429, "rate_limited"},
		{"report ok", nil, reportReq("7", "scam"), 204, ""},
		{"report non-positive id", nil, reportReq("0", "spam"), 404, "comment_not_found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := &commentStoreStub{}
			if c.mutate != nil {
				c.mutate(store)
			}
			var logs bytes.Buffer
			w := serveComment(newCommentServer(store, slog.New(slog.NewTextHandler(&logs, nil))), c.req())
			if w.Code != c.status {
				t.Fatalf("status = %d want %d body=%s", w.Code, c.status, w.Body.String())
			}
			if c.code != "" {
				if got := problemCode(t, w); got != c.code {
					t.Fatalf("code = %q want %q", got, c.code)
				}
			}
			if strings.Contains(logs.String(), commentTestDID) || strings.Contains(w.Body.String(), commentTestDID) {
				t.Fatalf("DID leaked: logs=%s body=%s", logs.String(), w.Body.String())
			}
		})
	}
}

func TestPostCommentInvalidBodyDetailNamesRule(t *testing.T) {
	w := serveComment(newCommentServer(&commentStoreStub{}, nil), commentRequest("POST", "/v1/tokens/"+commentTokenHex+"/comments", `{"body":"a`+string(rune(0x202e))+`b","wallet":"`+commentWalletHex+`"}`, true))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "comment rule: bidi") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

// The handler passes the raw body: the store normalizes it after the ban check.
func TestPostCommentPassesRawBodyAndDedupedWallets(t *testing.T) {
	store := &commentStoreStub{posted: social.Comment{ID: 1}}
	w := serveComment(newCommentServer(store, nil), commentRequest("POST", "/v1/tokens/"+commentTokenHex+"/comments", `{"body":"  gm\r\nfren  ","wallet":"`+commentWalletHex+`"}`, true))
	if w.Code != 201 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(store.bodies) != 1 || store.bodies[0] != "  gm\r\nfren  " {
		t.Fatalf("bodies = %q", store.bodies)
	}
	a := store.actors[0]
	if a.PrivyDID != commentTestDID || a.ChainID != 46630 || a.DeploymentID != "dep-1" || len(a.Wallets) != 1 {
		t.Fatalf("actor = %+v", a)
	}
}

func TestListTokenCommentsCacheHeader(t *testing.T) {
	store := &commentStoreStub{}
	server := newCommentServer(store, nil)
	tokenPath := "/v1/tokens/" + commentTokenHex + "/comments"
	if got := serveComment(server, commentRequest("GET", tokenPath, "", false)).Header().Get("Cache-Control"); got != "public, max-age=5" {
		t.Fatalf("list Cache-Control = %q", got)
	}
	for name, r := range map[string]*http.Request{
		"eligibility": commentRequest("GET", tokenPath+"/eligibility", "", true),
		"post":        commentRequest("POST", tokenPath, postBody, true),
		"report":      commentRequest("PUT", "/v1/comments/7/report", `{"reason":"spam"}`, true),
	} {
		if got := serveComment(server, r).Header().Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("%s Cache-Control = %q", name, got)
		}
	}
}

func TestPostCommentRetryAfter(t *testing.T) {
	store := &commentStoreStub{postErr: &social.RateLimitedError{RetryAfter: 1500 * time.Millisecond}}
	w := serveComment(newCommentServer(store, nil), commentRequest("POST", "/v1/tokens/"+commentTokenHex+"/comments", postBody, true))
	if w.Code != 429 || w.Header().Get("Retry-After") != "2" {
		t.Fatalf("status=%d Retry-After=%q", w.Code, w.Header().Get("Retry-After"))
	}
	store.postErr = &social.RateLimitedError{RetryAfter: 100 * time.Millisecond}
	w = serveComment(newCommentServer(store, nil), commentRequest("POST", "/v1/tokens/"+commentTokenHex+"/comments", postBody, true))
	if w.Header().Get("Retry-After") != "1" {
		t.Fatalf("minimum Retry-After = %q", w.Header().Get("Retry-After"))
	}
}

func TestCommentDTOShape(t *testing.T) {
	name := "alice"
	wallet := common.HexToAddress(commentWalletHex)
	created := time.Date(2026, 11, 9, 10, 0, 0, 123456000, time.UTC)
	store := &commentStoreStub{page: social.CommentPage{Count: 2, NextCursor: "next", Items: []social.Comment{
		{ID: 9007199254740993, Token: common.HexToAddress(commentTokenHex), Body: "a", CreatedAt: created, Author: social.Author{Username: &name, Role: social.RoleHolder}},
		{ID: 5, Token: common.HexToAddress(commentTokenHex), Body: "b", CreatedAt: created, Author: social.Author{Wallet: &wallet, Role: social.RoleCreator}},
	}}}
	w := serveComment(newCommentServer(store, nil), commentRequest("GET", "/v1/tokens/"+commentTokenHex+"/comments", "", false))
	var got struct {
		Items []struct {
			ID        any    `json:"id"`
			Token     string `json:"token"`
			CreatedAt string `json:"created_at"`
			Author    struct {
				Username *string `json:"username"`
				Wallet   *string `json:"wallet"`
				Role     string  `json:"role"`
			} `json:"author"`
		} `json:"items"`
		NextCursor   string `json:"next_cursor"`
		CommentCount int64  `json:"comment_count"`
	}
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if id, ok := got.Items[0].ID.(string); !ok || id != "9007199254740993" {
		t.Fatalf("id = %#v; want decimal string", got.Items[0].ID)
	}
	if got.Items[0].Author.Wallet != nil || got.Items[0].Author.Username == nil || *got.Items[0].Author.Username != "alice" {
		t.Fatalf("author 0 = %+v", got.Items[0].Author)
	}
	if got.Items[1].Author.Username != nil || got.Items[1].Author.Wallet == nil || *got.Items[1].Author.Wallet != wallet.Hex() || got.Items[1].Author.Role != "creator" {
		t.Fatalf("author 1 = %+v", got.Items[1].Author)
	}
	if got.Items[0].CreatedAt != "2026-11-09T10:00:00.123456Z" || got.NextCursor != "next" || got.CommentCount != 2 {
		t.Fatalf("got = %+v", got)
	}
	if !strings.Contains(w.Body.String(), `"wallet":null`) || !strings.Contains(w.Body.String(), `"username":null`) {
		t.Fatalf("nulls must be explicit: %s", w.Body.String())
	}
}

func TestEligibilityDTO(t *testing.T) {
	name := "bob"
	store := &commentStoreStub{elig: social.Eligibility{Banned: true, Username: &name, Wallets: []social.EligibleWallet{{Address: common.HexToAddress(commentWalletHex), Role: social.RoleHolder}}}}
	w := serveComment(newCommentServer(store, nil), commentRequest("GET", "/v1/tokens/"+commentTokenHex+"/comments/eligibility", "", true))
	var got struct {
		Wallets []struct {
			Address string `json:"address"`
			Role    string `json:"role"`
		} `json:"eligible_wallets"`
		Banned   bool    `json:"banned"`
		Username *string `json:"username"`
	}
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Wallets) != 1 || got.Wallets[0].Address != common.HexToAddress(commentWalletHex).Hex() || got.Wallets[0].Role != "holder" || !got.Banned || got.Username == nil || *got.Username != "bob" {
		t.Fatalf("got = %+v", got)
	}
	// An empty store result still marshals an array, not null.
	w = serveComment(newCommentServer(&commentStoreStub{}, nil), commentRequest("GET", "/v1/tokens/"+commentTokenHex+"/comments/eligibility", "", true))
	if !strings.Contains(w.Body.String(), `"eligible_wallets":[]`) || !strings.Contains(w.Body.String(), `"username":null`) {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestListCommentsPassesCursor(t *testing.T) {
	store := &commentStoreStub{}
	serveComment(newCommentServer(store, nil), commentRequest("GET", "/v1/tokens/"+commentTokenHex+"/comments?cursor=abc", "", false))
	if len(store.listArgs) != 1 || store.listArgs[0] != "abc" {
		t.Fatalf("listArgs = %v", store.listArgs)
	}
}

func TestCommentRoutesUnavailableWithoutDeps(t *testing.T) {
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterCommentRoutes(CommentRoutes{})
	if w := serveComment(server, commentRequest("GET", "/v1/tokens/"+commentTokenHex+"/comments/eligibility", "", true)); w.Code != 503 {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestCommentListErrorsAreNotCached(t *testing.T) {
	tokenPath := "/v1/tokens/" + commentTokenHex + "/comments"
	cases := map[string]struct {
		mutate func(*commentStoreStub)
		path   string
	}{
		"unknown token":        {func(s *commentStoreStub) { s.listErr = social.ErrUnknownToken }, tokenPath},
		"invalid store cursor": {func(s *commentStoreStub) { s.listErr = social.ErrInvalidCursor }, tokenPath + "?cursor=zz"},
		"invalid limit":        {nil, tokenPath + "?limit=0"},
		"internal error":       {func(s *commentStoreStub) { s.listErr = errors.New("boom") }, tokenPath},
	}
	for name, c := range cases {
		store := &commentStoreStub{}
		if c.mutate != nil {
			c.mutate(store)
		}
		w := serveComment(newCommentServer(store, nil), commentRequest("GET", c.path, "", false))
		if w.Code < 400 {
			t.Fatalf("%s: status=%d", name, w.Code)
		}
		if got := w.Header().Get("Cache-Control"); strings.Contains(got, "public") || strings.Contains(got, "max-age") {
			t.Fatalf("%s: error response Cache-Control = %q", name, got)
		}
	}
}

func TestCommentRoutesRejectVerifierFailureWith401(t *testing.T) {
	store := &commentStoreStub{}
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterCommentRoutes(CommentRoutes{
		Verifier: profileVerifierStub{err: errors.New("token expired")},
		Store:    store, ChainID: 46630, DeploymentID: "dep-1",
	})
	tokenPath := "/v1/tokens/" + commentTokenHex + "/comments"
	for name, r := range map[string]*http.Request{
		"eligibility": commentRequest("GET", tokenPath+"/eligibility", "", true),
		"post":        commentRequest("POST", tokenPath, postBody, true),
		"report":      commentRequest("PUT", "/v1/comments/7/report", `{"reason":"spam"}`, true),
	} {
		w := serveComment(server, r)
		if w.Code != 401 || problemCode(t, w) != "authentication_failed" {
			t.Fatalf("%s: status=%d body=%s", name, w.Code, w.Body.String())
		}
	}
	if len(store.actors) != 0 || len(store.bodies) != 0 {
		t.Fatal("store reached after verifier rejection")
	}
}
