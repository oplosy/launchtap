package config

import "strings"

// FundingConfig is the cmd/funding worker configuration
// (docs/specs/2026-11-12-funding-source-clusters-design.md). RPCURL is a secret (provider URLs
// carry API keys) and never appears in error text.
type FundingConfig struct {
	ChainID      uint64 `env:"CHAIN_ID"`
	DeploymentID string `env:"DEPLOYMENT_ID"`
	DatabaseURL  string `env:"DATABASE_URL"`
	LogLevel     string `env:"LOG_LEVEL"`
	// RPCURL must serve historical state and debug_traceBlockByNumber with the callTracer.
	RPCURL string `env:"FUNDING_RPC_URL"`
}

// LoadFunding parses the worker configuration; it performs no I/O.
func LoadFunding(getenv func(string) string) (FundingConfig, error) {
	if getenv == nil {
		return FundingConfig{}, &FieldError{Field: "getenv", Err: ErrMissing}
	}
	get := func(k string) string { return strings.TrimSpace(getenv(k)) }
	chainID, err := requiredPositiveUint64("CHAIN_ID", get("CHAIN_ID"))
	if err != nil {
		return FundingConfig{}, err
	}
	deploymentID := get("DEPLOYMENT_ID")
	if !deploymentIDPattern.MatchString(deploymentID) {
		return FundingConfig{}, invalidOrMissing("DEPLOYMENT_ID", deploymentID)
	}
	databaseURL := get("DATABASE_URL")
	if err := validateURL("DATABASE_URL", databaseURL, "postgres", "postgresql"); err != nil {
		return FundingConfig{}, err
	}
	logLevel, err := parseLogLevel(get("LOG_LEVEL"))
	if err != nil {
		return FundingConfig{}, err
	}
	rpcURL := get("FUNDING_RPC_URL")
	if err := validateURL("FUNDING_RPC_URL", rpcURL, "http", "https", "ws", "wss"); err != nil {
		return FundingConfig{}, err
	}
	return FundingConfig{ChainID: chainID, DeploymentID: deploymentID, DatabaseURL: databaseURL, LogLevel: logLevel, RPCURL: rpcURL}, nil
}
