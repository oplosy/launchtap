package indexer

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Contictus/launchtap/backend/internal/ledger"
)

// Health is the read-only operational snapshot exposed by the runtime. Writers
// update it only after a transaction outcome is known.
type Health struct {
	DeploymentID                       string
	ChainID, Observed, Safe, Finalized int64
	ObservedAt, SafeAt, FinalizedAt    time.Time
	ObservedLag, SafeLag, FinalizedLag time.Duration
	OwnershipHeld, RPCHealthy          bool
	DirtyWork                          int64
	PositionDirtyWork                  int64
	LastReorgID                        int64
	LastReorgDepth                     int64
	LastReorgAt                        time.Time
	PhaseCounts                        map[string]int64
	LastError                          string
	// ChainHead is the latest block number the RPC reported; zero until the first read.
	ChainHead  int64
	DatabaseUp bool
}

// OperationalHealth is the persisted portion of the runtime health snapshot.
// It is deliberately neutral so a store adapter, not generated SQL types,
// crosses the application boundary.
type OperationalHealth struct {
	DirtyWork         int64
	PositionDirtyWork int64
	LastReorgID       int64
	LastReorgDepth    int64
	LastReorgAt       time.Time
	PhaseCounts       map[string]int64
}

type HealthTracker struct {
	mu       sync.RWMutex
	snapshot Health
}

func (h *HealthTracker) Set(snapshot Health) {
	h.mu.Lock()
	defer h.mu.Unlock()
	snapshot.PhaseCounts = cloneCounts(snapshot.PhaseCounts)
	h.snapshot = snapshot
}

// Committed records watermarks only after the indexer's write transaction has
// committed. The health surface therefore never reports uncommitted progress.
func (h *HealthTracker) Committed(state State) {
	h.Update(func(snapshot *Health) {
		snapshot.ChainID = state.ChainID
		snapshot.DeploymentID = state.DeploymentID
		snapshot.Observed, snapshot.ObservedAt = healthBlock(state.Observed)
		snapshot.Safe, snapshot.SafeAt = healthBlock(state.Safe)
		snapshot.Finalized, snapshot.FinalizedAt = healthBlock(state.Finalized)
		now := time.Now().UTC()
		snapshot.ObservedLag = healthLag(now, snapshot.ObservedAt)
		snapshot.SafeLag = healthLag(now, snapshot.SafeAt)
		snapshot.FinalizedLag = healthLag(now, snapshot.FinalizedAt)
		snapshot.RPCHealthy = true
		snapshot.LastError = ""
	})
}

// ChainHead records the latest block number reported by the RPC.
func (h *HealthTracker) ChainHead(number int64) {
	h.Update(func(snapshot *Health) { snapshot.ChainHead = number })
}

// Database records whether the last operational database read succeeded. It does
// not affect readiness.
func (h *HealthTracker) Database(up bool) {
	h.Update(func(snapshot *Health) { snapshot.DatabaseUp = up })
}

// Failed makes the endpoint not-ready. A subsequent committed state clears
// the transient failure only after both the write and watermark update succeed.
func (h *HealthTracker) Failed(err error) {
	if err == nil {
		return
	}
	h.Update(func(snapshot *Health) {
		snapshot.LastError = err.Error()
		if errors.Is(err, ErrRPCUnhealthy) {
			snapshot.RPCHealthy = false
		}
	})
}

// OwnershipLost is terminal for this process because its session advisory lock
// no longer fences writes.
func (h *HealthTracker) OwnershipLost(err error) {
	h.Update(func(snapshot *Health) {
		snapshot.OwnershipHeld = false
		if err != nil {
			snapshot.LastError = err.Error()
		}
	})
}

func (h *HealthTracker) Update(update func(*Health)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	update(&h.snapshot)
	h.snapshot.PhaseCounts = cloneCounts(h.snapshot.PhaseCounts)
}
func (h *HealthTracker) Snapshot() Health {
	h.mu.RLock()
	defer h.mu.RUnlock()
	snapshot := h.snapshot
	now := time.Now().UTC()
	snapshot.ObservedLag = healthLag(now, snapshot.ObservedAt)
	snapshot.SafeLag = healthLag(now, snapshot.SafeAt)
	snapshot.FinalizedLag = healthLag(now, snapshot.FinalizedAt)
	snapshot.PhaseCounts = cloneCounts(snapshot.PhaseCounts)
	return snapshot
}

// Operational updates the persisted counters without changing transaction or
// ownership readiness. Values come from committed database state only.
func (h *HealthTracker) Operational(value OperationalHealth) {
	h.Update(func(snapshot *Health) {
		snapshot.DirtyWork = value.DirtyWork
		snapshot.PositionDirtyWork = value.PositionDirtyWork
		snapshot.LastReorgID = value.LastReorgID
		snapshot.LastReorgDepth = value.LastReorgDepth
		snapshot.LastReorgAt = value.LastReorgAt
		snapshot.PhaseCounts = cloneCounts(value.PhaseCounts)
	})
}

// HealthHandler exposes the operational snapshot as JSON for orchestrators.
func HealthHandler(tracker *HealthTracker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		snapshot := tracker.Snapshot()
		if !ready(snapshot) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		if err := json.NewEncoder(w).Encode(snapshot); err != nil {
			return
		}
	})
}

func ready(snapshot Health) bool {
	return snapshot.ChainID > 0 && snapshot.OwnershipHeld && snapshot.RPCHealthy && snapshot.LastError == "" && !snapshot.ObservedAt.IsZero()
}

func healthBlock(block *ledger.IndexedBlock) (int64, time.Time) {
	if block == nil {
		return 0, time.Time{}
	}
	return block.BlockNumber, block.BlockTime
}

func healthLag(now, at time.Time) time.Duration {
	if at.IsZero() || now.Before(at) {
		return 0
	}
	return now.Sub(at)
}
func cloneCounts(source map[string]int64) map[string]int64 {
	if source == nil {
		return nil
	}
	result := make(map[string]int64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
