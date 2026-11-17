package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLoadRPCFallback(t *testing.T) {
	t.Parallel()
	const primary = "https://primary.example/key-primary"
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}

	disabled, err := LoadRPCFallback(env(nil), primary)
	if err != nil || disabled.Enabled() || disabled.AfterFailures != 3 || disabled.ProbeInterval != 30*time.Second {
		t.Fatalf("defaults = %+v, %v", disabled, err)
	}

	enabled, err := LoadRPCFallback(env(map[string]string{
		"RPC_FALLBACK_URL":            " https://fallback.example/key-fallback ",
		"RPC_FALLBACK_AFTER_FAILURES": "5",
		"RPC_FALLBACK_PROBE_INTERVAL": "1m",
	}), primary)
	if err != nil || !enabled.Enabled() || enabled.URL != "https://fallback.example/key-fallback" || enabled.AfterFailures != 5 || enabled.ProbeInterval != time.Minute {
		t.Fatalf("enabled = %+v, %v", enabled, err)
	}

	invalid := map[string]map[string]string{
		"RPC_FALLBACK_URL":            {"RPC_FALLBACK_URL": "ws://fallback.example/key-fallback"},
		"RPC_FALLBACK_AFTER_FAILURES": {"RPC_FALLBACK_AFTER_FAILURES": "0"},
		"RPC_FALLBACK_PROBE_INTERVAL": {"RPC_FALLBACK_PROBE_INTERVAL": "500ms"},
	}
	invalid["RPC_FALLBACK_AFTER_FAILURES"+" high"] = map[string]string{"RPC_FALLBACK_AFTER_FAILURES": "101"}
	invalid["RPC_FALLBACK_PROBE_INTERVAL"+" high"] = map[string]string{"RPC_FALLBACK_PROBE_INTERVAL": "2h"}
	invalid["RPC_FALLBACK_URL"+" same"] = map[string]string{"RPC_FALLBACK_URL": primary}
	for name, values := range invalid {
		_, err := LoadRPCFallback(env(values), primary)
		var fieldErr *FieldError
		if !errors.As(err, &fieldErr) || fieldErr.Field != strings.Fields(name)[0] {
			t.Errorf("%s: error = %v", name, err)
		}
		if err != nil && (strings.Contains(err.Error(), "key-fallback") || strings.Contains(err.Error(), "key-primary")) {
			t.Errorf("%s: error leaks a provider URL: %v", name, err)
		}
	}
}
