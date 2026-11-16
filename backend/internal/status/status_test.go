package status

import (
	"testing"
	"time"
)

func TestIndexerStale(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	at := func(ago time.Duration) *time.Time {
		value := now.Add(-ago)
		return &value
	}
	for name, test := range map[string]struct {
		heartbeat *time.Time
		stale     bool
		age       time.Duration
		known     bool
	}{
		"never committed":       {heartbeat: nil, stale: true},
		"fresh":                 {heartbeat: at(3 * time.Second), age: 3 * time.Second, known: true},
		"at the threshold":      {heartbeat: at(IndexerStaleAfter), age: IndexerStaleAfter, known: true},
		"past the threshold":    {heartbeat: at(IndexerStaleAfter + time.Second), stale: true, age: IndexerStaleAfter + time.Second, known: true},
		"ahead of the database": {heartbeat: at(-5 * time.Second), age: 0, known: true},
	} {
		t.Run(name, func(t *testing.T) {
			status := Indexer{HeartbeatAt: test.heartbeat, CheckedAt: now}
			if got := status.Stale(); got != test.stale {
				t.Fatalf("Stale() = %v, want %v", got, test.stale)
			}
			if age, known := status.HeartbeatAge(); age != test.age || known != test.known {
				t.Fatalf("HeartbeatAge() = %v, %v, want %v, %v", age, known, test.age, test.known)
			}
		})
	}
}
