package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/deployments"
	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/config"
	"github.com/ethereum/go-ethereum/common"
)

func TestDialIndexerRPCWithoutFallbackKeepsOneProvider(t *testing.T) {
	deployment, code := startupDeployment()
	var methods []string
	var methodsMu sync.Mutex
	server := startupRPCServer(t, "0x1237", code, common.Address{}, "", &methods, &methodsMu)
	defer server.Close()
	client, err := dialIndexerRPCWithVerifier(t, server.URL, config.RPCFallbackConfig{}, deployment)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	methodsMu.Lock()
	defer methodsMu.Unlock()
	if len(methods) != 0 {
		t.Fatalf("dial without a fallback made RPC calls: %v", methods)
	}
}

func TestDialIndexerRPCRefusesFallbackOnWrongChain(t *testing.T) {
	deployment, code := startupDeployment()
	var primaryMethods, fallbackMethods []string
	var mu sync.Mutex
	primary := startupRPCServer(t, "0x1237", code, common.Address{}, "", &primaryMethods, &mu)
	defer primary.Close()
	fallbackServer := startupRPCServer(t, "0x1", code, common.Address{}, "", &fallbackMethods, &mu)
	defer fallbackServer.Close()
	fallback := config.RPCFallbackConfig{URL: fallbackServer.URL + "/secret-key", AfterFailures: 3, ProbeInterval: time.Second}
	_, err := dialIndexerRPCWithVerifier(t, primary.URL, fallback, deployment)
	if !errors.Is(err, chain.ErrChainIDMismatch) || !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("dialIndexerRPC() error = %v, want a fallback chain-id mismatch", err)
	}
}

func TestDialIndexerRPCRefusesFallbackWithBadBytecode(t *testing.T) {
	deployment, code := startupDeployment()
	var methods []string
	var mu sync.Mutex
	primary := startupRPCServer(t, "0x1237", code, common.Address{}, "", &methods, &mu)
	defer primary.Close()
	fallbackServer := startupRPCServer(t, "0x1237", code, deployment.Factory, "0x", &methods, &mu)
	defer fallbackServer.Close()
	fallback := config.RPCFallbackConfig{URL: fallbackServer.URL, AfterFailures: 3, ProbeInterval: time.Second}
	_, err := dialIndexerRPCWithVerifier(t, primary.URL, fallback, deployment)
	if !errors.Is(err, chain.ErrBytecodeMismatch) {
		t.Fatalf("dialIndexerRPC() error = %v, want a fallback bytecode mismatch", err)
	}
}

// headsFailingVerifier passes the identity checks but has no finality tags.
type headsFailingVerifier struct{ *chain.Client }

func (headsFailingVerifier) Heads(context.Context) (chain.Heads, error) {
	return chain.Heads{}, &chain.FinalityTagError{Tag: "safe", Err: errors.New("unsupported block tag")}
}

func TestVerifyFallbackRPCRequiresFinalityTags(t *testing.T) {
	deployment, code := startupDeployment()
	var methods []string
	var mu sync.Mutex
	server := startupRPCServer(t, "0x1237", code, common.Address{}, "", &methods, &mu)
	defer server.Close()
	client, err := chain.Dial(context.Background(), server.URL, chain.RPCConfig{Timeout: time.Second, RetryBackoff: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var tagErr *chain.FinalityTagError
	if err := verifyFallbackRPC(context.Background(), headsFailingVerifier{client}, deployment.ChainID, deployment); !errors.As(err, &tagErr) {
		t.Fatalf("verifyFallbackRPC() error = %v, want a finality tag error", err)
	}
}

func TestDialIndexerRPCStartsOnFallbackWhenPrimaryIsUnreachable(t *testing.T) {
	deployment, code := startupDeployment()
	var primaryMethods, fallbackMethods []string
	var mu sync.Mutex
	primary := startupRPCServer(t, "0x1237", code, common.Address{}, "", &primaryMethods, &mu)
	primaryURL := primary.URL
	primary.Close() // connection refused
	fallbackServer := startupRPCServer(t, "0x1237", code, common.Address{}, "", &fallbackMethods, &mu)
	defer fallbackServer.Close()
	fallback := config.RPCFallbackConfig{URL: fallbackServer.URL, AfterFailures: 3, ProbeInterval: time.Hour}
	client, err := dialIndexerRPCWithVerifier(t, primaryURL, fallback, deployment)
	if err != nil {
		t.Fatalf("dialIndexerRPC() error = %v, want a start on the fallback", err)
	}
	defer client.Close()
	if _, err := client.CodeAt(context.Background(), deployment.Factory); err != nil {
		t.Fatalf("CodeAt() on the fallback = %v", err)
	}
}

func TestDialIndexerRPCRefusesPrimaryWithWrongChain(t *testing.T) {
	deployment, code := startupDeployment()
	var methods []string
	var mu sync.Mutex
	primary := startupRPCServer(t, "0x1", code, common.Address{}, "", &methods, &mu)
	defer primary.Close()
	fallbackServer := startupRPCServer(t, "0x1237", code, common.Address{}, "", &methods, &mu)
	defer fallbackServer.Close()
	fallback := config.RPCFallbackConfig{URL: fallbackServer.URL, AfterFailures: 3, ProbeInterval: time.Hour}
	if _, err := dialIndexerRPCWithVerifier(t, primary.URL, fallback, deployment); !errors.Is(err, chain.ErrChainIDMismatch) {
		t.Fatalf("dialIndexerRPC() error = %v, want the primary's chain-id mismatch", err)
	}
}

// dialIndexerRPCWithVerifier runs dialIndexerRPC with the production fallback identity checks
// but without the finality-head read, which the single-call startup server cannot answer
// (TestVerifyFallbackRPCRequiresFinalityTags covers that read).
func dialIndexerRPCWithVerifier(t *testing.T, primaryURL string, fallback config.RPCFallbackConfig, deployment deployments.Deployment) (*chain.Client, error) {
	t.Helper()
	return dialIndexerRPC(context.Background(), primaryURL, fallback, chain.RPCConfig{Timeout: time.Second, RetryBackoff: time.Millisecond}, deployment.ChainID, deployment, func(ctx context.Context, client *chain.Client) error {
		return verifyPrimaryRPC(ctx, client, deployment.ChainID, deployment)
	}, func(context.Context, *chain.Client) error { return nil }, nil)
}
