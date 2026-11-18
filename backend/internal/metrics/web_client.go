package metrics

import "github.com/prometheus/client_golang/prometheus"

// webClientInstruments hold the browser-reported series
// (docs/specs/2026-11-18-client-telemetry-preflight.md). The API validates every label against a
// closed set before it calls in, so cardinality is bounded.
type webClientInstruments struct {
	errors *prometheus.CounterVec
	vitals *prometheus.HistogramVec
	cls    *prometheus.HistogramVec
}

func newWebClientInstruments() webClientInstruments {
	return webClientInstruments{
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "launchpad_web_client_errors_total",
			Help: "Browser errors reported by the web app, by source, route group and error class.",
		}, []string{"source", "route", "class"}),
		vitals: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "launchpad_web_vital_seconds",
			Help:    "Browser Web Vitals (LCP, INP, FCP, TTFB) by name and route group.",
			Buckets: []float64{0.05, 0.1, 0.2, 0.5, 0.8, 1, 1.5, 2, 2.5, 3, 4, 6, 10},
		}, []string{"name", "route"}),
		cls: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "launchpad_web_cls",
			Help:    "Browser cumulative layout shift by route group.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.15, 0.25, 0.5, 1},
		}, []string{"route"}),
	}
}

func (w webClientInstruments) collectors() []prometheus.Collector {
	return []prometheus.Collector{w.errors, w.vitals, w.cls}
}

// ClientError counts one browser error.
func (a *API) ClientError(source, route, class string) {
	a.web.errors.WithLabelValues(source, route, class).Inc()
}

// ClientVital records one Web Vital: milliseconds for timings, unitless for CLS.
func (a *API) ClientVital(name, route string, value float64) {
	if name == "CLS" {
		a.web.cls.WithLabelValues(route).Observe(value)
		return
	}
	a.web.vitals.WithLabelValues(name, route).Observe(value / 1000)
}
