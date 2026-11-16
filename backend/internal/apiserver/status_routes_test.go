package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/status"
)

type statusReaderStub struct {
	value status.Indexer
	err   error
}

func (s statusReaderStub) Indexer(context.Context, int64, string) (status.Indexer, error) {
	return s.value, s.err
}

func getStatus(t *testing.T, reader status.Reader) (*httptest.ResponseRecorder, statusBody) {
	t.Helper()
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterStatusRoutes(StatusRoutes{Reader: reader, ChainID: 46630, DeploymentID: "testnet"})
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	var body statusBody
	if response.Code == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	return response, body
}

func TestStatusReportsHeartbeatAge(t *testing.T) {
	checked := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	for name, test := range map[string]struct {
		ago   time.Duration
		stale bool
	}{
		"fresh": {ago: 4 * time.Second},
		"stale": {ago: status.IndexerStaleAfter + time.Second, stale: true},
	} {
		t.Run(name, func(t *testing.T) {
			beat := checked.Add(-test.ago)
			observed, safe := int64(12), int64(10)
			response, body := getStatus(t, statusReaderStub{value: status.Indexer{ObservedBlock: &observed, SafeBlock: &safe, HeartbeatAt: &beat, CheckedAt: checked}})
			if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
			}
			got := body.Indexer
			if got.Stale != test.stale || got.HeartbeatAgeSeconds == nil || *got.HeartbeatAgeSeconds != int64(test.ago/time.Second) ||
				got.HeartbeatAt == nil || got.ObservedBlock == nil || *got.ObservedBlock != 12 || got.FinalizedBlock != nil || got.StaleAfterSeconds != 120 {
				t.Fatalf("body=%s", response.Body.String())
			}
		})
	}
}

func TestStatusBeforeTheFirstStepIsStale(t *testing.T) {
	response, body := getStatus(t, statusReaderStub{value: status.Indexer{CheckedAt: time.Now()}})
	if response.Code != http.StatusOK || !body.Indexer.Stale || body.Indexer.HeartbeatAt != nil || body.Indexer.HeartbeatAgeSeconds != nil {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var raw struct {
		Indexer map[string]any `json:"indexer"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if value, present := raw.Indexer["heartbeat_at"]; !present || value != nil {
		t.Fatalf("heartbeat_at must be an explicit null: %s", response.Body.String())
	}
}

func TestStatusHidesStoreErrors(t *testing.T) {
	response, _ := getStatus(t, statusReaderStub{err: errors.New("dial tcp 10.0.0.1: secret detail")})
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "secret detail") || strings.Contains(response.Body.String(), "10.0.0.1") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
