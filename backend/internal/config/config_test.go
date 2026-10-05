package config

import (
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadParsesCompleteConfiguration(t *testing.T) {
	t.Parallel()

	env := validEnvironment()
	env["PRIVY_APP_ID"] = " app-id "
	env["PRIVY_VERIFICATION_KEY"] = " verification-key "
	env["LOG_LEVEL"] = "debug"
	env["API_ADDR"] = "127.0.0.1:9090"
	env["INDEXER_HEALTH_ADDR"] = "127.0.0.1:9091"
	env["INDEXER_CHUNK_SIZE"] = "2500"
	env["INDEXER_REORG_SEARCH_DEPTH"] = "512"
	env["INDEXER_REORG_RECOVERY_MODE"] = "true"
	env["INDEXER_LOG_ADDRESS_BATCH_SIZE"] = "750"
	env["INDEXER_POLL_INTERVAL"] = "2s"
	env["RPC_TIMEOUT"] = "15s"
	env["RPC_MAX_RETRIES"] = "5"
	env["RPC_RETRY_BACKOFF"] = "400ms"
	env["INDEXER_WORKER_ID"] = " worker-a "
	env["INDEXER_CONFIRMATIONS"] = "12"
	env["ETH_USD_SOURCE"] = " unconfigured-source "
	env["API_TRUSTED_PROXY_CIDRS"] = "172.16.0.0/12, 10.1.2.3/8"
	env["API_RATE_LIMIT_PER_MINUTE"] = "1200"
	env["API_RATE_LIMIT_BURST"] = "200"
	env["API_SSE_MAX_PER_CLIENT"] = "8"
	env["DATABASE_MAX_CONNS"] = "24"

	got, err := Load(mapGetenv(env))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	wantConfirmations := uint64(12)
	want := Config{
		ChainID:                    4663,
		DeploymentID:               "robinhood-mainnet-v1",
		RPCURL:                     "https://rpc.example.test/v2/key",
		DatabaseURL:                "postgresql://user:pass@db.example.test:5432/launchpad",
		PrivyAppID:                 "app-id",
		PrivyVerificationKey:       "verification-key",
		LogLevel:                   "debug",
		APIAddr:                    "127.0.0.1:9090",
		IndexerHealthAddr:          "127.0.0.1:9091",
		IndexerChunkSize:           2500,
		IndexerReorgSearchDepth:    512,
		IndexerReorgRecoveryMode:   true,
		IndexerLogAddressBatchSize: 750,
		IndexerPollInterval:        2 * time.Second,
		RPCTimeout:                 15 * time.Second,
		RPCMaxRetries:              5,
		RPCRetryBackoff:            400 * time.Millisecond,
		IndexerWorkerID:            "worker-a",
		IndexerConfirmations:       &wantConfirmations,
		ETHUSDSource:               "unconfigured-source",
		APITrustedProxyCIDRs:       []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("10.0.0.0/8")},
		APIRateLimitPerMinute:      1200,
		APIRateLimitBurst:          200,
		APISSEMaxPerClient:         8,
		DatabaseMaxConns:           24,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}

func TestConfigEnvironmentMapping(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"ChainID":                    "CHAIN_ID",
		"DeploymentID":               "DEPLOYMENT_ID",
		"RPCURL":                     "RPC_URL",
		"DatabaseURL":                "DATABASE_URL",
		"PrivyAppID":                 "PRIVY_APP_ID",
		"PrivyVerificationKey":       "PRIVY_VERIFICATION_KEY",
		"LogLevel":                   "LOG_LEVEL",
		"APIAddr":                    "API_ADDR",
		"APIAllowedOrigins":          "API_ALLOWED_ORIGINS",
		"APITrustedProxyCIDRs":       "API_TRUSTED_PROXY_CIDRS",
		"APIRateLimitPerMinute":      "API_RATE_LIMIT_PER_MINUTE",
		"APIRateLimitBurst":          "API_RATE_LIMIT_BURST",
		"APISSEMaxPerClient":         "API_SSE_MAX_PER_CLIENT",
		"DatabaseMaxConns":           "DATABASE_MAX_CONNS",
		"IndexerHealthAddr":          "INDEXER_HEALTH_ADDR",
		"IndexerChunkSize":           "INDEXER_CHUNK_SIZE",
		"IndexerReorgSearchDepth":    "INDEXER_REORG_SEARCH_DEPTH",
		"IndexerReorgRecoveryMode":   "INDEXER_REORG_RECOVERY_MODE",
		"IndexerLogAddressBatchSize": "INDEXER_LOG_ADDRESS_BATCH_SIZE",
		"IndexerPollInterval":        "INDEXER_POLL_INTERVAL",
		"RPCTimeout":                 "RPC_TIMEOUT",
		"RPCMaxRetries":              "RPC_MAX_RETRIES",
		"RPCRetryBackoff":            "RPC_RETRY_BACKOFF",
		"IndexerWorkerID":            "INDEXER_WORKER_ID",
		"IndexerConfirmations":       "INDEXER_CONFIRMATIONS",
		"ETHUSDSource":               "ETH_USD_SOURCE",
	}

	typeOfConfig := reflect.TypeFor[Config]()
	if typeOfConfig.NumField() != len(want) {
		t.Fatalf("Config fields = %d, want %d", typeOfConfig.NumField(), len(want))
	}
	for index := range typeOfConfig.NumField() {
		field := typeOfConfig.Field(index)
		if got := field.Tag.Get("env"); got != want[field.Name] {
			t.Errorf("Config.%s env tag = %q, want %q", field.Name, got, want[field.Name])
		}
	}
}

func TestAPIAllowedOrigins(t *testing.T) {
	env := validEnvironment()
	env["API_ALLOWED_ORIGINS"] = "https://app.example,http://localhost:3000"
	got, err := Load(mapGetenv(env))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.APIAllowedOrigins) != 2 {
		t.Fatalf("origins=%v", got.APIAllowedOrigins)
	}
	env["API_ALLOWED_ORIGINS"] = "*"
	if _, err := Load(mapGetenv(env)); err == nil {
		t.Fatal("wildcard origin accepted")
	}
}

func TestLoadUsesBoundedDefaults(t *testing.T) {
	t.Parallel()

	got, err := Load(mapGetenv(validEnvironment()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", got.LogLevel)
	}
	if got.APIAddr != ":8080" {
		t.Errorf("APIAddr = %q, want :8080", got.APIAddr)
	}
	if got.IndexerChunkSize != 100 {
		t.Errorf("IndexerChunkSize = %d, want 100", got.IndexerChunkSize)
	}
	if got.IndexerReorgSearchDepth != 128 || got.IndexerReorgRecoveryMode {
		t.Errorf("reorg search settings = %d/%v, want 128/false", got.IndexerReorgSearchDepth, got.IndexerReorgRecoveryMode)
	}
	if got.IndexerLogAddressBatchSize != 500 {
		t.Errorf("IndexerLogAddressBatchSize = %d, want 500", got.IndexerLogAddressBatchSize)
	}
	if got.IndexerPollInterval != time.Second {
		t.Errorf("IndexerPollInterval = %v, want 1s", got.IndexerPollInterval)
	}
	if got.RPCTimeout != 10*time.Second {
		t.Errorf("RPCTimeout = %v, want 10s", got.RPCTimeout)
	}
	if got.RPCMaxRetries != 3 {
		t.Errorf("RPCMaxRetries = %d, want 3", got.RPCMaxRetries)
	}
	if got.RPCRetryBackoff != 250*time.Millisecond {
		t.Errorf("RPCRetryBackoff = %v, want 250ms", got.RPCRetryBackoff)
	}
	if got.IndexerConfirmations != nil {
		t.Errorf("IndexerConfirmations = %v, want nil", got.IndexerConfirmations)
	}
}

func TestLoadIsDeterministicAndReadsEachFieldOnce(t *testing.T) {
	t.Parallel()

	env := validEnvironment()
	counts := make(map[string]int)
	getenv := func(key string) string {
		counts[key]++
		return env[key]
	}

	first, err := Load(getenv)
	if err != nil {
		t.Fatalf("first Load() error = %v", err)
	}
	second, err := Load(mapGetenv(env))
	if err != nil {
		t.Fatalf("second Load() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Load() results differ: %#v != %#v", first, second)
	}
	for key, count := range counts {
		if count != 1 {
			t.Errorf("getenv(%q) called %d times, want 1", key, count)
		}
	}
}

func TestLoadRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"CHAIN_ID", "DEPLOYMENT_ID", "RPC_URL", "DATABASE_URL"} {
		field := field
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			env := validEnvironment()
			env[field] = " \t "

			assertFieldError(t, Load, env, field, ErrMissing)
		})
	}
}

func TestLoadRejectsInvalidUniversalFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field string
		value string
	}{
		{name: "zero chain id", field: "CHAIN_ID", value: "0"},
		{name: "negative chain id", field: "CHAIN_ID", value: "-1"},
		{name: "overflowing chain id", field: "CHAIN_ID", value: "18446744073709551616"},
		{name: "non-decimal chain id", field: "CHAIN_ID", value: "0x1237"},
		{name: "uppercase deployment id", field: "DEPLOYMENT_ID", value: "Robinhood-mainnet"},
		{name: "leading deployment hyphen", field: "DEPLOYMENT_ID", value: "-mainnet"},
		{name: "short deployment id", field: "DEPLOYMENT_ID", value: "ab"},
		{name: "long deployment id", field: "DEPLOYMENT_ID", value: "a" + strings.Repeat("b", 64)},
		{name: "rpc scheme", field: "RPC_URL", value: "ws://rpc.example.test"},
		{name: "rpc host", field: "RPC_URL", value: "https:///missing-host"},
		{name: "database scheme", field: "DATABASE_URL", value: "mysql://db.example.test/launchpad"},
		{name: "database host", field: "DATABASE_URL", value: "postgres:///launchpad"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			env := validEnvironment()
			env[test.field] = test.value

			assertFieldError(t, Load, env, test.field, ErrInvalid)
		})
	}
}

func TestLoadAcceptsCanonicalDeploymentIDs(t *testing.T) {
	t.Parallel()

	for _, deploymentID := range []string{
		"abc",
		"release-v1",
		"release_v1",
		"release.v1",
		"a" + strings.Repeat("b", 63),
	} {
		deploymentID := deploymentID
		t.Run(deploymentID, func(t *testing.T) {
			t.Parallel()
			env := validEnvironment()
			env["DEPLOYMENT_ID"] = deploymentID

			got, err := Load(mapGetenv(env))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.DeploymentID != deploymentID {
				t.Fatalf("DeploymentID = %q, want %q", got.DeploymentID, deploymentID)
			}
		})
	}
}

func TestLoadRejectsInvalidBoundedSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field string
		value string
	}{
		{name: "unknown log level", field: "LOG_LEVEL", value: "trace"},
		{name: "uppercase log level", field: "LOG_LEVEL", value: "INFO"},
		{name: "address without port", field: "API_ADDR", value: "localhost"},
		{name: "zero port", field: "API_ADDR", value: ":0"},
		{name: "port overflow", field: "API_ADDR", value: ":65536"},
		{name: "indexer health address without port", field: "INDEXER_HEALTH_ADDR", value: "localhost"},
		{name: "indexer health zero port", field: "INDEXER_HEALTH_ADDR", value: ":0"},
		{name: "zero chunk", field: "INDEXER_CHUNK_SIZE", value: "0"},
		{name: "chunk above provisional maximum", field: "INDEXER_CHUNK_SIZE", value: "10001"},
		{name: "zero reorg search depth", field: "INDEXER_REORG_SEARCH_DEPTH", value: "0"},
		{name: "reorg search depth above maximum", field: "INDEXER_REORG_SEARCH_DEPTH", value: "100001"},
		{name: "invalid reorg recovery mode", field: "INDEXER_REORG_RECOVERY_MODE", value: "yes"},
		{name: "zero address batch", field: "INDEXER_LOG_ADDRESS_BATCH_SIZE", value: "0"},
		{name: "address batch above provider maximum", field: "INDEXER_LOG_ADDRESS_BATCH_SIZE", value: "2001"},
		{name: "zero poll interval", field: "INDEXER_POLL_INTERVAL", value: "0s"},
		{name: "invalid RPC timeout", field: "RPC_TIMEOUT", value: "soon"},
		{name: "too many retries", field: "RPC_MAX_RETRIES", value: "21"},
		{name: "negative retry backoff", field: "RPC_RETRY_BACKOFF", value: "-1s"},
		{name: "negative confirmations", field: "INDEXER_CONFIRMATIONS", value: "-1"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			env := validEnvironment()
			env[test.field] = test.value

			assertFieldError(t, Load, env, test.field, ErrInvalid)
		})
	}
}

func TestLoadRequiresExplicitModeForExpandedReorgSearch(t *testing.T) {
	t.Parallel()

	env := validEnvironment()
	env["INDEXER_REORG_SEARCH_DEPTH"] = "129"
	assertFieldError(t, Load, env, "INDEXER_REORG_RECOVERY_MODE", ErrInvalid)

	env["INDEXER_REORG_RECOVERY_MODE"] = "true"
	got, err := Load(mapGetenv(env))
	if err != nil {
		t.Fatalf("Load() with acknowledged expanded search: %v", err)
	}
	if got.IndexerReorgSearchDepth != 129 || !got.IndexerReorgRecoveryMode {
		t.Fatalf("expanded reorg settings = %d/%v, want 129/true", got.IndexerReorgSearchDepth, got.IndexerReorgRecoveryMode)
	}

	env["INDEXER_REORG_SEARCH_DEPTH"] = "128"
	assertFieldError(t, Load, env, "INDEXER_REORG_SEARCH_DEPTH", ErrInvalid)
}

func TestLoadAcceptsChunkAndConfirmationBoundaries(t *testing.T) {
	t.Parallel()

	for _, chunkSize := range []string{"1", "10000"} {
		chunkSize := chunkSize
		t.Run("chunk_"+chunkSize, func(t *testing.T) {
			t.Parallel()
			env := validEnvironment()
			env["INDEXER_CHUNK_SIZE"] = chunkSize
			env["INDEXER_CONFIRMATIONS"] = "0"

			got, err := Load(mapGetenv(env))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.IndexerConfirmations == nil || *got.IndexerConfirmations != 0 {
				t.Fatalf("IndexerConfirmations = %v, want pointer to zero", got.IndexerConfirmations)
			}
		})
	}
}

func TestLoadDoesNotRequireOptionalProcessSettings(t *testing.T) {
	t.Parallel()

	env := validEnvironment()
	env["ETH_USD_SOURCE"] = "future-adapter"

	got, err := Load(mapGetenv(env))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ETHUSDSource != "future-adapter" {
		t.Fatalf("ETHUSDSource = %q, want future-adapter", got.ETHUSDSource)
	}
}

func TestRequireAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*Config)
		wantField string
	}{
		{
			name: "complete",
			configure: func(config *Config) {
				config.PrivyAppID = "app-id"
				config.PrivyVerificationKey = "verification-key"
			},
		},
		{
			name: "missing app id",
			configure: func(config *Config) {
				config.PrivyVerificationKey = "verification-key"
			},
			wantField: "PRIVY_APP_ID",
		},
		{
			name: "missing verification key",
			configure: func(config *Config) {
				config.PrivyAppID = "app-id"
			},
			wantField: "PRIVY_VERIFICATION_KEY",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var config Config
			test.configure(&config)

			err := config.RequireAPI()
			if test.wantField == "" {
				if err != nil {
					t.Fatalf("RequireAPI() error = %v", err)
				}
				return
			}

			assertError(t, err, test.wantField, ErrMissing)
		})
	}
}

func TestRequireIndexer(t *testing.T) {
	t.Parallel()
	if err := (Config{IndexerWorkerID: "worker-a"}).RequireIndexer(); err != nil {
		t.Fatalf("RequireIndexer() error = %v", err)
	}
	assertError(t, (Config{}).RequireIndexer(), "INDEXER_WORKER_ID", ErrMissing)
}

func TestLoadDatabaseUsesReducedSurface(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"DATABASE_URL": " postgres://user:pass@127.0.0.1:5432/launchpad ",
	}
	got, err := LoadDatabase(mapGetenv(env))
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	if got.DatabaseURL != "postgres://user:pass@127.0.0.1:5432/launchpad" {
		t.Fatalf("DatabaseURL = %q", got.DatabaseURL)
	}
}

func TestNilGetenvFails(t *testing.T) {
	t.Parallel()

	_, err := Load(nil)
	assertError(t, err, "getenv", ErrMissing)

	_, err = LoadDatabase(nil)
	assertError(t, err, "getenv", ErrMissing)
}

func validEnvironment() map[string]string {
	return map[string]string{
		"CHAIN_ID":      "4663",
		"DEPLOYMENT_ID": "robinhood-mainnet-v1",
		"RPC_URL":       "https://rpc.example.test/v2/key",
		"DATABASE_URL":  "postgresql://user:pass@db.example.test:5432/launchpad",
	}
}

func mapGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func assertFieldError(
	t *testing.T,
	load func(func(string) string) (Config, error),
	env map[string]string,
	wantField string,
	wantCause error,
) {
	t.Helper()

	_, err := load(mapGetenv(env))
	assertError(t, err, wantField, wantCause)
}

func assertError(t *testing.T, err error, wantField string, wantCause error) {
	t.Helper()

	if err == nil {
		t.Fatalf("error = nil, want %s", wantField)
	}
	var fieldError *FieldError
	if !errors.As(err, &fieldError) {
		t.Fatalf("error type = %T, want *FieldError", err)
	}
	if fieldError.Field != wantField {
		t.Errorf("Field = %q, want %q", fieldError.Field, wantField)
	}
	if !errors.Is(err, wantCause) {
		t.Errorf("error = %v, want cause %v", err, wantCause)
	}
}

func TestAPIAbuseLimitDefaultsAndValidation(t *testing.T) {
	got, err := Load(mapGetenv(validEnvironment()))
	if err != nil {
		t.Fatal(err)
	}
	if got.APIRateLimitPerMinute != 600 || got.APIRateLimitBurst != 120 || got.APISSEMaxPerClient != 4 || got.DatabaseMaxConns != 0 || got.APITrustedProxyCIDRs != nil {
		t.Fatalf("defaults = %+v", got)
	}
	for field, value := range map[string]string{
		"API_TRUSTED_PROXY_CIDRS":   "not-a-cidr",
		"API_RATE_LIMIT_PER_MINUTE": "0",
		"API_RATE_LIMIT_BURST":      "0",
		"API_SSE_MAX_PER_CLIENT":    "1001",
		"DATABASE_MAX_CONNS":        "2",
	} {
		env := validEnvironment()
		env[field] = value
		assertFieldError(t, Load, env, field, ErrInvalid)
	}
}
