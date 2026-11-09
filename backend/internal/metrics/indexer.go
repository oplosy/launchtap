package metrics

import (
	"time"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/prometheus/client_golang/prometheus"
)

// Indexer holds the indexer process's instruments. Gauges are read from the
// health snapshot at scrape time; counters are fed by hooks.
type Indexer struct {
	rpc          *prometheus.CounterVec
	recomputed   prometheus.Counter
	notification *prometheus.CounterVec
	positionFail prometheus.Counter
	senderMiss   prometheus.Counter
	backfilled   prometheus.Counter
	backfillFail prometheus.Counter
}

var (
	rpcMethods    = map[string]bool{"eth_getBlockByNumber": true, "eth_getBlockByHash": true, "eth_getCode": true, "eth_chainId": true, "eth_getLogs": true, "eth_call": true, "eth_blockNumber": true, "eth_getTransactionByHash": true}
	rpcOutcomes   = map[string]bool{"ok": true, "timeout": true, "error": true}
	notifyChannel = map[string]bool{"market_dirty": true, "api_refresh": true}
)

// NewIndexer registers the indexer instruments on reg. snapshot must be cheap
// and concurrency-safe, such as (*indexer.HealthTracker).Snapshot.
func NewIndexer(reg prometheus.Registerer, snapshot func() indexer.Health) *Indexer {
	m := &Indexer{
		rpc: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "launchpad_indexer_rpc_requests_total",
			Help: "Chain RPC calls by JSON-RPC method and outcome (a batch counts once).",
		}, []string{"method", "outcome"}),
		recomputed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "launchpad_indexer_stats_recomputed_total",
			Help: "Token statistics recomputations completed by the aggregation worker.",
		}),
		notification: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "launchpad_indexer_notifications_total",
			Help: "PostgreSQL NOTIFY calls by channel and outcome.",
		}, []string{"channel", "outcome"}),
		positionFail: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "launchpad_aggregation_position_failures_total",
			Help: "Wallet position recomputations that failed and stay dirty for retry.",
		}),
		senderMiss: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "launchpad_indexer_sender_lookup_misses_total",
			Help: "Pool swap transactions stored without a sender (tx_from NULL) because the lookup failed or returned nothing.",
		}),
		backfilled: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "launchpad_indexer_sender_backfill_filled_total",
			Help: "Pool swap transactions whose missing sender the indexer's automatic backfill filled.",
		}),
		backfillFail: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "launchpad_indexer_sender_backfill_errors_total",
			Help: "Automatic sender backfill passes that failed (RPC or database error); the next pass retries.",
		}),
	}
	reg.MustRegister(m.rpc, m.recomputed, m.notification, m.positionFail, m.senderMiss, m.backfilled, m.backfillFail)
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "launchpad_aggregation_position_dirty_backlog",
		Help: "Pending wallet position recomputations (position_dirty rows for the chain).",
	}, func() float64 { return float64(snapshot().PositionDirtyWork) }))

	gauge := func(name, help string, value func(indexer.Health) float64) {
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "launchpad_indexer_" + name, Help: help}, func() float64 { return value(snapshot()) }))
	}
	counter := func(name, help string, value func(indexer.Health) float64) {
		reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "launchpad_indexer_" + name, Help: help}, func() float64 { return value(snapshot()) }))
	}
	levels := []struct {
		label  string
		number func(indexer.Health) int64
		lag    func(indexer.Health) time.Duration
	}{
		{"observed", func(h indexer.Health) int64 { return h.Observed }, func(h indexer.Health) time.Duration { return h.ObservedLag }},
		{"safe", func(h indexer.Health) int64 { return h.Safe }, func(h indexer.Health) time.Duration { return h.SafeLag }},
		{"finalized", func(h indexer.Health) int64 { return h.Finalized }, func(h indexer.Health) time.Duration { return h.FinalizedLag }},
	}
	for _, level := range levels {
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "launchpad_indexer_block_number", Help: "Last committed block number by finality level.",
			ConstLabels: prometheus.Labels{"level": level.label},
		}, func() float64 { return float64(level.number(snapshot())) }))
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "launchpad_indexer_lag_seconds", Help: "Age of the last committed block by finality level.",
			ConstLabels: prometheus.Labels{"level": level.label},
		}, func() float64 { return level.lag(snapshot()).Seconds() }))
	}
	gauge("chain_head_block", "Latest block number reported by the chain RPC.", func(h indexer.Health) float64 { return float64(h.ChainHead) })
	gauge("ownership_held", "1 while this process holds the writer lock.", func(h indexer.Health) float64 { return boolValue(h.OwnershipHeld) })
	gauge("rpc_healthy", "1 while the chain RPC is considered healthy.", func(h indexer.Health) float64 { return boolValue(h.RPCHealthy) })
	gauge("db_up", "1 while the last operational database read succeeded.", func(h indexer.Health) float64 { return boolValue(h.DatabaseUp) })
	gauge("dirty_work", "Pending token statistics recomputations.", func(h indexer.Health) float64 { return float64(h.DirtyWork) })
	gauge("last_reorg_depth", "Depth of the most recent reorg.", func(h indexer.Health) float64 { return float64(h.LastReorgDepth) })
	counter("reorgs_total", "Latest global reorg id: a monotonically increasing reorg sequence across the database, not a per-deployment count.", func(h indexer.Health) float64 { return float64(h.LastReorgID) })

	return m
}

// ObserveRPC implements chain.RPCObserver. Unknown methods and outcomes are ignored.
func (m *Indexer) ObserveRPC(method, outcome string) {
	if rpcMethods[method] && rpcOutcomes[outcome] {
		m.rpc.WithLabelValues(method, outcome).Inc()
	}
}

// Recomputed counts completed statistics recomputations.
func (m *Indexer) Recomputed(count int) {
	if count > 0 {
		m.recomputed.Add(float64(count))
	}
}

// PositionFailed counts one failed wallet position recomputation.
func (m *Indexer) PositionFailed() {
	m.positionFail.Inc()
}

// SenderLookupMissed counts pool swap transactions whose sender could not be resolved.
func (m *Indexer) SenderLookupMissed(count int) {
	if count > 0 {
		m.senderMiss.Add(float64(count))
	}
}

// SenderBackfillFilled counts pool swap transactions whose sender the automatic backfill wrote.
func (m *Indexer) SenderBackfillFilled(count int) {
	if count > 0 {
		m.backfilled.Add(float64(count))
	}
}

// SenderBackfillFailed counts one failed automatic sender backfill pass.
func (m *Indexer) SenderBackfillFailed() {
	m.backfillFail.Inc()
}

// Notified counts one NOTIFY outcome. Unknown channels are ignored.
func (m *Indexer) Notified(channel string, ok bool) {
	if !notifyChannel[channel] {
		return
	}
	outcome := "ok"
	if !ok {
		outcome = "error"
	}
	m.notification.WithLabelValues(channel, outcome).Inc()
}

func boolValue(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
