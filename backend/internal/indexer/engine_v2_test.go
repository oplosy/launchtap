package indexer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type recordedIngest struct {
	method      string
	coordinates ledger.EventCoordinates
	value       any
}

// recordingSink records every ingest in call order.
type recordingSink struct{ ingests []recordedIngest }

func (s *recordingSink) record(method string, c ledger.EventCoordinates, value any) (ledger.InsertResult, error) {
	s.ingests = append(s.ingests, recordedIngest{method: method, coordinates: c, value: value})
	return ledger.InsertResult{Inserted: true}, nil
}

func (s *recordingSink) IngestTokenLaunch(_ context.Context, v ledger.TokenLaunch) (ledger.InsertResult, error) {
	return s.record("IngestTokenLaunch", v.EventCoordinates, v)
}
func (s *recordingSink) IngestTrade(_ context.Context, v ledger.Trade) (ledger.InsertResult, error) {
	return s.record("IngestTrade", v.EventCoordinates, v)
}
func (s *recordingSink) IngestGraduation(_ context.Context, v ledger.Graduation) (ledger.InsertResult, error) {
	return s.record("IngestGraduation", v.EventCoordinates, v)
}
func (s *recordingSink) IngestCreatorFeeClaim(_ context.Context, v ledger.CreatorFeeClaim) (ledger.InsertResult, error) {
	return s.record("IngestCreatorFeeClaim", v.EventCoordinates, v)
}
func (s *recordingSink) IngestProtocolFeeClaim(_ context.Context, v ledger.ProtocolFeeClaim) (ledger.InsertResult, error) {
	return s.record("IngestProtocolFeeClaim", v.EventCoordinates, v)
}
func (s *recordingSink) IngestLaunchFeeClaim(_ context.Context, v ledger.LaunchFeeClaim) (ledger.InsertResult, error) {
	return s.record("IngestLaunchFeeClaim", v.EventCoordinates, v)
}
func (s *recordingSink) IngestRefundCredit(_ context.Context, v ledger.RefundCredit) (ledger.InsertResult, error) {
	return s.record("IngestRefundCredit", v.EventCoordinates, v)
}
func (s *recordingSink) IngestRefundClaim(_ context.Context, v ledger.RefundClaim) (ledger.InsertResult, error) {
	return s.record("IngestRefundClaim", v.EventCoordinates, v)
}
func (s *recordingSink) IngestTransfer(_ context.Context, v ledger.Transfer) (ledger.InsertResult, error) {
	return s.record("IngestTransfer", v.EventCoordinates, v)
}
func (s *recordingSink) IngestPoolMint(_ context.Context, v ledger.PoolMint) (ledger.InsertResult, error) {
	return s.record("IngestPoolMint", v.EventCoordinates, v)
}
func (s *recordingSink) IngestPoolBurn(_ context.Context, v ledger.PoolBurn) (ledger.InsertResult, error) {
	return s.record("IngestPoolBurn", v.EventCoordinates, v)
}
func (s *recordingSink) IngestPoolSwap(_ context.Context, v ledger.PoolSwap) (ledger.InsertResult, error) {
	return s.record("IngestPoolSwap", v.EventCoordinates, v)
}
func (s *recordingSink) IngestPoolSync(_ context.Context, v ledger.PoolSync) (ledger.InsertResult, error) {
	return s.record("IngestPoolSync", v.EventCoordinates, v)
}
func (s *recordingSink) IngestLaunchPauseEvent(_ context.Context, v ledger.LaunchPauseEvent) (ledger.InsertResult, error) {
	return s.record("IngestLaunchPauseEvent", v.EventCoordinates, v)
}
func (s *recordingSink) IngestTradingPauseEvent(_ context.Context, v ledger.TradingPauseEvent) (ledger.InsertResult, error) {
	return s.record("IngestTradingPauseEvent", v.EventCoordinates, v)
}
func (s *recordingSink) IngestEngineConfiguration(_ context.Context, v ledger.EngineConfiguration) (ledger.InsertResult, error) {
	return s.record("IngestEngineConfiguration", v.EventCoordinates, v)
}
func (s *recordingSink) IngestFutureDefaultsConfiguration(_ context.Context, v ledger.FutureDefaultsConfiguration) (ledger.InsertResult, error) {
	return s.record("IngestFutureDefaultsConfiguration", v.EventCoordinates, v)
}
func (s *recordingSink) IngestFutureTreasuryConfiguration(_ context.Context, v ledger.FutureTreasuryConfiguration) (ledger.InsertResult, error) {
	return s.record("IngestFutureTreasuryConfiguration", v.EventCoordinates, v)
}
func (s *recordingSink) IngestTokenLaunchV2(_ context.Context, v ledger.TokenLaunchV2) (ledger.InsertResult, error) {
	return s.record("IngestTokenLaunchV2", v.EventCoordinates, v)
}
func (s *recordingSink) IngestTradeV2(_ context.Context, v ledger.TradeV2) (ledger.InsertResult, error) {
	return s.record("IngestTradeV2", v.EventCoordinates, v)
}
func (s *recordingSink) IngestGraduationV2(_ context.Context, v ledger.GraduationV2) (ledger.InsertResult, error) {
	return s.record("IngestGraduationV2", v.EventCoordinates, v)
}
func (s *recordingSink) IngestSnipeTaxClaim(_ context.Context, v ledger.SnipeTaxClaim) (ledger.InsertResult, error) {
	return s.record("IngestSnipeTaxClaim", v.EventCoordinates, v)
}
func (s *recordingSink) IngestFeeRecipientProposal(_ context.Context, v ledger.FeeRecipientProposal) (ledger.InsertResult, error) {
	return s.record("IngestFeeRecipientProposal", v.EventCoordinates, v)
}
func (s *recordingSink) IngestFeeRecipientAcceptance(_ context.Context, v ledger.FeeRecipientAcceptance) (ledger.InsertResult, error) {
	return s.record("IngestFeeRecipientAcceptance", v.EventCoordinates, v)
}
func (s *recordingSink) IngestLpFeeCollection(_ context.Context, v ledger.LpFeeCollection) (ledger.InsertResult, error) {
	return s.record("IngestLpFeeCollection", v.EventCoordinates, v)
}
func (s *recordingSink) IngestLpFeeClaim(_ context.Context, v ledger.LpFeeClaim) (ledger.InsertResult, error) {
	return s.record("IngestLpFeeClaim", v.EventCoordinates, v)
}
func (s *recordingSink) IngestV4PoolInitialization(_ context.Context, v ledger.V4PoolInitialization) (ledger.InsertResult, error) {
	return s.record("IngestV4PoolInitialization", v.EventCoordinates, v)
}
func (s *recordingSink) IngestV4LiquidityChange(_ context.Context, v ledger.V4LiquidityChange) (ledger.InsertResult, error) {
	return s.record("IngestV4LiquidityChange", v.EventCoordinates, v)
}
func (s *recordingSink) IngestV4Swap(_ context.Context, v ledger.V4Swap) (ledger.InsertResult, error) {
	return s.record("IngestV4Swap", v.EventCoordinates, v)
}

// v2FixtureIngests is the frozen engine-2 fixture's emission order, as ingest calls.
var v2FixtureIngests = []string{
	"IngestEngineConfiguration", "IngestFutureTreasuryConfiguration", "IngestFutureDefaultsConfiguration",
	"IngestTransfer", "IngestTokenLaunchV2",
	"IngestTradeV2", "IngestTransfer", "IngestTradeV2", "IngestTransfer", "IngestTradeV2", "IngestTransfer",
	"IngestFeeRecipientProposal", "IngestFeeRecipientAcceptance",
	"IngestTradeV2", "IngestTransfer",
	"IngestV4PoolInitialization", "IngestV4LiquidityChange", "IngestTransfer", "IngestGraduationV2",
	"IngestTransfer", "IngestRefundCredit", "IngestRefundClaim",
	"IngestV4Swap", "IngestTransfer", "IngestV4Swap", "IngestTransfer",
	"IngestV4LiquidityChange", "IngestTransfer", "IngestLpFeeCollection",
	"IngestV4LiquidityChange", "IngestLpFeeClaim", "IngestTransfer",
	"IngestV4LiquidityChange", "IngestLpFeeClaim", "IngestTransfer",
	"IngestCreatorFeeClaim", "IngestCreatorFeeClaim", "IngestProtocolFeeClaim", "IngestSnipeTaxClaim",
	"IngestLaunchFeeClaim", "IngestLaunchPauseEvent", "IngestTradingPauseEvent",
	"IngestEngineConfiguration", "IngestFutureDefaultsConfiguration", "IngestFutureTreasuryConfiguration",
}

// loadV2Fixture reads the frozen engine-2 fixture as logs of one block, one transaction per log.
func loadV2Fixture(t *testing.T, blockHash common.Hash) ([]types.Log, chain.Emitters) {
	t.Helper()
	body, err := os.ReadFile("../chain/testdata/v2/event-logs-v2.json")
	if err != nil {
		t.Fatalf("read v2 fixture: %v", err)
	}
	var artifact struct {
		Logs []struct {
			EmitterKind string   `json:"emitterKind"`
			Address     string   `json:"address"`
			Topics      []string `json:"topics"`
			Data        string   `json:"data"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(body, &artifact); err != nil {
		t.Fatalf("decode v2 fixture: %v", err)
	}
	emitters := chain.Emitters{Curves: chain.NewAddressSet(), Tokens: chain.NewAddressSet(), Pairs: chain.NewAddressSet(), Pools: make(map[common.Hash]common.Address)}
	logs := make([]types.Log, 0, len(artifact.Logs))
	for index, fixture := range artifact.Logs {
		address := common.HexToAddress(fixture.Address)
		topics := make([]common.Hash, len(fixture.Topics))
		for topicIndex, topic := range fixture.Topics {
			topics[topicIndex] = common.HexToHash(topic)
		}
		switch fixture.EmitterKind {
		case "factory":
			emitters.Factory = address
		case "curve":
			emitters.Curves[address] = struct{}{}
		case "token":
			emitters.Tokens[address] = struct{}{}
		case "locker":
			emitters.Locker = address
		case "poolManager":
			emitters.PoolManager = address
			// Initialize indexes the pool id, currency0 and currency1; currency1 is the token.
			if len(topics) == 4 {
				emitters.Pools[topics[1]] = common.BytesToAddress(topics[3][12:])
			}
		default:
			t.Fatalf("unknown v2 fixture emitter kind %q", fixture.EmitterKind)
		}
		data, err := hex.DecodeString(strings.TrimPrefix(fixture.Data, "0x"))
		if err != nil {
			t.Fatalf("decode fixture data: %v", err)
		}
		logs = append(logs, types.Log{Address: address, Topics: topics, Data: data, BlockNumber: 1, BlockHash: blockHash, TxHash: common.BigToHash(big.NewInt(int64(index + 1))), TxIndex: uint(index), Index: uint(index)})
	}
	return logs, emitters
}

// fixtureDiscovery returns fixed logs and records the known emitters it was given.
type fixtureDiscovery struct {
	logs     []types.Log
	emitters chain.Emitters
	known    []chain.Emitters
}

func (d *fixtureDiscovery) Discover(_ context.Context, _, _ uint64, known chain.Emitters) (chain.DiscoveryResult, error) {
	d.known = append(d.known, known)
	return chain.DiscoveryResult{Logs: d.logs, Emitters: d.emitters}, nil
}

func newV2Engine(t *testing.T) (*Engine, *memoryStore, *recordingSink, *fixtureDiscovery, []types.Log) {
	t.Helper()
	e, store, source := newTestEngine(t)
	decoder, err := chain.NewDecoder(2)
	if err != nil {
		t.Fatal(err)
	}
	logs, emitters := loadV2Fixture(t, source.headers[1].Hash())
	sink := &recordingSink{}
	discovery := &fixtureDiscovery{logs: logs, emitters: emitters}
	e.decoder, e.discovery, e.router = decoder, discovery, LedgerRouter{Sink: sink, ChainID: 1}
	return e, store, sink, discovery, logs
}

func TestEngineIngestsEngineV2Fixture(t *testing.T) {
	e, _, sink, _, logs := newV2Engine(t)
	swapper := common.HexToAddress("0x5a5a")
	senders := make(map[common.Hash]common.Address)
	for _, log := range logs {
		senders[log.TxHash] = swapper
	}
	e.settings.Senders = &fakeSenders{senders: senders}
	if advanced, err := e.Step(t.Context()); err != nil || !advanced {
		t.Fatalf("Step() = %t, %v", advanced, err)
	}
	if len(sink.ingests) != len(logs) || len(logs) != len(v2FixtureIngests) {
		t.Fatalf("ingests = %d, logs = %d, want %d each", len(sink.ingests), len(logs), len(v2FixtureIngests))
	}
	blockTime := sink.ingests[0].coordinates.BlockTime
	if blockTime.IsZero() {
		t.Fatal("ingest has no block time")
	}
	var graduation ledger.GraduationV2
	var launch ledger.TokenLaunchV2
	swaps := 0
	for index, ingest := range sink.ingests {
		log := logs[index]
		if ingest.method != v2FixtureIngests[index] {
			t.Fatalf("ingest %d = %s, want %s", index, ingest.method, v2FixtureIngests[index])
		}
		want := ledger.EventCoordinates{ChainID: 1, BlockNumber: 1, BlockHash: log.BlockHash, BlockTime: blockTime, TransactionIndex: int32(log.TxIndex), TxHash: log.TxHash, LogIndex: int32(log.Index)}
		if ingest.coordinates != want {
			t.Fatalf("ingest %d coordinates = %+v, want %+v", index, ingest.coordinates, want)
		}
		switch value := ingest.value.(type) {
		case ledger.TokenLaunchV2:
			launch = value
		case ledger.TradeV2:
			if value.SnipeTax == nil || value.Token != launch.Token || value.ETHGross == nil {
				t.Fatalf("trade %d = %+v", index, value)
			}
		case ledger.GraduationV2:
			graduation = value
		case ledger.CreatorFeeClaim:
			if value.Token != launch.Token || value.Creator == (common.Address{}) || value.Amount == nil {
				t.Fatalf("creator fee claim %d = %+v", index, value)
			}
		case ledger.V4Swap:
			swaps++
			if value.PoolID != graduation.PoolID || value.TxFrom == nil || *value.TxFrom != swapper || value.Sender == swapper {
				t.Fatalf("swap %d = %+v, want pool %s and tx sender %s", index, value, graduation.PoolID.Hex(), swapper.Hex())
			}
		}
	}
	if launch.Hook == (common.Address{}) || launch.Locker == (common.Address{}) || launch.LPCreatorShareBPS == 0 {
		t.Fatalf("launch = %+v, want v2 fields", launch)
	}
	if graduation.Token != launch.Token || graduation.PoolID != chain.V4PoolID(launch.Token, launch.Hook) || graduation.PositionTokenID == nil {
		t.Fatalf("graduation = %+v", graduation)
	}
	if swaps != 2 {
		t.Fatalf("swaps = %d, want 2", swaps)
	}
}

func TestEngineRejectsIdentityOfAnotherEngine(t *testing.T) {
	e, store, sink, _, _ := newV2Engine(t)
	store.unit.identities = []TokenIdentity{{Token: common.HexToAddress("0x1001"), Curve: common.HexToAddress("0x2001"), Pair: common.HexToAddress("0x3001"), EngineVersion: 1}}
	if _, err := e.Step(t.Context()); err == nil || !strings.Contains(err.Error(), "engine version 1") {
		t.Fatalf("Step() error = %v, want an engine version mismatch", err)
	}
	if len(sink.ingests) != 0 {
		t.Fatalf("ingested %d events after an engine mismatch", len(sink.ingests))
	}
}

func TestEngineLoadsPersistedV4Pools(t *testing.T) {
	e, store, _, discovery, logs := newV2Engine(t)
	token, curve := common.HexToAddress("0x1001"), common.HexToAddress("0x2001")
	poolID := common.HexToHash("0xabcd")
	store.unit.identities = []TokenIdentity{{Token: token, Curve: curve, EngineVersion: 2}}
	store.unit.pools = map[common.Hash]common.Address{poolID: token}
	discovery.logs = logs[:0]
	if _, err := e.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	known := discovery.known[0]
	if known.Pools[poolID] != token || len(known.Pools) != 1 {
		t.Fatalf("known pools = %v, want %s -> %s", known.Pools, poolID.Hex(), token.Hex())
	}
	if _, ok := known.Curves[curve]; !ok || len(known.Pairs) != 0 {
		t.Fatalf("known curves = %v pairs = %v, want the curve and no zero pair", known.Curves, known.Pairs)
	}
}

func TestEngineRejectsPersistedPoolWithoutV2Identity(t *testing.T) {
	e, store, _, _, _ := newV2Engine(t)
	store.unit.pools = map[common.Hash]common.Address{common.HexToHash("0xabcd"): common.HexToAddress("0x1001")}
	if _, err := e.Step(t.Context()); err == nil || !strings.Contains(err.Error(), "no engine-2 token identity") {
		t.Fatalf("Step() error = %v, want a pool without identity error", err)
	}
}

func decodedAt(block uint64, logIndex uint, value any) chain.DecodedLog {
	return chain.DecodedLog{Coordinates: chain.LogCoordinates{BlockNumber: block, TransactionIndex: logIndex, LogIndex: logIndex}, Value: value}
}

func TestRouterOrdersV4SwapsAfterGraduation(t *testing.T) {
	token, poolID := common.HexToAddress("0x1001"), common.HexToHash("0xabcd")
	graduated := chain.GraduatedV2{Token: token, PoolID: poolID, PositionTokenID: big.NewInt(1), ETHToPool: big.NewInt(1), TokensToPool: big.NewInt(1), Liquidity: big.NewInt(1), SqrtPriceX96: big.NewInt(1), ETHRemainder: big.NewInt(0), TokenRemainder: big.NewInt(0)}
	swap := chain.V4Swap{PoolID: poolID, Amount0: big.NewInt(-1), Amount1: big.NewInt(1), SqrtPriceX96: big.NewInt(1), Liquidity: big.NewInt(1)}
	trade := chain.TradeV2{Token: token, ETHGross: big.NewInt(1), ETHRefund: big.NewInt(0), TokenAmount: big.NewInt(1), ProtocolFee: big.NewInt(0), CreatorFee: big.NewInt(0), NewETHReserve: big.NewInt(1), NewTokenReserve: big.NewInt(1), SnipeTax: big.NewInt(0)}
	persisted := []TokenIdentity{{Token: token, EngineVersion: 2, PoolID: &poolID}}
	for _, tc := range []struct {
		name       string
		logs       []chain.DecodedLog
		identities []TokenIdentity
		wantErr    string
	}{
		{"swap after graduation", []chain.DecodedLog{decodedAt(1, 1, graduated), decodedAt(1, 2, swap)}, nil, ""},
		{"swap before graduation in one block", []chain.DecodedLog{decodedAt(1, 1, swap), decodedAt(1, 2, graduated)}, nil, "V4 swap before graduation"},
		{"swap for an unknown pool", []chain.DecodedLog{decodedAt(1, 1, swap)}, nil, "V4 swap before graduation"},
		{"swap for a persisted pool", []chain.DecodedLog{decodedAt(1, 1, swap)}, persisted, ""},
		{"curve trade after graduation", []chain.DecodedLog{decodedAt(1, 1, graduated), decodedAt(2, 1, trade)}, nil, "trade after graduation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			err := LedgerRouter{Sink: sink, ChainID: 1}.Apply(t.Context(), nil, tc.logs, map[int64]ledger.IndexedBlock{}, tc.identities)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("Apply() error = %v, want %q", err, tc.wantErr)
			}
			if tc.wantErr != "" && len(sink.ingests) != 0 {
				t.Fatalf("rejected chunk ingested %d events", len(sink.ingests))
			}
		})
	}
}
