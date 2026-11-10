package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/realtime"
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

type recordingTail struct{ hinted []string }

func (t *recordingTail) Hint(event realtime.Event) { t.hinted = append(t.hinted, event.Type) }

func TestRefreshListenerSkipsTailForComment(t *testing.T) {
	hub := realtime.NewHub(4, 8)
	sub, err := hub.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	tail := &recordingTail{}
	receive := refreshReceiver(hub, tail, 1, "dep")
	token := "0x00000000000000000000000000000000000000aa"
	receive([]byte(`{"type":"comment","chain_id":1,"deployment_id":"dep","token":"` + token + `"}`))
	receive([]byte(`{"type":"token","chain_id":1,"deployment_id":"dep","token":"` + token + `"}`))
	receive([]byte(`{"type":"comment","chain_id":2,"deployment_id":"dep","token":"` + token + `"}`))
	if len(tail.hinted) != 1 || tail.hinted[0] != "token" {
		t.Fatalf("tail hints = %v", tail.hinted)
	}
	for _, want := range []string{"comment", "token"} {
		select {
		case event := <-sub.C:
			if event.Type != want {
				t.Fatalf("hub event = %s, want %s", event.Type, want)
			}
		default:
			t.Fatalf("hub did not receive %s", want)
		}
	}
	select {
	case event := <-sub.C:
		t.Fatalf("out-of-scope event published: %+v", event)
	default:
	}
}
