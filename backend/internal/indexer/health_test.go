package indexer

import "testing"

func TestHealthChainHeadAndDatabase(t *testing.T) {
	tracker := new(HealthTracker)
	fresh := tracker.Snapshot()
	if fresh.ObservedLag != 0 || fresh.ChainHead != 0 || fresh.DatabaseUp {
		t.Fatalf("fresh snapshot = %+v, want zero lag, head and database down", fresh)
	}

	tracker.ChainHead(105)
	tracker.Database(true)
	got := tracker.Snapshot()
	if got.ChainHead != 105 || !got.DatabaseUp {
		t.Fatalf("snapshot head/database = %d/%v, want 105/true", got.ChainHead, got.DatabaseUp)
	}

	tracker.Database(false)
	if tracker.Snapshot().DatabaseUp {
		t.Fatal("Database(false) must clear DatabaseUp")
	}
}
