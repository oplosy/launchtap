package chain

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// fakeProvider is one scripted JSON-RPC provider for failover tests.
type fakeProvider struct {
	latest     atomic.Int64
	chainID    atomic.Uint64
	down       atomic.Bool
	logsDown   atomic.Bool
	reverting  atomic.Bool
	capacity   atomic.Bool
	headsDelay atomic.Int64 // nanoseconds
	codeHits   atomic.Int64
	logsHits   atomic.Int64
}

func newFakeProvider(t *testing.T, latest int64) (*fakeProvider, *Client) {
	t.Helper()
	return newFakeProviderWithConfig(t, latest, RPCConfig{Timeout: time.Second, RetryBackoff: time.Millisecond})
}

func newFakeProviderWithConfig(t *testing.T, latest int64, config RPCConfig) (*fakeProvider, *Client) {
	t.Helper()
	provider := &fakeProvider{}
	provider.latest.Store(latest)
	provider.chainID.Store(46630)
	server := fakeRPCServer(t, func(request rpcRequest) any {
		if provider.down.Load() {
			return rpcFailure{Code: -32000, Message: "upstream unavailable"}
		}
		switch request.Method {
		case "eth_chainId":
			return hexutil.EncodeUint64(provider.chainID.Load())
		case "eth_getCode":
			provider.codeHits.Add(1)
			if provider.reverting.Load() {
				return rpcFailure{Code: -32000, Message: "execution reverted"}
			}
			return "0x60"
		case "eth_getLogs":
			provider.logsHits.Add(1)
			if provider.logsDown.Load() {
				return rpcFailure{Code: -32000, Message: "upstream unavailable"}
			}
			if provider.capacity.Load() {
				return rpcFailure{Code: -32005, Message: "query returned more than 10000 results"}
			}
			return []any{}
		case "eth_getBlockByNumber":
			time.Sleep(time.Duration(provider.headsDelay.Load()))
			var tag string
			if err := json.Unmarshal(request.Params[0], &tag); err != nil {
				t.Errorf("decode block tag: %v", err)
			}
			latest := provider.latest.Load()
			switch tag {
			case "latest":
				return testHeader(latest)
			case "safe":
				return testHeader(latest - 2)
			case "finalized":
				return testHeader(latest - 4)
			}
			number, err := hexutil.DecodeUint64(tag)
			if err != nil || int64(number) > latest {
				return nil
			}
			return testHeader(int64(number))
		}
		return rpcFailure{Code: -32601, Message: "method not found"}
	})
	t.Cleanup(server.Close)
	client, err := Dial(context.Background(), server.URL, config)
	if err != nil {
		t.Fatal(err)
	}
	return provider, client
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newFailoverPair(t *testing.T, primaryLatest, fallbackLatest int64) (*fakeProvider, *fakeProvider, *Client, *fakeClock) {
	t.Helper()
	primary, primaryClient := newFakeProvider(t, primaryLatest)
	fallback, fallbackClient := newFakeProvider(t, fallbackLatest)
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	client, err := WithFallback(primaryClient, fallbackClient, FailoverConfig{ChainID: 46630, AfterFailures: 3, ProbeInterval: 30 * time.Second, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return primary, fallback, client, clock
}

func mustHeadsAt(t *testing.T, client *Client, want int64) {
	t.Helper()
	heads, err := client.Heads(context.Background())
	if err != nil || heads.Latest.Number.Int64() != want {
		t.Fatalf("Heads() = %v, %v; want latest %d", heads.Latest, err, want)
	}
}

func failCodeCalls(t *testing.T, client *Client, n int) {
	t.Helper()
	for range n {
		if _, err := client.CodeAt(context.Background(), common.Address{}); err == nil || errors.Is(err, ErrRPCSwitching) {
			t.Fatalf("CodeAt() error = %v, want a provider failure", err)
		}
	}
}

func TestFailoverSwitchesAfterConsecutiveFailuresAndGatesUntilCaughtUp(t *testing.T) {
	t.Parallel()
	primary, fallback, client, _ := newFailoverPair(t, 100, 98)
	mustHeadsAt(t, client, 100)

	primary.down.Store(true)
	failCodeCalls(t, client, 3)

	// Switched: everything but Heads waits for the fallback to reach block 100.
	if _, err := client.CodeAt(context.Background(), common.Address{}); !errors.Is(err, ErrRPCSwitching) {
		t.Fatalf("CodeAt() while gated = %v, want ErrRPCSwitching", err)
	}
	var catchingUp *CatchingUpError
	if _, err := client.Heads(context.Background()); !errors.As(err, &catchingUp) || catchingUp.Latest != 98 || catchingUp.Target != 100 || catchingUp.Role != roleFallback {
		t.Fatalf("Heads() while behind = %v, want CatchingUpError 98 -> 100", err)
	}

	fallback.latest.Store(101)
	mustHeadsAt(t, client, 101)
	if _, err := client.CodeAt(context.Background(), common.Address{}); err != nil {
		t.Fatalf("CodeAt() after catch-up = %v", err)
	}
	if fallback.codeHits.Load() != 1 {
		t.Fatalf("fallback code hits = %d, want 1", fallback.codeHits.Load())
	}
}

func TestFailoverSwitchesBackOnlyAtHeadsWhenPrimaryHasCaughtUp(t *testing.T) {
	t.Parallel()
	primary, fallback, client, clock := newFailoverPair(t, 100, 100)
	mustHeadsAt(t, client, 100)
	primary.down.Store(true)
	failCodeCalls(t, client, 3)
	fallback.latest.Store(104)
	mustHeadsAt(t, client, 104)

	// Recovered but behind the highest head seen: stay on the fallback.
	primary.down.Store(false)
	primary.latest.Store(103)
	clock.Advance(30 * time.Second)
	mustHeadsAt(t, client, 104)

	// Inside the probe interval nothing is probed, even though the primary has caught up.
	primary.latest.Store(110)
	clock.Advance(10 * time.Second)
	mustHeadsAt(t, client, 104)

	clock.Advance(20 * time.Second)
	mustHeadsAt(t, client, 110)
	before := primary.codeHits.Load()
	if _, err := client.CodeAt(context.Background(), common.Address{}); err != nil || primary.codeHits.Load() != before+1 {
		t.Fatalf("CodeAt() after switch back = %v, primary hits %d -> %d", err, before, primary.codeHits.Load())
	}
}

func TestFailoverRefusesProviderWithWrongChainID(t *testing.T) {
	t.Parallel()
	primary, fallback, client, _ := newFailoverPair(t, 100, 100)
	fallback.chainID.Store(1)
	mustHeadsAt(t, client, 100)
	primary.down.Store(true)
	failCodeCalls(t, client, 4)

	primary.down.Store(false)
	if _, err := client.CodeAt(context.Background(), common.Address{}); err != nil {
		t.Fatalf("CodeAt() on primary = %v", err)
	}
	if fallback.codeHits.Load() != 0 {
		t.Fatal("a fallback with the wrong chain id served a call")
	}
}

func TestFailoverIgnoresCapacityErrorsAndCancellations(t *testing.T) {
	t.Parallel()
	primary, fallback, client, _ := newFailoverPair(t, 100, 100)
	primary.capacity.Store(true)
	for range 5 {
		if _, err := client.FilterLogs(context.Background(), ethereum.FilterQuery{}); !IsCapacityError(err) {
			t.Fatalf("FilterLogs() = %v, want a capacity error", err)
		}
	}
	primary.down.Store(true)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for range 5 {
		if _, err := client.CodeAt(canceled, common.Address{}); err == nil {
			t.Fatal("CodeAt() with a canceled context succeeded")
		}
	}
	primary.down.Store(false)
	if _, err := client.CodeAt(context.Background(), common.Address{}); err != nil {
		t.Fatalf("CodeAt() = %v; capacity errors or cancellations caused a switch", err)
	}
	if fallback.codeHits.Load() != 0 {
		t.Fatal("fallback served a call")
	}
}

func TestFailoverSuccessResetsTheFailureCount(t *testing.T) {
	t.Parallel()
	primary, fallback, client, _ := newFailoverPair(t, 100, 100)
	for range 3 {
		primary.down.Store(true)
		failCodeCalls(t, client, 2)
		primary.down.Store(false)
		if _, err := client.CodeAt(context.Background(), common.Address{}); err != nil {
			t.Fatalf("CodeAt() = %v", err)
		}
	}
	if fallback.codeHits.Load() != 0 {
		t.Fatal("non-consecutive failures caused a switch")
	}
}

func TestWithFallbackValidatesInputs(t *testing.T) {
	t.Parallel()
	_, primary := newFakeProvider(t, 1)
	_, fallback := newFakeProvider(t, 1)
	t.Cleanup(primary.Close)
	t.Cleanup(fallback.Close)
	good := FailoverConfig{ChainID: 1, AfterFailures: 1, ProbeInterval: time.Second}
	cases := map[string]func() (*Client, error){
		"nil fallback": func() (*Client, error) { return WithFallback(primary, nil, good) },
		"zero chain": func() (*Client, error) {
			return WithFallback(primary, fallback, FailoverConfig{AfterFailures: 1, ProbeInterval: time.Second})
		},
		"zero threshold": func() (*Client, error) {
			return WithFallback(primary, fallback, FailoverConfig{ChainID: 1, ProbeInterval: time.Second})
		},
		"zero interval": func() (*Client, error) {
			return WithFallback(primary, fallback, FailoverConfig{ChainID: 1, AfterFailures: 1})
		},
		"fallback start without primary verification": func() (*Client, error) {
			return WithFallback(primary, fallback, FailoverConfig{ChainID: 1, AfterFailures: 1, ProbeInterval: time.Second, StartOnFallback: true})
		},
	}
	for name, build := range cases {
		if _, err := build(); err == nil {
			t.Errorf("%s: WithFallback() succeeded", name)
		}
	}
	combined, err := WithFallback(primary, fallback, good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WithFallback(combined, fallback, good); err == nil {
		t.Error("nesting a failover client succeeded")
	}
}

func TestFailoverStartOnFallbackServesFromFallbackUntilPrimaryProbe(t *testing.T) {
	t.Parallel()
	primary, primaryClient := newFakeProvider(t, 100)
	fallback, fallbackClient := newFakeProvider(t, 100)
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	client, err := WithFallback(primaryClient, fallbackClient, FailoverConfig{ChainID: 46630, AfterFailures: 3, ProbeInterval: 30 * time.Second, StartOnFallback: true, VerifyPrimary: verifiedPrimary, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	mustHeadsAt(t, client, 100)
	if _, err := client.CodeAt(context.Background(), common.Address{}); err != nil || fallback.codeHits.Load() != 1 || primary.codeHits.Load() != 0 {
		t.Fatalf("CodeAt() = %v; fallback hits %d, primary hits %d", err, fallback.codeHits.Load(), primary.codeHits.Load())
	}
	clock.Advance(30 * time.Second)
	mustHeadsAt(t, client, 100)
	if _, err := client.CodeAt(context.Background(), common.Address{}); err != nil || primary.codeHits.Load() != 1 {
		t.Fatalf("CodeAt() after probe = %v; primary hits %d", err, primary.codeHits.Load())
	}
}

// An outage of one method (eth_getLogs) switches providers even while head reads keep
// succeeding in between: failures are counted per method.
func TestFailoverCountsFailuresPerMethod(t *testing.T) {
	t.Parallel()
	primary, fallback, client, _ := newFailoverPair(t, 100, 100)
	mustHeadsAt(t, client, 100)
	primary.logsDown.Store(true)
	for range 3 {
		if _, err := client.FilterLogs(context.Background(), ethereum.FilterQuery{}); err == nil {
			t.Fatal("FilterLogs() on a failing provider succeeded")
		}
		if _, err := client.CodeAt(context.Background(), common.Address{}); err != nil && !errors.Is(err, ErrRPCSwitching) {
			t.Fatalf("CodeAt() = %v", err)
		}
	}
	mustHeadsAt(t, client, 100)
	if _, err := client.FilterLogs(context.Background(), ethereum.FilterQuery{}); err != nil || fallback.logsHits.Load() != 1 {
		t.Fatalf("FilterLogs() after switch = %v; fallback logs hits %d", err, fallback.logsHits.Load())
	}
}

// A -32000 answer about the request (a revert) is not a provider outage.
func TestFailoverIgnoresRequestErrors(t *testing.T) {
	t.Parallel()
	primary, fallback, client, _ := newFailoverPair(t, 100, 100)
	primary.reverting.Store(true)
	for range 5 {
		if _, err := client.CodeAt(context.Background(), common.Address{}); err == nil || countsAsProviderFailure(context.Background(), err) {
			t.Fatalf("CodeAt() = %v, want a request error that does not count toward a switch", err)
		}
	}
	primary.reverting.Store(false)
	if _, err := client.CodeAt(context.Background(), common.Address{}); err != nil || fallback.codeHits.Load() != 0 {
		t.Fatalf("CodeAt() = %v; fallback hits %d, want no switch", err, fallback.codeHits.Load())
	}
}

// One timeout bounds the whole primary probe, however many retries the client allows.
func TestFailoverProbeIsBoundedByOneTimeout(t *testing.T) {
	t.Parallel()
	config := RPCConfig{Timeout: 200 * time.Millisecond, MaxRetries: 3, RetryBackoff: 50 * time.Millisecond}
	primary, primaryClient := newFakeProviderWithConfig(t, 100, config)
	_, fallbackClient := newFakeProviderWithConfig(t, 100, config)
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	client, err := WithFallback(primaryClient, fallbackClient, FailoverConfig{ChainID: 46630, AfterFailures: 3, ProbeInterval: time.Second, StartOnFallback: true, VerifyPrimary: verifiedPrimary, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	primary.headsDelay.Store(int64(time.Second))
	clock.Advance(time.Second)
	started := time.Now()
	mustHeadsAt(t, client, 100)
	// Unbounded, four attempts with backoff would take over a second.
	if elapsed := time.Since(started); elapsed > 700*time.Millisecond {
		t.Fatalf("Heads() with a hanging primary took %v", elapsed)
	}
}

// A head read in flight on the old provider when another caller triggers a switch must not
// return that provider's heads: the step aborts instead of mixing providers.
func TestFailoverInFlightCallOnOldProviderAbortsAfterSwitch(t *testing.T) {
	t.Parallel()
	primary, _, client, _ := newFailoverPair(t, 100, 100)
	mustHeadsAt(t, client, 100)
	primary.headsDelay.Store(int64(300 * time.Millisecond))
	result := make(chan error, 1)
	go func() {
		_, err := client.Heads(context.Background())
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	primary.logsDown.Store(true)
	for range 3 {
		if _, err := client.FilterLogs(context.Background(), ethereum.FilterQuery{}); err == nil {
			t.Fatal("FilterLogs() on a failing provider succeeded")
		}
	}
	if err := <-result; !errors.Is(err, ErrRPCSwitching) {
		t.Fatalf("in-flight Heads() across a switch = %v, want ErrRPCSwitching", err)
	}
	if _, err := client.CodeAt(context.Background(), common.Address{}); !errors.Is(err, ErrRPCSwitching) {
		t.Fatalf("CodeAt() after the switch = %v, want ErrRPCSwitching until the fallback is confirmed", err)
	}
}

func verifiedPrimary(context.Context) error { return nil }

// startOnFallbackWithVerification builds a failover started on the fallback whose primary
// verification returns the scripted results in order, then the last one forever.
func startOnFallbackWithVerification(t *testing.T, results ...error) (*fakeProvider, *fakeProvider, *Client, *fakeClock, *atomic.Int64) {
	t.Helper()
	primary, primaryClient := newFakeProvider(t, 100)
	fallback, fallbackClient := newFakeProvider(t, 100)
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	calls := &atomic.Int64{}
	verify := func(context.Context) error {
		call := calls.Add(1)
		return results[min(int(call), len(results))-1]
	}
	client, err := WithFallback(primaryClient, fallbackClient, FailoverConfig{ChainID: 46630, AfterFailures: 3, ProbeInterval: 30 * time.Second, StartOnFallback: true, VerifyPrimary: verify, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return primary, fallback, client, clock, calls
}

func mustServeFrom(t *testing.T, client *Client, provider *fakeProvider) {
	t.Helper()
	before := provider.codeHits.Load()
	if _, err := client.CodeAt(context.Background(), common.Address{}); err != nil || provider.codeHits.Load() != before+1 {
		t.Fatalf("CodeAt() = %v; want it served by the expected provider", err)
	}
}

// A primary never verified at startup is verified once before the first switch back, and not
// again on a later switch back.
func TestFailoverVerifiesUnverifiedPrimaryOnceBeforeSwitchingBack(t *testing.T) {
	t.Parallel()
	primary, fallback, client, clock, calls := startOnFallbackWithVerification(t, nil)
	clock.Advance(30 * time.Second)
	mustHeadsAt(t, client, 100)
	mustServeFrom(t, client, primary)
	if calls.Load() != 1 {
		t.Fatalf("primary verified %d times, want 1", calls.Load())
	}

	primary.down.Store(true)
	failCodeCalls(t, client, 3)
	primary.down.Store(false)
	mustHeadsAt(t, client, 100)
	mustServeFrom(t, client, fallback)
	clock.Advance(30 * time.Second)
	mustHeadsAt(t, client, 100)
	mustServeFrom(t, client, primary)
	if calls.Load() != 1 {
		t.Fatalf("primary verified %d times after a second switch back, want 1", calls.Load())
	}
}

// A primary that fails the identity checks (wrong bytecode) never takes over, and is not probed
// again until restart.
func TestFailoverKeepsFallbackWhenPrimaryFailsIdentityChecks(t *testing.T) {
	t.Parallel()
	_, fallback, client, clock, calls := startOnFallbackWithVerification(t, errors.New("deployment bytecode mismatch"))
	for range 3 {
		clock.Advance(30 * time.Second)
		mustHeadsAt(t, client, 100)
		mustServeFrom(t, client, fallback)
	}
	if calls.Load() != 1 {
		t.Fatalf("rejected primary verified %d times, want 1", calls.Load())
	}
}

// A provider error during verification is retried at the next probe, not treated as a rejection.
func TestFailoverRetriesPrimaryVerificationAfterProviderError(t *testing.T) {
	t.Parallel()
	primary, fallback, client, clock, calls := startOnFallbackWithVerification(t, context.DeadlineExceeded, nil)
	clock.Advance(30 * time.Second)
	mustHeadsAt(t, client, 100)
	mustServeFrom(t, client, fallback)
	clock.Advance(30 * time.Second)
	mustHeadsAt(t, client, 100)
	mustServeFrom(t, client, primary)
	if calls.Load() != 2 {
		t.Fatalf("primary verified %d times, want 2", calls.Load())
	}
}

// A fallback failure may not hand the indexer to a primary that was never verified or that
// failed the identity checks: the failure-driven switch runs the same verification.
func TestFailoverFallbackFailureDoesNotSwitchToUnverifiedPrimary(t *testing.T) {
	t.Parallel()
	_, fallback, client, clock, calls := startOnFallbackWithVerification(t, errors.New("deployment bytecode mismatch"))
	fallback.down.Store(true)
	failCodeCalls(t, client, 3)
	if calls.Load() != 1 {
		t.Fatalf("primary verified %d times on the failure switch, want 1", calls.Load())
	}
	fallback.down.Store(false)
	mustHeadsAt(t, client, 100)
	mustServeFrom(t, client, fallback)

	// Rejected now: neither another failure nor a probe tries the primary again.
	fallback.down.Store(true)
	failCodeCalls(t, client, 3)
	fallback.down.Store(false)
	clock.Advance(30 * time.Second)
	mustHeadsAt(t, client, 100)
	mustServeFrom(t, client, fallback)
	if calls.Load() != 1 {
		t.Fatalf("rejected primary verified %d times, want 1", calls.Load())
	}
}

// The failure-driven switch to an unverified primary that passes the checks goes through.
func TestFailoverFallbackFailureVerifiesThenSwitchesToPrimary(t *testing.T) {
	t.Parallel()
	primary, fallback, client, _, calls := startOnFallbackWithVerification(t, nil)
	fallback.down.Store(true)
	failCodeCalls(t, client, 3)
	mustHeadsAt(t, client, 100)
	mustServeFrom(t, client, primary)
	if calls.Load() != 1 {
		t.Fatalf("primary verified %d times, want 1", calls.Load())
	}
}

// A rejection is reported to OnPrimaryRejected exactly once, however many paths see it.
func TestFailoverReportsPrimaryRejectionOnce(t *testing.T) {
	t.Parallel()
	_, primaryClient := newFakeProvider(t, 100)
	fallback, fallbackClient := newFakeProvider(t, 100)
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	var reported atomic.Int64
	client, err := WithFallback(primaryClient, fallbackClient, FailoverConfig{
		ChainID: 46630, AfterFailures: 3, ProbeInterval: 30 * time.Second, StartOnFallback: true, Now: clock.Now,
		VerifyPrimary:     func(context.Context) error { return errors.New("deployment bytecode mismatch") },
		OnPrimaryRejected: func() { reported.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	clock.Advance(30 * time.Second)
	mustHeadsAt(t, client, 100)
	fallback.down.Store(true)
	failCodeCalls(t, client, 3)
	fallback.down.Store(false)
	if reported.Load() != 1 {
		t.Fatalf("OnPrimaryRejected called %d times, want 1", reported.Load())
	}
}

// The identity checks get their own budget: checks that need longer than one RPC timeout still
// complete and let the primary back.
func TestFailoverPrimaryVerificationOutlastsOneTimeout(t *testing.T) {
	t.Parallel()
	config := RPCConfig{Timeout: 100 * time.Millisecond, RetryBackoff: time.Millisecond}
	primary, primaryClient := newFakeProviderWithConfig(t, 100, config)
	_, fallbackClient := newFakeProviderWithConfig(t, 100, config)
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	slowChecks := func(ctx context.Context) error {
		select {
		case <-time.After(3 * config.Timeout):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	client, err := WithFallback(primaryClient, fallbackClient, FailoverConfig{ChainID: 46630, AfterFailures: 3, ProbeInterval: time.Second, StartOnFallback: true, VerifyPrimary: slowChecks, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	clock.Advance(time.Second)
	mustHeadsAt(t, client, 100)
	mustServeFrom(t, client, primary)
}
