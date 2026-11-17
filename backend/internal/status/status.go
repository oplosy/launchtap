// Package status reports whether the indexed data the API serves is current (production-readiness
// audit pass 3, L-5).
package status

import (
	"context"
	"time"
)

// IndexerStaleAfter is how long the indexer may go without a committed step before its data is
// reported stale. At the tip a step commits every poll interval (1 s by default), so two minutes
// tolerates RPC retries and a slow chunk without flapping.
const IndexerStaleAfter = 2 * time.Minute

// Indexer is the indexer's last committed state, read with the database clock so the age never
// depends on the API host's clock.
type Indexer struct {
	ObservedBlock  *int64
	SafeBlock      *int64
	FinalizedBlock *int64
	// HeartbeatAt is the database time of the last committed step; nil before the first one.
	HeartbeatAt *time.Time
	// CheckedAt is the database time of the read.
	CheckedAt time.Time
}

// HeartbeatAge returns how long ago the last step committed, and false before the first one.
// A heartbeat ahead of the read (clock adjustment) counts as age zero.
func (i Indexer) HeartbeatAge() (time.Duration, bool) {
	if i.HeartbeatAt == nil {
		return 0, false
	}
	return max(i.CheckedAt.Sub(*i.HeartbeatAt), 0), true
}

// Stale reports whether the indexer has never committed or last committed longer ago than
// IndexerStaleAfter.
func (i Indexer) Stale() bool {
	age, ok := i.HeartbeatAge()
	return !ok || age > IndexerStaleAfter
}

// Reader reads the indexer status of one deployment.
type Reader interface {
	Indexer(ctx context.Context, chainID int64, deploymentID string) (Indexer, error)
}
