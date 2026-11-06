// Package metrics owns the Prometheus registry wiring for the API and indexer
// processes. It is the only internal package (besides cmd/*) allowed to import
// github.com/prometheus; other packages expose plain Go hooks instead.
package metrics

import (
	"net/http"
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Process components accepted by NewRegistry.
const (
	ComponentAPI     = "api"
	ComponentIndexer = "indexer"
)

// NewRegistry returns a registry preloaded with the Go runtime, process and
// build info collectors. component is "api" or "indexer"; any other value is a
// programming error and panics, because it would create an unprefixed series.
func NewRegistry(component string) *prometheus.Registry {
	if component != ComponentAPI && component != ComponentIndexer {
		panic("metrics: unknown component " + component)
	}

	registry := prometheus.NewRegistry()
	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "launchpad_" + component + "_build_info",
		Help:        "Constant 1 labelled with the Go toolchain version the process was built with.",
		ConstLabels: prometheus.Labels{"go_version": runtime.Version()},
	})
	buildInfo.Set(1)
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewBuildInfoCollector(),
		buildInfo,
	)

	return registry
}

// Handler serves the gatherer in the Prometheus text format. Errors while
// gathering are reported as HTTP 500 rather than served as partial output.
func Handler(g prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(g, promhttp.HandlerOpts{ErrorHandling: promhttp.HTTPErrorOnError})
}
