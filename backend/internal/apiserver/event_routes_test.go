package apiserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/realtime"
	"github.com/ethereum/go-ethereum/common"
)

func TestSSEInitialRetryEventHeartbeatAndCancellation(t *testing.T) {
	hub := realtime.NewHub(4, 2)
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterEventRoutes(EventRoutes{Hub: hub, ChainID: 46630, DeploymentID: "testnet", Heartbeat: 20 * time.Millisecond})
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/v1/events", nil)
	request.Header.Set("Last-Event-ID", "ignored")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status=%d content-type=%q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(response.Body)
	initial := readUntil(t, reader, "refresh-only", time.Second)
	if !strings.Contains(initial, "retry: 3000") {
		t.Fatalf("initial=%q", initial)
	}
	heartbeat := readUntil(t, reader, "heartbeat", time.Second)
	if !strings.Contains(heartbeat, ": heartbeat") {
		t.Fatalf("heartbeat=%q", heartbeat)
	}
	for deadline := time.Now().Add(time.Second); hub.Active() != 1 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	hub.Publish(realtime.Event{Type: "token", ChainID: 46630, DeploymentID: "testnet", Token: "0xabc", AsOfBlock: 10})
	event := readUntil(t, reader, `"as_of_block":10`, time.Second)
	if !strings.Contains(event, "event: token") {
		t.Fatalf("event stream=%q", event)
	}
	cancel()
	_, _ = io.Copy(io.Discard, response.Body)
	for deadline := time.Now().Add(time.Second); hub.Active() != 0 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if hub.Active() != 0 {
		t.Fatalf("active=%d", hub.Active())
	}
}

func TestSSETokenFilterAndCoalescing(t *testing.T) {
	hub := realtime.NewHub(4, 32)
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterEventRoutes(EventRoutes{Hub: hub, ChainID: 46630, DeploymentID: "testnet", Heartbeat: time.Hour, Coalesce: 50 * time.Millisecond})
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()

	invalid, err := http.Get(httpServer.URL + "/v1/events?token=not-an-address")
	if err != nil {
		t.Fatal(err)
	}
	_ = invalid.Body.Close()
	if invalid.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid token filter status=%d", invalid.StatusCode)
	}

	token := "0x00000000000000000000000000000000000000AA"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/v1/events?token="+strings.ToLower(token), nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	reader := bufio.NewReader(response.Body)
	readUntil(t, reader, "refresh-only", time.Second)
	for deadline := time.Now().Add(time.Second); hub.Active() != 1 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	hub.Publish(realtime.Event{Type: "launch", ChainID: 46630, DeploymentID: "testnet", Token: "0x00000000000000000000000000000000000000bb", AsOfBlock: 1})
	hub.Publish(realtime.Event{Type: "token", ChainID: 46630, DeploymentID: "testnet", Token: "0x00000000000000000000000000000000000000bb", AsOfBlock: 2})
	for block := int64(3); block <= 7; block++ {
		hub.Publish(realtime.Event{Type: "token", ChainID: 46630, DeploymentID: "testnet", Token: token, AsOfBlock: block})
	}
	batch := readUntil(t, reader, `"as_of_block":7`, time.Second)
	if strings.Count(batch, "event: token") != 1 || strings.Contains(batch, "event: launch") || strings.Contains(batch, "0x00000000000000000000000000000000000000bb") {
		t.Fatalf("filtered and coalesced batch=%q", batch)
	}
	hub.Publish(realtime.Event{Type: "reorg", ChainID: 46630, DeploymentID: "testnet", AsOfBlock: 8, CommonAncestor: 5})
	if reorg := readUntil(t, reader, `"common_ancestor":5`, time.Second); !strings.Contains(reorg, "event: reorg") {
		t.Fatalf("reorg=%q", reorg)
	}
}

func TestSSEOutlivesServerWriteTimeout(t *testing.T) {
	hub := realtime.NewHub(2, 2)
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterEventRoutes(EventRoutes{Hub: hub, ChainID: 46630, DeploymentID: "testnet", Heartbeat: 10 * time.Millisecond})
	httpServer := httptest.NewUnstartedServer(server.Handler)
	httpServer.Config.WriteTimeout = 30 * time.Millisecond
	httpServer.Start()
	defer httpServer.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/v1/events", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	reader := bufio.NewReader(response.Body)
	readUntil(t, reader, "refresh-only", time.Second)
	time.Sleep(60 * time.Millisecond)
	hub.Publish(realtime.Event{Type: "token", ChainID: 46630, DeploymentID: "testnet", Token: "0xabc", AsOfBlock: 11})
	event := readUntil(t, reader, `"as_of_block":11`, time.Second)
	if !strings.Contains(event, "event: token") {
		t.Fatalf("event stream=%q", event)
	}
	cancel()
	_ = response.Body.Close()
}

func TestServerShutdownCancelsActiveSSEHandler(t *testing.T) {
	hub := realtime.NewHub(2, 2)
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterEventRoutes(EventRoutes{Hub: hub, ChainID: 46630, DeploymentID: "testnet", Heartbeat: time.Hour})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.HTTP.Serve(listener) }()

	response, err := http.Get("http://" + listener.Addr().String() + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	readUntil(t, reader, "refresh-only", time.Second)
	for deadline := time.Now().Add(time.Second); hub.Active() != 1 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if hub.Active() != 1 {
		_ = response.Body.Close()
		t.Fatalf("active subscriptions before shutdown=%d, want 1", hub.Active())
	}

	shutdownCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = response.Body.Close()
		t.Fatalf("shutdown with active stream: %v", err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		_ = response.Body.Close()
		t.Fatalf("read stream after shutdown: %v", err)
	}
	_ = response.Body.Close()
	if hub.Active() != 0 {
		t.Fatalf("active subscriptions after shutdown=%d, want 0", hub.Active())
	}
	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve error=%v, want http.ErrServerClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP Serve did not return after shutdown")
	}
}

func readUntil(t *testing.T, reader *bufio.Reader, wanted string, timeout time.Duration) string {
	t.Helper()
	result := make(chan string, 1)
	go func() {
		var value strings.Builder
		for {
			line, err := reader.ReadString('\n')
			value.WriteString(line)
			if strings.Contains(value.String(), wanted) || err != nil {
				result <- value.String()
				return
			}
		}
	}()
	select {
	case value := <-result:
		return value
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %q", wanted)
		return ""
	}
}

func feedTestTrade(token string, block int64) *realtime.Trade {
	buyer := common.HexToAddress("0x00000000000000000000000000000000000000cc")
	return &realtime.Trade{
		Token: common.HexToAddress(token), Name: "Alpha", Symbol: "ALP", Source: "curve", Trader: &buyer, Buy: true,
		ETHVolume: big.NewInt(1500), TokenVolume: big.NewInt(2500), Block: block, TxIndex: 1, LogIndex: 2,
		TxHash: common.HexToHash("0x01"), Time: time.Unix(1700000000, 0).UTC(), Finality: "provisional",
	}
}

func openFeedStream(t *testing.T, hub *realtime.Hub, query string) *bufio.Reader {
	t.Helper()
	server := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterEventRoutes(EventRoutes{Hub: hub, ChainID: 46630, DeploymentID: "testnet", Heartbeat: time.Hour, Coalesce: time.Hour})
	httpServer := httptest.NewServer(server.Handler)
	t.Cleanup(httpServer.Close)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/v1/events"+query, nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	reader := bufio.NewReader(response.Body)
	readUntil(t, reader, "refresh-only", time.Second)
	for deadline := time.Now().Add(time.Second); hub.Active() < 1 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	return reader
}

func TestEventStreamForwardsFeedEventsUncoalesced(t *testing.T) {
	hub := realtime.NewHub(4, 64)
	reader := openFeedStream(t, hub, "")
	token := "0x00000000000000000000000000000000000000aa"
	// The coalescing window is an hour, so a delivered trade proves feed events bypass it.
	hub.Publish(realtime.Event{Type: "trade", ChainID: 46630, DeploymentID: "testnet", Token: token, Trade: feedTestTrade(token, 10)})
	hub.Publish(realtime.Event{Type: "trade", ChainID: 46630, DeploymentID: "testnet", Token: token, Trade: feedTestTrade(token, 11)})
	got := readUntil(t, reader, `"block_number":11`, time.Second)
	if strings.Count(got, "event: trade\n") != 2 || !strings.Contains(got, `"block_number":10`) {
		t.Fatalf("stream=%q", got)
	}
	for _, want := range []string{`"source":"curve"`, `"side":"buy"`, `"eth_volume":"1500"`, `"token_volume":"2500"`, `"symbol":"ALP"`, `"deployment_id":"testnet"`, `"finality":"provisional"`, `"trader":"0x00000000000000000000000000000000000000cc"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %q", want, got)
		}
	}
}

func TestEventStreamAllFeedEventTypes(t *testing.T) {
	hub := realtime.NewHub(4, 64)
	reader := openFeedStream(t, hub, "")
	token := "0x00000000000000000000000000000000000000aa"
	start := time.Unix(1700000000, 0).UTC()
	hub.Publish(realtime.Event{Type: "graduation", ChainID: 46630, DeploymentID: "testnet", Token: token, Graduation: &realtime.Graduation{Token: common.HexToAddress(token), Name: "Alpha", Symbol: "ALP", Block: 12}})
	hub.Publish(realtime.Event{Type: "new_token", ChainID: 46630, DeploymentID: "testnet", Token: token, Launch: &realtime.Launch{Token: common.HexToAddress(token), Name: "Alpha", Symbol: "ALP", Block: 13}})
	hub.Publish(realtime.Event{Type: "trade_burst", ChainID: 46630, DeploymentID: "testnet", Burst: []realtime.BurstCount{{Token: token, Count: 7}}, BurstStart: start, BurstEnd: start.Add(time.Second)})
	hub.Publish(realtime.Event{Type: "finality", ChainID: 46630, DeploymentID: "testnet", SafeBlock: 20, FinalizedBlock: 18})
	got := readUntil(t, reader, `"finalized_block":18`, time.Second)
	for _, want := range []string{"event: graduation", `"block_number":12`, "event: new_token", `"block_number":13`, "event: trade_burst", `"count":7`, `"window_start":"2023-11-14T22:13:20Z"`, "event: finality", `"safe_block":20`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %q", want, got)
		}
	}
}

func TestEventStreamTradeBurstFoldedLaunches(t *testing.T) {
	hub := realtime.NewHub(4, 64)
	reader := openFeedStream(t, hub, "")
	start := time.Unix(1700000000, 0).UTC()
	hub.Publish(realtime.Event{Type: "trade_burst", ChainID: 46630, DeploymentID: "testnet", BurstLaunches: 2, BurstStart: start, BurstEnd: start.Add(time.Second)})
	hub.Publish(realtime.Event{Type: "trade_burst", ChainID: 46630, DeploymentID: "testnet", Burst: []realtime.BurstCount{{Token: "0x00000000000000000000000000000000000000aa", Count: 1}}, BurstStart: start, BurstEnd: start.Add(time.Second)})
	got := readUntil(t, reader, `"count":1`, time.Second)
	bursts := strings.Split(got, "event: trade_burst")
	if len(bursts) != 3 {
		t.Fatalf("want 2 trade_burst events in %q", got)
	}
	for _, want := range []string{`"launches":2`, `"items":[]`} {
		if !strings.Contains(bursts[1], want) {
			t.Fatalf("missing %s in %q", want, bursts[1])
		}
	}
	if strings.Contains(bursts[1], `"graduations"`) || strings.Contains(bursts[2], `"launches"`) {
		t.Fatalf("zero folded counts not omitted: %q", bursts[2])
	}
}

func TestEventStreamTradeBurstNotificationInputs(t *testing.T) {
	hub := realtime.NewHub(4, 64)
	reader := openFeedStream(t, hub, "")
	a, b := "0x00000000000000000000000000000000000000aa", "0x00000000000000000000000000000000000000bb"
	largest, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	hub.Publish(realtime.Event{Type: "trade_burst", ChainID: 46630, DeploymentID: "testnet", Burst: []realtime.BurstCount{
		{Token: a, Count: 3, MaxETH: largest, Traders: []string{"0x00000000000000000000000000000000000000cc"}, TradersTruncated: true},
		{Token: b, Count: 1},
	}})
	got := readUntil(t, reader, `"token":"`+b+`"`, time.Second)
	for _, want := range []string{
		`{"token":"` + a + `","count":3,"max_eth_wei":"123456789012345678901234567890","traders":["0x00000000000000000000000000000000000000cc"],"traders_truncated":true}`,
		`{"token":"` + b + `","count":1,"max_eth_wei":"0","traders":[],"traders_truncated":false}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %q", want, got)
		}
	}
}

func TestEventStreamTokenFilterAppliesToFeed(t *testing.T) {
	hub := realtime.NewHub(4, 64)
	a, b := "0x00000000000000000000000000000000000000aa", "0x00000000000000000000000000000000000000bb"
	reader := openFeedStream(t, hub, "?token="+a)
	hub.Publish(realtime.Event{Type: "trade", ChainID: 46630, DeploymentID: "testnet", Token: common.HexToAddress(b).Hex(), Trade: feedTestTrade(b, 1)})
	hub.Publish(realtime.Event{Type: "new_token", ChainID: 46630, DeploymentID: "testnet", Token: a, Launch: &realtime.Launch{Token: common.HexToAddress(a), Block: 2}})
	hub.Publish(realtime.Event{Type: "graduation", ChainID: 46630, DeploymentID: "testnet", Token: common.HexToAddress(b).Hex(), Graduation: &realtime.Graduation{Token: common.HexToAddress(b), Block: 3}})
	hub.Publish(realtime.Event{Type: "trade", ChainID: 46630, DeploymentID: "testnet", Token: common.HexToAddress(a).Hex(), Trade: feedTestTrade(a, 4)})
	hub.Publish(realtime.Event{Type: "graduation", ChainID: 46630, DeploymentID: "testnet", Token: common.HexToAddress(a).Hex(), Graduation: &realtime.Graduation{Token: common.HexToAddress(a), Block: 5}})
	hub.Publish(realtime.Event{Type: "trade_burst", ChainID: 46630, DeploymentID: "testnet", Burst: []realtime.BurstCount{{Token: b, Count: 3}}})
	hub.Publish(realtime.Event{Type: "finality", ChainID: 46630, DeploymentID: "testnet", SafeBlock: 9, FinalizedBlock: 8})
	got := readUntil(t, reader, `"finalized_block":8`, time.Second)
	if strings.Contains(got, "event: new_token") || strings.Contains(got, `"block_number":1,`) || strings.Contains(got, `"block_number":3`) {
		t.Fatalf("leaked events for other tokens: %q", got)
	}
	for _, want := range []string{"event: trade\n", `"block_number":4`, "event: graduation", `"block_number":5`, "event: trade_burst", "event: finality"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %q", want, got)
		}
	}
}

func TestEventStreamStalledSubscriberEvicted(t *testing.T) {
	hub := realtime.NewHub(4, 64)
	stalled, err := hub.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer stalled.Close()
	reader := openFeedStream(t, hub, "")
	for deadline := time.Now().Add(time.Second); hub.Active() != 2 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	token := "0x00000000000000000000000000000000000000aa"
	const total = 65 // the stalled subscriber's 64-slot buffer overflows on the 65th event
	received := 0
	for i := int64(1); i <= total; i++ {
		hub.Publish(realtime.Event{Type: "trade", ChainID: 46630, DeploymentID: "testnet", Token: token, Trade: feedTestTrade(token, i)})
		// Wait for the live stream to deliver this event before publishing the next, so it never lags.
		got := readUntil(t, reader, fmt.Sprintf(`"block_number":%d,`, i), 2*time.Second)
		received += strings.Count(got, "event: trade\n")
	}
	if received != total {
		t.Fatalf("live subscriber received %d trades, want %d", received, total)
	}
	for deadline := time.Now().Add(2 * time.Second); hub.Evictions() < 1 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if hub.Evictions() != 1 || hub.Active() != 1 {
		t.Fatalf("evictions=%d active=%d, want the stalled subscriber evicted alone", hub.Evictions(), hub.Active())
	}
	buffered := 0
	for range stalled.C { // closed by the eviction, so the loop ends after the buffered events
		buffered++
	}
	if buffered != 64 {
		t.Fatalf("stalled subscriber held %d events at eviction, want 64", buffered)
	}
}
