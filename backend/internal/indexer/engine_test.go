package indexer

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type memoryUnit struct {
	state          State
	blocks         map[int64]ledger.IndexedBlock
	fail           bool
	failRebuild    bool
	forcedAncestor *ledger.IndexedBlock
	reorgs         []ReorgRecord
	reorgCompleted bool
	transactionID  int
	actions        []memoryRecoveryAction
}
type memoryStore struct {
	unit             memoryUnit
	nextTransaction  int
	committedActions []memoryRecoveryAction
}

type memoryRecoveryAction struct {
	transactionID int
	name          string
}

func (s *memoryStore) Transaction(ctx context.Context, fn func(context.Context, UnitOfWork) error) error {
	copyUnit := s.unit
	copyUnit.blocks = make(map[int64]ledger.IndexedBlock)
	for n, b := range s.unit.blocks {
		copyUnit.blocks[n] = b
	}
	copyUnit.reorgs = append([]ReorgRecord(nil), s.unit.reorgs...)
	copyUnit.actions = nil
	s.nextTransaction++
	copyUnit.transactionID = s.nextTransaction
	if err := fn(ctx, &copyUnit); err != nil {
		return err
	}
	s.unit = copyUnit
	s.committedActions = append(s.committedActions, copyUnit.actions...)
	return nil
}
func (u *memoryUnit) ReadState(_ context.Context, chainID int64, deployment string) (State, error) {
	s := u.state
	s.ChainID = chainID
	s.DeploymentID = deployment
	return s, nil
}
func (u *memoryUnit) WriteState(_ context.Context, s State) error {
	if u.fail {
		return errors.New("watermark failure")
	}
	u.state = s
	u.action("write_state")
	return nil
}
func (u *memoryUnit) ReadBlock(_ context.Context, _ int64, n int64) (ledger.IndexedBlock, bool, error) {
	b, ok := u.blocks[n]
	return b, ok, nil
}
func (u *memoryUnit) UpsertIndexedBlock(_ context.Context, b ledger.IndexedBlock) (ledger.UpsertResult, error) {
	u.blocks[b.BlockNumber] = b
	return ledger.UpsertResult{Changed: true}, nil
}

func (u *memoryUnit) FindCommonAncestor(_ context.Context, _ int64, candidates []common.Hash) (ledger.IndexedBlock, error) {
	if u.forcedAncestor != nil {
		return *u.forcedAncestor, nil
	}
	hashes := make(map[common.Hash]struct{}, len(candidates))
	for _, hash := range candidates {
		hashes[hash] = struct{}{}
	}
	var ancestor ledger.IndexedBlock
	found := false
	for _, block := range u.blocks {
		if _, ok := hashes[block.BlockHash]; ok && (!found || block.BlockNumber > ancestor.BlockNumber) {
			ancestor = block
			found = true
		}
	}
	if !found {
		return ledger.IndexedBlock{}, errors.New("common ancestor not found")
	}
	return ancestor, nil
}

func (u *memoryUnit) AffectedTokensAbove(_ context.Context, _ int64, _ int64) ([]common.Address, error) {
	u.action("affected_tokens")
	return []common.Address{common.HexToAddress("0x1001")}, nil
}

func (u *memoryUnit) DeleteCanonicalAbove(_ context.Context, _ int64, ancestor int64) error {
	for number := range u.blocks {
		if number > ancestor {
			delete(u.blocks, number)
		}
	}
	u.action("delete_canonical")
	return nil
}

func (u *memoryUnit) RebuildTokenProjections(context.Context, int64, common.Address) error {
	u.action("rebuild_token_projections")
	if u.failRebuild {
		return errors.New("projection rebuild failure")
	}
	return nil
}

func (u *memoryUnit) DeleteTokenStats(context.Context, int64, common.Address) error {
	u.action("delete_token_stats")
	return nil
}

func (u *memoryUnit) RecomputeTokenStats(context.Context, int64, common.Address) error {
	u.action("recompute_token_stats")
	return nil
}

func (u *memoryUnit) RecomputeProtocolAggregates(context.Context, int64) error {
	u.action("recompute_protocol_aggregates")
	return nil
}

func (u *memoryUnit) RecordReorg(_ context.Context, record ReorgRecord) (int64, error) {
	u.reorgs = append(u.reorgs, record)
	u.action("record_reorg")
	return int64(len(u.reorgs)), nil
}

func (u *memoryUnit) CompleteReorg(context.Context, int64) error {
	u.reorgCompleted = true
	u.action("complete_reorg")
	return nil
}

func (u *memoryUnit) action(name string) {
	u.actions = append(u.actions, memoryRecoveryAction{transactionID: u.transactionID, name: name})
}
func (u *memoryUnit) PromoteBlocks(_ context.Context, _ int64, safe, final *ledger.IndexedBlock, previousSafe, previousFinalized int64) error {
	for n, b := range u.blocks {
		if final != nil && n > previousFinalized && n <= final.BlockNumber {
			b.FinalityStatus = "finalized"
		} else if safe != nil && n > previousSafe && n <= safe.BlockNumber {
			b.FinalityStatus = "safe"
		}
		u.blocks[n] = b
	}
	return nil
}
func (*memoryUnit) TokenIdentities(context.Context, int64) ([]TokenIdentity, error) { return nil, nil }

type fakeSource struct {
	headers            map[uint64]*types.Header
	headerResponses    map[uint64][]*types.Header
	headerResponseCall map[uint64]int
	heads              chain.Heads
	headsErr           error
	requests           []uint64
}

func (s *fakeSource) Heads(context.Context) (chain.Heads, error) { return s.heads, s.headsErr }
func (s *fakeSource) HeaderByNumber(_ context.Context, n uint64) (*types.Header, error) {
	s.requests = append(s.requests, n)
	if responses := s.headerResponses[n]; len(responses) > 0 {
		if s.headerResponseCall == nil {
			s.headerResponseCall = make(map[uint64]int)
		}
		call := s.headerResponseCall[n]
		s.headerResponseCall[n] = call + 1
		if call < len(responses) {
			return responses[call], nil
		}
	}
	h, ok := s.headers[n]
	if !ok {
		return nil, errors.New("header missing")
	}
	return h, nil
}

func (s *fakeSource) headerRequestsAt(number uint64) int {
	count := 0
	for _, requested := range s.requests {
		if requested == number {
			count++
		}
	}
	return count
}

type emptyDiscovery struct{}

func (emptyDiscovery) Discover(_ context.Context, _, _ uint64, e chain.Emitters) (chain.DiscoveryResult, error) {
	return chain.DiscoveryResult{Emitters: e}, nil
}

type emptyRouter struct{}

func (emptyRouter) Apply(context.Context, UnitOfWork, []chain.DecodedLog, map[int64]ledger.IndexedBlock, []TokenIdentity) error {
	return nil
}
func newTestEngine(t *testing.T) (*Engine, *memoryStore, *fakeSource) {
	t.Helper()
	store := &memoryStore{unit: memoryUnit{blocks: make(map[int64]ledger.IndexedBlock)}}
	source := &fakeSource{headers: make(map[uint64]*types.Header)}
	var parent common.Hash
	for n := uint64(1); n <= 5; n++ {
		h := &types.Header{Number: new(big.Int).SetUint64(n), ParentHash: parent, Time: 100 + n}
		source.headers[n] = h
		parent = h.Hash()
	}
	source.heads = chain.Heads{Latest: source.headers[5], Safe: source.headers[4], Finalized: source.headers[3]}
	decoder, err := chain.NewDecoder(1)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(Settings{ChainID: 1, DeploymentID: "local-test", StartBlock: 1, ChunkSize: 2, PollInterval: time.Millisecond}, store, source, emptyDiscovery{}, decoder, emptyRouter{})
	if err != nil {
		t.Fatal(err)
	}
	return e, store, source
}
func TestWatermarksAreBoundedByCommittedChunk(t *testing.T) {
	e, store, _ := newTestEngine(t)
	if advanced, err := e.Step(t.Context()); err != nil || !advanced {
		t.Fatalf("step: %t %v", advanced, err)
	}
	if store.unit.state.Observed.BlockNumber != 2 || store.unit.state.Safe.BlockNumber != 2 || store.unit.state.Finalized.BlockNumber != 2 {
		t.Fatalf("unprocessed head promoted: %+v", store.unit.state)
	}
	if len(store.unit.blocks) != 2 || store.unit.blocks[2].FinalityStatus != "finalized" {
		t.Fatalf("blocks: %+v", store.unit.blocks)
	}
	store.unit.fail = true
	if _, err := e.Step(t.Context()); err == nil {
		t.Fatal("expected watermark failure")
	}
	if len(store.unit.blocks) != 2 || store.unit.state.Observed.BlockNumber != 2 {
		t.Fatal("failed chunk partially committed")
	}
}

func TestEngineReportsOnlyCommittedWatermarks(t *testing.T) {
	e, store, _ := newTestEngine(t)
	var reported []State
	e.settings.OnCommitted = func(state State) { reported = append(reported, state) }
	if _, err := e.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(reported) != 1 || reported[0].Observed == nil || reported[0].Observed.BlockNumber != store.unit.state.Observed.BlockNumber {
		t.Fatalf("reported=%+v stored=%+v", reported, store.unit.state)
	}
	store.unit.fail = true
	if _, err := e.Step(t.Context()); err == nil {
		t.Fatal("expected failed transaction")
	}
	if len(reported) != 1 {
		t.Fatalf("reported uncommitted watermark: %+v", reported)
	}
}

func TestEngineReportsTypedRPCFailure(t *testing.T) {
	e, _, source := newTestEngine(t)
	source.headsErr = errors.New("RPC unavailable")
	var reported error
	e.settings.OnFailure = func(err error) { reported = err }
	if _, err := e.Step(t.Context()); !errors.Is(err, ErrRPCUnhealthy) {
		t.Fatalf("Step() error = %v, want RPC health error", err)
	}
	if !errors.Is(reported, ErrRPCUnhealthy) {
		t.Fatalf("reported error = %v, want RPC health error", reported)
	}
}

type batchSource struct {
	*fakeSource
	batches [][]uint64
}

func (s *batchSource) HeadersByNumbers(_ context.Context, numbers []uint64) ([]*types.Header, error) {
	s.batches = append(s.batches, append([]uint64(nil), numbers...))
	headers := make([]*types.Header, 0, len(numbers))
	for _, number := range numbers {
		header, ok := s.headers[number]
		if !ok {
			return nil, errors.New("header missing")
		}
		headers = append(headers, header)
	}
	return headers, nil
}

func TestChunkHeadersUseOneBatchWhenSupported(t *testing.T) {
	e, store, source := newTestEngine(t)
	batcher := &batchSource{fakeSource: source}
	e.source = batcher
	if advanced, err := e.Step(t.Context()); err != nil || !advanced {
		t.Fatalf("step: %t %v", advanced, err)
	}
	if len(batcher.batches) != 1 || len(batcher.batches[0]) != 2 || batcher.batches[0][0] != 1 || batcher.batches[0][1] != 2 {
		t.Fatalf("batches = %v", batcher.batches)
	}
	if source.headerRequestsAt(1) != 0 {
		t.Fatalf("chunk header fetched individually despite batch support: %v", source.requests)
	}
	if store.unit.state.Observed.BlockNumber != 2 {
		t.Fatalf("observed = %+v", store.unit.state.Observed)
	}
}

func TestRunRetriesTransientRPCFailuresInProcess(t *testing.T) {
	e, store, source := newTestEngine(t)
	source.headsErr = errors.New("RPC unavailable")
	failures := 0
	ctx, cancel := context.WithCancel(t.Context())
	e.settings.OnFailure = func(error) {
		failures++
		if failures == 2 {
			source.headsErr = nil
		}
	}
	e.settings.OnCommitted = func(state State) {
		if state.Observed != nil && state.Observed.BlockNumber == 5 {
			cancel()
		}
	}
	if err := e.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want cancellation after recovery", err)
	}
	if failures != 2 || store.unit.state.Observed.BlockNumber != 5 {
		t.Fatalf("failures=%d observed=%+v", failures, store.unit.state.Observed)
	}

	e, _, _ = newTestEngine(t)
	stored := e.store.(*memoryStore)
	stored.unit.fail = true
	if err := e.Run(t.Context()); err == nil || errors.Is(err, ErrRPCUnhealthy) {
		t.Fatalf("non-RPC failure was retried or hidden: %v", err)
	}
}

func TestSafeHashMismatchStopsBeforeWrites(t *testing.T) {
	e, store, source := newTestEngine(t)
	if _, err := e.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	replacement := *source.headers[2]
	replacement.Extra = []byte("fork")
	source.headers[2] = &replacement
	if _, err := e.Step(t.Context()); !errors.Is(err, ErrSafeViolation) {
		t.Fatalf("safe mismatch: %v", err)
	}
	if len(store.unit.blocks) != 2 {
		t.Fatal("wrote through safe mismatch")
	}
}

func TestNewRequiresAcknowledgementForExpandedReorgSearch(t *testing.T) {
	e, store, source := newTestEngine(t)
	if e.settings.ReorgSearchDepth != defaultReorgSearchDepth {
		t.Fatalf("default reorg search depth = %d, want %d", e.settings.ReorgSearchDepth, defaultReorgSearchDepth)
	}
	decoder, err := chain.NewDecoder(1)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name    string
		depth   uint64
		mode    bool
		wantErr bool
	}{
		{name: "expanded without recovery mode", depth: defaultReorgSearchDepth + 1, wantErr: true},
		{name: "recovery mode without expanded bound", depth: defaultReorgSearchDepth, mode: true, wantErr: true},
		{name: "above maximum", depth: maxReorgSearchDepth + 1, mode: true, wantErr: true},
		{name: "acknowledged bounded recovery", depth: defaultReorgSearchDepth + 1, mode: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := e.settings
			settings.ReorgSearchDepth = test.depth
			settings.ReorgRecoveryMode = test.mode
			_, err := New(settings, store, source, emptyDiscovery{}, decoder, emptyRouter{})
			if (err != nil) != test.wantErr {
				t.Fatalf("New() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestDeepReorgDefaultWindowFailsClosedWithoutWrites(t *testing.T) {
	e, store, source, state, tip := newDeepReorgFixture(t, defaultReorgSearchDepth, false, 140, 130, nil)
	beforeHash := store.unit.state.Observed.BlockHash
	beforeBlocks := len(store.unit.blocks)

	err := e.recoverReorg(t.Context(), state, tip)
	if err == nil {
		t.Fatal("recoverReorg() succeeded outside the default search window")
	}
	if len(source.requests) != int(defaultReorgSearchDepth) || source.requests[0] != 300 || source.requests[len(source.requests)-1] != 173 {
		t.Fatalf("candidate requests = %d, first/last=%v/%v, want 128 candidates 300..173", len(source.requests), source.requests[0], source.requests[len(source.requests)-1])
	}
	if len(store.committedActions) != 0 || len(store.unit.reorgs) != 0 {
		t.Fatalf("deep reorg wrote before finding an ancestor: actions=%v reorgs=%v", store.committedActions, store.unit.reorgs)
	}
	if len(store.unit.blocks) != beforeBlocks || store.unit.state.Observed.BlockHash != beforeHash {
		t.Fatal("failed bounded search changed canonical blocks or observed watermark")
	}
}

func TestAcknowledgedDeepReorgUsesAtomicRecoveryAboveSafe(t *testing.T) {
	e, store, source, state, tip := newDeepReorgFixture(t, 200, true, 140, 130, nil)
	if err := e.recoverReorg(t.Context(), state, tip); err != nil {
		t.Fatalf("recoverReorg() error = %v", err)
	}
	if len(source.requests) != 172 || source.requests[0] != 300 || source.requests[170] != 130 || source.requests[171] != 300 {
		t.Fatalf("candidate requests = %d, first/candidate-last/recheck=%v/%v/%v, want 171 candidates 300..130 followed by tip recheck", len(source.requests), source.requests[0], source.requests[len(source.requests)-2], source.requests[len(source.requests)-1])
	}
	if got := store.unit.reorgs; len(got) != 1 || got[0].CommonAncestorNumber != 140 || got[0].Depth != 160 {
		t.Fatalf("reorg records = %+v, want one ancestor 140 at depth 160", got)
	}
	if store.unit.state.Observed == nil || store.unit.state.Observed.BlockNumber != 140 || store.unit.state.Safe == nil || store.unit.state.Safe.BlockNumber != 130 || !store.unit.reorgCompleted {
		t.Fatalf("recovered state = %+v, completed=%v", store.unit.state, store.unit.reorgCompleted)
	}
	if len(store.unit.blocks) != 140 {
		t.Fatalf("canonical block count = %d, want surviving blocks through ancestor 140", len(store.unit.blocks))
	}

	var recoveryTransaction int
	var recoveryActions []memoryRecoveryAction
	for _, action := range store.committedActions {
		if action.name == "record_reorg" && action.transactionID != 1 {
			t.Fatalf("reorg audit record transaction = %d, want separate first transaction", action.transactionID)
		}
		if action.name == "delete_canonical" {
			recoveryTransaction = action.transactionID
		}
		if action.name != "record_reorg" {
			recoveryActions = append(recoveryActions, action)
		}
	}
	if recoveryTransaction == 0 {
		t.Fatalf("atomic recovery transaction not recorded: %v", store.committedActions)
	}
	wantActions := []string{
		"affected_tokens", "delete_canonical", "rebuild_token_projections", "delete_token_stats",
		"recompute_token_stats", "recompute_protocol_aggregates", "write_state", "complete_reorg",
	}
	if len(recoveryActions) != len(wantActions) {
		t.Fatalf("recovery actions = %v, want %v", recoveryActions, wantActions)
	}
	for index, action := range recoveryActions {
		if action.name != wantActions[index] || action.transactionID != recoveryTransaction {
			t.Fatalf("recovery action[%d] = %q in transaction %d, want %q in transaction %d; actions=%v", index, action.name, action.transactionID, wantActions[index], recoveryTransaction, recoveryActions)
		}
	}
}

func TestReorgRecoveryRejectsDetectedTipMismatchBeforeWrites(t *testing.T) {
	e, store, source, state, tip := newDeepReorgFixture(t, 200, true, 140, 130, nil)
	changed := *source.headers[uint64(tip.BlockNumber)]
	changed.Extra = []byte("different provider view")
	source.headers[uint64(tip.BlockNumber)] = &changed
	assertReorgRecoveryAbortedWithoutWrites(t, e, store, state, tip)
}

func TestReorgRecoveryRejectsDiscontinuousCandidateChainBeforeWrites(t *testing.T) {
	e, store, source, state, tip := newDeepReorgFixture(t, 200, true, 140, 130, nil)
	changed := *source.headers[uint64(tip.BlockNumber-1)]
	changed.Extra = []byte("mixed branch")
	source.headers[uint64(tip.BlockNumber-1)] = &changed
	assertReorgRecoveryAbortedWithoutWrites(t, e, store, state, tip)
}

func TestReorgRecoveryRejectsTipDriftBeforeWrites(t *testing.T) {
	e, store, source, state, tip := newDeepReorgFixture(t, 200, true, 140, 130, nil)
	changed := *source.headers[uint64(tip.BlockNumber)]
	changed.Extra = []byte("tip changed during recovery")
	source.headerResponses = map[uint64][]*types.Header{
		uint64(tip.BlockNumber): {source.headers[uint64(tip.BlockNumber)], &changed},
	}
	assertReorgRecoveryAbortedWithoutWrites(t, e, store, state, tip)
	if got := source.headerRequestsAt(uint64(tip.BlockNumber)); got != 2 {
		t.Fatalf("detected-tip RPC reads = %d, want candidate read plus final recheck", got)
	}
}

func assertReorgRecoveryAbortedWithoutWrites(t *testing.T, e *Engine, store *memoryStore, state State, tip ledger.IndexedBlock) {
	t.Helper()
	beforeBlocks := len(store.unit.blocks)
	beforeObserved := state.Observed.BlockHash
	if err := e.recoverReorg(t.Context(), state, tip); err == nil {
		t.Fatal("recoverReorg() succeeded with an inconsistent RPC view")
	}
	if len(store.unit.reorgs) != 0 || len(store.committedActions) != 0 {
		t.Fatalf("inconsistent RPC view caused recovery writes: reorgs=%+v actions=%v", store.unit.reorgs, store.committedActions)
	}
	if len(store.unit.blocks) != beforeBlocks || store.unit.state.Observed.BlockHash != beforeObserved {
		t.Fatal("inconsistent RPC view changed canonical blocks or observed watermark")
	}
}

func TestDeepReorgSearchIncludesSafeBoundaryButNeverSearchesBelow(t *testing.T) {
	e, store, source, state, tip := newDeepReorgFixture(t, 200, true, 130, 130, nil)
	if err := e.recoverReorg(t.Context(), state, tip); err != nil {
		t.Fatalf("recoverReorg() at safe boundary error = %v", err)
	}
	if len(source.requests) != 172 || source.requests[0] != 300 || source.requests[170] != 130 || source.requests[171] != 300 {
		t.Fatalf("candidate range/recheck = %d requests %v..%v then %v, want 171 headers 300..130 followed by tip recheck", len(source.requests), source.requests[0], source.requests[170], source.requests[171])
	}
	if store.unit.reorgs[0].CommonAncestorNumber != 130 {
		t.Fatalf("common ancestor = %d, want safe boundary 130", store.unit.reorgs[0].CommonAncestorNumber)
	}
}

func TestDeepReorgAncestorBelowSafeFailsBeforeRecordingOrDeleting(t *testing.T) {
	belowSafe := ledger.IndexedBlock{ChainID: 1, BlockNumber: 129, BlockHash: common.HexToHash("0x129")}
	e, store, source, state, tip := newDeepReorgFixture(t, 200, true, 140, 130, &belowSafe)
	beforeHash := store.unit.state.Observed.BlockHash
	beforeBlocks := len(store.unit.blocks)

	if err := e.recoverReorg(t.Context(), state, tip); !errors.Is(err, ErrSafeViolation) {
		t.Fatalf("recoverReorg() error = %v, want ErrSafeViolation", err)
	}
	if len(source.requests) == 0 || source.requests[len(source.requests)-1] != 130 {
		t.Fatalf("search did not stop at safe head: last request=%v", source.requests[len(source.requests)-1])
	}
	if len(store.unit.reorgs) != 0 || len(store.committedActions) != 0 || len(store.unit.blocks) != beforeBlocks || store.unit.state.Observed.BlockHash != beforeHash {
		t.Fatal("below-safe ancestor caused a write or watermark change")
	}
}

func TestFailedDeepReorgRebuildKeepsCanonicalStateAtomic(t *testing.T) {
	e, store, _, state, tip := newDeepReorgFixture(t, 200, true, 140, 130, nil)
	store.unit.failRebuild = true
	beforeHash := store.unit.state.Observed.BlockHash
	beforeBlocks := len(store.unit.blocks)

	if err := e.recoverReorg(t.Context(), state, tip); err == nil {
		t.Fatal("recoverReorg() succeeded despite projection rebuild failure")
	}
	if len(store.unit.reorgs) != 1 || store.unit.reorgCompleted {
		t.Fatalf("expected committed open incident record, got reorgs=%+v completed=%v", store.unit.reorgs, store.unit.reorgCompleted)
	}
	if len(store.unit.blocks) != beforeBlocks || store.unit.state.Observed.BlockHash != beforeHash {
		t.Fatal("failed recovery transaction partially changed canonical blocks or watermark")
	}
}

func newDeepReorgFixture(t *testing.T, searchDepth uint64, recoveryMode bool, commonAncestor, safeNumber int64, forcedAncestor *ledger.IndexedBlock) (*Engine, *memoryStore, *fakeSource, State, ledger.IndexedBlock) {
	t.Helper()
	const chainHeight = 300
	store := &memoryStore{unit: memoryUnit{blocks: make(map[int64]ledger.IndexedBlock), forcedAncestor: forcedAncestor}}
	source := &fakeSource{headers: make(map[uint64]*types.Header)}
	canonical := make(map[int64]ledger.IndexedBlock, chainHeight)
	var parent common.Hash
	for number := int64(1); number <= chainHeight; number++ {
		header := &types.Header{Number: big.NewInt(number), ParentHash: parent, Time: uint64(100 + number)}
		source.headers[uint64(number)] = header
		block := ledger.IndexedBlock{ChainID: 1, BlockNumber: number, BlockHash: header.Hash(), ParentHash: header.ParentHash, BlockTime: time.Unix(int64(header.Time), 0).UTC(), FinalityStatus: "observed"}
		canonical[number] = block
		store.unit.blocks[number] = block
		parent = header.Hash()
	}
	parent = canonical[commonAncestor].BlockHash
	for number := commonAncestor + 1; number <= chainHeight; number++ {
		header := &types.Header{Number: big.NewInt(number), ParentHash: parent, Time: uint64(100 + number), Extra: []byte("replacement")}
		source.headers[uint64(number)] = header
		parent = header.Hash()
	}
	observed := canonical[chainHeight]
	safe := canonical[safeNumber]
	finalized := canonical[safeNumber-10]
	state := State{
		ChainID: 1, DeploymentID: "local-test",
		Observed: &observed, Safe: &safe, Finalized: &finalized,
	}
	store.unit.state = state
	decoder, err := chain.NewDecoder(1)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(Settings{
		ChainID: 1, DeploymentID: "local-test", StartBlock: 1, ChunkSize: 2,
		PollInterval: time.Millisecond, ReorgSearchDepth: searchDepth, ReorgRecoveryMode: recoveryMode,
	}, store, source, emptyDiscovery{}, decoder, emptyRouter{})
	if err != nil {
		t.Fatal(err)
	}
	tipHeader := source.headers[uint64(chainHeight)]
	tip, err := e.block(tipHeader)
	if err != nil {
		t.Fatal(err)
	}
	return e, store, source, state, tip
}
