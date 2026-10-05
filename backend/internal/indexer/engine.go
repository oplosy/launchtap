package indexer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/ethereum/go-ethereum/core/types"
)

var ErrCanonicalMismatch = errors.New("canonical chain mismatch")
var ErrSafeViolation = errors.New("canonical mismatch at or below safe head")
var ErrRPCUnhealthy = errors.New("indexer RPC unavailable")

// Keep these bounds aligned with the stdlib-only configuration package.
const (
	defaultReorgSearchDepth uint64 = 128
	maxReorgSearchDepth     uint64 = 100000
	// headerBatchSize stays within common provider JSON-RPC batch limits.
	headerBatchSize = 100
	// Transient RPC failures are retried in-process with capped exponential backoff; a long
	// outage still exits so the orchestrator restarts the process and alerts.
	maxRPCRetryDelay          = 30 * time.Second
	maxConsecutiveRPCFailures = 20
)

// BatchHeaderSource is implemented by sources that can fetch many headers in one round trip.
type BatchHeaderSource interface {
	HeadersByNumbers(context.Context, []uint64) ([]*types.Header, error)
}

var _ BatchHeaderSource = (*chain.Client)(nil)

type Engine struct {
	settings  Settings
	store     Store
	source    Source
	discovery Discovery
	decoder   *chain.Decoder
	router    Router
}

func New(settings Settings, store Store, source Source, discovery Discovery, decoder *chain.Decoder, router Router) (*Engine, error) {
	if settings.ReorgSearchDepth == 0 {
		settings.ReorgSearchDepth = defaultReorgSearchDepth
	}
	if settings.ChainID <= 0 || settings.DeploymentID == "" || settings.StartBlock < 0 || settings.ChunkSize <= 0 || settings.PollInterval <= 0 || settings.ReorgSearchDepth > maxReorgSearchDepth || (settings.ReorgSearchDepth > defaultReorgSearchDepth && !settings.ReorgRecoveryMode) || (settings.ReorgRecoveryMode && settings.ReorgSearchDepth <= defaultReorgSearchDepth) || store == nil || source == nil || discovery == nil || decoder == nil || router == nil {
		return nil, errors.New("invalid indexer dependencies or settings")
	}
	return &Engine{settings: settings, store: store, source: source, discovery: discovery, decoder: decoder, router: router}, nil
}

// Step commits at most one block-aligned chunk. RPC snapshots are checked against
// each other before opening the write transaction; no watermark describes work
// that has not committed locally.
func (e *Engine) Step(ctx context.Context) (advanced bool, err error) {
	defer func() {
		if err != nil && e.settings.OnFailure != nil {
			e.settings.OnFailure(err)
		}
	}()
	var state State
	var identities []TokenIdentity
	if err = e.store.Transaction(ctx, func(ctx context.Context, u UnitOfWork) error {
		var err error
		state, err = u.ReadState(ctx, e.settings.ChainID, e.settings.DeploymentID)
		if err != nil {
			return err
		}
		identities, err = u.TokenIdentities(ctx, e.settings.ChainID)
		return err
	}); err != nil {
		return false, err
	}
	heads, err := e.source.Heads(ctx)
	if err != nil {
		return false, rpcFailure("read heads", err)
	}
	latest, err := e.block(heads.Latest)
	if err != nil {
		return false, err
	}
	safe, err := e.block(heads.Safe)
	if err != nil {
		return false, err
	}
	finalized, err := e.block(heads.Finalized)
	if err != nil {
		return false, err
	}
	if safe.BlockNumber > latest.BlockNumber || finalized.BlockNumber > safe.BlockNumber {
		return false, errors.New("inconsistent RPC head ordering")
	}
	for _, saved := range []*ledger.IndexedBlock{state.Safe, state.Observed} {
		if saved == nil {
			continue
		}
		remote, err := e.source.HeaderByNumber(ctx, uint64(saved.BlockNumber))
		if err != nil {
			return false, rpcFailure("read saved header", err)
		}
		if remote.Hash() != saved.BlockHash {
			if state.Safe != nil && saved.BlockNumber <= state.Safe.BlockNumber {
				return false, ErrSafeViolation
			}
			tip, err := e.block(remote)
			if err != nil {
				return false, err
			}
			if err := e.recoverReorg(ctx, state, tip); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	from := e.settings.StartBlock
	if state.Observed != nil {
		from = state.Observed.BlockNumber + 1
	}
	to := latest.BlockNumber
	if to-from >= e.settings.ChunkSize {
		to = from + e.settings.ChunkSize - 1
	}
	prefetched, err := e.prefetchHeaders(ctx, from, to)
	if err != nil {
		return false, err
	}
	blocks := make(map[int64]ledger.IndexedBlock)
	previous := state.Observed
	for number := from; number <= to; number++ {
		header := prefetched[number]
		if header == nil {
			if header, err = e.source.HeaderByNumber(ctx, uint64(number)); err != nil {
				return false, rpcFailure("read chunk header", err)
			}
		}
		block, err := e.block(header)
		if err != nil {
			return false, err
		}
		if block.BlockNumber != number {
			return false, errors.New("RPC header number differs from requested number")
		}
		if previous != nil && block.ParentHash != previous.BlockHash {
			if state.Safe != nil && previous.BlockNumber <= state.Safe.BlockNumber {
				return false, ErrSafeViolation
			}
			if err := e.recoverReorg(ctx, state, block); err != nil {
				return false, err
			}
			return true, nil
		}
		blocks[number] = block
		previous = &block
	}
	decoded := make([]chain.DecodedLog, 0)
	if len(blocks) > 0 {
		emitters := chain.Emitters{Factory: e.settings.Factory, Curves: chain.NewAddressSet(), Tokens: chain.NewAddressSet(), Pairs: chain.NewAddressSet()}
		for _, identity := range identities {
			if identity.EngineVersion != 1 {
				return false, fmt.Errorf("unsupported persisted engine version %d", identity.EngineVersion)
			}
			emitters.Curves[identity.Curve] = struct{}{}
			emitters.Tokens[identity.Token] = struct{}{}
			emitters.Pairs[identity.Pair] = struct{}{}
		}
		result, err := e.discovery.Discover(ctx, uint64(from), uint64(to), emitters)
		if err != nil {
			return false, err
		}
		for _, log := range result.Logs {
			if log.BlockNumber > math.MaxInt64 || log.TxIndex > math.MaxInt32 || log.Index > math.MaxInt32 || log.Removed {
				return false, errors.New("invalid log coordinates")
			}
			block, ok := blocks[int64(log.BlockNumber)]
			if !ok || block.BlockHash != log.BlockHash {
				return false, ErrCanonicalMismatch
			}
			event, err := e.decoder.Decode(log, result.Emitters)
			if err != nil {
				return false, err
			}
			decoded = append(decoded, event)
		}
		check, err := e.source.HeaderByNumber(ctx, uint64(to))
		if err != nil {
			return false, rpcFailure("recheck chunk header", err)
		}
		if check.Hash() != blocks[to].BlockHash {
			return false, ErrCanonicalMismatch
		}
		state.Observed = previous
	}
	if state.Observed == nil {
		return false, nil
	}
	previousSafe, previousFinalized := int64(-1), int64(-1)
	if state.Safe != nil {
		previousSafe = state.Safe.BlockNumber
	}
	if state.Finalized != nil {
		previousFinalized = state.Finalized.BlockNumber
	}
	// When RPC tags are ahead, promote only the locally processed intersection.
	for _, promotion := range []struct {
		remote ledger.IndexedBlock
		target **ledger.IndexedBlock
		status string
	}{{safe, &state.Safe, "safe"}, {finalized, &state.Finalized, "finalized"}} {
		number := min(promotion.remote.BlockNumber, state.Observed.BlockNumber)
		if number < e.settings.StartBlock || (*promotion.target != nil && number <= (*promotion.target).BlockNumber) {
			continue
		}
		var local ledger.IndexedBlock
		var ok bool
		local, ok = blocks[number]
		if !ok {
			if err := e.store.Transaction(ctx, func(ctx context.Context, u UnitOfWork) error {
				var err error
				local, ok, err = u.ReadBlock(ctx, e.settings.ChainID, number)
				return err
			}); err != nil {
				return false, err
			}
		}
		if !ok {
			return false, fmt.Errorf("missing promotion block %d", number)
		}
		remote, err := e.source.HeaderByNumber(ctx, uint64(number))
		if err != nil {
			return false, rpcFailure("read promotion header", err)
		}
		if remote.Hash() != local.BlockHash || (number == promotion.remote.BlockNumber && local.BlockHash != promotion.remote.BlockHash) {
			return false, ErrCanonicalMismatch
		}
		local.FinalityStatus = promotion.status
		*promotion.target = &local
	}
	err = e.store.Transaction(ctx, func(ctx context.Context, u UnitOfWork) error {
		for number := from; number <= to; number++ {
			result, err := u.UpsertIndexedBlock(ctx, blocks[number])
			if err != nil {
				return err
			}
			if !result.Changed {
				return ErrCanonicalMismatch
			}
		}
		if err := e.router.Apply(ctx, u, decoded, blocks, identities); err != nil {
			return err
		}
		if err := u.PromoteBlocks(ctx, e.settings.ChainID, state.Safe, state.Finalized, previousSafe, previousFinalized); err != nil {
			return err
		}
		return u.WriteState(ctx, state)
	})
	if err == nil && e.settings.OnCommitted != nil {
		e.settings.OnCommitted(state)
	}
	return len(blocks) > 0, err
}

func rpcFailure(operation string, err error) error {
	return fmt.Errorf("%w while %s: %w", ErrRPCUnhealthy, operation, err)
}

// prefetchHeaders reads a chunk's headers with batched RPC calls when the source supports it.
// The caller still validates numbering and parent links for every header.
func (e *Engine) prefetchHeaders(ctx context.Context, from, to int64) (map[int64]*types.Header, error) {
	batcher, ok := e.source.(BatchHeaderSource)
	if !ok || from > to {
		return nil, nil
	}
	headers := make(map[int64]*types.Header, to-from+1)
	for start := from; start <= to; start += headerBatchSize {
		end := min(start+headerBatchSize-1, to)
		numbers := make([]uint64, 0, end-start+1)
		for number := start; number <= end; number++ {
			numbers = append(numbers, uint64(number))
		}
		batch, err := batcher.HeadersByNumbers(ctx, numbers)
		if err != nil {
			return nil, rpcFailure("read chunk headers", err)
		}
		if len(batch) != len(numbers) {
			return nil, errors.New("RPC header batch length differs from request")
		}
		for index, header := range batch {
			headers[int64(numbers[index])] = header
		}
	}
	return headers, nil
}

func (e *Engine) Run(ctx context.Context) error {
	failures := 0
	for {
		advanced, err := e.Step(ctx)
		if err != nil {
			if !errors.Is(err, ErrRPCUnhealthy) || ctx.Err() != nil {
				return err
			}
			failures++
			if failures >= maxConsecutiveRPCFailures {
				return err
			}
			if err := sleep(ctx, min(e.settings.PollInterval<<min(failures, 6), maxRPCRetryDelay)); err != nil {
				return err
			}
			continue
		}
		failures = 0
		if advanced {
			continue
		}
		if err := sleep(ctx, e.settings.PollInterval); err != nil {
			return err
		}
	}
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (e *Engine) block(header *types.Header) (ledger.IndexedBlock, error) {
	if header == nil || header.Number == nil || !header.Number.IsInt64() || header.Number.Sign() < 0 || header.Time > math.MaxInt64 {
		return ledger.IndexedBlock{}, errors.New("invalid RPC header")
	}
	return ledger.IndexedBlock{ChainID: e.settings.ChainID, BlockNumber: header.Number.Int64(), BlockHash: header.Hash(), ParentHash: header.ParentHash, BlockTime: time.Unix(int64(header.Time), 0).UTC(), FinalityStatus: "observed"}, nil
}
