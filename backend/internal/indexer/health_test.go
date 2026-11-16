package indexer

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

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

// The health endpoint serves LastError, so a configured renderer must replace err.Error() on both
// failure paths while RPC-unhealthy classification still sees the original error chain.
func TestHealthLastErrorUsesRenderer(t *testing.T) {
	const secret = "https://rpc.example/v2/SECRETKEY"
	tracker := &HealthTracker{Render: func(err error) string {
		return strings.ReplaceAll(err.Error(), secret, "<rpc-url>")
	}}
	tracker.Failed(fmt.Errorf("dial %s: %w", secret, ErrRPCUnhealthy))
	got := tracker.Snapshot()
	if strings.Contains(got.LastError, "SECRETKEY") || got.RPCHealthy {
		t.Fatalf("after Failed: LastError=%q RPCHealthy=%v", got.LastError, got.RPCHealthy)
	}
	tracker.OwnershipLost(errors.New("lost lock via " + secret))
	if got := tracker.Snapshot().LastError; strings.Contains(got, "SECRETKEY") || !strings.Contains(got, "<rpc-url>") {
		t.Fatalf("after OwnershipLost: LastError=%q", got)
	}
}
