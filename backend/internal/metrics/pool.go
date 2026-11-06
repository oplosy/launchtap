package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// RegisterPool exposes pgx pool statistics, read on every scrape. prefix is the
// process prefix, for example "launchpad_api". stat must be cheap and
// concurrency-safe, such as pool.Stat.
func RegisterPool(reg prometheus.Registerer, prefix string, stat func() *pgxpool.Stat) {
	gauge := func(name, help string, value func(*pgxpool.Stat) float64) {
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: prefix + "_" + name, Help: help}, func() float64 { return value(stat()) }))
	}
	counter := func(name, help string, value func(*pgxpool.Stat) float64) {
		reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: prefix + "_" + name, Help: help}, func() float64 { return value(stat()) }))
	}
	gauge("db_pool_acquired_conns", "Connections currently checked out of the pool.", func(s *pgxpool.Stat) float64 { return float64(s.AcquiredConns()) })
	gauge("db_pool_idle_conns", "Idle connections in the pool.", func(s *pgxpool.Stat) float64 { return float64(s.IdleConns()) })
	gauge("db_pool_max_conns", "Configured maximum pool size.", func(s *pgxpool.Stat) float64 { return float64(s.MaxConns()) })
	counter("db_pool_acquire_wait_seconds_total", "Cumulative time spent acquiring connections.", func(s *pgxpool.Stat) float64 { return s.AcquireDuration().Seconds() })
	counter("db_pool_empty_acquire_total", "Acquires that had to wait because the pool had no idle connection.", func(s *pgxpool.Stat) float64 { return float64(s.EmptyAcquireCount()) })
}
