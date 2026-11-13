package chain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Pool key constants of every engine-2 graduation pool (spec §4). They come from the spec and
// the manifest, never from a log.
const (
	V4PoolFee         = 10_000
	V4PoolTickSpacing = 200
	bpsDenominator    = 10_000
	// The chain boundary may not import internal/curve, so the factory hard caps are restated.
	// mirrors LaunchTypesV2.MAX_ANTI_SNIPE_START_BPS and MAX_ANTI_SNIPE_DURATION_SECONDS
	maxAntiSnipeStartBPS        = 9_000
	maxAntiSnipeDurationSeconds = 300
)

// V4Config is the manifest's Uniswap V4 wiring an engine-2 discoverer trusts.
type V4Config struct {
	PoolManager common.Address
	Locker      common.Address
	Hook        common.Address
}

// V4PoolID derives a launch's pool id: keccak256(abi.encode(PoolKey)) with currency0 = native
// ETH (the zero address), currency1 = token, fee 10000, tickSpacing 200 and the launch hook.
// mirrors PoolIdLibrary.toId
func V4PoolID(token, hook common.Address) common.Hash {
	var encoded [5 * 32]byte
	copy(encoded[32+12:64], token[:])
	big.NewInt(V4PoolFee).FillBytes(encoded[64:96])
	// tickSpacing is a positive int24, so its two's-complement word is plain big-endian.
	big.NewInt(V4PoolTickSpacing).FillBytes(encoded[96:128])
	copy(encoded[128+12:], hook[:])
	return crypto.Keccak256Hash(encoded[:])
}

// VerifyLaunchV2 fails closed when a TokenLaunched log disagrees with the manifest's V4 wiring
// or carries a profile outside the factory's hard caps (LaunchTypesV2 and BondingCurveV2).
func VerifyLaunchV2(launch TokenLaunchedV2, config V4Config) error {
	switch {
	case launch.EngineVersion != 2:
		return fmt.Errorf("%w: token %s reports engine %d", ErrLaunchMismatch, launch.Token.Hex(), launch.EngineVersion)
	case launch.Token == (common.Address{}) || launch.Curve == (common.Address{}):
		return fmt.Errorf("%w: launch has a zero token or curve", ErrLaunchMismatch)
	case launch.Hook != config.Hook:
		return fmt.Errorf("%w: token %s hook is %s, want %s", ErrLaunchMismatch, launch.Token.Hex(), launch.Hook.Hex(), config.Hook.Hex())
	case launch.Locker != config.Locker:
		return fmt.Errorf("%w: token %s locker is %s, want %s", ErrLaunchMismatch, launch.Token.Hex(), launch.Locker.Hex(), config.Locker.Hex())
	case launch.AntiSnipeStartBPS > maxAntiSnipeStartBPS:
		return fmt.Errorf("%w: token %s anti-snipe start %d bps exceeds %d", ErrLaunchMismatch, launch.Token.Hex(), launch.AntiSnipeStartBPS, maxAntiSnipeStartBPS)
	case launch.AntiSnipeDurationSeconds > maxAntiSnipeDurationSeconds:
		return fmt.Errorf("%w: token %s anti-snipe duration %ds exceeds %ds", ErrLaunchMismatch, launch.Token.Hex(), launch.AntiSnipeDurationSeconds, maxAntiSnipeDurationSeconds)
	case launch.LPCreatorShareBPS > bpsDenominator:
		return fmt.Errorf("%w: token %s LP creator share %d bps exceeds %d", ErrLaunchMismatch, launch.Token.Hex(), launch.LPCreatorShareBPS, bpsDenominator)
	}
	return nil
}

// NewDiscovererV2 builds an engine-2 discoverer. Launches are verified against the manifest
// without an RPC call: the hook and locker are manifest addresses and the caps are constants.
func NewDiscovererV2(source LogSource, decoder *Decoder, factory common.Address, addressBatchSize uint64, config V4Config) (*Discoverer, error) {
	if decoder == nil || decoder.engineVersion != 2 {
		return nil, errors.New("engine-2 discovery requires an engine-2 decoder")
	}
	if config.PoolManager == (common.Address{}) || config.Locker == (common.Address{}) || config.Hook == (common.Address{}) {
		return nil, errors.New("engine-2 discovery requires the PoolManager, locker and hook addresses")
	}
	discoverer, err := NewDiscoverer(source, decoder, factory, addressBatchSize)
	if err != nil {
		return nil, err
	}
	discoverer.v4 = &config
	return discoverer, nil
}

// discoverV2 mirrors Discover for engine 2. Curves are fetched by topic as in v1; launch tokens
// are address-filtered; the locker is address-filtered because it emits for every v2 token; the
// singleton PoolManager is filtered by address and by pool id (topic1), because it emits for
// every pool on the chain. A pool becomes known at its verified Graduated log and is refetched
// from that block, so a swap later in the graduation block is kept.
func (d *Discoverer) discoverV2(ctx context.Context, from, to uint64, known Emitters) (DiscoveryResult, error) {
	emitters := copyEmitters(known)
	if emitters.Factory != (common.Address{}) && emitters.Factory != d.factory {
		return DiscoveryResult{}, errors.New("known factory differs from discoverer factory")
	}
	if (known.Locker != (common.Address{}) && known.Locker != d.v4.Locker) || (known.PoolManager != (common.Address{}) && known.PoolManager != d.v4.PoolManager) {
		return DiscoveryResult{}, errors.New("known V4 emitters differ from the discoverer's manifest")
	}
	emitters.Factory, emitters.Locker, emitters.PoolManager = d.factory, d.v4.Locker, d.v4.PoolManager

	factoryLogs, err := d.fetch(ctx, from, to, []common.Address{d.factory}, d.decoder.Topics(EmitterFactory))
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("fetch factory logs: %w", err)
	}
	// Decode each log once even when the provider repeats it.
	if factoryLogs, err = deduplicateAndSort(factoryLogs); err != nil {
		return DiscoveryResult{}, err
	}
	allLogs := append([]types.Log(nil), factoryLogs...)
	launches := make([]TokenLaunchedV2, 0)
	freshTokens := make(map[uint64]AddressSet)
	for _, log := range factoryLogs {
		decoded, decodeErr := d.decoder.Decode(log, emitters)
		if decodeErr != nil {
			return DiscoveryResult{}, decodeErr
		}
		launch, ok := decoded.Value.(TokenLaunchedV2)
		if !ok {
			continue
		}
		if verifyErr := VerifyLaunchV2(launch, *d.v4); verifyErr != nil {
			return DiscoveryResult{}, fmt.Errorf("verify discovered launch at block %d: %w", log.BlockNumber, verifyErr)
		}
		launches = append(launches, launch)
		emitters.Curves[launch.Curve] = struct{}{}
		emitters.Tokens[launch.Token] = struct{}{}
		if freshTokens[log.BlockNumber] == nil {
			freshTokens[log.BlockNumber] = NewAddressSet()
		}
		freshTokens[log.BlockNumber][launch.Token] = struct{}{}
	}

	// The factory logs above name the new curves and tokens; the queries below are independent of
	// each other and may run concurrently (WithLogConcurrency). Only the refetch of newly graduated
	// pools waits, because the curve logs name those pools. The curve query stays job 0.
	jobs := []logJob{func(ctx context.Context) ([]types.Log, error) {
		logs, fetchErr := d.fetchByTopics(ctx, from, to, d.decoder.Topics(EmitterCurve), emitters.Curves)
		if fetchErr != nil {
			return nil, fmt.Errorf("fetch curve logs by topic: %w", fetchErr)
		}
		return logs, nil
	}}
	for _, block := range sortedKeys(freshTokens) {
		jobs = append(jobs, func(ctx context.Context) ([]types.Log, error) {
			logs, fetchErr := d.fetch(ctx, block, to, setAddresses(freshTokens[block]), d.decoder.Topics(EmitterToken))
			if fetchErr != nil {
				return nil, fmt.Errorf("refetch launched tokens from block %d: %w", block, fetchErr)
			}
			return logs, nil
		})
	}
	jobs = append(jobs, func(ctx context.Context) ([]types.Log, error) {
		logs, fetchErr := d.fetch(ctx, from, to, addressesFor(known, EmitterToken), d.decoder.Topics(EmitterToken))
		if fetchErr != nil {
			return nil, fmt.Errorf("fetch known token logs: %w", fetchErr)
		}
		return logs, nil
	}, func(ctx context.Context) ([]types.Log, error) {
		logs, fetchErr := d.fetch(ctx, from, to, []common.Address{d.v4.Locker}, d.decoder.Topics(EmitterLocker))
		if fetchErr != nil {
			return nil, fmt.Errorf("fetch locker logs: %w", fetchErr)
		}
		return logs, nil
	})
	knownPools := make([]common.Hash, 0, len(known.Pools))
	for poolID := range known.Pools {
		knownPools = append(knownPools, poolID)
	}
	jobs = append(jobs, func(ctx context.Context) ([]types.Log, error) {
		logs, fetchErr := d.fetchPools(ctx, from, to, knownPools)
		if fetchErr != nil {
			return nil, fmt.Errorf("fetch known pool logs: %w", fetchErr)
		}
		return logs, nil
	})
	results, err := d.runLogJobs(ctx, jobs)
	if err != nil {
		return DiscoveryResult{}, err
	}
	curveLogs, err := deduplicateAndSort(results[0])
	if err != nil {
		return DiscoveryResult{}, err
	}
	allLogs = append(allLogs, curveLogs...)
	allLogs = append(allLogs, concatLogs(results[1:])...)
	freshPools := make(map[uint64][]common.Hash)
	for _, log := range curveLogs {
		decoded, decodeErr := d.decoder.Decode(log, emitters)
		if decodeErr != nil {
			return DiscoveryResult{}, decodeErr
		}
		graduated, ok := decoded.Value.(GraduatedV2)
		if !ok {
			continue
		}
		if err := d.registerPool(graduated, log, emitters, known, freshPools); err != nil {
			return DiscoveryResult{}, err
		}
	}

	poolJobs := make([]logJob, 0, len(freshPools))
	for _, block := range sortedKeys(freshPools) {
		poolJobs = append(poolJobs, func(ctx context.Context) ([]types.Log, error) {
			logs, fetchErr := d.fetchPools(ctx, block, to, freshPools[block])
			if fetchErr != nil {
				return nil, fmt.Errorf("refetch graduated pools from block %d: %w", block, fetchErr)
			}
			return logs, nil
		})
	}
	poolResults, err := d.runLogJobs(ctx, poolJobs)
	if err != nil {
		return DiscoveryResult{}, err
	}
	allLogs = append(allLogs, concatLogs(poolResults)...)

	deduplicated, err := deduplicateAndSort(allLogs)
	if err != nil {
		return DiscoveryResult{}, err
	}
	return DiscoveryResult{Logs: deduplicated, LaunchesV2: launches, Emitters: emitters}, nil
}

// registerPool admits a graduation's pool only when its id is the one derived from the token and
// the manifest hook; a pool with any other key is not this deployment's.
func (d *Discoverer) registerPool(graduated GraduatedV2, log types.Log, emitters, known Emitters, fresh map[uint64][]common.Hash) error {
	if _, ok := emitters.Tokens[graduated.Token]; !ok {
		return fmt.Errorf("%w: Graduated at block %d names token %s, which this deployment did not launch", ErrPoolMismatch, log.BlockNumber, graduated.Token.Hex())
	}
	if expected := V4PoolID(graduated.Token, d.v4.Hook); graduated.PoolID != expected {
		return fmt.Errorf("%w: Graduated at block %d for token %s has pool id %s, want %s", ErrPoolMismatch, log.BlockNumber, graduated.Token.Hex(), graduated.PoolID.Hex(), expected.Hex())
	}
	if token, ok := emitters.Pools[graduated.PoolID]; ok {
		if token != graduated.Token {
			return fmt.Errorf("%w: pool %s is registered to token %s, not %s", ErrPoolMismatch, graduated.PoolID.Hex(), token.Hex(), graduated.Token.Hex())
		}
		if _, wasKnown := known.Pools[graduated.PoolID]; wasKnown {
			return nil
		}
	}
	emitters.Pools[graduated.PoolID] = graduated.Token
	fresh[log.BlockNumber] = append(fresh[log.BlockNumber], graduated.PoolID)
	return nil
}

// fetchPools queries PoolManager logs for pool ids in batches of addressBatchSize. Every returned
// log must come from the PoolManager, carry a requested signature and a requested pool id;
// anything else is a provider fault and fails closed.
func (d *Discoverer) fetchPools(ctx context.Context, from, to uint64, poolIDs []common.Hash) ([]types.Log, error) {
	if len(poolIDs) == 0 {
		return nil, nil
	}
	poolIDs = slices.Clone(poolIDs)
	slices.SortFunc(poolIDs, func(a, b common.Hash) int { return bytes.Compare(a[:], b[:]) })
	poolIDs = slices.Compact(poolIDs)
	topics := d.decoder.Topics(EmitterPoolManager)
	allowedTopics := make(map[common.Hash]struct{}, len(topics))
	for _, topic := range topics {
		allowedTopics[topic] = struct{}{}
	}
	jobs := make([]logJob, 0, (len(poolIDs)+d.addressBatchSize-1)/d.addressBatchSize)
	for start := 0; start < len(poolIDs); start += d.addressBatchSize {
		batch := poolIDs[start:min(start+d.addressBatchSize, len(poolIDs))]
		jobs = append(jobs, func(ctx context.Context) ([]types.Log, error) {
			logs, err := d.fetchRangeTopics(ctx, from, to, []common.Address{d.v4.PoolManager}, [][]common.Hash{topics, batch})
			if err != nil {
				return nil, err
			}
			for _, log := range logs {
				if log.Address != d.v4.PoolManager {
					return nil, &EmitterError{Address: log.Address, Coordinates: coordinates(log)}
				}
				if len(log.Topics) < 2 {
					return nil, &LogError{Coordinates: coordinates(log), Err: fmt.Errorf("%w: PoolManager log without a pool id", ErrMalformedLog)}
				}
				if _, ok := allowedTopics[log.Topics[0]]; !ok {
					return nil, &LogError{Coordinates: coordinates(log), Err: fmt.Errorf("%w: %s", ErrUnknownTopic, log.Topics[0])}
				}
				if !slices.Contains(batch, log.Topics[1]) {
					return nil, &EmitterError{Address: log.Address, Coordinates: coordinates(log)}
				}
			}
			return logs, nil
		})
	}
	results, err := d.runLogJobs(ctx, jobs)
	if err != nil {
		return nil, err
	}
	return concatLogs(results), nil
}

func sortedKeys[V any](values map[uint64]V) []uint64 {
	keys := make([]uint64, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func setAddresses(set AddressSet) []common.Address {
	result := make([]common.Address, 0, len(set))
	for address := range set {
		result = append(result, address)
	}
	return result
}
