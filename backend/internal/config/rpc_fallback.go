package config

import (
	"strings"
	"time"
)

const (
	defaultRPCFallbackAfterFailures = uint64(3)
	maxRPCFallbackAfterFailures     = uint64(100)
	defaultRPCFallbackProbeInterval = 30 * time.Second
	minRPCFallbackProbeInterval     = time.Second
	maxRPCFallbackProbeInterval     = time.Hour
)

// RPCFallbackConfig is the indexer's optional second RPC provider
// (docs/specs/2026-11-17-rpc-failover-preflight.md). URL is a secret (provider URLs carry API
// keys) and never appears in error text. An empty URL disables failover.
type RPCFallbackConfig struct {
	URL           string        `env:"RPC_FALLBACK_URL"`
	AfterFailures uint64        `env:"RPC_FALLBACK_AFTER_FAILURES"`
	ProbeInterval time.Duration `env:"RPC_FALLBACK_PROBE_INTERVAL"`
}

// Enabled reports whether a fallback provider is configured.
func (c RPCFallbackConfig) Enabled() bool { return c.URL != "" }

// LoadRPCFallback parses the fallback settings; primaryURL is the already-validated RPC_URL. It
// performs no I/O.
func LoadRPCFallback(getenv func(string) string, primaryURL string) (RPCFallbackConfig, error) {
	if getenv == nil {
		return RPCFallbackConfig{}, &FieldError{Field: "getenv", Err: ErrMissing}
	}
	get := func(k string) string { return strings.TrimSpace(getenv(k)) }
	afterFailures, err := optionalBoundedUint64("RPC_FALLBACK_AFTER_FAILURES", get("RPC_FALLBACK_AFTER_FAILURES"), defaultRPCFallbackAfterFailures, 1, maxRPCFallbackAfterFailures)
	if err != nil {
		return RPCFallbackConfig{}, err
	}
	probeInterval, err := optionalPositiveDuration("RPC_FALLBACK_PROBE_INTERVAL", get("RPC_FALLBACK_PROBE_INTERVAL"), defaultRPCFallbackProbeInterval)
	if err != nil {
		return RPCFallbackConfig{}, err
	}
	if probeInterval < minRPCFallbackProbeInterval || probeInterval > maxRPCFallbackProbeInterval {
		return RPCFallbackConfig{}, &FieldError{Field: "RPC_FALLBACK_PROBE_INTERVAL", Err: ErrInvalid}
	}
	fallbackURL := get("RPC_FALLBACK_URL")
	if fallbackURL == "" {
		return RPCFallbackConfig{AfterFailures: afterFailures, ProbeInterval: probeInterval}, nil
	}
	if err := validateURL("RPC_FALLBACK_URL", fallbackURL, "http", "https"); err != nil {
		return RPCFallbackConfig{}, err
	}
	if fallbackURL == strings.TrimSpace(primaryURL) {
		return RPCFallbackConfig{}, &FieldError{Field: "RPC_FALLBACK_URL", Err: ErrInvalid}
	}
	return RPCFallbackConfig{URL: fallbackURL, AfterFailures: afterFailures, ProbeInterval: probeInterval}, nil
}
