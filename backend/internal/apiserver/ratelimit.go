package apiserver

import (
	"context"
	"math"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	clientKeyContextKey  ctxKey = "client_key"
	maxRateLimiterKeys          = 100_000
	rateLimiterIdleAfter        = 10 * time.Minute
)

// ClientKey returns the rate-limit identity resolved by the middleware for this request.
func ClientKey(ctx context.Context) string { v, _ := ctx.Value(clientKeyContextKey).(string); return v }

// clientAddress resolves the client IP. X-Forwarded-For is only honored when the TCP peer is a
// trusted proxy, and it is walked right to left so a client cannot spoof its own entry.
func clientAddress(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	address := peer.Addr().Unmap()
	if !trustedAddress(address, trusted) {
		return address
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for index := len(hops) - 1; index >= 0; index-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[index]))
		if err != nil {
			return address
		}
		address = hop.Unmap()
		if !trustedAddress(address, trusted) {
			return address
		}
	}
	return address
}

func trustedAddress(address netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// clientKeyFor groups IPv6 clients by /64, the smallest block an end user normally controls.
func clientKeyFor(address netip.Addr) string {
	if !address.IsValid() {
		return "unknown"
	}
	if address.Is6() {
		prefix, err := address.Prefix(64)
		if err == nil {
			return prefix.String()
		}
	}
	return address.String()
}

type tokenBucket struct {
	tokens float64
	seen   time.Time
}

// RateLimiter is an in-memory per-client token bucket. It protects a single API process; a
// horizontally scaled deployment also needs an edge or shared limiter.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]tokenBucket
	rate    float64
	burst   float64
}

func NewRateLimiter(perMinute, burst uint64) *RateLimiter {
	if perMinute == 0 || burst == 0 {
		return nil
	}
	return &RateLimiter{buckets: make(map[string]tokenBucket), rate: float64(perMinute) / 60, burst: float64(burst)}
}

// Allow consumes one token and otherwise returns the wait until the next token.
func (l *RateLimiter) Allow(key string, now time.Time) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, exists := l.buckets[key]
	if !exists {
		if len(l.buckets) >= maxRateLimiterKeys {
			l.evictIdle(now)
		}
		bucket = tokenBucket{tokens: l.burst, seen: now}
	} else if elapsed := now.Sub(bucket.seen).Seconds(); elapsed > 0 {
		bucket.tokens = math.Min(l.burst, bucket.tokens+elapsed*l.rate)
		bucket.seen = now
	}
	if bucket.tokens < 1 {
		l.buckets[key] = bucket
		return false, time.Duration((1 - bucket.tokens) / l.rate * float64(time.Second))
	}
	bucket.tokens--
	l.buckets[key] = bucket
	return true, 0
}

func (l *RateLimiter) evictIdle(now time.Time) {
	for key, bucket := range l.buckets {
		if now.Sub(bucket.seen) >= rateLimiterIdleAfter {
			delete(l.buckets, key)
		}
	}
	if len(l.buckets) < maxRateLimiterKeys {
		return
	}
	// Under a key-flooding attack, forget everyone rather than grow without bound.
	clear(l.buckets)
}

// StreamLimiter caps concurrent long-lived streams per client so one client cannot exhaust
// the global SSE subscriber capacity.
type StreamLimiter struct {
	mu      sync.Mutex
	active  map[string]int
	maximum int
}

func NewStreamLimiter(maximum int) *StreamLimiter {
	if maximum <= 0 {
		return nil
	}
	return &StreamLimiter{active: make(map[string]int), maximum: maximum}
}

// Acquire returns a release function, or false when the client is already at its cap.
func (l *StreamLimiter) Acquire(key string) (func(), bool) {
	if l == nil {
		return func() {}, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active[key] >= l.maximum {
		return nil, false
	}
	l.active[key]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.active[key]--; l.active[key] <= 0 {
				delete(l.active, key)
			}
		})
	}, true
}
