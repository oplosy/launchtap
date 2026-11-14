package pricefeed

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "cg-secret-key-123"

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

var epoch = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func body(price string, at time.Time) string {
	return fmt.Sprintf(`{"ethereum":{"usd":%s,"last_updated_at":%d}}`, price, at.Unix())
}

func newFeed(t *testing.T, h http.HandlerFunc) (*Feed, *clock) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := &clock{t: epoch}
	f := New(Options{BaseURL: srv.URL, APIKey: testKey, Now: c.Now, Jitter: func(d time.Duration) time.Duration { return d }})
	return f, c
}

func serve(status int, b string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(b))
	}
}

func TestRefreshSendsKeyHeader(t *testing.T) {
	f, _ := newFeed(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/simple/price" {
			t.Errorf("path=%q", r.URL.Path)
		}
		if got := r.Header.Get("x-cg-pro-api-key"); got != testKey {
			t.Errorf("key header=%q", got)
		}
		if got, want := r.URL.RawQuery, "ids=ethereum&vs_currencies=usd&include_last_updated_at=true&precision=full"; got != want {
			t.Errorf("query=%q want %q", got, want)
		}
		if strings.Contains(r.URL.String(), testKey) {
			t.Errorf("key leaked into URL")
		}
		_, _ = w.Write([]byte(body("3456.12", epoch.Add(-30*time.Second))))
	})
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDemoPlanUsesDemoHeaderAndCadence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-cg-demo-api-key"); got != testKey {
			t.Errorf("demo key header=%q", got)
		}
		if got := r.Header.Get("x-cg-pro-api-key"); got != "" {
			t.Errorf("pro key header sent on the demo plan")
		}
		_, _ = w.Write([]byte(body("3456.12", epoch.Add(-4*time.Minute))))
	}))
	t.Cleanup(srv.Close)
	c := &clock{t: epoch}
	f := New(Options{Plan: PlanDemo, BaseURL: srv.URL, APIKey: testKey, Now: c.Now, Jitter: func(d time.Duration) time.Duration { return d }})
	if f.interval != demoInterval || f.maxAge != demoMaxAge {
		t.Fatalf("interval=%v maxAge=%v, want %v and %v", f.interval, f.maxAge, demoInterval, demoMaxAge)
	}
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A four-minute-old sample is beyond the Pro budget but fresh on the Demo cadence.
	if f.Snapshot().Price == nil {
		t.Fatal("demo sample reported stale")
	}
	if got := New(Options{Plan: PlanDemo}).baseURL; got != demoBaseURL {
		t.Fatalf("demo base URL=%q", got)
	}
}

func TestSnapshotFresh(t *testing.T) {
	f, _ := newFeed(t, serve(200, body("3456.12", epoch.Add(-30*time.Second))))
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := f.Snapshot()
	if s.Price == nil || *s.Price != "3456.12" {
		t.Fatalf("price=%v", s.Price)
	}
	if s.ProviderUpdatedAt == nil || !s.ProviderUpdatedAt.Equal(epoch.Add(-30*time.Second)) || s.FetchedAt == nil || !s.FetchedAt.Equal(epoch) {
		t.Fatalf("timestamps %v %v", s.ProviderUpdatedAt, s.FetchedAt)
	}
}

func TestSnapshotEmptyBeforeFirstSample(t *testing.T) {
	f, _ := newFeed(t, serve(200, ""))
	s := f.Snapshot()
	if s.Price != nil || s.ProviderUpdatedAt != nil || s.FetchedAt != nil {
		t.Fatalf("%+v", s)
	}
}

func TestSnapshotStaleAfterMaxAge(t *testing.T) {
	f, c := newFeed(t, serve(200, body("3456.12", epoch.Add(-30*time.Second))))
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Add(90 * time.Second) // exactly 120s old: still fresh
	if f.Snapshot().Price == nil {
		t.Fatal("expected fresh at exactly MaxAge")
	}
	c.Add(time.Second) // 121s
	if f.Snapshot().Price != nil {
		t.Fatal("expected stale")
	}
}

func TestRejectsFutureTimestamp(t *testing.T) {
	f, _ := newFeed(t, serve(200, body("3456.12", epoch.Add(5*time.Second))))
	if err := f.Refresh(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if f.Snapshot().Price != nil {
		t.Fatal("sample accepted")
	}
}

func TestRejectsMissingTimestamp(t *testing.T) {
	f, _ := newFeed(t, serve(200, `{"ethereum":{"usd":3456.12}}`))
	if err := f.Refresh(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if f.Snapshot().Price != nil {
		t.Fatal("sample accepted")
	}
}

func TestRejectsInvalidPrice(t *testing.T) {
	for _, p := range []string{"0", "-5", "3.1e3", "0.0000000000000000001"} {
		f, _ := newFeed(t, serve(200, body(p, epoch.Add(-time.Second))))
		if err := f.Refresh(context.Background()); err == nil || f.Snapshot().Price != nil {
			t.Errorf("price %s accepted", p)
		}
	}
}

func TestMalformedKeepsPreviousSample(t *testing.T) {
	var n atomic.Int32
	f, c := newFeed(t, func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			_, _ = w.Write([]byte(body("3456.12", epoch.Add(-10*time.Second))))
			return
		}
		_, _ = w.Write([]byte(`{not json`))
	})
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Add(60 * time.Second)
	if err := f.Refresh(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	s := f.Snapshot()
	if s.Price == nil || *s.Price != "3456.12" {
		t.Fatalf("previous sample lost: %+v", s)
	}
	c.Add(61 * time.Second) // 131s old
	if f.Snapshot().Price != nil {
		t.Fatal("expected stale")
	}
}

func Test5xxKeepsPreviousSample(t *testing.T) {
	var n atomic.Int32
	f, c := newFeed(t, func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			_, _ = w.Write([]byte(body("3456.12", epoch.Add(-10*time.Second))))
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	})
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Add(30 * time.Second)
	if err := f.Refresh(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if f.Snapshot().Price == nil {
		t.Fatal("previous sample lost")
	}
}

func Test429BacksOff(t *testing.T) {
	var status atomic.Int32
	status.Store(429)
	f, _ := newFeed(t, func(w http.ResponseWriter, _ *http.Request) {
		if s := int(status.Load()); s != 200 {
			w.WriteHeader(s)
			return
		}
		_, _ = w.Write([]byte(body("3456.12", epoch)))
	})
	if got := f.nextDelay(); got != 60*time.Second {
		t.Fatalf("initial delay %v", got)
	}
	for _, want := range []time.Duration{60 * time.Second, 120 * time.Second, 240 * time.Second} {
		if err := f.Refresh(context.Background()); err == nil {
			t.Fatal("expected error")
		}
		if got := f.nextDelay(); got != want {
			t.Fatalf("delay %v want %v", got, want)
		}
	}
	for i := 0; i < 10; i++ {
		_ = f.Refresh(context.Background())
	}
	if got := f.nextDelay(); got != 15*time.Minute {
		t.Fatalf("cap %v", got)
	}
	status.Store(200)
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.nextDelay(); got != 60*time.Second {
		t.Fatalf("reset %v", got)
	}
}

func TestDefaultJitterNeverBelowInterval(t *testing.T) {
	f := New(Options{})
	for i := 0; i < 200; i++ {
		d := f.nextDelay()
		if d < 60*time.Second || d > 72*time.Second {
			t.Fatalf("delay %v out of range", d)
		}
	}
}

func TestRefreshCoalesces(t *testing.T) {
	var hits atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	f, _ := newFeed(t, func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			close(started)
		}
		<-release
		_, _ = w.Write([]byte(body("3456.12", epoch)))
	})
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- f.Refresh(context.Background()) }()
	}
	<-started
	deadline := time.Now().Add(10 * time.Second)
	for {
		f.mu.Lock()
		n := f.waiters
		f.mu.Unlock()
		if n == 9 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiters=%d, want 9", n)
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("requests=%d want 1", got)
	}
}

func TestErrorsDoNotContainKey(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"500":       serve(500, "boom "+testKey),
		"429":       serve(429, ""),
		"malformed": serve(200, "{"),
		"future":    serve(200, body("1", epoch.Add(time.Hour))),
	}
	for name, h := range cases {
		f, _ := newFeed(t, h)
		err := f.Refresh(context.Background())
		if err == nil || strings.Contains(err.Error(), testKey) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	srv := httptest.NewServer(serve(200, ""))
	url := srv.URL
	srv.Close()
	f := New(Options{BaseURL: url, APIKey: testKey})
	err := f.Refresh(context.Background())
	if err == nil || strings.Contains(err.Error(), testKey) {
		t.Errorf("transport: err=%v", err)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	f, _ := newFeed(t, serve(200, body("3456.12", epoch)))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for f.Snapshot().Price == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.Snapshot().Price == nil {
		t.Fatal("run did not refresh")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop")
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func TestRunLogsProviderErrorsWithoutKey(t *testing.T) {
	srv := httptest.NewServer(serve(401, "bad key "+testKey))
	t.Cleanup(srv.Close)
	buf := &syncBuf{}
	f := New(Options{BaseURL: srv.URL, APIKey: testKey, Logger: slog.New(slog.NewTextHandler(buf, nil))})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for buf.String() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	out := buf.String()
	if !strings.Contains(out, "rejected credentials") {
		t.Fatalf("log=%q", out)
	}
	if strings.Contains(out, testKey) {
		t.Fatalf("key leaked in log: %q", out)
	}
}

func TestDoesNotForwardKeyOnRedirect(t *testing.T) {
	var leaked atomic.Bool
	var hit atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Store(true)
		if r.Header.Get("x-cg-pro-api-key") != "" {
			leaked.Store(true)
		}
		_, _ = w.Write([]byte(body("1", epoch)))
	}))
	t.Cleanup(other.Close)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/simple/price", http.StatusFound)
	}))
	t.Cleanup(first.Close)
	f := New(Options{BaseURL: first.URL, APIKey: testKey})
	if err := f.Refresh(context.Background()); err == nil {
		t.Fatal("expected error on redirect")
	}
	if hit.Load() || leaked.Load() {
		t.Fatalf("redirect followed: hit=%v leaked=%v", hit.Load(), leaked.Load())
	}
}

func TestFeedObserveOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		client  *http.Client
		want    string
	}{
		{"ok", serve(200, body("3000.5", epoch.Add(-time.Second))), nil, "ok"},
		{"rate_limited", serve(429, ""), nil, "rate_limited"},
		{"rejected", serve(403, ""), nil, "rejected"},
		{"http_error", serve(500, ""), nil, "http_error"},
		{"malformed", serve(200, body("3000.0000000000000000001", epoch.Add(-time.Second))), nil, "malformed"},
		{"timeout", func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(300 * time.Millisecond)
			w.WriteHeader(200)
		}, &http.Client{Timeout: 30 * time.Millisecond}, "timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			t.Cleanup(srv.Close)
			c := &clock{t: epoch}
			var mu sync.Mutex
			var got []string
			f := New(Options{
				BaseURL: srv.URL, APIKey: testKey, Now: c.Now, Client: tc.client,
				Observe: func(o string) { mu.Lock(); got = append(got, o); mu.Unlock() },
			})
			_ = f.Refresh(context.Background())
			mu.Lock()
			defer mu.Unlock()
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("outcomes = %v, want exactly [%s]", got, tc.want)
			}
		})
	}
}

func TestFeedObserveOncePerCoalescedFetch(t *testing.T) {
	release := make(chan struct{})
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		<-release
		_, _ = w.Write([]byte(body("3000.5", epoch.Add(-time.Second))))
	}))
	t.Cleanup(srv.Close)
	var observed atomic.Int32
	c := &clock{t: epoch}
	f := New(Options{BaseURL: srv.URL, Now: c.Now, Observe: func(string) { observed.Add(1) }})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = f.Refresh(context.Background()) }()
	}
	// Wait until the three other callers have joined the in-flight call.
	for {
		f.mu.Lock()
		joined := f.waiters
		f.mu.Unlock()
		if joined == 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	if hits.Load() != 1 || observed.Load() != 1 {
		t.Fatalf("hits=%d observed=%d, want 1 and 1", hits.Load(), observed.Load())
	}
}
