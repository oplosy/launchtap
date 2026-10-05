package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLogLevel          = "info"
	defaultAPIAddr           = ":8080"
	defaultIndexerHealthAddr = ":8081"
	defaultIndexerChunkSize  = uint64(100)
	maxIndexerChunkSize      = uint64(10000)
	// Keep these bounds aligned with the indexer engine's reorg-search policy.
	defaultIndexerReorgSearchDepth = uint64(128)
	maxIndexerReorgSearchDepth     = uint64(100000)
	defaultLogAddressBatch         = uint64(500)
	maxLogAddressBatch             = uint64(2000)
	defaultPollInterval            = time.Second
	defaultRPCTimeout              = 10 * time.Second
	defaultRPCMaxRetries           = uint64(3)
	defaultRPCRetryBackoff         = 250 * time.Millisecond
	defaultAPIRateLimitPerMinute   = uint64(600)
	maxAPIRateLimitPerMinute       = uint64(600000)
	defaultAPIRateLimitBurst       = uint64(120)
	maxAPIRateLimitBurst           = uint64(100000)
	defaultAPISSEMaxPerClient      = uint64(4)
	maxAPISSEMaxPerClient          = uint64(1000)
	minDatabaseMaxConns            = uint64(4)
	maxDatabaseMaxConns            = uint64(500)
)

var (
	ErrInvalid = errors.New("invalid configuration value")
	ErrMissing = errors.New("missing configuration value")

	deploymentIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)
)

// FieldError identifies a configuration field that failed validation.
type FieldError struct {
	Field string
	Err   error
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s: %v", e.Field, e.Err)
}

func (e *FieldError) Unwrap() error {
	return e.Err
}

// Config contains the shared backend process configuration. Environment values
// are trimmed before validation. IndexerConfirmations is nil when the local-only
// override is absent; deployment validation decides whether it is permitted.
type Config struct {
	ChainID              uint64   `env:"CHAIN_ID"`
	DeploymentID         string   `env:"DEPLOYMENT_ID"`
	RPCURL               string   `env:"RPC_URL"`
	DatabaseURL          string   `env:"DATABASE_URL"`
	PrivyAppID           string   `env:"PRIVY_APP_ID"`
	PrivyVerificationKey string   `env:"PRIVY_VERIFICATION_KEY"`
	LogLevel             string   `env:"LOG_LEVEL"`
	APIAddr              string   `env:"API_ADDR"`
	APIAllowedOrigins    []string `env:"API_ALLOWED_ORIGINS"`
	// APITrustedProxyCIDRs lists reverse proxies whose X-Forwarded-For entries identify the
	// client for rate limiting. Empty means the TCP peer address is the client.
	APITrustedProxyCIDRs  []netip.Prefix `env:"API_TRUSTED_PROXY_CIDRS"`
	APIRateLimitPerMinute uint64         `env:"API_RATE_LIMIT_PER_MINUTE"`
	APIRateLimitBurst     uint64         `env:"API_RATE_LIMIT_BURST"`
	APISSEMaxPerClient    uint64         `env:"API_SSE_MAX_PER_CLIENT"`
	// DatabaseMaxConns is zero when unset; the store then applies its own default.
	DatabaseMaxConns           uint64        `env:"DATABASE_MAX_CONNS"`
	IndexerHealthAddr          string        `env:"INDEXER_HEALTH_ADDR"`
	IndexerChunkSize           uint64        `env:"INDEXER_CHUNK_SIZE"`
	IndexerReorgSearchDepth    uint64        `env:"INDEXER_REORG_SEARCH_DEPTH"`
	IndexerReorgRecoveryMode   bool          `env:"INDEXER_REORG_RECOVERY_MODE"`
	IndexerLogAddressBatchSize uint64        `env:"INDEXER_LOG_ADDRESS_BATCH_SIZE"`
	IndexerPollInterval        time.Duration `env:"INDEXER_POLL_INTERVAL"`
	RPCTimeout                 time.Duration `env:"RPC_TIMEOUT"`
	RPCMaxRetries              uint64        `env:"RPC_MAX_RETRIES"`
	RPCRetryBackoff            time.Duration `env:"RPC_RETRY_BACKOFF"`
	IndexerWorkerID            string        `env:"INDEXER_WORKER_ID"`
	IndexerConfirmations       *uint64       `env:"INDEXER_CONFIRMATIONS"`
	ETHUSDSource               string        `env:"ETH_USD_SOURCE"`
}

// DatabaseConfig is the reduced configuration surface used by migration-only
// commands, which do not require chain or API settings.
type DatabaseConfig struct {
	DatabaseURL string
}

// Load parses shared API and indexer configuration using only the supplied
// environment lookup function. It performs syntactic validation and no I/O.
func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, &FieldError{Field: "getenv", Err: ErrMissing}
	}

	values := readEnvironment(getenv)

	chainID, err := requiredPositiveUint64("CHAIN_ID", values.chainID)
	if err != nil {
		return Config{}, err
	}
	addressBatchSize, err := optionalBoundedUint64(
		"INDEXER_LOG_ADDRESS_BATCH_SIZE", values.indexerLogAddressBatchSize,
		defaultLogAddressBatch, 1, maxLogAddressBatch,
	)
	if err != nil {
		return Config{}, err
	}
	pollInterval, err := optionalPositiveDuration("INDEXER_POLL_INTERVAL", values.indexerPollInterval, defaultPollInterval)
	if err != nil {
		return Config{}, err
	}
	rpcTimeout, err := optionalPositiveDuration("RPC_TIMEOUT", values.rpcTimeout, defaultRPCTimeout)
	if err != nil {
		return Config{}, err
	}
	rpcMaxRetries, err := optionalBoundedUint64("RPC_MAX_RETRIES", values.rpcMaxRetries, defaultRPCMaxRetries, 0, 20)
	if err != nil {
		return Config{}, err
	}
	rpcRetryBackoff, err := optionalPositiveDuration("RPC_RETRY_BACKOFF", values.rpcRetryBackoff, defaultRPCRetryBackoff)
	if err != nil {
		return Config{}, err
	}
	if !deploymentIDPattern.MatchString(values.deploymentID) {
		return Config{}, invalidOrMissing("DEPLOYMENT_ID", values.deploymentID)
	}
	if err := validateURL("RPC_URL", values.rpcURL, "http", "https"); err != nil {
		return Config{}, err
	}
	if err := validateURL("DATABASE_URL", values.databaseURL, "postgres", "postgresql"); err != nil {
		return Config{}, err
	}

	logLevel, err := parseLogLevel(values.logLevel)
	if err != nil {
		return Config{}, err
	}
	apiAddr, err := parseAPIAddr(values.apiAddr)
	if err != nil {
		return Config{}, err
	}
	apiOrigins, err := parseOrigins(values.apiAllowedOrigins)
	if err != nil {
		return Config{}, err
	}
	trustedProxies, err := parseTrustedProxies(values.apiTrustedProxyCIDRs)
	if err != nil {
		return Config{}, err
	}
	rateLimitPerMinute, err := optionalBoundedUint64("API_RATE_LIMIT_PER_MINUTE", values.apiRateLimitPerMinute, defaultAPIRateLimitPerMinute, 1, maxAPIRateLimitPerMinute)
	if err != nil {
		return Config{}, err
	}
	rateLimitBurst, err := optionalBoundedUint64("API_RATE_LIMIT_BURST", values.apiRateLimitBurst, defaultAPIRateLimitBurst, 1, maxAPIRateLimitBurst)
	if err != nil {
		return Config{}, err
	}
	sseMaxPerClient, err := optionalBoundedUint64("API_SSE_MAX_PER_CLIENT", values.apiSSEMaxPerClient, defaultAPISSEMaxPerClient, 1, maxAPISSEMaxPerClient)
	if err != nil {
		return Config{}, err
	}
	databaseMaxConns := uint64(0)
	if values.databaseMaxConns != "" {
		databaseMaxConns, err = optionalBoundedUint64("DATABASE_MAX_CONNS", values.databaseMaxConns, 0, minDatabaseMaxConns, maxDatabaseMaxConns)
		if err != nil {
			return Config{}, err
		}
	}
	indexerHealthAddr, err := parseListenerAddr("INDEXER_HEALTH_ADDR", values.indexerHealthAddr, defaultIndexerHealthAddr)
	if err != nil {
		return Config{}, err
	}
	chunkSize, err := optionalBoundedUint64(
		"INDEXER_CHUNK_SIZE",
		values.indexerChunkSize,
		defaultIndexerChunkSize,
		1,
		maxIndexerChunkSize,
	)
	if err != nil {
		return Config{}, err
	}
	reorgSearchDepth, err := optionalBoundedUint64(
		"INDEXER_REORG_SEARCH_DEPTH", values.indexerReorgSearchDepth,
		defaultIndexerReorgSearchDepth, 1, maxIndexerReorgSearchDepth,
	)
	if err != nil {
		return Config{}, err
	}
	reorgRecoveryMode, err := optionalRecoveryMode(values.indexerReorgRecoveryMode)
	if err != nil {
		return Config{}, err
	}
	if reorgSearchDepth > defaultIndexerReorgSearchDepth && !reorgRecoveryMode {
		return Config{}, &FieldError{Field: "INDEXER_REORG_RECOVERY_MODE", Err: ErrInvalid}
	}
	if reorgRecoveryMode && reorgSearchDepth <= defaultIndexerReorgSearchDepth {
		return Config{}, &FieldError{Field: "INDEXER_REORG_SEARCH_DEPTH", Err: ErrInvalid}
	}
	confirmations, err := optionalUint64("INDEXER_CONFIRMATIONS", values.indexerConfirmations)
	if err != nil {
		return Config{}, err
	}

	return Config{
		ChainID:                    chainID,
		DeploymentID:               values.deploymentID,
		RPCURL:                     values.rpcURL,
		DatabaseURL:                values.databaseURL,
		PrivyAppID:                 values.privyAppID,
		PrivyVerificationKey:       values.privyVerificationKey,
		LogLevel:                   logLevel,
		APIAddr:                    apiAddr,
		APIAllowedOrigins:          apiOrigins,
		APITrustedProxyCIDRs:       trustedProxies,
		APIRateLimitPerMinute:      rateLimitPerMinute,
		APIRateLimitBurst:          rateLimitBurst,
		APISSEMaxPerClient:         sseMaxPerClient,
		DatabaseMaxConns:           databaseMaxConns,
		IndexerHealthAddr:          indexerHealthAddr,
		IndexerChunkSize:           chunkSize,
		IndexerReorgSearchDepth:    reorgSearchDepth,
		IndexerReorgRecoveryMode:   reorgRecoveryMode,
		IndexerLogAddressBatchSize: addressBatchSize,
		IndexerPollInterval:        pollInterval,
		RPCTimeout:                 rpcTimeout,
		RPCMaxRetries:              rpcMaxRetries,
		RPCRetryBackoff:            rpcRetryBackoff,
		IndexerWorkerID:            values.indexerWorkerID,
		IndexerConfirmations:       confirmations,
		ETHUSDSource:               values.ethUSDSource,
	}, nil
}

// LoadDatabase parses only the configuration required by migration commands.
func LoadDatabase(getenv func(string) string) (DatabaseConfig, error) {
	if getenv == nil {
		return DatabaseConfig{}, &FieldError{Field: "getenv", Err: ErrMissing}
	}

	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
	if err := validateURL("DATABASE_URL", databaseURL, "postgres", "postgresql"); err != nil {
		return DatabaseConfig{}, err
	}

	return DatabaseConfig{DatabaseURL: databaseURL}, nil
}

// RequireAPI validates settings required only by the API process.
func (c Config) RequireAPI() error {
	if strings.TrimSpace(c.PrivyAppID) == "" {
		return &FieldError{Field: "PRIVY_APP_ID", Err: ErrMissing}
	}
	if strings.TrimSpace(c.PrivyVerificationKey) == "" {
		return &FieldError{Field: "PRIVY_VERIFICATION_KEY", Err: ErrMissing}
	}

	return nil
}

// RequireIndexer validates settings that identify a running indexer worker.
func (c Config) RequireIndexer() error {
	if strings.TrimSpace(c.IndexerWorkerID) == "" {
		return &FieldError{Field: "INDEXER_WORKER_ID", Err: ErrMissing}
	}
	return nil
}

type environmentValues struct {
	chainID                    string
	deploymentID               string
	rpcURL                     string
	databaseURL                string
	privyAppID                 string
	privyVerificationKey       string
	logLevel                   string
	apiAddr                    string
	apiAllowedOrigins          string
	apiTrustedProxyCIDRs       string
	apiRateLimitPerMinute      string
	apiRateLimitBurst          string
	apiSSEMaxPerClient         string
	databaseMaxConns           string
	indexerHealthAddr          string
	indexerChunkSize           string
	indexerReorgSearchDepth    string
	indexerReorgRecoveryMode   string
	indexerLogAddressBatchSize string
	indexerPollInterval        string
	rpcTimeout                 string
	rpcMaxRetries              string
	rpcRetryBackoff            string
	indexerWorkerID            string
	indexerConfirmations       string
	ethUSDSource               string
}

func readEnvironment(getenv func(string) string) environmentValues {
	return environmentValues{
		chainID:                    strings.TrimSpace(getenv("CHAIN_ID")),
		deploymentID:               strings.TrimSpace(getenv("DEPLOYMENT_ID")),
		rpcURL:                     strings.TrimSpace(getenv("RPC_URL")),
		databaseURL:                strings.TrimSpace(getenv("DATABASE_URL")),
		privyAppID:                 strings.TrimSpace(getenv("PRIVY_APP_ID")),
		privyVerificationKey:       strings.TrimSpace(getenv("PRIVY_VERIFICATION_KEY")),
		logLevel:                   strings.TrimSpace(getenv("LOG_LEVEL")),
		apiAddr:                    strings.TrimSpace(getenv("API_ADDR")),
		apiAllowedOrigins:          strings.TrimSpace(getenv("API_ALLOWED_ORIGINS")),
		apiTrustedProxyCIDRs:       strings.TrimSpace(getenv("API_TRUSTED_PROXY_CIDRS")),
		apiRateLimitPerMinute:      strings.TrimSpace(getenv("API_RATE_LIMIT_PER_MINUTE")),
		apiRateLimitBurst:          strings.TrimSpace(getenv("API_RATE_LIMIT_BURST")),
		apiSSEMaxPerClient:         strings.TrimSpace(getenv("API_SSE_MAX_PER_CLIENT")),
		databaseMaxConns:           strings.TrimSpace(getenv("DATABASE_MAX_CONNS")),
		indexerHealthAddr:          strings.TrimSpace(getenv("INDEXER_HEALTH_ADDR")),
		indexerChunkSize:           strings.TrimSpace(getenv("INDEXER_CHUNK_SIZE")),
		indexerReorgSearchDepth:    strings.TrimSpace(getenv("INDEXER_REORG_SEARCH_DEPTH")),
		indexerReorgRecoveryMode:   strings.TrimSpace(getenv("INDEXER_REORG_RECOVERY_MODE")),
		indexerLogAddressBatchSize: strings.TrimSpace(getenv("INDEXER_LOG_ADDRESS_BATCH_SIZE")),
		indexerPollInterval:        strings.TrimSpace(getenv("INDEXER_POLL_INTERVAL")),
		rpcTimeout:                 strings.TrimSpace(getenv("RPC_TIMEOUT")),
		rpcMaxRetries:              strings.TrimSpace(getenv("RPC_MAX_RETRIES")),
		rpcRetryBackoff:            strings.TrimSpace(getenv("RPC_RETRY_BACKOFF")),
		indexerWorkerID:            strings.TrimSpace(getenv("INDEXER_WORKER_ID")),
		indexerConfirmations:       strings.TrimSpace(getenv("INDEXER_CONFIRMATIONS")),
		ethUSDSource:               strings.TrimSpace(getenv("ETH_USD_SOURCE")),
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
