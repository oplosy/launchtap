package chain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type LogSource interface {
	FilterLogs(context.Context, ethereum.FilterQuery) ([]types.Log, error)
}

type Discoverer struct {
	source           LogSource
	decoder          *Decoder
	factory          common.Address
	addressBatchSize int
	verifyLaunchPair func(context.Context, TokenLaunched) error
}

func NewDiscoverer(source LogSource, decoder *Decoder, factory common.Address, addressBatchSize uint64) (*Discoverer, error) {
	if source == nil || decoder == nil {
		return nil, errors.New("log source and decoder are required")
	}
	if factory == (common.Address{}) {
		return nil, errors.New("factory address is zero")
	}
	if addressBatchSize == 0 || addressBatchSize > uint64(^uint(0)>>1) {
		return nil, errors.New("address batch size is invalid")
	}
	return &Discoverer{source: source, decoder: decoder, factory: factory, addressBatchSize: int(addressBatchSize)}, nil
}

func NewDiscovererWithPairVerification(source LogSource, decoder *Decoder, factory common.Address, addressBatchSize uint64, verifyLaunchPair func(context.Context, TokenLaunched) error) (*Discoverer, error) {
	if verifyLaunchPair == nil {
		return nil, errors.New("launch pair verifier is required")
	}
	discoverer, err := NewDiscoverer(source, decoder, factory, addressBatchSize)
	if err != nil {
		return nil, err
	}
	discoverer.verifyLaunchPair = verifyLaunchPair
	return discoverer, nil
}

type DiscoveryResult struct {
	Logs     []types.Log
	Launches []TokenLaunched
	Emitters Emitters
}

func (d *Discoverer) Discover(ctx context.Context, from, to uint64, known Emitters) (DiscoveryResult, error) {
	if from > to {
		return DiscoveryResult{}, fmt.Errorf("invalid discovery range %d..%d", from, to)
	}
	emitters := copyEmitters(known)
	if emitters.Factory != (common.Address{}) && emitters.Factory != d.factory {
		return DiscoveryResult{}, errors.New("known factory differs from discoverer factory")
	}
	emitters.Factory = d.factory

	factoryLogs, err := d.fetch(ctx, from, to, []common.Address{d.factory}, d.decoder.Topics(EmitterFactory))
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("fetch factory logs: %w", err)
	}
	allLogs := append([]types.Log(nil), factoryLogs...)
	launches := make([]TokenLaunched, 0)
	newByBlock := make(map[uint64]Emitters)
	for _, log := range factoryLogs {
		decoded, decodeErr := d.decoder.Decode(log, emitters)
		if decodeErr != nil {
			return DiscoveryResult{}, decodeErr
		}
		launch, ok := decoded.Value.(TokenLaunched)
		if !ok {
			continue
		}
		if d.verifyLaunchPair != nil {
			if verifyErr := d.verifyLaunchPair(ctx, launch); verifyErr != nil {
				return DiscoveryResult{}, fmt.Errorf("verify discovered launch pair for token %s at block %d: %w", launch.Token.Hex(), log.BlockNumber, verifyErr)
			}
		}
		launches = append(launches, launch)
		emitters.Curves[launch.Curve] = struct{}{}
		emitters.Tokens[launch.Token] = struct{}{}
		emitters.Pairs[launch.LPPair] = struct{}{}
		blockSet := newByBlock[log.BlockNumber]
		if blockSet.Curves == nil {
			blockSet = emptyEmitters(d.factory)
		}
		blockSet.Curves[launch.Curve] = struct{}{}
		blockSet.Tokens[launch.Token] = struct{}{}
		blockSet.Pairs[launch.LPPair] = struct{}{}
		newByBlock[log.BlockNumber] = blockSet
	}

	// Curve events have protocol-unique signatures, so one topic-only query covers every known
	// and newly launched curve instead of one address-batched query per 500 curves. Logs from
	// other contracts that reuse these signatures are not ours and are dropped.
	curveLogs, err := d.fetchByTopics(ctx, from, to, d.decoder.Topics(EmitterCurve), emitters.Curves)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("fetch curve logs by topic: %w", err)
	}
	allLogs = append(allLogs, curveLogs...)

	launchBlocks := make([]uint64, 0, len(newByBlock))
	for block := range newByBlock {
		launchBlocks = append(launchBlocks, block)
	}
	slices.Sort(launchBlocks)
	for _, block := range launchBlocks {
		fresh := newByBlock[block]
		// Token Transfer and pair events use standard ERC-20/Uniswap signatures, so they stay
		// address-filtered.
		for _, kind := range []EmitterKind{EmitterToken, EmitterPair} {
			logs, fetchErr := d.fetch(ctx, block, to, addressesFor(fresh, kind), d.decoder.Topics(kind))
			if fetchErr != nil {
				return DiscoveryResult{}, fmt.Errorf("refetch discovered addresses from block %d: %w", block, fetchErr)
			}
			allLogs = append(allLogs, logs...)
		}
	}

	for _, kind := range []EmitterKind{EmitterToken, EmitterPair} {
		logs, fetchErr := d.fetch(ctx, from, to, addressesFor(known, kind), d.decoder.Topics(kind))
		if fetchErr != nil {
			return DiscoveryResult{}, fmt.Errorf("fetch known emitter kind %d: %w", kind, fetchErr)
		}
		allLogs = append(allLogs, logs...)
	}

	deduplicated, err := deduplicateAndSort(allLogs)
	if err != nil {
		return DiscoveryResult{}, err
	}
	return DiscoveryResult{Logs: deduplicated, Launches: launches, Emitters: emitters}, nil
}

func (d *Discoverer) fetch(ctx context.Context, from, to uint64, addresses []common.Address, topics []common.Hash) ([]types.Log, error) {
	if len(addresses) == 0 {
		return nil, nil
	}
	slices.SortFunc(addresses, func(a, b common.Address) int { return bytes.Compare(a[:], b[:]) })
	result := make([]types.Log, 0)
	for start := 0; start < len(addresses); start += d.addressBatchSize {
		end := min(start+d.addressBatchSize, len(addresses))
		batch := addresses[start:end]
		logs, err := d.fetchRange(ctx, from, to, batch, topics)
		if err != nil {
			return nil, err
		}
		allowedAddresses := NewAddressSet(batch...)
		allowedTopics := make(map[common.Hash]struct{}, len(topics))
		for _, topic := range topics {
			allowedTopics[topic] = struct{}{}
		}
		for _, log := range logs {
			if _, ok := allowedAddresses[log.Address]; !ok {
				return nil, &EmitterError{Address: log.Address, Coordinates: coordinates(log)}
			}
			if len(log.Topics) == 0 {
				return nil, &LogError{Coordinates: coordinates(log), Err: fmt.Errorf("%w: missing topic0", ErrMalformedLog)}
			}
			if _, ok := allowedTopics[log.Topics[0]]; !ok {
				return nil, &LogError{Coordinates: coordinates(log), Err: fmt.Errorf("%w: %s", ErrUnknownTopic, log.Topics[0])}
			}
		}
		result = append(result, logs...)
	}
	return result, nil
}

// fetchByTopics queries a range by event signature only and keeps logs from allowed emitters.
// A log with a signature outside the requested set is still a provider fault and fails closed.
func (d *Discoverer) fetchByTopics(ctx context.Context, from, to uint64, topics []common.Hash, allowed AddressSet) ([]types.Log, error) {
	if len(allowed) == 0 || len(topics) == 0 {
		return nil, nil
	}
	logs, err := d.fetchRange(ctx, from, to, nil, topics)
	if err != nil {
		return nil, err
	}
	allowedTopics := make(map[common.Hash]struct{}, len(topics))
	for _, topic := range topics {
		allowedTopics[topic] = struct{}{}
	}
	result := make([]types.Log, 0, len(logs))
	for _, log := range logs {
		if len(log.Topics) == 0 {
			return nil, &LogError{Coordinates: coordinates(log), Err: fmt.Errorf("%w: missing topic0", ErrMalformedLog)}
		}
		if _, ok := allowedTopics[log.Topics[0]]; !ok {
			return nil, &LogError{Coordinates: coordinates(log), Err: fmt.Errorf("%w: %s", ErrUnknownTopic, log.Topics[0])}
		}
		if _, ok := allowed[log.Address]; ok {
			result = append(result, log)
		}
	}
	return result, nil
}

func (d *Discoverer) fetchRange(ctx context.Context, from, to uint64, addresses []common.Address, topics []common.Hash) ([]types.Log, error) {
	query := ethereum.FilterQuery{FromBlock: new(big.Int).SetUint64(from), ToBlock: new(big.Int).SetUint64(to), Addresses: addresses}
	if len(topics) > 0 {
		query.Topics = [][]common.Hash{topics}
	}
	logs, err := d.source.FilterLogs(ctx, query)
	if err == nil {
		return logs, nil
	}
	if !IsCapacityError(err) {
		return nil, err
	}
	if from == to {
		return nil, fmt.Errorf("%w for block %d: %v", ErrRPCCapacity, from, err)
	}
	middle := from + (to-from)/2
	left, leftErr := d.fetchRange(ctx, from, middle, addresses, topics)
	if leftErr != nil {
		return nil, leftErr
	}
	right, rightErr := d.fetchRange(ctx, middle+1, to, addresses, topics)
	if rightErr != nil {
		return nil, rightErr
	}
	return append(left, right...), nil
}

func (d *Decoder) Topics(kind EmitterKind) []common.Hash {
	topics := make([]common.Hash, 0)
	for topic, definition := range d.events {
		if _, ok := definition.kinds[kind]; ok {
			topics = append(topics, topic)
		}
	}
	slices.SortFunc(topics, func(a, b common.Hash) int { return bytes.Compare(a[:], b[:]) })
	return topics
}

func emptyEmitters(factory common.Address) Emitters {
	return Emitters{Factory: factory, Curves: NewAddressSet(), Tokens: NewAddressSet(), Pairs: NewAddressSet()}
}

func copyEmitters(source Emitters) Emitters {
	result := emptyEmitters(source.Factory)
	for address := range source.Curves {
		result.Curves[address] = struct{}{}
	}
	for address := range source.Tokens {
		result.Tokens[address] = struct{}{}
	}
	for address := range source.Pairs {
		result.Pairs[address] = struct{}{}
	}
	return result
}

func addressesFor(emitters Emitters, kind EmitterKind) []common.Address {
	var set AddressSet
	switch kind {
	case EmitterCurve:
		set = emitters.Curves
	case EmitterToken:
		set = emitters.Tokens
	case EmitterPair:
		set = emitters.Pairs
	}
	result := make([]common.Address, 0, len(set))
	for address := range set {
		result = append(result, address)
	}
	return result
}

type logKey struct {
	block       uint64
	transaction uint
	index       uint
}

func deduplicateAndSort(logs []types.Log) ([]types.Log, error) {
	unique := make(map[logKey]types.Log, len(logs))
	for _, log := range logs {
		key := logKey{log.BlockNumber, log.TxIndex, log.Index}
		if previous, exists := unique[key]; exists {
			if !sameLog(previous, log) {
				return nil, fmt.Errorf("provider returned conflicting logs at block=%d tx_index=%d log_index=%d", key.block, key.transaction, key.index)
			}
			continue
		}
		unique[key] = log
	}
	result := make([]types.Log, 0, len(unique))
	for _, log := range unique {
		result = append(result, log)
	}
	slices.SortFunc(result, func(a, b types.Log) int {
		if a.BlockNumber != b.BlockNumber {
			if a.BlockNumber < b.BlockNumber {
				return -1
			}
			return 1
		}
		if a.TxIndex != b.TxIndex {
			if a.TxIndex < b.TxIndex {
				return -1
			}
			return 1
		}
		if a.Index < b.Index {
			return -1
		}
		if a.Index > b.Index {
			return 1
		}
		return 0
	})
	return result, nil
}

func sameLog(a, b types.Log) bool {
	return a.Address == b.Address && a.BlockHash == b.BlockHash && a.TxHash == b.TxHash && a.BlockNumber == b.BlockNumber && a.TxIndex == b.TxIndex && a.Index == b.Index && slices.Equal(a.Topics, b.Topics) && bytes.Equal(a.Data, b.Data)
}
