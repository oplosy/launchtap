package metrics

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestUnsafeLabelsFlagsSensitiveValues(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_requests_total", Help: "test"}, []string{"route"})
	registry.MustRegister(counter)

	unsafe := []string{
		"/v1/tokens/0x" + strings.Repeat("a", 40),
		"0x" + strings.Repeat("B", 64),
		"did:privy:abc",
		"https://rpc.example/key",
	}
	for _, value := range unsafe {
		counter.WithLabelValues(value).Inc()
	}
	counter.WithLabelValues("GET /v1/tokens/{token}").Inc()

	got, err := UnsafeLabels(registry)
	if err != nil {
		t.Fatal(err)
	}

	var want []string
	for _, value := range unsafe {
		want = append(want, `test_requests_total{route="`+value+`"}`)
	}
	sort.Strings(want)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UnsafeLabels() = %v, want %v", got, want)
	}
}
