package indexer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/ethereum/go-ethereum/common"
)

type recordingDiscovery struct{ known []chain.Emitters }

func (d *recordingDiscovery) Discover(_ context.Context, _, _ uint64, known chain.Emitters) (chain.DiscoveryResult, error) {
	d.known = append(d.known, known)
	return chain.DiscoveryResult{Emitters: known}, nil
}

func mustDecoder(t *testing.T, version uint16) *chain.Decoder {
	t.Helper()
	decoder, err := chain.NewDecoder(version)
	if err != nil {
		t.Fatal(err)
	}
	return decoder
}

func dualEngine(t *testing.T) (*Engine, *recordingDiscovery, *recordingDiscovery) {
	t.Helper()
	_, store, source := newTestEngine(t)
	v1, v2 := &recordingDiscovery{}, &recordingDiscovery{}
	// Given in reverse order: the engine orders its sources by version.
	e, err := NewWithEngines(Settings{ChainID: 1, DeploymentID: "local-test", StartBlock: 1, ChunkSize: 2, PollInterval: time.Millisecond}, store, source, []EngineSource{
		{Discovery: v2, Decoder: mustDecoder(t, 2), Factory: common.HexToAddress("0xf2")},
		{Discovery: v1, Decoder: mustDecoder(t, 1), Factory: common.HexToAddress("0xf1")},
	}, emptyRouter{})
	if err != nil {
		t.Fatal(err)
	}
	return e, v1, v2
}

func TestNewWithEnginesRejectsDuplicateEngineOrFactory(t *testing.T) {
	_, store, source := newTestEngine(t)
	settings := Settings{ChainID: 1, DeploymentID: "local-test", StartBlock: 1, ChunkSize: 2, PollInterval: time.Millisecond}
	factory := common.HexToAddress("0xf1")
	for name, test := range map[string]struct {
		engines []EngineSource
		want    string
	}{
		"no engine":        {engines: nil, want: "invalid indexer dependencies"},
		"same version":     {engines: []EngineSource{{Discovery: emptyDiscovery{}, Decoder: mustDecoder(t, 1), Factory: factory}, {Discovery: emptyDiscovery{}, Decoder: mustDecoder(t, 1), Factory: common.HexToAddress("0xf2")}}, want: "engine version 1 is configured twice"},
		"same factory":     {engines: []EngineSource{{Discovery: emptyDiscovery{}, Decoder: mustDecoder(t, 1), Factory: factory}, {Discovery: emptyDiscovery{}, Decoder: mustDecoder(t, 2), Factory: factory}}, want: "configured for two engines"},
		"missing decoder":  {engines: []EngineSource{{Discovery: emptyDiscovery{}, Factory: factory}}, want: "invalid indexer dependencies"},
		"missing discover": {engines: []EngineSource{{Decoder: mustDecoder(t, 1), Factory: factory}}, want: "invalid indexer dependencies"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewWithEngines(settings, store, source, test.engines, emptyRouter{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewWithEngines() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDecodeChunkGivesEachEngineOnlyItsOwnTokens(t *testing.T) {
	e, v1, v2 := dualEngine(t)
	pool := common.HexToHash("0x99")
	identities := []TokenIdentity{
		{Token: common.HexToAddress("0xa1"), Curve: common.HexToAddress("0xc1"), Pair: common.HexToAddress("0xb1"), EngineVersion: 1},
		{Token: common.HexToAddress("0xa2"), Curve: common.HexToAddress("0xc2"), EngineVersion: 2, PoolID: &pool},
	}
	if _, err := e.decodeChunk(t.Context(), stepChunk{from: 1, to: 2}, identities); err != nil {
		t.Fatalf("decodeChunk() error = %v", err)
	}
	if len(v1.known) != 1 || len(v2.known) != 1 {
		t.Fatalf("discoveries ran %d and %d times, want once each", len(v1.known), len(v2.known))
	}
	first, second := v1.known[0], v2.known[0]
	if first.Factory != common.HexToAddress("0xf1") || second.Factory != common.HexToAddress("0xf2") {
		t.Fatalf("factories = %s, %s", first.Factory.Hex(), second.Factory.Hex())
	}
	if _, ok := first.Tokens[common.HexToAddress("0xa1")]; !ok || len(first.Tokens) != 1 || len(first.Pairs) != 1 || len(first.Pools) != 0 {
		t.Fatalf("engine 1 emitters = %+v", first)
	}
	if _, ok := second.Tokens[common.HexToAddress("0xa2")]; !ok || len(second.Tokens) != 1 || len(second.Pairs) != 0 || second.Pools[pool] != common.HexToAddress("0xa2") {
		t.Fatalf("engine 2 emitters = %+v", second)
	}
}

func TestDecodeChunkRefusesTokenOfAnUnconfiguredEngine(t *testing.T) {
	e, _, _ := newTestEngine(t) // engine 1 only
	_, err := e.decodeChunk(t.Context(), stepChunk{from: 1, to: 2}, []TokenIdentity{{Token: common.HexToAddress("0xa2"), EngineVersion: 2}})
	if err == nil || !strings.Contains(err.Error(), "no configured engine decodes") {
		t.Fatalf("decodeChunk() error = %v", err)
	}
}

func TestMergeEngineLogsOrdersByBlockAndLogIndex(t *testing.T) {
	at := func(block uint64, index uint, name string) chain.DecodedLog {
		return chain.DecodedLog{Coordinates: chain.LogCoordinates{BlockNumber: block, LogIndex: index}, Name: name}
	}
	// Engine 1's logs first, then engine 2's, as decodeChunk collects them.
	merged, err := mergeEngineLogs([]chain.DecodedLog{at(3, 0, "v1-c"), at(5, 2, "v1-e"), at(3, 4, "v2-d"), at(1, 7, "v2-a"), at(3, 1, "v2-c2")})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(merged))
	for _, log := range merged {
		got = append(got, log.Name)
	}
	if want := "v2-a v1-c v2-c2 v2-d v1-e"; strings.Join(got, " ") != want {
		t.Fatalf("order = %v, want %s", got, want)
	}
	if _, err := mergeEngineLogs([]chain.DecodedLog{at(3, 1, "v1"), at(3, 1, "v2")}); err == nil {
		t.Fatal("a log decoded by two engines was accepted")
	}
}
