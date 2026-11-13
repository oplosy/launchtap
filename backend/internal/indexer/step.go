package indexer

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/ethereum/go-ethereum/common"
)

// stepHeads are the RPC's latest, safe and finalized blocks, checked for ordering.
type stepHeads struct{ latest, safe, finalized ledger.IndexedBlock }

// stepChunk is the contiguous block range one step reads. tip is the last linked block: the
// chunk's last block, or the saved Observed block when the chunk is empty.
type stepChunk struct {
	from, to int64
	blocks   map[int64]ledger.IndexedBlock
	tip      *ledger.IndexedBlock
}

// Step commits at most one block-aligned chunk. RPC snapshots are checked against
// each other before opening the write transaction; no watermark describes work
// that has not committed locally.
func (e *Engine) Step(ctx context.Context) (advanced bool, err error) {
	defer func() {
		if err != nil {
			// A failed step may still have committed (a lost commit acknowledgement), so the
			// cache is rebuilt from the database.
			e.invalidateIdentities()
			if e.settings.OnFailure != nil {
				e.settings.OnFailure(err)
			}
		}
	}()
	state, identities, err := e.loadStepState(ctx)
	if err != nil {
		return false, err
	}
	e.identities = identities
	heads, err := e.readHeads(ctx)
	if err != nil {
		return false, err
	}
	if recovered, err := e.verifySavedBlocks(ctx, state); err != nil || recovered {
		return recovered, err
	}
	chunk, recovered, err := e.readChunk(ctx, state, heads.latest)
	if err != nil || recovered {
		return recovered, err
	}
	decoded := make([]chain.DecodedLog, 0)
	var misses senderMisses
	if len(chunk.blocks) > 0 {
		if decoded, err = e.decodeChunk(ctx, chunk, identities); err != nil {
			return false, err
		}
		misses = e.attachSenders(ctx, decoded)
		state.Observed = chunk.tip
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
	if err := e.resolvePromotions(ctx, &state, chunk, heads); err != nil {
		return false, err
	}
	err = e.commitChunk(ctx, state, chunk, decoded, identities, previousSafe, previousFinalized)
	if err == nil {
		if changesIdentities(decoded) {
			e.invalidateIdentities()
		}
		e.reportSenderMisses(misses)
		if e.settings.OnCommitted != nil {
			e.settings.OnCommitted(state)
		}
	}
	return len(chunk.blocks) > 0, err
}

// loadStepState reads the indexer state, refusing a chain that holds another deployment's
// state, and the token identities unless the engine's cache already holds them.
func (e *Engine) loadStepState(ctx context.Context) (State, []TokenIdentity, error) {
	var state State
	identities := e.identities
	if err := e.store.Transaction(ctx, func(ctx context.Context, u UnitOfWork) error {
		var err error
		others, err := u.ForeignDeployments(ctx, e.settings.ChainID, e.settings.DeploymentID)
		if err != nil {
			return err
		}
		if len(others) != 0 {
			return &ForeignDeploymentError{ChainID: e.settings.ChainID, DeploymentID: e.settings.DeploymentID, Others: others}
		}
		state, err = u.ReadState(ctx, e.settings.ChainID, e.settings.DeploymentID)
		if err != nil {
			return err
		}
		if identities != nil {
			return nil
		}
		loaded, err := u.TokenIdentities(ctx, e.settings.ChainID)
		if err != nil {
			return err
		}
		pools, err := u.GraduatedV4Pools(ctx, e.settings.ChainID)
		if err != nil {
			return err
		}
		if err := attachPools(loaded, pools); err != nil {
			return err
		}
		// A non-nil empty slice marks a loaded chain without tokens.
		identities = append(make([]TokenIdentity, 0, len(loaded)), loaded...)
		return nil
	}); err != nil {
		return State{}, nil, err
	}
	return state, identities, nil
}

// readHeads reads the RPC's latest, safe and finalized heads and checks their ordering.
func (e *Engine) readHeads(ctx context.Context) (stepHeads, error) {
	heads, err := e.source.Heads(ctx)
	if err != nil {
		return stepHeads{}, rpcFailure("read heads", err)
	}
	if e.settings.OnHeads != nil && heads.Latest != nil && heads.Latest.Number != nil {
		e.settings.OnHeads(heads.Latest.Number.Int64())
	}
	latest, err := e.block(heads.Latest)
	if err != nil {
		return stepHeads{}, err
	}
	safe, err := e.block(heads.Safe)
	if err != nil {
		return stepHeads{}, err
	}
	finalized, err := e.block(heads.Finalized)
	if err != nil {
		return stepHeads{}, err
	}
	if safe.BlockNumber > latest.BlockNumber || finalized.BlockNumber > safe.BlockNumber {
		return stepHeads{}, errors.New("inconsistent RPC head ordering")
	}
	return stepHeads{latest: latest, safe: safe, finalized: finalized}, nil
}

// verifySavedBlocks checks the saved Safe and Observed blocks against the RPC. A replaced
// Observed block starts reorg recovery and reports recovered; a replaced Safe block is a
// safety violation.
func (e *Engine) verifySavedBlocks(ctx context.Context, state State) (recovered bool, err error) {
	savedBlocks := make([]*ledger.IndexedBlock, 0, 2)
	savedNumbers := make([]int64, 0, 2)
	for _, saved := range []*ledger.IndexedBlock{state.Safe, state.Observed} {
		if saved != nil {
			savedBlocks = append(savedBlocks, saved)
			savedNumbers = append(savedNumbers, saved.BlockNumber)
		}
	}
	savedHeaders, err := e.fetchHeaders(ctx, "read saved headers", savedNumbers)
	if err != nil {
		return false, err
	}
	for _, saved := range savedBlocks {
		remote := savedHeaders[saved.BlockNumber]
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
	return false, nil
}

// readChunk reads the next chunk's headers up to latest and links each to its parent. A
// broken link starts reorg recovery and reports recovered.
func (e *Engine) readChunk(ctx context.Context, state State, latest ledger.IndexedBlock) (chunk stepChunk, recovered bool, err error) {
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
		return stepChunk{}, false, err
	}
	blocks := make(map[int64]ledger.IndexedBlock)
	previous := state.Observed
	for number := from; number <= to; number++ {
		header := prefetched[number]
		if header == nil {
			if header, err = e.source.HeaderByNumber(ctx, uint64(number)); err != nil {
				return stepChunk{}, false, rpcFailure("read chunk header", err)
			}
		}
		block, err := e.block(header)
		if err != nil {
			return stepChunk{}, false, err
		}
		if block.BlockNumber != number {
			return stepChunk{}, false, errors.New("RPC header number differs from requested number")
		}
		if previous != nil && block.ParentHash != previous.BlockHash {
			if state.Safe != nil && previous.BlockNumber <= state.Safe.BlockNumber {
				return stepChunk{}, false, ErrSafeViolation
			}
			if err := e.recoverReorg(ctx, state, block); err != nil {
				return stepChunk{}, false, err
			}
			return stepChunk{}, true, nil
		}
		blocks[number] = block
		previous = &block
	}
	return stepChunk{from: from, to: to, blocks: blocks, tip: previous}, false, nil
}

// decodeChunk discovers the chunk's logs for the factory and every persisted token's emitters
// and decodes them. Every log must belong to a block of the chunk.
func (e *Engine) decodeChunk(ctx context.Context, chunk stepChunk, identities []TokenIdentity) ([]chain.DecodedLog, error) {
	emitters := chain.Emitters{Factory: e.settings.Factory, Curves: chain.NewAddressSet(), Tokens: chain.NewAddressSet(), Pairs: chain.NewAddressSet(), Pools: make(map[common.Hash]common.Address)}
	engineVersion := e.decoder.EngineVersion()
	for _, identity := range identities {
		// One deployment per chain (pre-flight B2), so every persisted token was launched by
		// the decoder's engine.
		if identity.EngineVersion != engineVersion {
			return nil, fmt.Errorf("persisted token %s has engine version %d, deployment decodes engine %d", identity.Token.Hex(), identity.EngineVersion, engineVersion)
		}
		emitters.Curves[identity.Curve] = struct{}{}
		emitters.Tokens[identity.Token] = struct{}{}
		if engineVersion == 1 {
			emitters.Pairs[identity.Pair] = struct{}{}
		}
		if identity.PoolID != nil {
			emitters.Pools[*identity.PoolID] = identity.Token
		}
	}
	result, err := e.discovery.Discover(ctx, uint64(chunk.from), uint64(chunk.to), emitters)
	if err != nil {
		return nil, err
	}
	decoded := make([]chain.DecodedLog, 0)
	for _, log := range result.Logs {
		if log.BlockNumber > math.MaxInt64 || log.TxIndex > math.MaxInt32 || log.Index > math.MaxInt32 || log.Removed {
			return nil, errors.New("invalid log coordinates")
		}
		block, ok := chunk.blocks[int64(log.BlockNumber)]
		if !ok || block.BlockHash != log.BlockHash {
			return nil, ErrCanonicalMismatch
		}
		event, err := e.decoder.Decode(log, result.Emitters)
		if err != nil {
			return nil, err
		}
		decoded = append(decoded, event)
	}
	return decoded, nil
}

// resolvePromotions moves state.Safe and state.Finalized up to the RPC tags, capped at the
// locally processed tip, after rechecking the chunk tip and each promotion block against the RPC.
func (e *Engine) resolvePromotions(ctx context.Context, state *State, chunk stepChunk, heads stepHeads) error {
	type promotion struct {
		number int64
		remote ledger.IndexedBlock
		target **ledger.IndexedBlock
		status string
	}
	promotions := make([]promotion, 0, 2)
	for _, candidate := range []promotion{{remote: heads.safe, target: &state.Safe, status: "safe"}, {remote: heads.finalized, target: &state.Finalized, status: "finalized"}} {
		candidate.number = min(candidate.remote.BlockNumber, state.Observed.BlockNumber)
		if candidate.number < e.settings.StartBlock || (*candidate.target != nil && candidate.number <= (*candidate.target).BlockNumber) {
			continue
		}
		promotions = append(promotions, candidate)
	}
	locals := make(map[int64]ledger.IndexedBlock, len(promotions))
	var unread []int64
	for _, candidate := range promotions {
		if local, ok := chunk.blocks[candidate.number]; ok {
			locals[candidate.number] = local
		} else if len(unread) == 0 || unread[len(unread)-1] != candidate.number {
			unread = append(unread, candidate.number)
		}
	}
	if len(unread) > 0 {
		if err := e.store.Transaction(ctx, func(ctx context.Context, u UnitOfWork) error {
			for _, number := range unread {
				local, ok, err := u.ReadBlock(ctx, e.settings.ChainID, number)
				if err != nil {
					return err
				}
				if ok {
					locals[number] = local
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	// The chunk-tip recheck and the promotion headers share one batch; a number requested
	// twice (the tip is often the safe promotion) is fetched once, so both checks see it.
	checkNumbers := make([]int64, 0, 1+len(promotions))
	if len(chunk.blocks) > 0 {
		checkNumbers = append(checkNumbers, chunk.to)
	}
	for _, candidate := range promotions {
		checkNumbers = append(checkNumbers, candidate.number)
	}
	checkHeaders, err := e.fetchHeaders(ctx, "recheck chunk and promotion headers", checkNumbers)
	if err != nil {
		return err
	}
	if len(chunk.blocks) > 0 && checkHeaders[chunk.to].Hash() != chunk.blocks[chunk.to].BlockHash {
		return ErrCanonicalMismatch
	}
	for _, candidate := range promotions {
		local, ok := locals[candidate.number]
		if !ok {
			return fmt.Errorf("missing promotion block %d", candidate.number)
		}
		if checkHeaders[candidate.number].Hash() != local.BlockHash || (candidate.number == candidate.remote.BlockNumber && local.BlockHash != candidate.remote.BlockHash) {
			return ErrCanonicalMismatch
		}
		local.FinalityStatus = candidate.status
		*candidate.target = &local
	}
	return nil
}

// commitChunk writes the chunk's blocks, applies its decoded events, promotes the finality
// watermarks and saves the state in one transaction.
func (e *Engine) commitChunk(ctx context.Context, state State, chunk stepChunk, decoded []chain.DecodedLog, identities []TokenIdentity, previousSafe, previousFinalized int64) error {
	ordered := make([]ledger.IndexedBlock, 0, len(chunk.blocks))
	for number := chunk.from; number <= chunk.to; number++ {
		ordered = append(ordered, chunk.blocks[number])
	}
	return e.store.Transaction(ctx, func(ctx context.Context, u UnitOfWork) error {
		if len(ordered) > 0 {
			result, err := u.UpsertIndexedBlocks(ctx, ordered)
			if err != nil {
				return err
			}
			if !result.Changed {
				return ErrCanonicalMismatch
			}
		}
		if err := e.router.Apply(ctx, u, decoded, chunk.blocks, identities); err != nil {
			return err
		}
		if err := u.PromoteBlocks(ctx, e.settings.ChainID, state.Safe, state.Finalized, previousSafe, previousFinalized); err != nil {
			return err
		}
		return u.WriteState(ctx, state)
	})
}
