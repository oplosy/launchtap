package apiserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestClientAddressOnlyTrustsForwardedForFromProxies(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	request := func(remote string, forwarded ...string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/v1/tokens", nil)
		r.RemoteAddr = remote
		for _, value := range forwarded {
			r.Header.Add("X-Forwarded-For", value)
		}
		return r
	}
	cases := []struct {
		name    string
		request *http.Request
		want    string
	}{
		{"direct client ignores header", request("203.0.113.9:4000", "198.51.100.1"), "203.0.113.9"},
		{"proxy uses nearest untrusted hop", request("10.0.0.2:4000", "198.51.100.1, 203.0.113.7"), "203.0.113.7"},
		{"spoofed left entry is ignored", request("10.0.0.2:4000", "1.1.1.1", "203.0.113.7, 10.0.0.5"), "203.0.113.7"},
		{"malformed hop stops at last trusted", request("10.0.0.2:4000", "garbage"), "10.0.0.2"},
		{"proxy without header", request("10.0.0.2:4000"), "10.0.0.2"},
	}
	for _, tc := range cases {
		if got := clientAddress(tc.request, proxies).String(); got != tc.want {
			t.Errorf("%s: client=%s want %s", tc.name, got, tc.want)
		}
	}
	if got := clientKeyFor(netip.MustParseAddr("2001:db8:1:2:3:4:5:6")); got != "2001:db8:1:2::/64" {
		t.Fatalf("IPv6 key=%s", got)
	}
}

func TestRateLimiterRefillsAndBoundsBurst(t *testing.T) {
	limiter := NewRateLimiter(60, 2)
	now := time.Unix(1_700_000_000, 0)
	for range 2 {
		if ok, _ := limiter.Allow("a", now); !ok {
			t.Fatal("burst token rejected")
		}
	}
	ok, wait := limiter.Allow("a", now)
	if ok || wait <= 0 || wait > time.Second {
		t.Fatalf("exhausted bucket ok=%v wait=%s", ok, wait)
	}
	if ok, _ := limiter.Allow("b", now); !ok {
		t.Fatal("independent client was limited")
	}
	if ok, _ := limiter.Allow("a", now.Add(time.Second)); !ok {
		t.Fatal("bucket did not refill")
	}
	if ok, _ := limiter.Allow("a", now.Add(time.Hour)); !ok {
		t.Fatal("idle bucket did not refill")
	}
	if ok, _ := limiter.Allow("a", now.Add(time.Hour)); !ok {
		t.Fatal("refill exceeded or lost burst")
	}
	if ok, _ := limiter.Allow("a", now.Add(time.Hour)); ok {
		t.Fatal("refill exceeded burst")
	}
}

func TestMiddlewareRateLimitsClientsButNotProbes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RateLimitPerMinute, cfg.RateLimitBurst = 1, 1
	s := New(cfg, ReadyFunc(func(context.Context) error { return nil }), nil)
	serve := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "203.0.113.9:4000"
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, r)
		return w
	}
	if w := serve("/v1/unknown"); w.Code == http.StatusTooManyRequests {
		t.Fatal("first request limited")
	}
	w := serve("/v1/unknown")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("second request status=%d retry=%q", w.Code, w.Header().Get("Retry-After"))
	}
	if w := serve("/healthz"); w.Code != http.StatusOK {
		t.Fatalf("probe limited: %d", w.Code)
	}
}

func TestRequestIDIsReplacedWhenUnsafe(t *testing.T) {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	for value, keep := range map[string]bool{"trace-1.a:b_c": true, "bad id\nforged": false, strings.Repeat("a", 129): false} {
		r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		r.Header.Set("X-Request-ID", value)
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, r)
		if got := w.Header().Get("X-Request-ID"); (got == value) != keep || got == "" {
			t.Fatalf("request id %q became %q", value, got)
		}
	}
}

func TestStreamLimiterCapsConcurrentStreamsPerClient(t *testing.T) {
	limiter := NewStreamLimiter(2)
	first, ok := limiter.Acquire("a")
	if !ok {
		t.Fatal("first stream rejected")
	}
	if _, ok := limiter.Acquire("a"); !ok {
		t.Fatal("second stream rejected")
	}
	if _, ok := limiter.Acquire("a"); ok {
		t.Fatal("third stream accepted")
	}
	if _, ok := limiter.Acquire("b"); !ok {
		t.Fatal("other client rejected")
	}
	first()
	first()
	if _, ok := limiter.Acquire("a"); !ok {
		t.Fatal("released stream slot was not reusable")
	}
	if _, ok := limiter.Acquire("a"); ok {
		t.Fatal("double release freed two slots")
	}
}
