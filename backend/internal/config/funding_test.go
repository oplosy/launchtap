package config

import (
	"errors"
	"strings"
	"testing"
)

func fundingEnv(overrides map[string]string) func(string) string {
	values := map[string]string{
		"CHAIN_ID": "46630", "DEPLOYMENT_ID": "robinhood-mainnet", "DATABASE_URL": "postgres://u:p@db/launchpad",
		"LOG_LEVEL": "info", "FUNDING_RPC_URL": "https://archive.example/v1/secret-key",
	}
	for k, v := range overrides {
		values[k] = v
	}
	return func(k string) string { return values[k] }
}

func TestLoadFunding(t *testing.T) {
	c, err := LoadFunding(fundingEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.ChainID != 46630 || c.DeploymentID != "robinhood-mainnet" || c.RPCURL != "https://archive.example/v1/secret-key" {
		t.Fatalf("config = %+v", c)
	}
}

func TestLoadFundingRejectsBadRPCURLWithoutEchoingIt(t *testing.T) {
	for _, value := range []string{"", "ftp://archive.example/secret-key", "secret-key"} {
		_, err := LoadFunding(fundingEnv(map[string]string{"FUNDING_RPC_URL": value}))
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "FUNDING_RPC_URL" {
			t.Fatalf("%q: err = %v", value, err)
		}
		if value != "" && strings.Contains(err.Error(), "secret-key") {
			t.Fatalf("error leaks the URL: %v", err)
		}
	}
}
