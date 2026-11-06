package metrics

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

func TestAPIMetricsLabelsAreSafe(t *testing.T) {
	t.Parallel()

	reg := NewRegistry(ComponentAPI)
	api := NewAPI(reg)

	api.ObserveRequest("GET /v1/tokens/{token}/trades", "GET", 200, 5*time.Millisecond)
	api.ObserveRequest("unmatched", "GET", 404, time.Millisecond)
	api.ObserveRequest("rate_limited", "PROPFIND", 429, time.Millisecond)
	api.WriteRejected("rate_limited")
	api.WriteRejected("unauthorized")
	api.StreamOpened()
	api.StreamOpened()
	api.StreamClosed()

	unsafe, err := UnsafeLabels(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsafe) != 0 {
		t.Fatalf("unsafe labels: %v", unsafe)
	}
	if got := seriesValue(t, reg, "launchpad_api_http_requests_total", map[string]string{"route": "unmatched", "method": "GET", "code_class": "4xx"}); got != 1 {
		t.Fatalf(`requests{route="unmatched",method="GET",code_class="4xx"} = %v, want 1`, got)
	}
	if got := seriesValue(t, reg, "launchpad_api_http_requests_total", map[string]string{"route": "rate_limited", "method": "other", "code_class": "4xx"}); got != 1 {
		t.Fatalf("unknown method must be normalised to other, got %v", got)
	}
	if got := seriesValue(t, reg, "launchpad_api_write_rejections_total", map[string]string{"reason": "rate_limited"}); got != 1 {
		t.Fatalf("rejected rate_limited = %v, want 1", got)
	}
	if got := seriesValue(t, reg, "launchpad_api_sse_clients", nil); got != 1 {
		t.Fatalf("sse clients = %v, want 1", got)
	}
}

func TestRegisterPoolExposesStatSeries(t *testing.T) {
	t.Parallel()

	pool, err := pgxpool.New(context.Background(), "postgres://user@127.0.0.1:1/db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	reg := prometheus.NewRegistry()
	RegisterPool(reg, "launchpad_api", pool.Stat)
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range families {
		names = append(names, f.GetName())
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{
		"launchpad_api_db_pool_acquired_conns", "launchpad_api_db_pool_idle_conns", "launchpad_api_db_pool_max_conns",
		"launchpad_api_db_pool_acquire_wait_seconds_total", "launchpad_api_db_pool_empty_acquire_total",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing series %s in %s", want, joined)
		}
	}
}

// seriesValue returns the counter or gauge value of the series whose labels equal want, or -1.
func seriesValue(t *testing.T, g prometheus.Gatherer, name string, want map[string]string) float64 {
	t.Helper()
	families, err := g.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
	metric:
		for _, m := range f.GetMetric() {
			if len(m.GetLabel()) != len(want) {
				continue
			}
			for _, l := range m.GetLabel() {
				if want[l.GetName()] != l.GetValue() {
					continue metric
				}
			}
			if m.GetCounter() != nil {
				return m.GetCounter().GetValue()
			}
			return m.GetGauge().GetValue()
		}
	}
	return -1
}

func TestPriceFeedMetrics(t *testing.T) {
	t.Parallel()

	reg := NewRegistry(ComponentAPI)
	api := NewAPI(reg)
	if got := seriesValue(t, reg, "launchpad_api_eth_usd_last_success_timestamp_seconds", nil); got != -1 {
		t.Fatalf("price feed series must not exist before RegisterPriceFeed, got %v", got)
	}
	api.RegisterPriceFeed(reg)

	api.ObservePriceFeed("ok")
	api.ObservePriceFeed("timeout")
	api.ObservePriceFeed("timeout")

	if got := seriesValue(t, reg, "launchpad_api_eth_usd_last_success_timestamp_seconds", nil); got <= 0 {
		t.Fatalf("last success timestamp = %v, want > 0", got)
	}
	if got := seriesValue(t, reg, "launchpad_api_eth_usd_fetch_failures_total", map[string]string{"kind": "timeout"}); got != 2 {
		t.Fatalf(`fetch_failures_total{kind="timeout"} = %v, want 2`, got)
	}
	unsafe, err := UnsafeLabels(reg)
	if err != nil || len(unsafe) != 0 {
		t.Fatalf("unsafe labels: %v %v", unsafe, err)
	}
}
