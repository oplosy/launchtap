package indexer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/ethereum/go-ethereum/common"
)

// EngineSource is one launch engine the indexer ingests: its factory, the discoverer that
// fetches its logs and the decoder for its ABI. Engine 1 and engine 2 run side by side on one
// chain timeline (docs/specs/2026-11-20-dual-engine-preflight.md, decisions 1 and 3).
type EngineSource struct {
	Discovery Discovery
	Decoder   *chain.Decoder
	Factory   common.Address
}

// NewWithEngines builds an indexer that ingests every given engine. Each engine version and
// each factory may appear once.
func NewWithEngines(settings Settings, store Store, source Source, engines []EngineSource, router Router) (*Engine, error) {
	if settings.ReorgSearchDepth == 0 {
		settings.ReorgSearchDepth = defaultReorgSearchDepth
	}
	if settings.ChainID <= 0 || settings.DeploymentID == "" || settings.StartBlock < 0 || settings.ChunkSize <= 0 || settings.PollInterval <= 0 || settings.ReorgSearchDepth > maxReorgSearchDepth || (settings.ReorgSearchDepth > defaultReorgSearchDepth && !settings.ReorgRecoveryMode) || (settings.ReorgRecoveryMode && settings.ReorgSearchDepth <= defaultReorgSearchDepth) || store == nil || source == nil || router == nil {
		return nil, errors.New("invalid indexer dependencies or settings")
	}
	ordered, err := validateEngines(engines)
	if err != nil {
		return nil, err
	}
	return &Engine{settings: settings, store: store, source: source, engines: ordered, router: router}, nil
}

func validateEngines(engines []EngineSource) ([]EngineSource, error) {
	if len(engines) == 0 {
		return nil, errors.New("invalid indexer dependencies or settings")
	}
	versions := make(map[uint16]struct{}, len(engines))
	factories := make(map[common.Address]struct{}, len(engines))
	for _, engine := range engines {
		// The discoverers themselves refuse a zero factory (chain.NewDiscoverer).
		if engine.Discovery == nil || engine.Decoder == nil {
			return nil, errors.New("invalid indexer dependencies or settings")
		}
		version := engine.Decoder.EngineVersion()
		if _, seen := versions[version]; seen {
			return nil, fmt.Errorf("engine version %d is configured twice", version)
		}
		if _, seen := factories[engine.Factory]; seen {
			return nil, fmt.Errorf("factory %s is configured for two engines", engine.Factory.Hex())
		}
		versions[version] = struct{}{}
		factories[engine.Factory] = struct{}{}
	}
	ordered := slices.Clone(engines)
	slices.SortFunc(ordered, func(a, b EngineSource) int {
		return cmp.Compare(a.Decoder.EngineVersion(), b.Decoder.EngineVersion())
	})
	return ordered, nil
}

// decodeChunk discovers the chunk's logs for every engine's factory and the persisted tokens of
// that engine, decodes each log with its own engine's decoder and returns them in chain order.
// Every log must belong to a block of the chunk.
func (e *Engine) decodeChunk(ctx context.Context, chunk stepChunk, identities []TokenIdentity) ([]chain.DecodedLog, error) {
	emitters := make([]chain.Emitters, len(e.engines))
	index := make(map[uint16]int, len(e.engines))
	for position, engine := range e.engines {
		emitters[position] = chain.Emitters{Factory: engine.Factory, Curves: chain.NewAddressSet(), Tokens: chain.NewAddressSet(), Pairs: chain.NewAddressSet(), Pools: make(map[common.Hash]common.Address)}
		index[engine.Decoder.EngineVersion()] = position
	}
	for _, identity := range identities {
		// Every persisted token belongs to exactly one engine for its whole life; a token of an
		// engine this indexer no longer ingests would silently stop updating.
		position, ok := index[identity.EngineVersion]
		if !ok {
			return nil, fmt.Errorf("persisted token %s has engine version %d, which no configured engine decodes", identity.Token.Hex(), identity.EngineVersion)
		}
		set := emitters[position]
		set.Curves[identity.Curve] = struct{}{}
		set.Tokens[identity.Token] = struct{}{}
		if identity.EngineVersion == 1 {
			set.Pairs[identity.Pair] = struct{}{}
		}
		if identity.PoolID != nil {
			set.Pools[*identity.PoolID] = identity.Token
		}
	}
	decoded := make([]chain.DecodedLog, 0)
	for position, engine := range e.engines {
		result, err := engine.Discovery.Discover(ctx, uint64(chunk.from), uint64(chunk.to), emitters[position])
		if err != nil {
			// A provider outage or a failover switch retries the step like a head read; any
			// other discovery error (a bad log, a failed pair check) still stops the indexer.
			if chain.IsProviderUnavailable(err) || errors.Is(err, chain.ErrRPCSwitching) {
				return nil, rpcFailure("discover logs", err)
			}
			return nil, err
		}
		for _, log := range result.Logs {
			if log.BlockNumber > math.MaxInt64 || log.TxIndex > math.MaxInt32 || log.Index > math.MaxInt32 || log.Removed {
				return nil, errors.New("invalid log coordinates")
			}
			block, ok := chunk.blocks[int64(log.BlockNumber)]
			if !ok || block.BlockHash != log.BlockHash {
				return nil, ErrCanonicalMismatch
			}
			event, err := engine.Decoder.Decode(log, result.Emitters)
			if err != nil {
				return nil, err
			}
			decoded = append(decoded, event)
		}
	}
	if len(e.engines) == 1 {
		// One discoverer already returns its logs deduplicated and in chain order.
		return decoded, nil
	}
	return mergeEngineLogs(decoded)
}

// mergeEngineLogs orders the engines' logs by block and log index. The engines watch disjoint
// emitters, so one log reported by two engines means their emitter sets overlap.
func mergeEngineLogs(decoded []chain.DecodedLog) ([]chain.DecodedLog, error) {
	slices.SortStableFunc(decoded, func(a, b chain.DecodedLog) int {
		return cmp.Or(
			cmp.Compare(a.Coordinates.BlockNumber, b.Coordinates.BlockNumber),
			cmp.Compare(a.Coordinates.LogIndex, b.Coordinates.LogIndex),
		)
	})
	for i := 1; i < len(decoded); i++ {
		previous, current := decoded[i-1].Coordinates, decoded[i].Coordinates
		if previous.BlockNumber == current.BlockNumber && previous.LogIndex == current.LogIndex {
			return nil, fmt.Errorf("two engines decoded the same log %s", current)
		}
	}
	return decoded, nil
}
