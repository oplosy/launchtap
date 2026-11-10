package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/forum"
	"github.com/Contictus/launchtap/backend/internal/social"
	"github.com/ethereum/go-ethereum/common"
)

type forumStoreStub struct {
	page        forum.PostPage
	communities []forum.Community
	err         error
	scope       social.Scope
	cursor      string
	limit       int
	sort        forum.Sort
}

func (s *forumStoreStub) Posts(_ context.Context, sc social.Scope, cursor string, limit int) (forum.PostPage, error) {
	s.scope, s.cursor, s.limit = sc, cursor, limit
	return s.page, s.err
}

func (s *forumStoreStub) Communities(_ context.Context, sc social.Scope, sort forum.Sort, limit int) ([]forum.Community, error) {
	s.scope, s.sort, s.limit = sc, sort, limit
	return s.communities, s.err
}

func forumServer(store *forumStoreStub) *Server {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterForumRoutes(ForumRoutes{Store: store, ChainID: 46630, DeploymentID: "dep"})
	return s
}

func forumGet(s *Server, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestForumPostsMapsPage(t *testing.T) {
	name := "alice"
	wallet := common.HexToAddress("0x00000000000000000000000000000000000000c1")
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	stub := &forumStoreStub{page: forum.PostPage{NextCursor: "next", Items: []forum.Post{
		{Comment: social.Comment{ID: 9, Token: common.HexToAddress("0x10"), Body: "<b>hi</b>", CreatedAt: at,
			Author: social.Author{Username: &name, Role: social.RoleHolder}}, Name: "Pons", Symbol: "PONS"},
		{Comment: social.Comment{ID: 8, Token: common.HexToAddress("0x11"), Body: "gm", CreatedAt: at,
			Author: social.Author{Wallet: &wallet, Role: social.RoleCreator}}, Name: "Other", Symbol: "OTH"},
	}}}
	rec := forumGet(forumServer(stub), "/v1/forum/posts?cursor=abc&limit=5")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != forumPostsCacheControl {
		t.Fatalf("status=%d cache=%q body=%s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
	}
	if stub.scope != (social.Scope{ChainID: 46630, DeploymentID: "dep"}) || stub.cursor != "abc" || stub.limit != 5 {
		t.Fatalf("store called with %+v %q %d", stub.scope, stub.cursor, stub.limit)
	}
	var body struct {
		Items []struct {
			ID     string `json:"id"`
			Token  string `json:"token"`
			Body   string `json:"body"`
			Name   string `json:"name"`
			Symbol string `json:"symbol"`
			Author struct {
				Username *string `json:"username"`
				Wallet   *string `json:"wallet"`
				Role     string  `json:"role"`
			} `json:"author"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 || body.NextCursor != "next" {
		t.Fatalf("body=%s", rec.Body.String())
	}
	first, second := body.Items[0], body.Items[1]
	if first.ID != "9" || first.Body != "<b>hi</b>" || first.Symbol != "PONS" || first.Name != "Pons" ||
		first.Author.Username == nil || *first.Author.Username != "alice" || first.Author.Wallet != nil || first.Author.Role != "holder" {
		t.Fatalf("first=%+v", first)
	}
	if second.Author.Username != nil || second.Author.Wallet == nil || *second.Author.Wallet != wallet.Hex() || second.Author.Role != "creator" {
		t.Fatalf("second=%+v", second)
	}
}

func TestForumCommunities(t *testing.T) {
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	stub := &forumStoreStub{communities: []forum.Community{{Token: common.HexToAddress("0x10"), Name: "Pons", Symbol: "PONS", CommentCount: 4, LastCommentAt: at}}}
	s := forumServer(stub)
	rec := forumGet(s, "/v1/forum/communities")
	if rec.Code != http.StatusOK || stub.sort != forum.SortHot || stub.limit != 20 || rec.Header().Get("Cache-Control") != forumCommunitiesCacheControl {
		t.Fatalf("status=%d sort=%q limit=%d cache=%q", rec.Code, stub.sort, stub.limit, rec.Header().Get("Cache-Control"))
	}
	var body struct {
		Items []forumCommunityDTO `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Items) != 1 ||
		body.Items[0].CommentCount != 4 || body.Items[0].LastCommentAt != "2026-11-10T09:00:00.000000Z" || body.Items[0].Symbol != "PONS" {
		t.Fatalf("body=%s err=%v", rec.Body.String(), err)
	}
	if rec := forumGet(s, "/v1/forum/communities?sort=top&limit=3"); rec.Code != http.StatusOK || stub.sort != forum.SortTop || stub.limit != 3 {
		t.Fatalf("top: status=%d sort=%q limit=%d", rec.Code, stub.sort, stub.limit)
	}
}

func TestForumValidationAndFailures(t *testing.T) {
	s := forumServer(&forumStoreStub{err: social.ErrInvalidCursor})
	for _, tc := range []struct {
		target string
		status int
		code   string
	}{
		{"/v1/forum/posts?cursor=bad", http.StatusUnprocessableEntity, "invalid_cursor"},
		{"/v1/forum/posts?limit=0", http.StatusUnprocessableEntity, "invalid_limit"},
		{"/v1/forum/posts?limit=51", http.StatusUnprocessableEntity, "invalid_limit"},
		{"/v1/forum/communities?sort=new", http.StatusBadRequest, "invalid_sort"},
		{"/v1/forum/communities?limit=51", http.StatusUnprocessableEntity, "invalid_limit"},
	} {
		rec := forumGet(s, tc.target)
		var problem struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &problem)
		if rec.Code != tc.status || problem.Type != "urn:launchpad:problem:"+tc.code {
			t.Fatalf("%s: status=%d body=%s", tc.target, rec.Code, rec.Body.String())
		}
	}
	failing := forumServer(&forumStoreStub{err: errors.New("db down")})
	for _, target := range []string{"/v1/forum/posts", "/v1/forum/communities"} {
		rec := forumGet(failing, target)
		if rec.Code != http.StatusInternalServerError || rec.Header().Get("Cache-Control") == forumPostsCacheControl ||
			rec.Header().Get("Cache-Control") == forumCommunitiesCacheControl {
			t.Fatalf("%s: status=%d cache=%q", target, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
}
