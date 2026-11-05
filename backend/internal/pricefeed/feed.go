package pricefeed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// Source is the provider name exposed by the API.
	Source = "coingecko"
	// AttributionURL is the link required by the provider's commercial terms
	// (docs/runbooks/eth-usd-enrichment.md, "Numeric and licensing rules").
	AttributionURL = "https://www.coingecko.com/en/api"

	defaultBaseURL  = "https://pro-api.coingecko.com/api/v3"
	defaultInterval = 60 * time.Second
	defaultMaxAge   = 120 * time.Second
	defaultTimeout  = 10 * time.Second
	maxBackoff      = 15 * time.Minute
	maxBodyBytes    = 64 << 10
	query           = "ids=ethereum&vs_currencies=usd&include_last_updated_at=true&precision=full"
)

// Options configures a Feed. Zero values select the documented defaults.
type Options struct {
	BaseURL  string       // default "https://pro-api.coingecko.com/api/v3"
	APIKey   string       // sent only in the x-cg-pro-api-key header
	Client   *http.Client // default timeout 10s
	Now      func() time.Time
	Interval time.Duration // default 60s
	MaxAge   time.Duration // default 120s
	// Jitter maps a delay to the delay actually used. The default applies +/-20%;
	// Feed never polls sooner than the base delay (jitter below it is clamped).
	Jitter func(time.Duration) time.Duration
	// Logger receives key-free provider errors from Run. Optional.
	Logger *slog.Logger
}

// Snapshot is the read-time view of the latest accepted sample.
type Snapshot struct {
	Price             *string // nil unless fresh at read time
	ProviderUpdatedAt *time.Time
	FetchedAt         *time.Time
}

type sample struct {
	price      string
	providerAt time.Time
	fetchedAt  time.Time
}

type call struct {
	done chan struct{}
	err  error
}

// Feed polls the provider and serves the latest fresh sample.
type Feed struct {
	baseURL  string
	apiKey   string
	client   *http.Client
	now      func() time.Time
	interval time.Duration
	maxAge   time.Duration
	jitter   func(time.Duration) time.Duration
	logger   *slog.Logger

	mu       sync.Mutex
	last     *sample
	failures int // consecutive 429 responses
	inflight *call
	waiters  int // callers currently waiting on the in-flight call
}

// New builds a Feed from o.
func New(o Options) *Feed {
	f := &Feed{
		baseURL:  strings.TrimRight(o.BaseURL, "/"),
		apiKey:   o.APIKey,
		client:   o.Client,
		now:      o.Now,
		interval: o.Interval,
		maxAge:   o.MaxAge,
		jitter:   o.Jitter,
		logger:   o.Logger,
	}
	if f.baseURL == "" {
		f.baseURL = defaultBaseURL
	}
	if f.client == nil {
		// Never follow redirects: the API key header must not reach another host.
		f.client = &http.Client{Timeout: defaultTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if f.now == nil {
		f.now = time.Now
	}
	if f.interval <= 0 {
		f.interval = defaultInterval
	}
	if f.maxAge <= 0 {
		f.maxAge = defaultMaxAge
	}
	if f.jitter == nil {
		f.jitter = defaultJitter
	}
	return f
}

func defaultJitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}

// Snapshot returns the latest sample; Price is nil when no sample exists or it
// is older than MaxAge at read time. Timestamps are kept for diagnostics.
func (f *Feed) Snapshot() Snapshot {
	f.mu.Lock()
	s := f.last
	f.mu.Unlock()
	if s == nil {
		return Snapshot{}
	}
	provider, fetched := s.providerAt, s.fetchedAt
	out := Snapshot{ProviderUpdatedAt: &provider, FetchedAt: &fetched}
	if f.now().Sub(s.providerAt) <= f.maxAge {
		price := s.price
		out.Price = &price
	}
	return out
}

// Refresh performs one provider call. Concurrent callers share a single
// in-flight request and receive its result.
func (f *Feed) Refresh(ctx context.Context) error {
	f.mu.Lock()
	if c := f.inflight; c != nil {
		f.waiters++
		f.mu.Unlock()
		defer func() { f.mu.Lock(); f.waiters--; f.mu.Unlock() }()
		select {
		case <-c.done:
			return c.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c := &call{done: make(chan struct{})}
	f.inflight = c
	f.mu.Unlock()

	c.err = f.fetch(ctx)

	f.mu.Lock()
	f.inflight = nil
	f.mu.Unlock()
	close(c.done)
	return c.err
}

// Run refreshes until ctx ends and returns nil on cancel.
func (f *Feed) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		// Failures keep the previous sample; Snapshot reports staleness.
		if err := f.Refresh(ctx); err != nil && ctx.Err() == nil && f.logger != nil {
			f.logger.Warn("eth/usd provider refresh failed", "error", err)
		}
		timer.Reset(f.nextDelay())
	}
}

// nextDelay is the jittered wait before the next poll: the base interval, or
// interval*2^(n-1) after n consecutive 429s, capped at 15 minutes.
func (f *Feed) nextDelay() time.Duration {
	f.mu.Lock()
	n := f.failures
	f.mu.Unlock()
	d := f.interval
	for i := 1; i < n && d < maxBackoff; i++ {
		d *= 2
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	// Jitter may only lengthen the wait: never poll sooner than the base delay.
	if j := f.jitter(d); j > d {
		return j
	}
	return d
}

type providerResponse struct {
	Ethereum *struct {
		USD           *json.Number `json:"usd"`
		LastUpdatedAt *int64       `json:"last_updated_at"`
	} `json:"ethereum"`
}

func (f *Feed) fetch(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/simple/price?"+query, nil)
	if err != nil {
		return errors.New("pricefeed: build request failed")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-cg-pro-api-key", f.apiKey)
	resp, err := f.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("pricefeed: provider request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		f.mu.Lock()
		f.failures++
		f.mu.Unlock()
		return errors.New("pricefeed: provider rate limited (429)")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("pricefeed: provider rejected credentials (%d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pricefeed: provider status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return errors.New("pricefeed: read response failed")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var parsed providerResponse
	if err := dec.Decode(&parsed); err != nil || parsed.Ethereum == nil || parsed.Ethereum.USD == nil || parsed.Ethereum.LastUpdatedAt == nil {
		return errors.New("pricefeed: malformed provider response")
	}
	price, err := ParsePrice(*parsed.Ethereum.USD)
	if err != nil {
		return err
	}
	now := f.now()
	providerAt := time.Unix(*parsed.Ethereum.LastUpdatedAt, 0).UTC()
	if *parsed.Ethereum.LastUpdatedAt <= 0 || providerAt.After(now) {
		return errors.New("pricefeed: provider timestamp missing or in the future")
	}
	f.mu.Lock()
	f.last = &sample{price: price, providerAt: providerAt, fetchedAt: now.UTC()}
	f.failures = 0
	f.mu.Unlock()
	return nil
}
