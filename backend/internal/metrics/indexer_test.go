package metrics

import (
	"math"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/reconcile"
)

func TestIndexerMetricsMatchHealthSnapshot(t *testing.T) {
	t.Parallel()

	tracker := new(indexer.HealthTracker)
	now := time.Now().UTC()
	tracker.Set(indexer.Health{ChainID: 1, OwnershipHeld: true})
	tracker.Committed(indexer.State{
		ChainID:   1,
		Observed:  &ledger.IndexedBlock{BlockNumber: 100, BlockTime: now.Add(-3 * time.Second)},
		Safe:      &ledger.IndexedBlock{BlockNumber: 90, BlockTime: now.Add(-30 * time.Second)},
		Finalized: &ledger.IndexedBlock{BlockNumber: 80, BlockTime: now.Add(-60 * time.Second)},
	})
	tracker.ChainHead(105)
	tracker.Database(true)
	tracker.Operational(indexer.OperationalHealth{DirtyWork: 4, LastReorgID: 2, LastReorgDepth: 3})

	reg := NewRegistry(ComponentIndexer)
	m := NewIndexer(reg, tracker.Snapshot)
	m.ObserveRPC("eth_getLogs", "ok")
	m.ObserveRPC("eth_getLogs", "ok")
	m.ObserveRPC("eth_getLogs", "timeout")
	m.ObserveRPC("eth_unknown", "ok")
	m.ObserveRPC("eth_getLogs", "weird")
	m.Recomputed(2)
	m.Recomputed(0)
	m.Notified("market_dirty", true)
	m.Notified("api_refresh", false)
	m.Notified("other", true)

	snapshot := tracker.Snapshot()
	checks := []struct {
		name   string
		labels map[string]string
		want   float64
	}{
		{"launchpad_indexer_block_number", map[string]string{"level": "observed"}, 100},
		{"launchpad_indexer_block_number", map[string]string{"level": "safe"}, 90},
		{"launchpad_indexer_block_number", map[string]string{"level": "finalized"}, 80},
		{"launchpad_indexer_chain_head_block", map[string]string{}, 105},
		{"launchpad_indexer_ownership_held", map[string]string{}, 1},
		{"launchpad_indexer_rpc_healthy", map[string]string{}, 1},
		{"launchpad_indexer_db_up", map[string]string{}, 1},
		{"launchpad_indexer_dirty_work", map[string]string{}, 4},
		{"launchpad_indexer_last_reorg_depth", map[string]string{}, 3},
		{"launchpad_indexer_reorgs_total", map[string]string{}, 2},
		{"launchpad_indexer_rpc_requests_total", map[string]string{"method": "eth_getLogs", "outcome": "ok"}, 2},
		{"launchpad_indexer_rpc_requests_total", map[string]string{"method": "eth_getLogs", "outcome": "timeout"}, 1},
		{"launchpad_indexer_stats_recomputed_total", map[string]string{}, 2},
		{"launchpad_indexer_notifications_total", map[string]string{"channel": "market_dirty", "outcome": "ok"}, 1},
		{"launchpad_indexer_notifications_total", map[string]string{"channel": "api_refresh", "outcome": "error"}, 1},
	}
	for _, check := range checks {
		if got := seriesValue(t, reg, check.name, check.labels); got != check.want {
			t.Errorf("%s%v = %v, want %v", check.name, check.labels, got, check.want)
		}
	}
	for level, lag := range map[string]time.Duration{"observed": snapshot.ObservedLag, "safe": snapshot.SafeLag, "finalized": snapshot.FinalizedLag} {
		got := seriesValue(t, reg, "launchpad_indexer_lag_seconds", map[string]string{"level": level})
		if math.Abs(got-lag.Seconds()) > 1 || got < 1 {
			t.Errorf("lag_seconds{level=%q} = %v, want about %v", level, got, lag.Seconds())
		}
	}
	for _, absent := range []map[string]string{{"method": "eth_unknown", "outcome": "ok"}, {"method": "eth_getLogs", "outcome": "weird"}} {
		if got := seriesValue(t, reg, "launchpad_indexer_rpc_requests_total", absent); got != -1 {
			t.Errorf("unknown rpc series %v exported with value %v", absent, got)
		}
	}
	if got := seriesValue(t, reg, "launchpad_indexer_notifications_total", map[string]string{"channel": "other", "outcome": "ok"}); got != -1 {
		t.Errorf("unknown channel exported with value %v", got)
	}

	unsafe, err := UnsafeLabels(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsafe) != 0 {
		t.Fatalf("unsafe labels: %v", unsafe)
	}
}

func TestPositionMetricsRegistered(t *testing.T) {
	t.Parallel()

	tracker := new(indexer.HealthTracker)
	tracker.Operational(indexer.OperationalHealth{PositionDirtyWork: 7})
	reg := NewRegistry(ComponentIndexer)
	m := NewIndexer(reg, tracker.Snapshot)
	m.PositionFailed()
	m.PositionFailed()
	m.SenderLookupMissed(3)
	m.SenderLookupMissed(0)
	m.SenderLookupMissed(-1)

	if got := seriesValue(t, reg, "launchpad_aggregation_position_failures_total", map[string]string{}); got != 2 {
		t.Errorf("launchpad_aggregation_position_failures_total = %v, want 2", got)
	}
	if got := seriesValue(t, reg, "launchpad_aggregation_position_dirty_backlog", map[string]string{}); got != 7 {
		t.Errorf("launchpad_aggregation_position_dirty_backlog = %v, want 7", got)
	}
	if got := seriesValue(t, reg, "launchpad_indexer_sender_lookup_misses_total", map[string]string{}); got != 3 {
		t.Errorf("launchpad_indexer_sender_lookup_misses_total = %v, want 3", got)
	}
}

func TestSenderBackfillMetricsRegistered(t *testing.T) {
	t.Parallel()

	reg := NewRegistry(ComponentIndexer)
	m := NewIndexer(reg, new(indexer.HealthTracker).Snapshot)
	m.SenderBackfillFilled(4)
	m.SenderBackfillFilled(0)
	m.SenderBackfillFilled(-2)
	m.SenderBackfillFailed()
	m.SenderBackfillPending(9)
	m.SenderBackfillPending(3)

	if got := seriesValue(t, reg, "launchpad_indexer_sender_backfill_filled_total", map[string]string{}); got != 4 {
		t.Errorf("launchpad_indexer_sender_backfill_filled_total = %v, want 4", got)
	}
	if got := seriesValue(t, reg, "launchpad_indexer_sender_backfill_errors_total", map[string]string{}); got != 1 {
		t.Errorf("launchpad_indexer_sender_backfill_errors_total = %v, want 1", got)
	}
	if got := seriesValue(t, reg, "launchpad_indexer_sender_backfill_pending", map[string]string{}); got != 3 {
		t.Errorf("launchpad_indexer_sender_backfill_pending = %v, want the latest count 3", got)
	}
}

func TestReconciliationMetrics(t *testing.T) {
	t.Parallel()

	reg := NewRegistry(ComponentIndexer)
	m := NewIndexer(reg, new(indexer.HealthTracker).Snapshot)
	m.Reconciled(reconcile.Result{Outcome: reconcile.OutcomeDrift, Checks: 9, Mismatches: make([]reconcile.Mismatch, 2)})
	// A discarded or failed pass keeps the last completed comparison's gauges.
	m.Reconciled(reconcile.Result{Outcome: reconcile.OutcomeReorg})
	m.Reconciled(reconcile.Result{Outcome: reconcile.OutcomeError})

	checks := []struct {
		name   string
		labels map[string]string
		want   float64
	}{
		{"launchpad_indexer_reconciliation_runs_total", map[string]string{"outcome": "drift"}, 1},
		{"launchpad_indexer_reconciliation_runs_total", map[string]string{"outcome": "reorg"}, 1},
		{"launchpad_indexer_reconciliation_runs_total", map[string]string{"outcome": "error"}, 1},
		{"launchpad_indexer_reconciliation_mismatches", map[string]string{}, 2},
		{"launchpad_indexer_reconciliation_checks", map[string]string{}, 9},
	}
	for _, check := range checks {
		if got := seriesValue(t, reg, check.name, check.labels); got != check.want {
			t.Errorf("%s%v = %v, want %v", check.name, check.labels, got, check.want)
		}
	}
	if got := seriesValue(t, reg, "launchpad_indexer_reconciliation_last_success_timestamp_seconds", map[string]string{}); got < float64(time.Now().Add(-time.Minute).Unix()) {
		t.Errorf("last success timestamp = %v, want about now", got)
	}
	m.Reconciled(reconcile.Result{Outcome: reconcile.OutcomeOK, Checks: 9})
	if got := seriesValue(t, reg, "launchpad_indexer_reconciliation_mismatches", map[string]string{}); got != 0 {
		t.Errorf("mismatches after a clean pass = %v, want 0", got)
	}
}
