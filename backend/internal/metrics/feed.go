package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// feedReadOps is the closed set of feed.Observer.ReadFailed operations.
var feedReadOps = map[string]bool{"newest": true, "trades": true, "graduations": true, "launches": true, "watermarks": true}

// feedEventTypes is the closed set of feed event types published by the tail.
var feedEventTypes = map[string]bool{"trade": true, "graduation": true, "new_token": true, "trade_burst": true, "finality": true}

type feedInstruments struct {
	published  *prometheus.CounterVec
	bursts     prometheus.Counter
	lag        prometheus.Gauge
	readErrors *prometheus.CounterVec
}

func newFeedInstruments() feedInstruments {
	return feedInstruments{
		published: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "launchpad_api_feed_events_published_total",
			Help: "Live feed events published to the SSE hub by type.",
		}, []string{"type"}),
		bursts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "launchpad_api_feed_bursts_total",
			Help: "Trade windows rolled up into a trade_burst event.",
		}),
		lag: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "launchpad_api_feed_tail_lag_seconds",
			Help: "Seconds between now and the block time of the newest published trade.",
		}),
		readErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "launchpad_api_feed_read_errors_total",
			Help: "Failed feed tail database reads by operation.",
		}, []string{"op"}),
	}
}

func (f feedInstruments) collectors() []prometheus.Collector {
	return []prometheus.Collector{f.published, f.bursts, f.lag, f.readErrors}
}

// Published counts one feed event; unknown types are ignored to keep the label set closed.
func (a *API) Published(eventType string) {
	if feedEventTypes[eventType] {
		a.feed.published.WithLabelValues(eventType).Inc()
	}
}

// Burst counts one trade_burst window.
func (a *API) Burst() { a.feed.bursts.Inc() }

// Lag records how far the newest published trade trails the wall clock.
func (a *API) Lag(d time.Duration) { a.feed.lag.Set(d.Seconds()) }

// ReadFailed counts a failed feed read; unknown operations are ignored.
func (a *API) ReadFailed(op string) {
	if feedReadOps[op] {
		a.feed.readErrors.WithLabelValues(op).Inc()
	}
}
