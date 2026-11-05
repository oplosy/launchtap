package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/pricefeed"
)

type priceStub struct{ snap pricefeed.Snapshot }

func (s priceStub) Snapshot() pricefeed.Snapshot { return s.snap }

func getEthUsd(t *testing.T, prices PriceSource) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterReferenceRoutes(ReferenceRoutes{Prices: prices})
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/reference/eth-usd", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return w, body
}

func TestEthUsdDisabledAllNull(t *testing.T) {
	w, body := getEthUsd(t, nil)
	for _, k := range []string{"price", "provider_updated_at", "fetched_at", "source", "attribution_url"} {
		v, ok := body[k]
		if !ok || v != nil {
			t.Errorf("%s = %v (present=%v), want null", k, v, ok)
		}
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=15" {
		t.Errorf("Cache-Control=%q", got)
	}
}

func TestEthUsdFresh(t *testing.T) {
	price := "3456.12"
	provider := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	fetched := provider.Add(5 * time.Second)
	w, body := getEthUsd(t, priceStub{pricefeed.Snapshot{Price: &price, ProviderUpdatedAt: &provider, FetchedAt: &fetched}})
	want := map[string]any{
		"price":               "3456.12",
		"provider_updated_at": "2026-11-05T12:00:00Z",
		"fetched_at":          "2026-11-05T12:00:05Z",
		"source":              "coingecko",
		"attribution_url":     "https://www.coingecko.com/en/api",
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("%s = %v, want %v", k, body[k], v)
		}
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=15" {
		t.Errorf("Cache-Control=%q", got)
	}
}

func TestEthUsdStaleNullPriceKeepsSource(t *testing.T) {
	_, body := getEthUsd(t, priceStub{})
	if v, ok := body["price"]; !ok || v != nil {
		t.Errorf("price=%v present=%v", v, ok)
	}
	if body["source"] != "coingecko" || body["attribution_url"] != "https://www.coingecko.com/en/api" {
		t.Errorf("source/attribution missing: %v", body)
	}
	for _, k := range []string{"provider_updated_at", "fetched_at"} {
		if v, ok := body[k]; !ok || v != nil {
			t.Errorf("%s=%v present=%v", k, v, ok)
		}
	}
}
