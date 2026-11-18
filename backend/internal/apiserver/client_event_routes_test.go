package apiserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testOrigin = "https://app.example"

type recordedClientEvent struct {
	kind, a, b, c string
	value         float64
}

type recordingTelemetry struct {
	mu     sync.Mutex
	events []recordedClientEvent
}

func (r *recordingTelemetry) ClientError(source, route, class string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedClientEvent{kind: "error", a: source, b: route, c: class})
}

func (r *recordingTelemetry) ClientVital(name, route string, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedClientEvent{kind: "vital", a: name, b: route, value: value})
}

func newClientEventServer(t *testing.T, telemetry ClientTelemetry, metrics Metrics, limiter *SubjectLimiter) *Server {
	t.Helper()
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{testOrigin}
	cfg.Metrics = metrics
	s := New(cfg, ReadyFunc(func(context.Context) error { return nil }), slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.RegisterClientEventRoutes(ClientEventRoutes{Telemetry: telemetry, AllowedOrigins: cfg.AllowedOrigins, Limiter: limiter})
	return s
}

func postClientEvents(s *Server, origin, contentType, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/v1/client-events", strings.NewReader(body))
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	request.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, request)
	return w
}

func TestClientEventsRecordsAValidBatch(t *testing.T) {
	telemetry := &recordingTelemetry{}
	s := newClientEventServer(t, telemetry, nil, nil)
	body := `{"events":[{"type":"error","source":"window_error","route":"token","class":"TypeError"},` +
		`{"type":"vital","name":"LCP","route":"home","value":1234.5},{"type":"vital","name":"CLS","route":"home","value":0}]}`
	for _, contentType := range []string{"text/plain;charset=UTF-8", "application/json"} {
		telemetry.events = nil
		w := postClientEvents(s, testOrigin, contentType, body)
		if w.Code != http.StatusNoContent {
			t.Fatalf("%s: status = %d, body = %s", contentType, w.Code, w.Body.String())
		}
		want := []recordedClientEvent{
			{kind: "error", a: "window_error", b: "token", c: "TypeError"},
			{kind: "vital", a: "LCP", b: "home", value: 1234.5},
			{kind: "vital", a: "CLS", b: "home", value: 0},
		}
		if len(telemetry.events) != len(want) {
			t.Fatalf("%s: events = %+v", contentType, telemetry.events)
		}
		for i := range want {
			if telemetry.events[i] != want[i] {
				t.Fatalf("%s: event %d = %+v, want %+v", contentType, i, telemetry.events[i], want[i])
			}
		}
	}
}

func TestClientEventsRejectsInvalidBatchesWhole(t *testing.T) {
	valid := `{"type":"error","source":"window_error","route":"token","class":"TypeError"}`
	cases := map[string]string{
		"not json":         `events`,
		"empty":            `{"events":[]}`,
		"unknown field":    `{"events":[` + valid + `],"url":"https://x"}`,
		"event field":      `{"events":[{"type":"error","source":"window_error","route":"token","class":"TypeError","message":"0xabc"}]}`,
		"trailing data":    `{"events":[` + valid + `]} {}`,
		"unknown route":    `{"events":[{"type":"error","source":"window_error","route":"/token/0xabc","class":"TypeError"}]}`,
		"unknown source":   `{"events":[{"type":"error","source":"console","route":"token","class":"TypeError"}]}`,
		"unknown class":    `{"events":[{"type":"error","source":"window_error","route":"token","class":"WalletError"}]}`,
		"error with value": `{"events":[{"type":"error","source":"window_error","route":"token","class":"Error","value":1}]}`,
		"unknown vital":    `{"events":[{"type":"vital","name":"FID","route":"token","value":1}]}`,
		"missing value":    `{"events":[{"type":"vital","name":"LCP","route":"token"}]}`,
		"negative value":   `{"events":[{"type":"vital","name":"LCP","route":"token","value":-1}]}`,
		"timing too high":  `{"events":[{"type":"vital","name":"INP","route":"token","value":60001}]}`,
		"cls too high":     `{"events":[{"type":"vital","name":"CLS","route":"token","value":10.5}]}`,
		"unknown type":     `{"events":[{"type":"log","route":"token"}]}`,
		"one bad of two":   `{"events":[` + valid + `,{"type":"vital","name":"LCP","route":"token","value":-1}]}`,
		"too many events":  `{"events":[` + strings.TrimSuffix(strings.Repeat(valid+",", maxClientEvents+1), ",") + `]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			telemetry := &recordingTelemetry{}
			w := postClientEvents(newClientEventServer(t, telemetry, nil, nil), testOrigin, "text/plain", body)
			if w.Code != http.StatusBadRequest || len(telemetry.events) != 0 {
				t.Fatalf("status = %d, events = %+v, body = %s", w.Code, telemetry.events, w.Body.String())
			}
		})
	}
}

func TestClientEventsRejectsOversizedBody(t *testing.T) {
	telemetry := &recordingTelemetry{}
	body := `{"events":[{"type":"error","source":"window_error","route":"token","class":"` + strings.Repeat("x", maxClientEventBytes) + `"}]}`
	w := postClientEvents(newClientEventServer(t, telemetry, nil, nil), testOrigin, "text/plain", body)
	if w.Code != http.StatusRequestEntityTooLarge && w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 413 or 400", w.Code)
	}
	if len(telemetry.events) != 0 {
		t.Fatalf("events = %+v", telemetry.events)
	}
}

func TestClientEventsRequiresAnAllowedOrigin(t *testing.T) {
	body := `{"events":[{"type":"vital","name":"TTFB","route":"home","value":10}]}`
	for _, origin := range []string{"", "https://evil.example", "*"} {
		telemetry := &recordingTelemetry{}
		w := postClientEvents(newClientEventServer(t, telemetry, nil, nil), origin, "text/plain", body)
		if w.Code != http.StatusForbidden || len(telemetry.events) != 0 {
			t.Fatalf("origin %q: status = %d, events = %+v", origin, w.Code, telemetry.events)
		}
	}
}

func TestClientEventsAreRateLimitedWithoutCountingWriteRejections(t *testing.T) {
	telemetry := &recordingTelemetry{}
	metrics := &recordingMetrics{}
	s := newClientEventServer(t, telemetry, metrics, NewSubjectLimiter(2, time.Minute))
	body := `{"events":[{"type":"vital","name":"TTFB","route":"home","value":10}]}`
	for i := range 2 {
		if w := postClientEvents(s, testOrigin, "text/plain", body); w.Code != http.StatusNoContent {
			t.Fatalf("request %d: status = %d", i, w.Code)
		}
	}
	if w := postClientEvents(s, testOrigin, "text/plain", body); w.Code != http.StatusTooManyRequests {
		t.Fatalf("third request: status = %d, want 429", w.Code)
	}
	if w := postClientEvents(s, testOrigin, "text/plain", `bad`); w.Code != http.StatusTooManyRequests && w.Code != http.StatusBadRequest {
		t.Fatalf("bad request: status = %d", w.Code)
	}
	if len(telemetry.events) != 2 {
		t.Fatalf("events = %d, want 2", len(telemetry.events))
	}
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if len(metrics.rejected) != 0 {
		t.Fatalf("write rejections = %v, want none for telemetry", metrics.rejected)
	}
	if got := metrics.requests[len(metrics.requests)-1].route; got != clientEventsPattern {
		t.Fatalf("route label = %q, want %q", got, clientEventsPattern)
	}
}
