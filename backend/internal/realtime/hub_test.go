package realtime

import (
	"errors"
	"sync"
	"testing"
)

func TestHubBoundsAndEvictsSlowSubscribers(t *testing.T) {
	hub := NewHub(2, 1)
	first, err := hub.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := hub.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := hub.Subscribe(); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity error=%v", err)
	}
	hub.Publish(Event{Type: "token", ChainID: 1, DeploymentID: "test"})
	hub.Publish(Event{Type: "token", ChainID: 1, DeploymentID: "test"})
	if hub.Evictions() != 2 || hub.Active() != 0 {
		t.Fatalf("active=%d evictions=%d", hub.Active(), hub.Evictions())
	}
}

func TestHubConcurrentPublishSubscribeAndClose(t *testing.T) {
	hub := NewHub(256, 4)
	var wait sync.WaitGroup
	for index := 0; index < 64; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			subscription, err := hub.Subscribe()
			if err != nil {
				return
			}
			hub.Publish(Event{Type: "launch", ChainID: 1, DeploymentID: "test"})
			select {
			case <-subscription.C:
			default:
			}
			subscription.Close()
		}()
	}
	wait.Wait()
	if hub.Active() != 0 {
		t.Fatalf("active=%d", hub.Active())
	}
}

func TestDecodeRejectsInvalidScopeAndType(t *testing.T) {
	for _, payload := range []string{`{}`, `{"type":"other","chain_id":1,"deployment_id":"d"}`, `{"type":"token","chain_id":0,"deployment_id":"d"}`} {
		if _, err := Decode([]byte(payload)); err == nil {
			t.Fatalf("accepted %s", payload)
		}
	}
}

func TestDecodeCommentEvent(t *testing.T) {
	event, err := Decode([]byte(`{"type":"comment","chain_id":1,"deployment_id":"d","token":"0x00000000000000000000000000000000000000aA"}`))
	if err != nil || event.Type != "comment" || event.Token != "0x00000000000000000000000000000000000000aA" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	for _, payload := range []string{
		`{"type":"comment","chain_id":1,"deployment_id":"d"}`,
		`{"type":"comment","chain_id":1,"deployment_id":"d","token":""}`,
		`{"type":"comment","chain_id":1,"deployment_id":"d","token":"0x12"}`,
		`{"type":"comment","chain_id":1,"deployment_id":"d","token":"00000000000000000000000000000000000000aa"}`,
		`{"type":"comment","chain_id":1,"deployment_id":"d","token":"0xzz000000000000000000000000000000000000aa"}`,
	} {
		if _, err := Decode([]byte(payload)); err == nil {
			t.Fatalf("accepted %s", payload)
		}
	}
}
