package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/redact"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var ErrCanonicalMismatch = errors.New("canonical chain mismatch")
var ErrSafeViolation = errors.New("canonical mismatch at or below safe head")
var ErrRPCUnhealthy = errors.New("indexer RPC unavailable")

// ForeignDeploymentError stops an indexer whose chain already holds state for another
// deployment. The ledger and its reorg deletes are chain-scoped, so two deployments on one
// chain in one database would delete each other's rows (one deployment per chain, pre-flight
// B2). It is not an RPC failure, so Run returns it instead of retrying.
type ForeignDeploymentError struct {
	ChainID      int64
	DeploymentID string
	Others       []string
}

func (e *ForeignDeploymentError) Error() string {
	return fmt.Sprintf("chain %d already holds indexer state for deployment(s) %s; deployment %q needs its own database", e.ChainID, strings.Join(e.Others, ", "), e.DeploymentID)
}

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
var _ SenderSource = (*chain.Client)(nil)

// Engine is not safe for concurrent Step calls; Run drives it from one goroutine.
type Engine struct {
	settings  Settings
	store     Store
	source    Source
	discovery Discovery
	decoder   *chain.Decoder
	router    Router
	// identities caches the persisted token identities, with their V4 pools attached, across
	// steps; nil means "load inside the next step's state transaction". Only this engine writes
	// the rows behind it, so it is dropped on every write that can change them
	// (docs/specs/2026-11-13-indexer-identity-cache-preflight.md). The slice is read-only once
	// cached.
	identities []TokenIdentity
}

func (e *Engine) invalidateIdentities() { e.identities = nil }

// changesIdentities reports whether a committed chunk can change the persisted token
// identities or graduated V4 pools.
func changesIdentities(decoded []chain.DecodedLog) bool {
	for _, log := range decoded {
		switch log.Value.(type) {
		case chain.TokenLaunched, chain.TokenLaunchedV2, chain.Graduated, chain.GraduatedV2:
			return true
		}
	}
	return false
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

// attachPools sets PoolID on the engine-2 identity of each persisted pool's token. A pool whose
// token has no engine-2 identity means the ledger and the tokens projection disagree.
func attachPools(identities []TokenIdentity, pools map[common.Hash]common.Address) error {
	index := make(map[common.Address]int, len(identities))
	for position, identity := range identities {
		index[identity.Token] = position
	}
	for poolID, token := range pools {
		position, ok := index[token]
		if !ok || identities[position].EngineVersion != 2 {
			return fmt.Errorf("persisted V4 pool %s names token %s, which has no engine-2 token identity", poolID.Hex(), token.Hex())
		}
		id := poolID
		identities[position].PoolID = &id
	}
	return nil
}

// senderMisses describes the transactions of one chunk left without a sender.
type senderMisses struct {
	misses, total int
	err           error
}

// attachSenders sets PoolSwap.TxFrom and V4Swap.TxFrom from one batched sender lookup. The V4
// Swap sender is the router, so the transaction sender is the only trader attribution
// (pre-flight R4). A failed lookup is ignored: ingestion never stops for enrichment, the swaps
// are written with a nil sender. Pool swaps are backfilled later; V4 swaps are not yet (backlog). It returns the transactions left without a sender (a failed call, an
// errored element or a null result); Step reports them only once the chunk commits, so a
// retried step never counts the same miss twice.
func (e *Engine) attachSenders(ctx context.Context, decoded []chain.DecodedLog) senderMisses {
	if e.settings.Senders == nil {
		return senderMisses{}
	}
	seen := make(map[common.Hash]struct{})
	var hashes []common.Hash
	for _, log := range decoded {
		if !isSwap(log.Value) {
			continue
		}
		if _, dup := seen[log.Coordinates.TransactionHash]; !dup {
			seen[log.Coordinates.TransactionHash] = struct{}{}
			hashes = append(hashes, log.Coordinates.TransactionHash)
		}
	}
	if len(hashes) == 0 {
		return senderMisses{}
	}
	senders, err := e.settings.Senders.TransactionSenders(ctx, hashes)
	if err != nil {
		return senderMisses{misses: len(hashes), total: len(hashes), err: err}
	}
	misses := 0
	for _, hash := range hashes {
		if _, found := senders[hash]; !found {
			misses++
		}
	}
	for index, log := range decoded {
		sender, found := senders[log.Coordinates.TransactionHash]
		if !found {
			continue
		}
		switch swap := log.Value.(type) {
		case chain.PoolSwap:
			swap.TxFrom = &sender
			decoded[index].Value = swap
		case chain.V4Swap:
			swap.TxFrom = &sender
			decoded[index].Value = swap
		}
	}
	return senderMisses{misses: misses, total: len(hashes)}
}

func isSwap(value any) bool {
	switch value.(type) {
	case chain.PoolSwap, chain.V4Swap:
		return true
	}
	return false
}

// reportSenderMisses logs one warning and feeds the miss counter for a committed chunk with
// unresolved senders. The error is redacted: an RPC *url.Error carries the provider URL and
// its API key.
func (e *Engine) reportSenderMisses(m senderMisses) {
	if m.misses == 0 {
		return
	}
	if e.settings.OnSenderMisses != nil {
		e.settings.OnSenderMisses(m.misses)
	}
	attrs := []any{"chain_id", e.settings.ChainID, "transactions", m.total, "misses", m.misses}
	if m.err != nil {
		attrs = append(attrs, "error", redact.Error(m.err))
	}
	slog.Warn("pool swap sender lookup missed; swaps stored without tx_from, the indexer retries them once their block is safe", attrs...)
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
	numbers := make([]int64, 0, to-from+1)
	for number := from; number <= to; number++ {
		numbers = append(numbers, number)
	}
	return batchHeaders(ctx, batcher, "read chunk headers", numbers)
}

// fetchHeaders reads each distinct number once, in batches when the source supports it and
// one by one otherwise. Every returned header is non-nil. Transport errors are RPC failures
// named after operation; a malformed reply (wrong batch length, missing header) is not.
func (e *Engine) fetchHeaders(ctx context.Context, operation string, numbers []int64) (map[int64]*types.Header, error) {
	unique := make([]int64, 0, len(numbers))
	seen := make(map[int64]struct{}, len(numbers))
	for _, number := range numbers {
		if _, ok := seen[number]; !ok {
			seen[number] = struct{}{}
			unique = append(unique, number)
		}
	}
	var headers map[int64]*types.Header
	if batcher, ok := e.source.(BatchHeaderSource); ok {
		var err error
		if headers, err = batchHeaders(ctx, batcher, operation, unique); err != nil {
			return nil, err
		}
	} else {
		headers = make(map[int64]*types.Header, len(unique))
		for _, number := range unique {
			header, err := e.source.HeaderByNumber(ctx, uint64(number))
			if err != nil {
				return nil, rpcFailure(operation, err)
			}
			headers[number] = header
		}
	}
	for _, number := range unique {
		if headers[number] == nil {
			return nil, fmt.Errorf("RPC returned no header for block %d", number)
		}
	}
	return headers, nil
}

// batchHeaders reads numbers in groups of headerBatchSize, one round trip per group.
func batchHeaders(ctx context.Context, batcher BatchHeaderSource, operation string, numbers []int64) (map[int64]*types.Header, error) {
	headers := make(map[int64]*types.Header, len(numbers))
	for start := 0; start < len(numbers); start += headerBatchSize {
		group := numbers[start:min(start+headerBatchSize, len(numbers))]
		request := make([]uint64, len(group))
		for index, number := range group {
			request[index] = uint64(number)
		}
		batch, err := batcher.HeadersByNumbers(ctx, request)
		if err != nil {
			return nil, rpcFailure(operation, err)
		}
		if len(batch) != len(request) {
			return nil, errors.New("RPC header batch length differs from request")
		}
		for index, header := range batch {
			headers[group[index]] = header
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
