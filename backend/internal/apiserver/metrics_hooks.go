package apiserver

import (
	"net/http"
	"time"
)

// Metrics receives request-level telemetry. Implementations live outside this
// package so the HTTP boundary stays stdlib-only; a nil Config.Metrics is a no-op.
// Every string argument comes from a closed set (route patterns registered on the mux,
// the reserved routes below and the rejection reasons), never from request data.
type Metrics interface {
	ObserveRequest(route, method string, status int, elapsed time.Duration)
	WriteRejected(reason string)
	StreamOpened()
	StreamClosed()
}

// Reserved route labels for requests that never reach a registered pattern.
const (
	routeUnmatched   = "unmatched"
	routeRateLimited = "rate_limited"
	routePreflight   = "preflight"
)

type noopMetrics struct{}

func (noopMetrics) ObserveRequest(string, string, int, time.Duration) {}
func (noopMetrics) WriteRejected(string)                              {}
func (noopMetrics) StreamOpened()                                     {}
func (noopMetrics) StreamClosed()                                     {}

// metricsOrNoop returns m, or a no-op when m is nil.
func metricsOrNoop(m Metrics) Metrics {
	if m == nil {
		return noopMetrics{}
	}
	return m
}

// routePattern returns the mux pattern a request will be dispatched to, or
// routeUnmatched. The middleware wraps the mux, so r.Pattern is not set yet.
func routePattern(mux *http.ServeMux, r *http.Request) string {
	if _, pattern := mux.Handler(r); pattern != "" {
		return pattern
	}
	return routeUnmatched
}

// rejectionReason maps a rejected write to its closed-set reason. It returns
// false for reads and for statuses that are not rejections.
func rejectionReason(method string, status int) (string, bool) {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete:
	default:
		return "", false
	}
	switch status {
	case http.StatusTooManyRequests:
		return "rate_limited", true
	case http.StatusUnauthorized:
		return "unauthorized", true
	case http.StatusForbidden:
		return "forbidden", true
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "invalid", true
	case http.StatusConflict, http.StatusPreconditionFailed:
		return "conflict", true
	case http.StatusRequestEntityTooLarge:
		return "too_large", true
	}
	return "", false
}
