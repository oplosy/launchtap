package chain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
)

// Provider failover for the indexer (docs/specs/2026-11-17-rpc-failover-preflight.md).

const (
	rolePrimary  = "primary"
	roleFallback = "fallback"
)

// ErrRPCSwitching is returned by every call except Heads while a provider switch waits for the
// new provider to reach the highest head seen before the switch. The step in flight aborts
// without committing; the next step's Heads call opens the gate.
var ErrRPCSwitching = errors.New("RPC provider switch in progress")

// CatchingUpError is returned by Heads while the provider switched to is still below the highest
// head seen before the switch.
type CatchingUpError struct {
	Role   string
	Latest uint64
	Target uint64
}

func (e *CatchingUpError) Error() string {
	return fmt.Sprintf("%s RPC provider at block %d is catching up to block %d", e.Role, e.Latest, e.Target)
}

// FailoverConfig tunes WithFallback.
type FailoverConfig struct {
	// ChainID is re-checked on the provider switched to before every switch.
	ChainID uint64
	// AfterFailures is the number of consecutive provider failures that triggers a switch.
	AfterFailures uint64
	// ProbeInterval is how often Heads probes the primary while the fallback is active.
	ProbeInterval time.Duration
	// StartOnFallback makes the fallback active from the start (the primary was unreachable
	// at startup); Heads probes the primary as usual.
	StartOnFallback bool
	// Now is optional; tests set it.
	Now func() time.Time
}

type failover struct {
	config   FailoverConfig
	switchMu sync.Mutex // serializes switches, which make RPC calls

	mu     sync.Mutex
	active int
	// failures counts consecutive provider failures per JSON-RPC method on the active
	// provider, so an outage of one method (eth_getLogs) is not reset by another (heads).
	failures   map[string]uint64
	gated      bool
	gateHeight uint64
	maxLatest  uint64
	lastProbe  time.Time
}

// WithFallback combines two single-provider clients into one that fails over between them. The
// returned client owns both connections: close it, not primary or fallback. Both providers must
// already have passed the caller's startup checks (chain id, bytecode, finality tags).
func WithFallback(primary, fallback *Client, config FailoverConfig) (*Client, error) {
	if primary == nil || fallback == nil || len(primary.endpoints) != 1 || len(fallback.endpoints) != 1 {
		return nil, errors.New("failover needs two single-provider clients")
	}
	if config.ChainID == 0 || config.AfterFailures == 0 || config.ProbeInterval <= 0 {
		return nil, errors.New("failover chain id, failure threshold and probe interval must be positive")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	fallbackEndpoint := *fallback.endpoints[0]
	fallbackEndpoint.role = roleFallback
	state := &failover{config: config, failures: map[string]uint64{}}
	if config.StartOnFallback {
		state.active = 1
		state.lastProbe = config.Now()
	}
	return &Client{
		config:    primary.config,
		endpoints: []*endpoint{primary.endpoints[0], &fallbackEndpoint},
		failover:  state,
	}, nil
}

// activeEndpoint returns the provider calls go to. Unless heads is set, it refuses while a
// switch is gated.
func (c *Client) activeEndpoint(heads bool) (*endpoint, error) {
	f := c.failover
	if f == nil {
		return c.endpoints[0], nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gated && !heads {
		return nil, ErrRPCSwitching
	}
	return c.endpoints[f.active], nil
}

// IsProviderUnavailable reports whether err is a transient provider error worth retrying later
// (timeouts, network errors, HTTP 429/5xx, JSON-RPC -32000..-32099), as opposed to a capacity
// limit (handled by batching) or a cancellation.
func IsProviderUnavailable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, ErrRPCSwitching) || IsCapacityError(err) {
		return false
	}
	return isRetryable(err)
}

// requestErrorPatterns are -32000-range messages about the request rather than provider health:
// a revert, or a block a lagging backend does not have yet. They are worth retrying later but
// are no reason to switch providers.
var requestErrorPatterns = []string{"revert", "header not found", "block not found", "unknown block", "missing trie node", "not found", "invalid opcode", "out of gas", "insufficient funds"}

func isRequestRPCError(err error) bool {
	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) {
		return false
	}
	message := strings.ToLower(rpcErr.Error())
	for _, pattern := range requestErrorPatterns {
		if strings.Contains(message, pattern) {
			return true
		}
	}
	return false
}

// countsAsProviderFailure reports whether a call whose own context is still live failed because
// of the provider, not the request.
func countsAsProviderFailure(ctx context.Context, err error) bool {
	return ctx.Err() == nil && IsProviderUnavailable(err) && !isRequestRPCError(err)
}

// recordOutcome counts consecutive provider failures on the active provider and switches to the
// other provider once the threshold is reached.
func (c *Client) recordOutcome(ctx context.Context, method string, ep *endpoint, err error) {
	f := c.failover
	if f == nil {
		return
	}
	f.mu.Lock()
	if c.endpoints[f.active] != ep {
		f.mu.Unlock()
		return
	}
	if err == nil {
		delete(f.failures, method)
		f.mu.Unlock()
		return
	}
	if !countsAsProviderFailure(ctx, err) {
		f.mu.Unlock()
		return
	}
	f.failures[method]++
	reached := f.failures[method] >= f.config.AfterFailures
	target := 1 - f.active
	f.mu.Unlock()
	if reached {
		c.switchTo(ctx, method, target)
	}
}

// switchTo makes target the active provider after its chain id is confirmed, and gates every
// call until target reaches the highest head seen so far. A failed chain-id check keeps the
// current provider and resets the failure count.
func (c *Client) switchTo(ctx context.Context, method string, target int) {
	f := c.failover
	f.switchMu.Lock()
	defer f.switchMu.Unlock()
	f.mu.Lock()
	if f.active == target || f.failures[method] < f.config.AfterFailures {
		// Another caller already switched or the provider recovered.
		f.mu.Unlock()
		return
	}
	from := c.endpoints[f.active].role
	f.mu.Unlock()

	checkContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.config.Timeout)
	err := c.checkChainID(checkContext, c.endpoints[target])
	cancel()
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		clear(f.failures)
		// Transport errors carry the provider URL (an API key): log only the class.
		slog.Warn("RPC provider switch refused", "from", from, "to", c.endpoints[target].role, "failing_method", method, "reason", chainIDCheckReason(err))
		return
	}
	f.active = target
	clear(f.failures)
	f.gated = true
	f.gateHeight = f.maxLatest
	// The primary is probed one full interval after it was left, not at the next head read.
	f.lastProbe = f.config.Now()
	slog.Warn("RPC provider switched", "from", from, "to", c.endpoints[target].role, "failing_method", method, "wait_for_block", f.gateHeight)
}

var errChainIDMismatch = errors.New("RPC chain id does not match")

func (c *Client) checkChainID(ctx context.Context, ep *endpoint) error {
	chainID, err := ep.eth.ChainID(ctx)
	if err != nil {
		return fmt.Errorf("read %s chain id: %w", ep.role, err)
	}
	if !chainID.IsUint64() || chainID.Uint64() != c.failover.config.ChainID {
		return fmt.Errorf("%s %w %d: got %s", ep.role, errChainIDMismatch, c.failover.config.ChainID, chainID)
	}
	return nil
}

// chainIDCheckReason names a failed chain-id check without the error text.
func chainIDCheckReason(err error) string {
	if errors.Is(err, errChainIDMismatch) {
		return "chain_id_mismatch"
	}
	return rpcOutcome(err)
}

// failoverHeads is Heads with failover: it may take the primary back (only here, so a step never
// mixes providers), and it opens a switch gate once the new provider has caught up.
func (c *Client) failoverHeads(ctx context.Context) (Heads, error) {
	if heads, ok := c.probePrimary(ctx); ok {
		return heads, nil
	}
	ep, err := c.activeEndpoint(true)
	if err != nil {
		return Heads{}, err
	}
	heads, err := c.headsOn(ctx, ep)
	c.recordOutcome(ctx, "eth_getBlockByNumber", ep, err)
	if err != nil {
		return Heads{}, err
	}
	latest := heads.Latest.Number.Uint64()
	f := c.failover
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.endpoints[f.active] != ep {
		return Heads{}, ErrRPCSwitching
	}
	if f.gated {
		if latest < f.gateHeight {
			return Heads{}, &CatchingUpError{Role: ep.role, Latest: latest, Target: f.gateHeight}
		}
		f.gated = false
		slog.Info("RPC provider caught up", "role", ep.role, "block", latest)
	}
	f.maxLatest = max(f.maxLatest, latest)
	return heads, nil
}

// probePrimary checks the primary at most once per probe interval while the fallback is active.
// It switches back, and returns the primary's heads, when the primary reports the right chain id
// and is at or above the highest head seen.
func (c *Client) probePrimary(ctx context.Context) (Heads, bool) {
	f := c.failover
	f.mu.Lock()
	now := f.config.Now()
	if f.active == 0 || now.Sub(f.lastProbe) < f.config.ProbeInterval {
		f.mu.Unlock()
		return Heads{}, false
	}
	f.lastProbe = now
	f.mu.Unlock()

	// One timeout bounds the whole probe, so a hanging primary delays this head read by at most
	// that much once per probe interval.
	probeContext, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	primary := c.endpoints[0]
	if err := c.checkChainID(probeContext, primary); err != nil {
		return Heads{}, false
	}
	heads, err := c.headsOn(probeContext, primary)
	if err != nil {
		return Heads{}, false
	}
	latest := heads.Latest.Number.Uint64()

	f.switchMu.Lock()
	defer f.switchMu.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.active == 0 || latest < f.maxLatest {
		return Heads{}, false
	}
	f.active = 0
	clear(f.failures)
	f.gated = false
	f.maxLatest = latest
	slog.Warn("RPC provider switched", "from", roleFallback, "to", rolePrimary, "block", latest)
	return heads, true
}
