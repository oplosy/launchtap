package apiserver

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/realtime"
)

type observedRequest struct {
	route, method string
	status        int
}

type recordingMetrics struct {
	mu       sync.Mutex
	requests []observedRequest
	rejected []string
	opened   int
	closed   int
}

func (m *recordingMetrics) ObserveRequest(route, method string, status int, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, observedRequest{route, method, status})
}
func (m *recordingMetrics) WriteRejected(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rejected = append(m.rejected, reason)
}
func (m *recordingMetrics) StreamOpened() { m.mu.Lock(); m.opened++; m.mu.Unlock() }
func (m *recordingMetrics) StreamClosed() { m.mu.Lock(); m.closed++; m.mu.Unlock() }

func (m *recordingMetrics) last(t *testing.T) observedRequest {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		t.Fatal("no request observed")
	}
	return m.requests[len(m.requests)-1]
}

func newMetricsServer(m Metrics, limit uint64) *Server {
	cfg := DefaultConfig()
	cfg.Metrics = m
	cfg.AllowedOrigins = []string{"https://app.example"}
	if limit > 0 {
		cfg.RateLimitPerMinute, cfg.RateLimitBurst = limit, limit
	}
	s := New(cfg, ReadyFunc(func(context.Context) error { return nil }), slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.RegisterPublicRoutes(PublicRoutes{})
	s.RegisterWatchlistRoutes(WatchlistRoutes{
		Verifier: profileVerifierStub{principal: privyauth.Principal{PrivyDID: "did:privy:x"}}, Store: &watchlistStoreStub{},
		ChainID: 1, DeploymentID: "dep",
	})
	return s
}

func serve(s *Server, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestMiddlewareReportsRoutePatternNotPath(t *testing.T) {
	m := &recordingMetrics{}
	s := newMetricsServer(m, 0)

	serve(s, http.MethodGet, "/v1/tokens/0x"+strings.Repeat("a", 40)+"/trades")
	if got := m.last(t); got.route != "GET /v1/tokens/{token}/trades" || got.method != http.MethodGet {
		t.Fatalf("route = %+v, want GET /v1/tokens/{token}/trades", got)
	}

	serve(s, http.MethodGet, "/nope")
	if got := m.last(t); got.route != "unmatched" || got.status != http.StatusNotFound {
		t.Fatalf("unmatched request = %+v", got)
	}

	pre := httptest.NewRequest(http.MethodOptions, "/v1/healthz", nil)
	pre.Header.Set("Origin", "https://app.example")
	s.Handler.ServeHTTP(httptest.NewRecorder(), pre)
	if got := m.last(t); got.route != "preflight" || got.status != http.StatusNoContent {
		t.Fatalf("preflight = %+v", got)
	}
	if len(m.rejected) != 0 {
		t.Fatalf("unexpected rejections: %v", m.rejected)
	}
}

func TestMiddlewareReportsRateLimitedRequests(t *testing.T) {
	m := &recordingMetrics{}
	s := newMetricsServer(m, 1)

	serve(s, http.MethodGet, "/nope") // consumes the only token
	if w := serve(s, http.MethodGet, "/nope"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if got := m.last(t); got.route != "rate_limited" || got.status != http.StatusTooManyRequests {
		t.Fatalf("limited GET = %+v", got)
	}
	if len(m.rejected) != 0 {
		t.Fatalf("GET must not count as a rejected write: %v", m.rejected)
	}

	if w := serve(s, http.MethodPut, "/v1/me/watchlist/0x"+strings.Repeat("b", 40)); w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if len(m.rejected) != 1 || m.rejected[0] != "rate_limited" {
		t.Fatalf("rejected = %v, want [rate_limited]", m.rejected)
	}
}

func TestMiddlewareReportsUnauthorizedWrite(t *testing.T) {
	m := &recordingMetrics{}
	s := newMetricsServer(m, 0)

	if w := serve(s, http.MethodPut, "/v1/me/watchlist/0x"+strings.Repeat("b", 40)); w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if got := m.last(t); got.route != "PUT /v1/me/watchlist/{address}" {
		t.Fatalf("route = %q", got.route)
	}
	if len(m.rejected) != 1 || m.rejected[0] != "unauthorized" {
		t.Fatalf("rejected = %v, want [unauthorized]", m.rejected)
	}
}

func TestPublicListenerHasNoMetricsRoute(t *testing.T) {
	s := newMetricsServer(&recordingMetrics{}, 0)
	if w := serve(s, http.MethodGet, "/metrics"); w.Code != http.StatusNotFound {
		t.Fatalf("/metrics status = %d, want 404", w.Code)
	}
}

func TestNilMetricsIsNoop(t *testing.T) {
	s := newMetricsServer(nil, 0)
	if w := serve(s, http.MethodGet, "/v1/healthz"); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestRejectionReason(t *testing.T) {
	cases := []struct {
		method string
		status int
		want   string
	}{
		{"POST", 429, "rate_limited"}, {"PUT", 401, "unauthorized"}, {"DELETE", 403, "forbidden"},
		{"POST", 400, "invalid"}, {"PUT", 422, "invalid"}, {"PUT", 409, "conflict"}, {"PUT", 412, "conflict"},
		{"POST", 413, "too_large"}, {"GET", 429, ""}, {"POST", 500, ""}, {"POST", 404, ""},
	}
	for _, c := range cases {
		got, ok := rejectionReason(c.method, c.status)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("rejectionReason(%s, %d) = %q, %v; want %q", c.method, c.status, got, ok, c.want)
		}
	}
}

func TestSSEReportsStreamOpenAndClose(t *testing.T) {
	m := &recordingMetrics{}
	cfg := DefaultConfig()
	cfg.Metrics = m
	server := New(cfg, ReadyFunc(func(context.Context) error { return nil }), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.RegisterEventRoutes(EventRoutes{Hub: realtime.NewHub(4, 2), ChainID: 1, DeploymentID: "dep", Heartbeat: time.Hour})
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()

	ctx, cancel := context.WithCancel(t.Context())
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/v1/events", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	readUntil(t, bufio.NewReader(response.Body), "refresh-only", time.Second)
	m.mu.Lock()
	opened := m.opened
	m.mu.Unlock()
	if opened != 1 {
		t.Fatalf("opened = %d, want 1", opened)
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		closed := m.closed
		m.mu.Unlock()
		if closed == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("StreamClosed was not called")
}
