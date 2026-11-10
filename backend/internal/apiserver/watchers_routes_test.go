package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

type watcherCounterStub struct {
	count        int64
	err          error
	chainID      int64
	deploymentID string
	token        common.Address
}

func (s *watcherCounterStub) CountWatchers(_ context.Context, chainID int64, deploymentID string, t common.Address) (int64, error) {
	s.chainID, s.deploymentID, s.token = chainID, deploymentID, t
	return s.count, s.err
}

func watchersServer(counter *watcherCounterStub) *Server {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterWatchersRoutes(WatchersRoutes{Counter: counter, ChainID: 46630, DeploymentID: "dep"})
	return s
}

func TestTokenWatchers(t *testing.T) {
	stub := &watcherCounterStub{count: 7}
	s := watchersServer(stub)
	token := "0x00000000000000000000000000000000000000Aa"
	rec := httptest.NewRecorder()
	s.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tokens/"+token+"/watchers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		WatchCount int64 `json:"watch_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.WatchCount != 7 {
		t.Fatalf("body=%s err=%v", rec.Body.String(), err)
	}
	if got := rec.Header().Get("Cache-Control"); got != watchersCacheControl {
		t.Fatalf("Cache-Control=%q", got)
	}
	if stub.chainID != 46630 || stub.deploymentID != "dep" || stub.token != common.HexToAddress(token) {
		t.Fatalf("counter called with %d %q %s", stub.chainID, stub.deploymentID, stub.token)
	}

	for _, bad := range []string{"0x12", "nothex00000000000000000000000000000000000000"} {
		rec := httptest.NewRecorder()
		s.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tokens/"+bad+"/watchers", nil))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status=%d", bad, rec.Code)
		}
	}

	failing := watchersServer(&watcherCounterStub{err: errors.New("db down")})
	rec = httptest.NewRecorder()
	failing.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tokens/"+token+"/watchers", nil))
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Cache-Control") == watchersCacheControl {
		t.Fatalf("failure: status=%d cache=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}
