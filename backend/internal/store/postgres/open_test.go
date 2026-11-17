package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOpenPoolRejectsTooFewConnections(t *testing.T) {
	_, err := OpenPool(t.Context(), "postgres://user:pass@127.0.0.1:5432/launchpad", PoolOptions{MaxConns: 3})
	if err == nil || !strings.Contains(err.Error(), "below minimum 4") {
		t.Fatalf("OpenPool error = %v, want minimum connection error", err)
	}
}

func TestOpenPoolUsesDefaultAndExplicitCapacity(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for _, maxConns := range []int32{0, 4, 8} {
		pool, err := OpenPool(ctx, "postgres://user:pass@127.0.0.1:5432/launchpad", PoolOptions{MaxConns: maxConns})
		if err != nil {
			t.Fatalf("OpenPool(%d): %v", maxConns, err)
		}
		pool.Close()
	}
}

func TestOpenPoolSetsStatementTimeoutOnlyWhenAsked(t *testing.T) {
	for _, test := range []struct {
		timeout time.Duration
		want    string
	}{{0, ""}, {250 * time.Millisecond, "250"}, {10 * time.Second, "10000"}} {
		pool, err := OpenPool(t.Context(), "postgres://user:pass@127.0.0.1:5432/launchpad", PoolOptions{StatementTimeout: test.timeout})
		if err != nil {
			t.Fatal(err)
		}
		if got := pool.Config().ConnConfig.RuntimeParams["statement_timeout"]; got != test.want {
			t.Errorf("timeout %v: statement_timeout = %q, want %q", test.timeout, got, test.want)
		}
		pool.Close()
	}
}
