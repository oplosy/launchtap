package chain

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type fixtureArtifact struct {
	SchemaVersion uint16       `json:"schemaVersion"`
	EngineVersion uint16       `json:"engineVersion"`
	Logs          []fixtureLog `json:"logs"`
}

type fixtureLog struct {
	EmitterKind string   `json:"emitterKind"`
	Address     string   `json:"address"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
}

func loadFixtureLogs(t *testing.T) (fixtureArtifact, []types.Log, Emitters) {
	t.Helper()
	body, err := os.ReadFile("testdata/event-logs-v1.json")
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var artifact fixtureArtifact
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&artifact); err != nil {
		t.Fatalf("decode fixtures: %v", err)
	}
	emitters := emptyEmitters(common.Address{})
	logs := make([]types.Log, 0, len(artifact.Logs))
	for index, fixture := range artifact.Logs {
		address := common.HexToAddress(fixture.Address)
		switch fixture.EmitterKind {
		case "factory":
			emitters.Factory = address
		case "curve":
			emitters.Curves[address] = struct{}{}
		case "token":
			emitters.Tokens[address] = struct{}{}
		case "pair":
			emitters.Pairs[address] = struct{}{}
		default:
			t.Fatalf("unknown fixture emitter kind %q", fixture.EmitterKind)
		}
		data, err := hex.DecodeString(strings.TrimPrefix(fixture.Data, "0x"))
		if err != nil {
			t.Fatalf("decode fixture data: %v", err)
		}
		topics := make([]common.Hash, len(fixture.Topics))
		for topicIndex, topic := range fixture.Topics {
			topics[topicIndex] = common.HexToHash(topic)
		}
		logs = append(logs, types.Log{Address: address, Topics: topics, Data: data, BlockNumber: uint64(index + 1), BlockHash: common.BigToHash(big.NewInt(1)), TxHash: common.BigToHash(big.NewInt(2)), TxIndex: uint(index), Index: uint(index)})
	}
	return artifact, logs, emitters
}

func TestDecoderDecodesAllFoundryFixtures(t *testing.T) {
	t.Parallel()
	artifact, logs, emitters := loadFixtureLogs(t)
	decoder, err := NewDecoder(artifact.EngineVersion)
	if err != nil {
		t.Fatalf("NewDecoder() error = %v", err)
	}
	names := make(map[string]struct{})
	var sawLaunchPause, sawTradingPause bool
	for _, log := range logs {
		decoded, err := decoder.Decode(log, emitters)
		if err != nil {
			t.Fatalf("Decode(%s) error = %v", log.Topics[0], err)
		}
		names[decoded.Name] = struct{}{}
		switch value := decoded.Value.(type) {
		case TokenLaunched:
			if value.Name != "Pons 🐸 launch" || value.Symbol != "PØNS" {
				t.Errorf("TokenLaunched strings = %q/%q", value.Name, value.Symbol)
			}
		case Trade:
			if value.ETHGross.String() == "101" && value.ETHRefund.String() != "2" {
				t.Errorf("Trade ETH fields lost adjacency: gross=%s refund=%s", value.ETHGross, value.ETHRefund)
			}
		case LaunchPauseSet:
			sawLaunchPause = value.Paused
		case TradingPauseSet:
			sawTradingPause = value.Paused
		}
	}
	if len(names) != 18 {
		t.Fatalf("decoded unique event names = %d, want 18: %v", len(names), names)
	}
	if !sawLaunchPause || !sawTradingPause {
		t.Fatal("pause events were not decoded into their distinct types")
	}
}

func TestDecoderRejectsMalformedAndMisroutedLogs(t *testing.T) {
	t.Parallel()
	_, logs, emitters := loadFixtureLogs(t)
	decoder, err := NewDecoder(1)
	if err != nil {
		t.Fatal(err)
	}
	transfer := findFixture(t, decoder, logs, emitters, "Transfer")

	tests := []struct {
		name   string
		mutate func(types.Log) types.Log
		cause  error
	}{
		{"missing topic", func(log types.Log) types.Log { log.Topics = nil; return log }, ErrMalformedLog},
		{"unknown topic", func(log types.Log) types.Log { log.Topics[0] = common.HexToHash("0x1234"); return log }, ErrUnknownTopic},
		{"wrong topic count", func(log types.Log) types.Log { log.Topics = log.Topics[:2]; return log }, ErrMalformedLog},
		{"non-padded address", func(log types.Log) types.Log { log.Topics[1][0] = 1; return log }, ErrMalformedLog},
		{"short data", func(log types.Log) types.Log { log.Data = []byte{1}; return log }, ErrMalformedLog},
		{"unknown emitter", func(log types.Log) types.Log { log.Address = common.HexToAddress("0xdead"); return log }, ErrUnknownEmitter},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			log := transfer
			log.Topics = append([]common.Hash(nil), transfer.Topics...)
			_, err := decoder.Decode(test.mutate(log), emitters)
			if !errors.Is(err, test.cause) {
				t.Fatalf("Decode() error = %v, want cause %v", err, test.cause)
			}
		})
	}
}

func TestDecoderRejectsUnknownEngineVersion(t *testing.T) {
	t.Parallel()
	if _, err := NewDecoder(3); !errors.Is(err, ErrUnsupportedEngine) {
		t.Fatalf("NewDecoder(3) error = %v", err)
	}
	_, logs, emitters := loadFixtureLogs(t)
	decoder, _ := NewDecoder(1)
	configured := findFixture(t, decoder, logs, emitters, "EngineConfigured")
	configured.Topics = append([]common.Hash(nil), configured.Topics...)
	configured.Topics[1] = common.BigToHash(big.NewInt(2))
	if _, err := decoder.Decode(configured, emitters); !errors.Is(err, ErrUnsupportedEngine) {
		t.Fatalf("Decode engine v2 error = %v", err)
	}
}

func TestDecoderRejectsNonPaddedAddressInData(t *testing.T) {
	t.Parallel()
	_, logs, emitters := loadFixtureLogs(t)
	decoder, _ := NewDecoder(1)
	launch := findFixture(t, decoder, logs, emitters, "TokenLaunched")
	launch.Data = append([]byte(nil), launch.Data...)
	launch.Data[0] = 1
	if _, err := decoder.Decode(launch, emitters); !errors.Is(err, ErrMalformedLog) {
		t.Fatalf("Decode() error = %v, want ErrMalformedLog", err)
	}
}

func findFixture(t *testing.T, decoder *Decoder, logs []types.Log, emitters Emitters, name string) types.Log {
	t.Helper()
	for _, log := range logs {
		decoded, err := decoder.Decode(log, emitters)
		if err == nil && decoded.Name == name {
			return log
		}
	}
	t.Fatalf("fixture %s not found", name)
	return types.Log{}
}
