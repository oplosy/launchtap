package chain

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// v2DiscoveryLogs picks one log of each kind discovery must route from the v2 fixture.
type v2DiscoveryLogs struct {
	decoder                                *Decoder
	emitters                               Emitters
	launch                                 TokenLaunchedV2
	launchLog, graduatedLog, initializeLog types.Log
	swapLog, lockerLog                     types.Log
	poolID                                 common.Hash
}

func loadV2DiscoveryLogs(t *testing.T) v2DiscoveryLogs {
	t.Helper()
	_, logs, emitters := loadFixtureLogsV2(t)
	decoder, err := NewDecoder(2)
	if err != nil {
		t.Fatal(err)
	}
	var result v2DiscoveryLogs
	result.decoder, result.emitters = decoder, emitters
	for _, log := range logs {
		decoded, err := decoder.Decode(log, emitters)
		if err != nil {
			t.Fatal(err)
		}
		switch value := decoded.Value.(type) {
		case TokenLaunchedV2:
			if result.launchLog.Address == (common.Address{}) {
				result.launch, result.launchLog = value, log
			}
		case GraduatedV2:
			if result.graduatedLog.Address == (common.Address{}) && value.Token == result.launch.Token {
				result.graduatedLog, result.poolID = log, value.PoolID
			}
		}
	}
	for _, log := range logs {
		decoded, _ := decoder.Decode(log, emitters)
		switch value := decoded.Value.(type) {
		case V4Initialize:
			if value.PoolID == result.poolID && result.initializeLog.Address == (common.Address{}) {
				result.initializeLog = log
			}
		case V4Swap:
			if value.PoolID == result.poolID && result.swapLog.Address == (common.Address{}) {
				result.swapLog = log
			}
		case LpFeesCollected, LpFeesClaimed:
			if result.lockerLog.Address == (common.Address{}) {
				result.lockerLog = log
			}
		}
	}
	for name, log := range map[string]types.Log{"launch": result.launchLog, "graduated": result.graduatedLog, "initialize": result.initializeLog, "swap": result.swapLog, "locker": result.lockerLog} {
		if log.Address == (common.Address{}) {
			t.Fatalf("v2 fixture has no %s log for the first launch", name)
		}
	}
	return result
}

func (f v2DiscoveryLogs) config() V4Config {
	return V4Config{PoolManager: f.emitters.PoolManager, Locker: f.emitters.Locker, Hook: f.launch.Hook}
}

func at(log types.Log, block uint64, txIndex, index uint) types.Log {
	log.BlockNumber, log.TxIndex, log.Index = block, txIndex, index
	log.BlockHash = common.BigToHash(common.Big1)
	log.TxHash = common.BigToHash(common.Big2)
	return log
}

func TestV4PoolIDMatchesFixtureGraduation(t *testing.T) {
	t.Parallel()
	fixture := loadV2DiscoveryLogs(t)
	if got := V4PoolID(fixture.launch.Token, fixture.launch.Hook); got != fixture.poolID {
		t.Fatalf("V4PoolID = %s, want fixture poolId %s", got.Hex(), fixture.poolID.Hex())
	}
}

func TestDiscovererV2IncludesSameBlockSwapAfterGraduation(t *testing.T) {
	t.Parallel()
	fixture := loadV2DiscoveryLogs(t)
	// The pool is created and initialized inside the graduation transaction, before Graduated;
	// a router swap follows in a later transaction of the same block.
	logs := []types.Log{
		at(fixture.launchLog, 10, 0, 0),
		at(fixture.initializeLog, 12, 1, 3),
		at(fixture.graduatedLog, 12, 1, 5),
		at(fixture.swapLog, 12, 2, 0),
		at(fixture.lockerLog, 12, 3, 0),
	}
	discoverer, err := NewDiscovererV2(&fixtureLogSource{logs: logs}, fixture.decoder, fixture.emitters.Factory, 10, fixture.config())
	if err != nil {
		t.Fatal(err)
	}
	result, err := discoverer.Discover(context.Background(), 10, 12, emptyEmitters(fixture.emitters.Factory))
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	for _, want := range []types.Log{logs[1], logs[3], logs[4]} {
		if !slices.ContainsFunc(result.Logs, func(log types.Log) bool { return sameLog(log, want) }) {
			t.Fatalf("discovered logs miss %s log at block %d index %d", want.Address.Hex(), want.BlockNumber, want.Index)
		}
	}
	if token := result.Emitters.Pools[fixture.poolID]; token != fixture.launch.Token {
		t.Fatalf("registered pool token = %s, want %s", token.Hex(), fixture.launch.Token.Hex())
	}
	if len(result.LaunchesV2) != 1 || result.LaunchesV2[0].Token != fixture.launch.Token {
		t.Fatalf("v2 launches = %+v, want the fixture launch", result.LaunchesV2)
	}
	if result.Emitters.PoolManager != fixture.emitters.PoolManager || result.Emitters.Locker != fixture.emitters.Locker {
		t.Fatal("V4 emitters were not set from the manifest")
	}
}

func TestDiscovererV2ExcludesSwapOnForeignHookPool(t *testing.T) {
	t.Parallel()
	fixture := loadV2DiscoveryLogs(t)
	// Same token, different hook: a different pool key, so a different pool id.
	foreign := at(fixture.swapLog, 12, 2, 1)
	foreign.Topics = slices.Clone(foreign.Topics)
	foreign.Topics[1] = V4PoolID(fixture.launch.Token, common.HexToAddress("0xbad0000000000000000000000000000000000080"))
	known := emptyEmitters(fixture.emitters.Factory)
	known.Curves[fixture.launch.Curve] = struct{}{}
	known.Tokens[fixture.launch.Token] = struct{}{}
	known.Pools[fixture.poolID] = fixture.launch.Token
	own := at(fixture.swapLog, 12, 2, 0)
	discoverer, err := NewDiscovererV2(&fixtureLogSource{logs: []types.Log{own, foreign}}, fixture.decoder, fixture.emitters.Factory, 10, fixture.config())
	if err != nil {
		t.Fatal(err)
	}
	result, err := discoverer.Discover(context.Background(), 12, 12, known)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(result.Logs) != 1 || !sameLog(result.Logs[0], own) {
		t.Fatalf("logs = %+v, want only the own pool's swap", result.Logs)
	}
}

func TestDiscovererV2RejectsProviderPoolLogOutsideRequestedPools(t *testing.T) {
	t.Parallel()
	fixture := loadV2DiscoveryLogs(t)
	rogue := at(fixture.swapLog, 12, 2, 1)
	rogue.Topics = slices.Clone(rogue.Topics)
	rogue.Topics[1] = common.HexToHash("0xbeef")
	known := emptyEmitters(fixture.emitters.Factory)
	known.Pools[fixture.poolID] = fixture.launch.Token
	discoverer, err := NewDiscovererV2(&poolIgnoringSource{logs: []types.Log{rogue}, poolManager: fixture.emitters.PoolManager}, fixture.decoder, fixture.emitters.Factory, 10, fixture.config())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := discoverer.Discover(context.Background(), 12, 12, known); !errors.Is(err, ErrUnknownEmitter) {
		t.Fatalf("Discover() error = %v, want ErrUnknownEmitter", err)
	}
}

func TestDiscovererV2RejectsGraduationWithUnexpectedPoolID(t *testing.T) {
	t.Parallel()
	fixture := loadV2DiscoveryLogs(t)
	graduated := at(fixture.graduatedLog, 12, 1, 5)
	graduated.Topics = slices.Clone(graduated.Topics)
	graduated.Topics[2] = common.HexToHash("0x0bad")
	known := emptyEmitters(fixture.emitters.Factory)
	known.Curves[fixture.launch.Curve] = struct{}{}
	known.Tokens[fixture.launch.Token] = struct{}{}
	discoverer, err := NewDiscovererV2(&fixtureLogSource{logs: []types.Log{graduated}}, fixture.decoder, fixture.emitters.Factory, 10, fixture.config())
	if err != nil {
		t.Fatal(err)
	}
	result, err := discoverer.Discover(context.Background(), 12, 12, known)
	if !errors.Is(err, ErrPoolMismatch) {
		t.Fatalf("Discover() error = %v, want ErrPoolMismatch", err)
	}
	if len(result.Logs) != 0 {
		t.Fatal("failed discovery returned canonical events")
	}
}

func TestDiscovererV2RejectsLaunchWithForeignHook(t *testing.T) {
	t.Parallel()
	fixture := loadV2DiscoveryLogs(t)
	config := fixture.config()
	config.Hook = common.HexToAddress("0xbad0000000000000000000000000000000000080")
	discoverer, err := NewDiscovererV2(&fixtureLogSource{logs: []types.Log{at(fixture.launchLog, 10, 0, 0)}}, fixture.decoder, fixture.emitters.Factory, 10, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := discoverer.Discover(context.Background(), 10, 10, emptyEmitters(fixture.emitters.Factory)); !errors.Is(err, ErrLaunchMismatch) {
		t.Fatalf("Discover() error = %v, want ErrLaunchMismatch", err)
	}
}

func TestVerifyLaunchV2RejectsManifestAndCapMismatches(t *testing.T) {
	t.Parallel()
	fixture := loadV2DiscoveryLogs(t)
	config := fixture.config()
	if err := VerifyLaunchV2(fixture.launch, config); err != nil {
		t.Fatalf("fixture launch rejected: %v", err)
	}
	for name, mutate := range map[string]func(*TokenLaunchedV2){
		"locker":     func(launch *TokenLaunchedV2) { launch.Locker = common.HexToAddress("0x1") },
		"hook":       func(launch *TokenLaunchedV2) { launch.Hook = common.HexToAddress("0x1") },
		"start tax":  func(launch *TokenLaunchedV2) { launch.AntiSnipeStartBPS = 9001 },
		"duration":   func(launch *TokenLaunchedV2) { launch.AntiSnipeDurationSeconds = 301 },
		"lp share":   func(launch *TokenLaunchedV2) { launch.LPCreatorShareBPS = 10001 },
		"engine":     func(launch *TokenLaunchedV2) { launch.EngineVersion = 1 },
		"zero token": func(launch *TokenLaunchedV2) { launch.Token = common.Address{} },
	} {
		launch := fixture.launch
		mutate(&launch)
		if err := VerifyLaunchV2(launch, config); !errors.Is(err, ErrLaunchMismatch) {
			t.Errorf("%s: VerifyLaunchV2 error = %v, want ErrLaunchMismatch", name, err)
		}
	}
}

func TestDiscovererV2BatchesPoolIDs(t *testing.T) {
	t.Parallel()
	fixture := loadV2DiscoveryLogs(t)
	known := emptyEmitters(fixture.emitters.Factory)
	known.Pools[fixture.poolID] = fixture.launch.Token
	known.Pools[common.HexToHash("0x01")] = common.HexToAddress("0x01")
	known.Pools[common.HexToHash("0x02")] = common.HexToAddress("0x02")
	own := at(fixture.swapLog, 12, 2, 0)
	source := &recordingLogSource{fixtureLogSource: fixtureLogSource{logs: []types.Log{own}}}
	discoverer, err := NewDiscovererV2(source, fixture.decoder, fixture.emitters.Factory, 1, fixture.config())
	if err != nil {
		t.Fatal(err)
	}
	result, err := discoverer.Discover(context.Background(), 12, 12, known)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(result.Logs) != 1 || !sameLog(result.Logs[0], own) {
		t.Fatalf("logs = %+v, want the own pool's swap", result.Logs)
	}
	poolQueries := 0
	for _, query := range source.queries {
		if slices.Contains(query.Addresses, fixture.emitters.PoolManager) {
			poolQueries++
			if len(query.Topics) != 2 || len(query.Topics[1]) != 1 {
				t.Fatalf("pool query topics = %v, want one pool id per batch", query.Topics)
			}
		}
	}
	if poolQueries != 3 {
		t.Fatalf("pool queries = %d, want 3 batches of one pool id", poolQueries)
	}
}

func TestNewDiscovererV2RequiresEngineTwoDecoderAndV4Addresses(t *testing.T) {
	t.Parallel()
	fixture := loadV2DiscoveryLogs(t)
	v1, _ := NewDecoder(1)
	if _, err := NewDiscovererV2(&fixtureLogSource{}, v1, fixture.emitters.Factory, 10, fixture.config()); err == nil {
		t.Fatal("engine-1 decoder accepted")
	}
	config := fixture.config()
	config.Locker = common.Address{}
	if _, err := NewDiscovererV2(&fixtureLogSource{}, fixture.decoder, fixture.emitters.Factory, 10, config); err == nil {
		t.Fatal("zero locker accepted")
	}
}

// poolIgnoringSource returns every PoolManager log regardless of the requested pool ids, like a
// provider that drops the topic1 filter.
type poolIgnoringSource struct {
	logs        []types.Log
	poolManager common.Address
}

func (s *poolIgnoringSource) FilterLogs(_ context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	if slices.Contains(query.Addresses, s.poolManager) {
		return s.logs, nil
	}
	return nil, nil
}
