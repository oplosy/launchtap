package chain

import (
	"embed"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

//go:embed abi/v1/*.json abi/v2/*.json
var eventArtifacts embed.FS

type eventDefinition struct {
	event abi.Event
	kinds map[EmitterKind]struct{}
}

// Decoder is an immutable ABI registry selected by engine version.
type Decoder struct {
	engineVersion uint16
	events        map[common.Hash]eventDefinition
}

func NewDecoder(engineVersion uint16) (*Decoder, error) {
	switch engineVersion {
	case 1:
		return newDecoderV1()
	case 2:
		return newDecoderV2()
	default:
		return nil, &EngineVersionError{Version: engineVersion}
	}
}

func newDecoderV1() (*Decoder, error) {
	const engineVersion = 1
	launch, err := readABI("abi/v1/ILaunchEvents.json")
	if err != nil {
		return nil, err
	}
	pair, err := readABI("abi/v1/IUniswapV2PairEvents.json")
	if err != nil {
		return nil, err
	}
	token, err := readABI("abi/v1/LaunchToken.json")
	if err != nil {
		return nil, err
	}

	decoder := &Decoder{engineVersion: engineVersion, events: make(map[common.Hash]eventDefinition, 18)}
	factory := []string{"TokenLaunched", "LaunchFeesClaimed", "LaunchPauseSet", "TradingPauseSet", "EngineConfigured", "FutureDefaultsConfigured", "FutureTreasuryConfigured"}
	curve := []string{"Trade", "Graduated", "CreatorFeesClaimed", "ProtocolFeesClaimed", "RefundCredited", "RefundClaimed"}
	if err := decoder.add(launch, factory, EmitterFactory); err != nil {
		return nil, err
	}
	if err := decoder.add(launch, curve, EmitterCurve); err != nil {
		return nil, err
	}
	if err := decoder.add(token, []string{"Transfer"}, EmitterToken); err != nil {
		return nil, err
	}
	if err := decoder.add(pair, []string{"Mint", "Burn", "Swap", "Sync"}, EmitterPair); err != nil {
		return nil, err
	}
	if len(decoder.events) != 18 {
		return nil, fmt.Errorf("load event ABI registry: got %d topics, want 18", len(decoder.events))
	}
	return decoder, nil
}

// newDecoderV2 loads the frozen engine-2 ABIs. Topic ownership follows the emitter of every log
// in fixtures/v2: the curve emits the fee recipient events, the locker its fee events and the
// singleton PoolManager the pool events.
func newDecoderV2() (*Decoder, error) {
	const engineVersion = 2
	launch, err := readABI("abi/v2/ILaunchEventsV2.json")
	if err != nil {
		return nil, err
	}
	locker, err := readABI("abi/v2/ILpFeeLockerEvents.json")
	if err != nil {
		return nil, err
	}
	poolManager, err := readABI("abi/v2/IPoolManagerEvents.json")
	if err != nil {
		return nil, err
	}
	token, err := readABI("abi/v2/LaunchTokenV2.json")
	if err != nil {
		return nil, err
	}

	decoder := &Decoder{engineVersion: engineVersion, events: make(map[common.Hash]eventDefinition, 22)}
	factory := []string{"TokenLaunched", "LaunchFeesClaimed", "LaunchPauseSet", "TradingPauseSet", "EngineConfigured", "FutureDefaultsConfigured", "FutureTreasuryConfigured"}
	curve := []string{"Trade", "Graduated", "CreatorFeesClaimed", "ProtocolFeesClaimed", "RefundCredited", "RefundClaimed", "SnipeTaxClaimed", "FeeRecipientProposed", "FeeRecipientAccepted"}
	for _, group := range []struct {
		parsed abi.ABI
		names  []string
		kind   EmitterKind
	}{
		{launch, factory, EmitterFactory},
		{launch, curve, EmitterCurve},
		{token, []string{"Transfer"}, EmitterToken},
		{locker, []string{"LpFeesCollected", "LpFeesClaimed"}, EmitterLocker},
		{poolManager, []string{"Initialize", "ModifyLiquidity", "Swap"}, EmitterPoolManager},
	} {
		if err := decoder.add(group.parsed, group.names, group.kind); err != nil {
			return nil, err
		}
	}
	if len(decoder.events) != 22 {
		return nil, fmt.Errorf("load engine-2 event ABI registry: got %d topics, want 22", len(decoder.events))
	}
	return decoder, nil
}

func readABI(path string) (abi.ABI, error) {
	body, err := eventArtifacts.ReadFile(path)
	if err != nil {
		return abi.ABI{}, fmt.Errorf("read %s: %w", path, err)
	}
	parsed, err := abi.JSON(strings.NewReader(string(body)))
	if err != nil {
		return abi.ABI{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return parsed, nil
}

func (d *Decoder) add(parsed abi.ABI, names []string, kind EmitterKind) error {
	for _, name := range names {
		event, ok := parsed.Events[name]
		if !ok {
			return fmt.Errorf("event ABI is missing %s", name)
		}
		definition, exists := d.events[event.ID]
		if exists {
			definition.kinds[kind] = struct{}{}
			d.events[event.ID] = definition
			continue
		}
		d.events[event.ID] = eventDefinition{event: event, kinds: map[EmitterKind]struct{}{kind: {}}}
	}
	return nil
}

func coordinates(log types.Log) LogCoordinates {
	return LogCoordinates{BlockNumber: log.BlockNumber, BlockHash: log.BlockHash, TransactionIndex: log.TxIndex, TransactionHash: log.TxHash, LogIndex: log.Index}
}

func (d *Decoder) Decode(log types.Log, emitters Emitters) (DecodedLog, error) {
	coordinate := coordinates(log)
	kind, ok := emitters.Kind(log.Address)
	if !ok {
		return DecodedLog{}, &EmitterError{Address: log.Address, Coordinates: coordinate}
	}
	if len(log.Topics) == 0 {
		return DecodedLog{}, &LogError{Coordinates: coordinate, Err: fmt.Errorf("%w: missing topic0", ErrMalformedLog)}
	}
	definition, ok := d.events[log.Topics[0]]
	if !ok {
		return DecodedLog{}, &LogError{Coordinates: coordinate, Err: fmt.Errorf("%w: %s", ErrUnknownTopic, log.Topics[0])}
	}
	if _, allowed := definition.kinds[kind]; !allowed {
		return DecodedLog{}, &LogError{Coordinates: coordinate, Err: fmt.Errorf("%w: event %s is not valid for emitter kind %d", ErrMalformedLog, definition.event.Name, kind)}
	}

	indexed := indexedArguments(definition.event.Inputs)
	if len(log.Topics) != len(indexed)+1 {
		return DecodedLog{}, &LogError{Coordinates: coordinate, Err: fmt.Errorf("%w: event %s has %d topics, want %d", ErrMalformedLog, definition.event.Name, len(log.Topics), len(indexed)+1)}
	}
	// Every PoolManager event indexes the pool id first. A pool outside the known set is not ours.
	if kind == EmitterPoolManager {
		if _, known := emitters.Pools[log.Topics[1]]; !known {
			return DecodedLog{}, &EmitterError{Address: log.Address, Coordinates: coordinate}
		}
	}
	for index, argument := range indexed {
		if argument.Type.T == abi.AddressTy && !isPaddedAddress(log.Topics[index+1]) {
			return DecodedLog{}, &LogError{Coordinates: coordinate, Err: fmt.Errorf("%w: event %s indexed address %s is not zero-padded", ErrMalformedLog, definition.event.Name, argument.Name)}
		}
	}
	values := make(map[string]any, len(definition.event.Inputs))
	if argument, ok := invalidDataAddressPadding(definition.event.Inputs.NonIndexed(), log.Data); ok {
		return DecodedLog{}, &LogError{Coordinates: coordinate, Err: fmt.Errorf("%w: event %s address %s is not zero-padded", ErrMalformedLog, definition.event.Name, argument)}
	}
	if err := definition.event.Inputs.NonIndexed().UnpackIntoMap(values, log.Data); err != nil {
		return DecodedLog{}, &LogError{Coordinates: coordinate, Err: fmt.Errorf("%w: unpack %s data: %v", ErrMalformedLog, definition.event.Name, err)}
	}
	if err := abi.ParseTopicsIntoMap(values, indexed, log.Topics[1:]); err != nil {
		return DecodedLog{}, &LogError{Coordinates: coordinate, Err: fmt.Errorf("%w: unpack %s topics: %v", ErrMalformedLog, definition.event.Name, err)}
	}
	typed := typedEvent
	if d.engineVersion == 2 {
		typed = typedEventV2
	}
	value, err := typed(definition.event.Name, log.Address, values)
	if err != nil {
		return DecodedLog{}, &LogError{Coordinates: coordinate, Err: fmt.Errorf("%w: %v", ErrMalformedLog, err)}
	}
	if launched, ok := value.(TokenLaunched); ok && launched.EngineVersion != d.engineVersion {
		return DecodedLog{}, &EngineVersionError{Version: launched.EngineVersion}
	}
	if launched, ok := value.(TokenLaunchedV2); ok && launched.EngineVersion != d.engineVersion {
		return DecodedLog{}, &EngineVersionError{Version: launched.EngineVersion}
	}
	if configured, ok := value.(EngineConfigured); ok && configured.EngineVersion != d.engineVersion {
		return DecodedLog{}, &EngineVersionError{Version: configured.EngineVersion}
	}
	return DecodedLog{Coordinates: coordinate, Emitter: log.Address, Kind: kind, Name: definition.event.Name, Value: value}, nil
}

func invalidDataAddressPadding(arguments abi.Arguments, data []byte) (string, bool) {
	for index, argument := range arguments {
		if argument.Type.T != abi.AddressTy {
			continue
		}
		offset := index * 32
		if offset+32 > len(data) {
			continue
		}
		for _, value := range data[offset : offset+12] {
			if value != 0 {
				return argument.Name, true
			}
		}
	}
	return "", false
}

func indexedArguments(arguments abi.Arguments) abi.Arguments {
	result := make(abi.Arguments, 0, len(arguments))
	for _, argument := range arguments {
		if argument.Indexed {
			result = append(result, argument)
		}
	}
	return result
}

func isPaddedAddress(topic common.Hash) bool {
	for _, value := range topic[:12] {
		if value != 0 {
			return false
		}
	}
	return true
}

func typedEvent(name string, emitter common.Address, v map[string]any) (any, error) {
	switch name {
	case "TokenLaunched":
		return TokenLaunched{Token: addr(v, "token"), Curve: addr(v, "curve"), Creator: addr(v, "creator"), LPPair: addr(v, "lpPair"), WETH: addr(v, "weth"), ProtocolTreasury: addr(v, "protocolTreasury"), EngineVersion: u16(v, "engineVersion"), Name: text(v, "name"), Symbol: text(v, "symbol"), TotalSupply: integer(v, "totalSupply"), VirtualETH: integer(v, "virtualEth"), VirtualToken: integer(v, "virtualToken"), CurveTokens: integer(v, "curveTokens"), LPTokens: integer(v, "lpTokens"), GraduationETH: integer(v, "graduationEth"), LaunchFeePaid: integer(v, "launchFeePaid"), TradeFeeBPS: u16(v, "tradeFeeBps"), ProtocolShareBPS: u16(v, "protocolShareBps")}, nil
	case "Trade":
		return Trade{Token: addr(v, "token"), Trader: addr(v, "trader"), IsBuy: boolean(v, "isBuy"), ETHGross: integer(v, "ethGross"), ETHRefund: integer(v, "ethRefund"), TokenAmount: integer(v, "tokenAmount"), ProtocolFee: integer(v, "protocolFee"), CreatorFee: integer(v, "creatorFee"), NewETHReserve: integer(v, "newEthReserve"), NewTokenReserve: integer(v, "newTokenReserve")}, nil
	case "Graduated":
		return Graduated{addr(v, "token"), addr(v, "lpPair"), integer(v, "ethToPool"), integer(v, "tokensToPool"), integer(v, "lpLiquidityBurned")}, nil
	case "CreatorFeesClaimed":
		return CreatorFeesClaimed{addr(v, "token"), addr(v, "creator"), integer(v, "amount")}, nil
	case "ProtocolFeesClaimed":
		return ProtocolFeesClaimed{addr(v, "token"), addr(v, "treasury"), integer(v, "amount")}, nil
	case "LaunchFeesClaimed":
		return LaunchFeesClaimed{addr(v, "treasury"), integer(v, "amount")}, nil
	case "RefundCredited":
		return RefundCredited{addr(v, "token"), addr(v, "account"), integer(v, "amount")}, nil
	case "RefundClaimed":
		return RefundClaimed{addr(v, "token"), addr(v, "account"), integer(v, "amount")}, nil
	case "LaunchPauseSet":
		return LaunchPauseSet{boolean(v, "paused")}, nil
	case "TradingPauseSet":
		return TradingPauseSet{boolean(v, "paused")}, nil
	case "EngineConfigured":
		return EngineConfigured{u16(v, "engineVersion"), addr(v, "implementation"), boolean(v, "enabled")}, nil
	case "FutureDefaultsConfigured":
		return FutureDefaultsConfigured{hash(v, "configHash")}, nil
	case "FutureTreasuryConfigured":
		return FutureTreasuryConfigured{addr(v, "previousTreasury"), addr(v, "newTreasury")}, nil
	case "Transfer":
		return Transfer{emitter, addr(v, "from"), addr(v, "to"), integer(v, "value")}, nil
	case "Mint":
		return PoolMint{emitter, addr(v, "sender"), integer(v, "amount0"), integer(v, "amount1")}, nil
	case "Burn":
		return PoolBurn{emitter, addr(v, "sender"), integer(v, "amount0"), integer(v, "amount1"), addr(v, "to")}, nil
	case "Swap":
		return PoolSwap{emitter, addr(v, "sender"), integer(v, "amount0In"), integer(v, "amount1In"), integer(v, "amount0Out"), integer(v, "amount1Out"), addr(v, "to"), nil}, nil
	case "Sync":
		return PoolSync{emitter, integer(v, "reserve0"), integer(v, "reserve1")}, nil
	default:
		return nil, fmt.Errorf("unknown decoded event %s", name)
	}
}

// typedEventV2 builds engine-2 values. Events with a v1-identical ABI reuse typedEvent.
func typedEventV2(name string, emitter common.Address, v map[string]any) (any, error) {
	switch name {
	case "TokenLaunched":
		return TokenLaunchedV2{Token: addr(v, "token"), Curve: addr(v, "curve"), Creator: addr(v, "creator"), ProtocolTreasury: addr(v, "protocolTreasury"), EngineVersion: u16(v, "engineVersion"), Name: text(v, "name"), Symbol: text(v, "symbol"), TotalSupply: integer(v, "totalSupply"), VirtualETH: integer(v, "virtualEth"), VirtualToken: integer(v, "virtualToken"), CurveTokens: integer(v, "curveTokens"), LPTokens: integer(v, "lpTokens"), GraduationETH: integer(v, "graduationEth"), LaunchFeePaid: integer(v, "launchFeePaid"), TradeFeeBPS: u16(v, "tradeFeeBps"), ProtocolShareBPS: u16(v, "protocolShareBps"), Hook: addr(v, "hook"), Locker: addr(v, "locker"), AntiSnipeStartBPS: u16(v, "antiSnipeStartBps"), AntiSnipeDurationSeconds: u32(v, "antiSnipeDurationSeconds"), LPCreatorShareBPS: u16(v, "lpCreatorShareBps")}, nil
	case "Trade":
		return TradeV2{Token: addr(v, "token"), Trader: addr(v, "trader"), IsBuy: boolean(v, "isBuy"), ETHGross: integer(v, "ethGross"), ETHRefund: integer(v, "ethRefund"), TokenAmount: integer(v, "tokenAmount"), ProtocolFee: integer(v, "protocolFee"), CreatorFee: integer(v, "creatorFee"), NewETHReserve: integer(v, "newEthReserve"), NewTokenReserve: integer(v, "newTokenReserve"), SnipeTax: integer(v, "snipeTax")}, nil
	case "Graduated":
		return GraduatedV2{Token: addr(v, "token"), PoolID: hash(v, "poolId"), PositionTokenID: integer(v, "positionTokenId"), ETHToPool: integer(v, "ethToPool"), TokensToPool: integer(v, "tokensToPool"), Liquidity: integer(v, "liquidity"), SqrtPriceX96: integer(v, "sqrtPriceX96"), ETHRemainder: integer(v, "ethRemainder"), TokenRemainder: integer(v, "tokenRemainder")}, nil
	case "CreatorFeesClaimed":
		return CreatorFeesClaimedV2{addr(v, "token"), addr(v, "account"), integer(v, "amount")}, nil
	case "SnipeTaxClaimed":
		return SnipeTaxClaimed{addr(v, "token"), addr(v, "treasury"), integer(v, "amount")}, nil
	case "FeeRecipientProposed":
		return FeeRecipientProposed{addr(v, "token"), addr(v, "current"), addr(v, "next")}, nil
	case "FeeRecipientAccepted":
		return FeeRecipientAccepted{addr(v, "token"), addr(v, "previous"), addr(v, "next")}, nil
	case "LpFeesCollected":
		return LpFeesCollected{addr(v, "token"), integer(v, "ethAmount"), integer(v, "tokenAmount")}, nil
	case "LpFeesClaimed":
		return LpFeesClaimed{addr(v, "token"), addr(v, "recipient"), integer(v, "ethAmount"), integer(v, "tokenAmount")}, nil
	case "Initialize":
		fee, err := smallUint(v, "fee", 24)
		if err != nil {
			return nil, err
		}
		spacing, err := smallInt(v, "tickSpacing", 24)
		if err != nil {
			return nil, err
		}
		tick, err := smallInt(v, "tick", 24)
		if err != nil {
			return nil, err
		}
		return V4Initialize{PoolID: hash(v, "id"), Currency0: addr(v, "currency0"), Currency1: addr(v, "currency1"), Fee: fee, TickSpacing: spacing, Hooks: addr(v, "hooks"), SqrtPriceX96: integer(v, "sqrtPriceX96"), Tick: tick}, nil
	case "ModifyLiquidity":
		lower, err := smallInt(v, "tickLower", 24)
		if err != nil {
			return nil, err
		}
		upper, err := smallInt(v, "tickUpper", 24)
		if err != nil {
			return nil, err
		}
		return V4ModifyLiquidity{PoolID: hash(v, "id"), Sender: addr(v, "sender"), TickLower: lower, TickUpper: upper, LiquidityDelta: integer(v, "liquidityDelta"), Salt: hash(v, "salt")}, nil
	case "Swap":
		tick, err := smallInt(v, "tick", 24)
		if err != nil {
			return nil, err
		}
		fee, err := smallUint(v, "fee", 24)
		if err != nil {
			return nil, err
		}
		return V4Swap{PoolID: hash(v, "id"), Sender: addr(v, "sender"), Amount0: integer(v, "amount0"), Amount1: integer(v, "amount1"), SqrtPriceX96: integer(v, "sqrtPriceX96"), Liquidity: integer(v, "liquidity"), Tick: tick, Fee: fee}, nil
	case "LaunchFeesClaimed", "ProtocolFeesClaimed", "RefundCredited", "RefundClaimed", "LaunchPauseSet", "TradingPauseSet", "EngineConfigured", "FutureDefaultsConfigured", "FutureTreasuryConfigured", "Transfer":
		return typedEvent(name, emitter, v)
	default:
		return nil, fmt.Errorf("unknown decoded engine-2 event %s", name)
	}
}

// smallUint and smallInt narrow the ABI's *big.Int for uint24/int24 fields, which go-ethereum
// does not map to native integers. The ABI decoder already range-checks the width; this
// rechecks it so a narrowing can never wrap.
func smallUint(v map[string]any, name string, bits uint) (uint32, error) {
	value := v[name].(*big.Int)
	if value.Sign() < 0 || value.BitLen() > int(bits) {
		return 0, fmt.Errorf("%s %s exceeds uint%d", name, value, bits)
	}
	return uint32(value.Uint64()), nil
}

func smallInt(v map[string]any, name string, bits uint) (int32, error) {
	value := v[name].(*big.Int)
	limit := new(big.Int).Lsh(big.NewInt(1), bits-1)
	if value.Cmp(limit) >= 0 || value.Cmp(new(big.Int).Neg(limit)) < 0 {
		return 0, fmt.Errorf("%s %s exceeds int%d", name, value, bits)
	}
	return int32(value.Int64()), nil
}

func u32(v map[string]any, name string) uint32 { return v[name].(uint32) }

func addr(v map[string]any, name string) common.Address { return v[name].(common.Address) }
func boolean(v map[string]any, name string) bool        { return v[name].(bool) }
func text(v map[string]any, name string) string         { return v[name].(string) }
func u16(v map[string]any, name string) uint16          { return v[name].(uint16) }
func integer(v map[string]any, name string) *big.Int    { return new(big.Int).Set(v[name].(*big.Int)) }
func hash(v map[string]any, name string) common.Hash {
	raw := v[name].([32]byte)
	return common.Hash(raw)
}
