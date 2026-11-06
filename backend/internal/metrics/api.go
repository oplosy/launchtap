package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// API holds the API process's request-level instruments. It implements
// apiserver.Metrics; later API-side instruments are added as further methods.
type API struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	rejected *prometheus.CounterVec
	sse      prometheus.Gauge
}

// NewAPI registers the API instruments on reg.
func NewAPI(reg prometheus.Registerer) *API {
	a := &API{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "launchpad_api_http_requests_total",
			Help: "HTTP requests by route pattern, method and status class.",
		}, []string{"route", "method", "code_class"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "launchpad_api_http_request_duration_seconds",
			Help:    "HTTP request duration by route pattern and method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method"}),
		rejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "launchpad_api_write_rejected_total",
			Help: "Rejected write requests (POST, PUT, DELETE) by reason.",
		}, []string{"reason"}),
		sse: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "launchpad_api_sse_clients",
			Help: "Currently open server-sent event streams.",
		}),
	}
	reg.MustRegister(a.requests, a.duration, a.rejected, a.sse)

	return a
}

// ObserveRequest records one finished request. route must be a registered
// route pattern or one of the reserved labels; method and status are
// normalised to closed sets here.
func (a *API) ObserveRequest(route, method string, status int, elapsed time.Duration) {
	method = methodLabel(method)
	a.requests.WithLabelValues(route, method, codeClass(status)).Inc()
	a.duration.WithLabelValues(route, method).Observe(elapsed.Seconds())
}

// WriteRejected counts one rejected write by reason.
func (a *API) WriteRejected(reason string) { a.rejected.WithLabelValues(reason).Inc() }

// StreamOpened and StreamClosed track open SSE streams.
func (a *API) StreamOpened() { a.sse.Inc() }
func (a *API) StreamClosed() { a.sse.Dec() }

func methodLabel(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodHead:
		return method
	}
	return "other"
}

func codeClass(status int) string {
	if status < 100 || status > 599 {
		return "other"
	}
	return strconv.Itoa(status/100) + "xx"
}
