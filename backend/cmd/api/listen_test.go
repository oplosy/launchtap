package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestListenLoopReportsReconnectAfterRestartOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts, reconnects atomic.Int32
	reconnected := make(chan struct{}, 4)
	listen := func(ctx context.Context, ready chan<- struct{}) error {
		n := attempts.Add(1)
		close(ready)
		if n == 1 {
			return errors.New("connection lost")
		}
		<-ctx.Done()
		return ctx.Err()
	}
	done := make(chan struct{})
	go func() {
		listenLoop(ctx, listen, func() { reconnects.Add(1); reconnected <- struct{}{} }, time.Millisecond)
		close(done)
	}()
	select {
	case <-reconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect was not reported")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("listenLoop did not return after cancel")
	}
	if attempts.Load() != 2 || reconnects.Load() != 1 {
		t.Fatalf("attempts=%d reconnects=%d", attempts.Load(), reconnects.Load())
	}
}

func TestListenLoopSkipsReconnectWhenListenNeverReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var attempts, reconnects atomic.Int32
	listen := func(context.Context, chan<- struct{}) error {
		if attempts.Add(1) >= 3 {
			cancel()
		}
		return errors.New("acquire failed")
	}
	listenLoop(ctx, listen, func() { reconnects.Add(1) }, time.Millisecond)
	if reconnects.Load() != 0 {
		t.Fatalf("reconnect reported for a listener that never became ready: %d", reconnects.Load())
	}
}
