package postgres

import (
	"context"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
)

// UnattributedSwapTx is one transaction that still has a v1 pool swap or a V4 swap without a
// known sender.
type UnattributedSwapTx struct {
	TxHash      common.Hash
	BlockNumber int64
}

// ListUnattributedSwapTxs returns up to limit distinct transactions with a NULL tx_from swap in
// blocks at or below throughBlock, ordered by (block_number, tx_hash) and strictly after the
// (afterBlock, afterHash) cursor.
func (adapter *Adapter) ListUnattributedSwapTxs(ctx context.Context, chainID, throughBlock, afterBlock int64, afterHash common.Hash, limit int32) ([]UnattributedSwapTx, error) {
	rows, err := adapter.queries.ListUnattributedPoolSwaps(ctx, sqlc.ListUnattributedPoolSwapsParams{
		ChainID: chainID, ThroughBlock: throughBlock, AfterBlock: afterBlock, AfterHash: afterHash[:], RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list unattributed pool swaps: %w", err)
	}
	result := make([]UnattributedSwapTx, len(rows))
	for index, row := range rows {
		result[index] = UnattributedSwapTx{TxHash: common.Hash(row.TxHash), BlockNumber: row.BlockNumber}
	}
	return result, nil
}

// CountUnattributedSwaps counts the chain's v1 pool swaps and V4 swaps without a sender at or
// below throughBlock.
func (adapter *Adapter) CountUnattributedSwaps(ctx context.Context, chainID, throughBlock int64) (int64, error) {
	pending, err := adapter.queries.CountUnattributedPoolSwaps(ctx, sqlc.CountUnattributedPoolSwapsParams{ChainID: chainID, ThroughBlock: throughBlock})
	if err != nil {
		return 0, fmt.Errorf("count unattributed pool swaps: %w", err)
	}
	return pending, nil
}

// AttributeSwapSender fills tx_from on every still-unattributed v1 pool swap and V4 swap of
// txHash and marks the affected (sender, token) positions dirty; a V4 swap also re-marks its
// graduated token's aggregate, as IngestV4Swap does. It returns the number of swaps updated; a
// swap that already has a sender is left alone, so a rerun updates nothing.
func (adapter *Adapter) AttributeSwapSender(ctx context.Context, chainID int64, txHash common.Hash, sender common.Address) (int, error) {
	pairs, err := adapter.queries.SetPoolSwapTxFrom(ctx, sqlc.SetPoolSwapTxFromParams{ChainID: chainID, TxHash: txHash[:], TxFrom: sender[:]})
	if err != nil {
		return 0, fmt.Errorf("set pool swap sender: %w", err)
	}
	marked := make(map[sqlc.Address]struct{}, len(pairs))
	for _, pair := range pairs {
		if _, done := marked[pair]; done {
			continue
		}
		marked[pair] = struct{}{}
		if err := adapter.queries.MarkPoolSwapPositionDirty(ctx, sqlc.MarkPoolSwapPositionDirtyParams{ChainID: chainID, Pair: pair[:], TxFrom: sender[:], Factory: adapter.factory[:]}); err != nil {
			return 0, fmt.Errorf("mark pool swap position dirty: %w", err)
		}
	}
	pools, err := adapter.queries.SetV4SwapTxFrom(ctx, sqlc.SetV4SwapTxFromParams{ChainID: chainID, TxHash: txHash[:], TxFrom: sender[:]})
	if err != nil {
		return 0, fmt.Errorf("set V4 swap sender: %w", err)
	}
	markedPools := make(map[sqlc.Hash]struct{}, len(pools))
	for _, poolID := range pools {
		if _, done := markedPools[poolID]; done {
			continue
		}
		markedPools[poolID] = struct{}{}
		if err := adapter.queries.MarkV4PoolTokenDirty(ctx, sqlc.MarkV4PoolTokenDirtyParams{ChainID: chainID, PoolID: &poolID}); err != nil {
			return 0, fmt.Errorf("mark V4 pool token dirty: %w", err)
		}
		if err := adapter.queries.MarkV4SwapPositionDirty(ctx, sqlc.MarkV4SwapPositionDirtyParams{ChainID: chainID, PoolID: poolID[:], TxFrom: sender[:], Factory: adapter.factory[:]}); err != nil {
			return 0, fmt.Errorf("mark V4 swap position dirty: %w", err)
		}
	}
	return len(pairs) + len(pools), nil
}

// PositionSeed counts what SeedPositionDirty found and wrote.
type PositionSeed struct {
	Tokens int64 // token chunks processed (one statement each)
	Pairs  int64 // distinct (wallet, token) pairs in the ledger after the R4 exclusions
	Marked int64 // position_dirty rows inserted or re-generated (0 in dry-run mode)
}

// seedTokenPage is how many token addresses one keyset page of the seed reads.
const seedTokenPage = 500

// SeedPositionDirty marks every (wallet, token) pair of the chain's existing ledger history
// dirty (curve trades by trader, transfers from/to, attributed pool swaps), excluding the zero
// address, the adapter's factory and every curve and pair (R4). It walks the chain's tokens with
// a keyset cursor and runs one bounded statement per token, each committed on its own; it is
// idempotent, so an interrupted run is simply rerun. With dryRun it only counts.
func (adapter *Adapter) SeedPositionDirty(ctx context.Context, chainID int64, dryRun bool) (PositionSeed, error) {
	return adapter.SeedPositionDirtyPaged(ctx, chainID, dryRun, seedTokenPage)
}

// SeedPositionDirtyPaged is SeedPositionDirty with an explicit token page size.
func (adapter *Adapter) SeedPositionDirtyPaged(ctx context.Context, chainID int64, dryRun bool, pageSize int32) (PositionSeed, error) {
	if pageSize <= 0 {
		return PositionSeed{}, fmt.Errorf("seed position dirty: page size %d must be positive", pageSize)
	}
	var seed PositionSeed
	after := []byte{}
	for {
		tokens, err := adapter.queries.ListSeedTokens(ctx, sqlc.ListSeedTokensParams{ChainID: chainID, AfterToken: after, RowLimit: pageSize})
		if err != nil {
			return seed, fmt.Errorf("list seed tokens: %w", err)
		}
		for _, token := range tokens {
			row, err := adapter.queries.SeedTokenPositionDirty(ctx, sqlc.SeedTokenPositionDirtyParams{
				ChainID: chainID, TokenAddress: token[:], Factory: adapter.factory[:], DryRun: dryRun,
			})
			if err != nil {
				return seed, fmt.Errorf("seed position dirty for token %x: %w", token[:], err)
			}
			seed.Tokens++
			seed.Pairs += row.PairCount
			seed.Marked += row.Marked
		}
		if len(tokens) < int(pageSize) {
			return seed, nil
		}
		last := tokens[len(tokens)-1]
		after = last[:]
	}
}
