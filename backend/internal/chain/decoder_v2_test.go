package chain

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func loadFixtureLogsV2(t *testing.T) (fixtureArtifact, []types.Log, Emitters) {
	t.Helper()
	body, err := os.ReadFile("testdata/v2/event-logs-v2.json")
	if err != nil {
		t.Fatalf("read v2 fixtures: %v", err)
	}
	var artifact fixtureArtifact
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&artifact); err != nil {
		t.Fatalf("decode v2 fixtures: %v", err)
	}
	emitters := emptyEmitters(common.Address{})
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
			// Every PoolManager event indexes the pool id first; Initialize's currency1 is the token.
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
		logs = append(logs, types.Log{Address: address, Topics: topics, Data: data, BlockNumber: uint64(index + 1), BlockHash: common.BigToHash(big.NewInt(1)), TxHash: common.BigToHash(big.NewInt(2)), TxIndex: uint(index), Index: uint(index)})
	}
	return artifact, logs, emitters
}

// genericEventsV2 parses the frozen v2 ABIs independently of the decoder, so the field-mapping
// test below compares two separate readings of every log.
func genericEventsV2(t *testing.T) map[common.Hash]abi.Event {
	t.Helper()
	events := make(map[common.Hash]abi.Event)
	for _, name := range []string{"ILaunchEventsV2", "ILpFeeLockerEvents", "IPoolManagerEvents", "LaunchTokenV2"} {
		body, err := os.ReadFile("abi/v2/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := abi.JSON(strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range parsed.Events {
			events[event.ID] = event
		}
	}
	return events
}

func genericValues(t *testing.T, event abi.Event, log types.Log) map[string]string {
	t.Helper()
	values := make(map[string]any)
	if err := event.Inputs.NonIndexed().UnpackIntoMap(values, log.Data); err != nil {
		t.Fatal(err)
	}
	if err := abi.ParseTopicsIntoMap(values, indexedArguments(event.Inputs), log.Topics[1:]); err != nil {
		t.Fatal(err)
	}
	return normalized(values)
}

func normalized(values map[string]any) map[string]string {
	result := make(map[string]string, len(values))
	for name, value := range values {
		if raw, ok := value.([32]byte); ok {
			value = common.Hash(raw)
		}
		result[name] = fmt.Sprint(value)
	}
	return result
}

// typedFields maps a decoded value back to its ABI argument names.
func typedFields(t *testing.T, value any) map[string]any {
	t.Helper()
	switch v := value.(type) {
	case TokenLaunchedV2:
		return map[string]any{"token": v.Token, "curve": v.Curve, "creator": v.Creator, "protocolTreasury": v.ProtocolTreasury, "engineVersion": v.EngineVersion, "name": v.Name, "symbol": v.Symbol, "totalSupply": v.TotalSupply, "virtualEth": v.VirtualETH, "virtualToken": v.VirtualToken, "curveTokens": v.CurveTokens, "lpTokens": v.LPTokens, "graduationEth": v.GraduationETH, "launchFeePaid": v.LaunchFeePaid, "tradeFeeBps": v.TradeFeeBPS, "protocolShareBps": v.ProtocolShareBPS, "hook": v.Hook, "locker": v.Locker, "antiSnipeStartBps": v.AntiSnipeStartBPS, "antiSnipeDurationSeconds": v.AntiSnipeDurationSeconds, "lpCreatorShareBps": v.LPCreatorShareBPS}
	case TradeV2:
		return map[string]any{"token": v.Token, "trader": v.Trader, "isBuy": v.IsBuy, "ethGross": v.ETHGross, "ethRefund": v.ETHRefund, "tokenAmount": v.TokenAmount, "protocolFee": v.ProtocolFee, "creatorFee": v.CreatorFee, "newEthReserve": v.NewETHReserve, "newTokenReserve": v.NewTokenReserve, "snipeTax": v.SnipeTax}
	case GraduatedV2:
		return map[string]any{"token": v.Token, "poolId": v.PoolID, "positionTokenId": v.PositionTokenID, "ethToPool": v.ETHToPool, "tokensToPool": v.TokensToPool, "liquidity": v.Liquidity, "sqrtPriceX96": v.SqrtPriceX96, "ethRemainder": v.ETHRemainder, "tokenRemainder": v.TokenRemainder}
	case CreatorFeesClaimedV2:
		return map[string]any{"token": v.Token, "account": v.Account, "amount": v.Amount}
	case ProtocolFeesClaimed:
		return map[string]any{"token": v.Token, "treasury": v.Treasury, "amount": v.Amount}
	case LaunchFeesClaimed:
		return map[string]any{"treasury": v.Treasury, "amount": v.Amount}
	case RefundCredited:
		return map[string]any{"token": v.Token, "account": v.Account, "amount": v.Amount}
	case RefundClaimed:
		return map[string]any{"token": v.Token, "account": v.Account, "amount": v.Amount}
	case SnipeTaxClaimed:
		return map[string]any{"token": v.Token, "treasury": v.Treasury, "amount": v.Amount}
	case FeeRecipientProposed:
		return map[string]any{"token": v.Token, "current": v.Current, "next": v.Next}
	case FeeRecipientAccepted:
		return map[string]any{"token": v.Token, "previous": v.Previous, "next": v.Next}
	case LaunchPauseSet:
		return map[string]any{"paused": v.Paused}
	case TradingPauseSet:
		return map[string]any{"paused": v.Paused}
	case EngineConfigured:
		return map[string]any{"engineVersion": v.EngineVersion, "implementation": v.Implementation, "enabled": v.Enabled}
	case FutureDefaultsConfigured:
		return map[string]any{"configHash": v.ConfigHash}
	case FutureTreasuryConfigured:
		return map[string]any{"previousTreasury": v.PreviousTreasury, "newTreasury": v.NewTreasury}
	case Transfer:
		return map[string]any{"from": v.From, "to": v.To, "value": v.Value}
	case LpFeesCollected:
		return map[string]any{"token": v.Token, "ethAmount": v.ETHAmount, "tokenAmount": v.TokenAmount}
	case LpFeesClaimed:
		return map[string]any{"token": v.Token, "recipient": v.Recipient, "ethAmount": v.ETHAmount, "tokenAmount": v.TokenAmount}
	case V4Initialize:
		return map[string]any{"id": v.PoolID, "currency0": v.Currency0, "currency1": v.Currency1, "fee": v.Fee, "tickSpacing": v.TickSpacing, "hooks": v.Hooks, "sqrtPriceX96": v.SqrtPriceX96, "tick": v.Tick}
	case V4ModifyLiquidity:
		return map[string]any{"id": v.PoolID, "sender": v.Sender, "tickLower": v.TickLower, "tickUpper": v.TickUpper, "liquidityDelta": v.LiquidityDelta, "salt": v.Salt}
	case V4Swap:
		return map[string]any{"id": v.PoolID, "sender": v.Sender, "amount0": v.Amount0, "amount1": v.Amount1, "sqrtPriceX96": v.SqrtPriceX96, "liquidity": v.Liquidity, "tick": v.Tick, "fee": v.Fee}
	default:
		t.Fatalf("no field map for decoded type %T", value)
		return nil
	}
}

func TestDecoderV2DecodesEveryFixtureFieldForField(t *testing.T) {
	t.Parallel()
	artifact, logs, emitters := loadFixtureLogsV2(t)
	if artifact.EngineVersion != 2 {
		t.Fatalf("fixture engine version = %d", artifact.EngineVersion)
	}
	decoder, err := NewDecoder(2)
	if err != nil {
		t.Fatalf("NewDecoder(2) error = %v", err)
	}
	generic := genericEventsV2(t)
	names := make(map[string]struct{})
	for _, log := range logs {
		decoded, err := decoder.Decode(log, emitters)
		if err != nil {
			t.Fatalf("Decode(%s) error = %v", log.Topics[0], err)
		}
		names[decoded.Name] = struct{}{}
		want := genericValues(t, generic[log.Topics[0]], log)
		got := normalized(typedFields(t, decoded.Value))
		if !mapsEqual(got, want) {
			t.Errorf("%s fields differ\n got  %v\n want %v", decoded.Name, got, want)
		}
		if transfer, ok := decoded.Value.(Transfer); ok && transfer.Token != log.Address {
			t.Errorf("Transfer token = %s, want emitter %s", transfer.Token.Hex(), log.Address.Hex())
		}
	}
	if len(names) != 22 {
		t.Fatalf("decoded unique event names = %d, want 22: %v", len(names), names)
	}
}

// Invariants that hold for the generated lifecycle; exact on-chain values are the e2e's job.
func TestDecoderV2FixtureLifecycleInvariants(t *testing.T) {
	t.Parallel()
	_, logs, emitters := loadFixtureLogsV2(t)
	decoder, err := NewDecoder(2)
	if err != nil {
		t.Fatal(err)
	}
	hooks := make(map[common.Address]common.Address)
	initialized := make(map[common.Hash]V4Initialize)
	var graduations []GraduatedV2
	var taxedBuys int
	for _, log := range logs {
		decoded, err := decoder.Decode(log, emitters)
		if err != nil {
			t.Fatal(err)
		}
		switch value := decoded.Value.(type) {
		case TokenLaunchedV2:
			if value.EngineVersion != 2 || value.Hook == (common.Address{}) || value.Locker != emitters.Locker {
				t.Errorf("TokenLaunched = %+v", value)
			}
			hooks[value.Token] = value.Hook
		case TradeV2:
			fees := new(big.Int).Add(value.SnipeTax, new(big.Int).Add(value.ProtocolFee, value.CreatorFee))
			if value.IsBuy && value.ETHGross.Cmp(fees) <= 0 {
				t.Errorf("buy ethGross %s does not exceed tax and fees %s", value.ETHGross, fees)
			}
			if !value.IsBuy && value.SnipeTax.Sign() != 0 {
				t.Errorf("sell carries snipe tax %s", value.SnipeTax)
			}
			if value.SnipeTax.Sign() > 0 {
				taxedBuys++
			}
		case V4Initialize:
			initialized[value.PoolID] = value
		case GraduatedV2:
			graduations = append(graduations, value)
		case V4Swap:
			if value.Amount0.Sign() == 0 || value.Amount0.Sign() == value.Amount1.Sign() {
				t.Errorf("swap deltas %s/%s are not opposite-signed", value.Amount0, value.Amount1)
			}
		}
	}
	if taxedBuys == 0 || len(graduations) == 0 {
		t.Fatalf("fixture lacks a taxed buy (%d) or a graduation (%d)", taxedBuys, len(graduations))
	}
	for _, graduation := range graduations {
		pool, ok := initialized[graduation.PoolID]
		if !ok {
			t.Fatalf("graduated pool %s was never initialized", graduation.PoolID)
		}
		if pool.Currency0 != (common.Address{}) || pool.Currency1 != graduation.Token || pool.Fee != 10000 || pool.TickSpacing != 200 || pool.Hooks != hooks[graduation.Token] {
			t.Errorf("pool key = %+v, want ETH/%s fee 10000 spacing 200 hook %s", pool, graduation.Token.Hex(), hooks[graduation.Token].Hex())
		}
		if pool.SqrtPriceX96.Cmp(graduation.SqrtPriceX96) != 0 {
			t.Errorf("initialize price %s differs from graduation price %s", pool.SqrtPriceX96, graduation.SqrtPriceX96)
		}
	}
}

func TestDecoderV2RejectsForeignAndMalformedLogs(t *testing.T) {
	t.Parallel()
	_, logs, emitters := loadFixtureLogsV2(t)
	decoder, err := NewDecoder(2)
	if err != nil {
		t.Fatal(err)
	}
	find := func(name string) types.Log {
		log := findFixture(t, decoder, logs, emitters, name)
		log.Topics = slices.Clone(log.Topics)
		log.Data = slices.Clone(log.Data)
		return log
	}

	swap := find("Swap")
	swap.Topics[1] = common.HexToHash("0xfeed")
	if _, err := decoder.Decode(swap, emitters); !errors.Is(err, ErrUnknownEmitter) {
		t.Errorf("swap on an unknown pool: error = %v, want ErrUnknownEmitter", err)
	}

	accepted := find("FeeRecipientAccepted")
	accepted.Topics[2][0] = 1
	if _, err := decoder.Decode(accepted, emitters); !errors.Is(err, ErrMalformedLog) {
		t.Errorf("non-padded indexed address: error = %v, want ErrMalformedLog", err)
	}

	collected := find("LpFeesCollected")
	for curve := range emitters.Curves {
		collected.Address = curve
		break
	}
	if _, err := decoder.Decode(collected, emitters); !errors.Is(err, ErrMalformedLog) {
		t.Errorf("locker event from a curve: error = %v, want ErrMalformedLog", err)
	}

	configured := find("EngineConfigured")
	configured.Topics[1] = common.BigToHash(big.NewInt(1))
	if _, err := decoder.Decode(configured, emitters); !errors.Is(err, ErrUnsupportedEngine) {
		t.Errorf("engine-1 configuration: error = %v, want ErrUnsupportedEngine", err)
	}

	_, v1Logs, v1Emitters := loadFixtureLogs(t)
	v1Decoder, err := NewDecoder(1)
	if err != nil {
		t.Fatal(err)
	}
	v1Launch := findFixture(t, v1Decoder, v1Logs, v1Emitters, "TokenLaunched")
	v1Launch.Address = emitters.Factory
	if _, err := decoder.Decode(v1Launch, emitters); !errors.Is(err, ErrUnknownTopic) {
		t.Errorf("v1 TokenLaunched: error = %v, want ErrUnknownTopic", err)
	}
}

func TestDecoderV2TopicsByEmitterKind(t *testing.T) {
	t.Parallel()
	decoder, err := NewDecoder(2)
	if err != nil {
		t.Fatal(err)
	}
	want := map[EmitterKind]int{EmitterFactory: 7, EmitterCurve: 9, EmitterToken: 1, EmitterLocker: 2, EmitterPoolManager: 3, EmitterPair: 0}
	for kind, count := range want {
		if got := len(decoder.Topics(kind)); got != count {
			t.Errorf("Topics(%d) = %d, want %d", kind, got, count)
		}
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}
