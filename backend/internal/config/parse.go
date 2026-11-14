package config

import (
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func parseETHUSDSource(source, apiKey string) (string, string, error) {
	switch source {
	case "":
		if apiKey != "" {
			return "", "", &FieldError{Field: "COINGECKO_API_KEY", Err: ErrInvalid}
		}
		return "", "", nil
	case ETHUSDSourceCoinGecko:
		if apiKey == "" {
			return "", "", &FieldError{Field: "COINGECKO_API_KEY", Err: ErrMissing}
		}
		return source, apiKey, nil
	default:
		return "", "", &FieldError{Field: "ETH_USD_SOURCE", Err: ErrInvalid}
	}
}

// parseCoinGeckoPlan defaults an enabled CoinGecko source to the Pro plan and rejects a plan
// set while USD pricing is disabled, so a stray value cannot hide a misconfiguration.
func parseCoinGeckoPlan(source, plan string) (string, error) {
	if source == "" {
		if plan != "" {
			return "", &FieldError{Field: "COINGECKO_API_PLAN", Err: ErrInvalid}
		}
		return "", nil
	}
	switch plan {
	case "":
		return CoinGeckoPlanPro, nil
	case CoinGeckoPlanPro, CoinGeckoPlanDemo:
		return plan, nil
	default:
		return "", &FieldError{Field: "COINGECKO_API_PLAN", Err: ErrInvalid}
	}
}

func parseOrigins(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin == "" || origin == "*" {
			return nil, &FieldError{Field: "API_ALLOWED_ORIGINS", Err: ErrInvalid}
		}
		u, err := url.Parse(origin)
		if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, &FieldError{Field: "API_ALLOWED_ORIGINS", Err: ErrInvalid}
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, &FieldError{Field: "API_ALLOWED_ORIGINS", Err: ErrInvalid}
		}
		if _, ok := seen[origin]; ok {
			continue
		}
		seen[origin] = struct{}{}
		out = append(out, origin)
	}
	return out, nil
}

func parseTrustedProxies(value string) ([]netip.Prefix, error) {
	if value == "" {
		return nil, nil
	}
	var out []netip.Prefix
	for _, part := range strings.Split(value, ",") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return nil, &FieldError{Field: "API_TRUSTED_PROXY_CIDRS", Err: ErrInvalid}
		}
		out = append(out, prefix.Masked())
	}
	return out, nil
}

func optionalPositiveDuration(field, value string, defaultValue time.Duration) (time.Duration, error) {
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, &FieldError{Field: field, Err: ErrInvalid}
	}
	return parsed, nil
}

// optionalDisableableDuration accepts 0 (disabled) or a duration within [minValue, maxValue].
func optionalDisableableDuration(field, value string, defaultValue, minValue, maxValue time.Duration) (time.Duration, error) {
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || (parsed != 0 && (parsed < minValue || parsed > maxValue)) {
		return 0, &FieldError{Field: field, Err: ErrInvalid}
	}
	return parsed, nil
}

func requiredPositiveUint64(field, value string) (uint64, error) {
	if value == "" {
		return 0, &FieldError{Field: field, Err: ErrMissing}
	}

	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return 0, &FieldError{Field: field, Err: ErrInvalid}
	}

	return parsed, nil
}

func optionalUint64(field, value string) (*uint64, error) {
	if value == "" {
		return nil, nil
	}

	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return nil, &FieldError{Field: field, Err: ErrInvalid}
	}

	return &parsed, nil
}

func optionalBoundedUint64(field, value string, defaultValue, minValue, maxValue uint64) (uint64, error) {
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed < minValue || parsed > maxValue {
		return 0, &FieldError{Field: field, Err: ErrInvalid}
	}

	return parsed, nil
}

func optionalRecoveryMode(value string) (bool, error) {
	switch value {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, &FieldError{Field: "INDEXER_REORG_RECOVERY_MODE", Err: ErrInvalid}
	}
}

func validateURL(field, value string, allowedSchemes ...string) error {
	if value == "" {
		return &FieldError{Field: field, Err: ErrMissing}
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return &FieldError{Field: field, Err: ErrInvalid}
	}

	for _, scheme := range allowedSchemes {
		if parsed.Scheme == scheme {
			return nil
		}
	}

	return &FieldError{Field: field, Err: ErrInvalid}
}

func parseLogLevel(value string) (string, error) {
	if value == "" {
		return defaultLogLevel, nil
	}

	switch value {
	case "debug", "info", "warn", "error":
		return value, nil
	default:
		return "", &FieldError{Field: "LOG_LEVEL", Err: ErrInvalid}
	}
}

func parseAPIAddr(value string) (string, error) {
	return parseListenerAddr("API_ADDR", value, defaultAPIAddr)
}

// parseAPIMetricsAddr validates the optional metrics listener. Empty disables it;
// a value equal to the API listener is rejected so metrics never share the
// public port.
func parseAPIMetricsAddr(value, apiAddr string) (string, error) {
	if value == "" {
		return "", nil
	}
	addr, err := parseListenerAddr("API_METRICS_ADDR", value, "")
	if err != nil {
		return "", err
	}
	if addr == apiAddr {
		return "", &FieldError{Field: "API_METRICS_ADDR", Err: ErrInvalid}
	}

	return addr, nil
}

func parseListenerAddr(field, value, defaultValue string) (string, error) {
	if value == "" {
		value = defaultValue
	}

	// API processes need a stable numeric listener. Ephemeral port zero and
	// service names such as "http" are intentionally rejected.
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return "", &FieldError{Field: field, Err: ErrInvalid}
	}
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		return "", &FieldError{Field: field, Err: ErrInvalid}
	}

	return value, nil
}

func invalidOrMissing(field, value string) error {
	if value == "" {
		return &FieldError{Field: field, Err: ErrMissing}
	}

	return &FieldError{Field: field, Err: ErrInvalid}
}
