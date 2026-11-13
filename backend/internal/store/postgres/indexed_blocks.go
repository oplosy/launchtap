package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Indexed block headers and per-deployment sync state.

func (adapter *Adapter) UpsertIndexedBlock(ctx context.Context, block ledger.IndexedBlock) (ledger.UpsertResult, error) {
	arg := indexedBlockParams(block)
	rows, err := adapter.queries.UpsertIndexedBlock(ctx, arg)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return ledger.UpsertResult{}, invariantConflict(
				"indexed_block",
				fmt.Sprintf("chain_id=%d block_number=%d", arg.ChainID, arg.BlockNumber),
				err,
			)
		}
		return ledger.UpsertResult{}, fmt.Errorf("upsert indexed block: %w", err)
	}
	if rows == 1 {
		return ledger.UpsertResult{Changed: true}, nil
	}
	stored, err := adapter.queries.GetIndexedBlockByNumber(ctx, sqlc.GetIndexedBlockByNumberParams{
		ChainID: arg.ChainID, BlockNumber: arg.BlockNumber,
	})
	if err != nil {
		return ledger.UpsertResult{}, fmt.Errorf("read conflicting indexed block: %w", err)
	}
	if equalIndexedBlockIdentity(stored, block) {
		return ledger.UpsertResult{}, fmt.Errorf("upsert indexed block affected no rows despite identical identity")
	}
	return ledger.UpsertResult{}, invariantConflict(
		"indexed_block",
		fmt.Sprintf("chain_id=%d block_number=%d", arg.ChainID, arg.BlockNumber),
		nil,
	)
}

// UpsertIndexedBlocks writes blocks of one chain, each block number once, with one multi-row
// upsert under UpsertIndexedBlock's conflict rule. When fewer rows are affected than given, the
// blocks are replayed through UpsertIndexedBlock (on the same connection, so inside the caller's
// transaction) to return the exact conflicting block's error. A unique violation aborts the
// statement, so it is reported for the whole block range.
func (adapter *Adapter) UpsertIndexedBlocks(ctx context.Context, blocks []ledger.IndexedBlock) (ledger.UpsertResult, error) {
	if len(blocks) == 0 {
		return ledger.UpsertResult{Changed: true}, nil
	}
	arg := sqlc.UpsertIndexedBlocksParams{
		ChainID:          blocks[0].ChainID,
		BlockNumbers:     make([]int64, len(blocks)),
		BlockHashes:      make([][]byte, len(blocks)),
		ParentHashes:     make([][]byte, len(blocks)),
		BlockTimes:       make([]pgtype.Timestamptz, len(blocks)),
		FinalityStatuses: make([]string, len(blocks)),
	}
	seen := make(map[int64]struct{}, len(blocks))
	low, high := blocks[0].BlockNumber, blocks[0].BlockNumber
	for index := range blocks {
		block := &blocks[index]
		if block.ChainID != arg.ChainID {
			return ledger.UpsertResult{}, fmt.Errorf("upsert indexed blocks: mixed chains %d and %d", arg.ChainID, block.ChainID)
		}
		if _, duplicate := seen[block.BlockNumber]; duplicate {
			return ledger.UpsertResult{}, fmt.Errorf("upsert indexed blocks: block %d given twice", block.BlockNumber)
		}
		seen[block.BlockNumber] = struct{}{}
		low, high = min(low, block.BlockNumber), max(high, block.BlockNumber)
		arg.BlockNumbers[index] = block.BlockNumber
		arg.BlockHashes[index] = block.BlockHash.Bytes()
		arg.ParentHashes[index] = block.ParentHash.Bytes()
		arg.BlockTimes[index] = timestamptz(block.BlockTime)
		arg.FinalityStatuses[index] = block.FinalityStatus
	}
	rows, err := adapter.queries.UpsertIndexedBlocks(ctx, arg)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return ledger.UpsertResult{}, invariantConflict(
				"indexed_block",
				fmt.Sprintf("chain_id=%d block_number=%d..%d", arg.ChainID, low, high),
				err,
			)
		}
		return ledger.UpsertResult{}, fmt.Errorf("upsert indexed blocks: %w", err)
	}
	if rows == int64(len(blocks)) {
		return ledger.UpsertResult{Changed: true}, nil
	}
	for _, block := range blocks {
		if _, err := adapter.UpsertIndexedBlock(ctx, block); err != nil {
			return ledger.UpsertResult{}, err
		}
	}
	return ledger.UpsertResult{}, fmt.Errorf("upsert indexed blocks affected %d of %d rows but no block conflicts", rows, len(blocks))
}

func (adapter *Adapter) GetIndexedBlockByNumber(
	ctx context.Context,
	chainID int64,
	blockNumber int64,
) (IndexedBlock, error) {
	block, err := adapter.queries.GetIndexedBlockByNumber(ctx, sqlc.GetIndexedBlockByNumberParams{
		ChainID: chainID, BlockNumber: blockNumber,
	})
	if err != nil {
		return IndexedBlock{}, fmt.Errorf("get indexed block by number: %w", err)
	}
	return ledger.IndexedBlock{
		ChainID: block.ChainID, BlockNumber: block.BlockNumber, BlockHash: common.Hash(block.BlockHash),
		ParentHash: common.Hash(block.ParentHash), BlockTime: block.BlockTime.Time, FinalityStatus: block.FinalityStatus,
	}, nil
}

func (adapter *Adapter) GetIndexedBlockByHash(
	ctx context.Context,
	chainID int64,
	blockHash common.Hash,
) (IndexedBlock, error) {
	block, err := adapter.queries.GetIndexedBlockByHash(ctx, sqlc.GetIndexedBlockByHashParams{
		ChainID: chainID, BlockHash: sqlc.Hash(blockHash),
	})
	if err != nil {
		return IndexedBlock{}, fmt.Errorf("get indexed block by hash: %w", err)
	}
	return ledger.IndexedBlock{
		ChainID: block.ChainID, BlockNumber: block.BlockNumber, BlockHash: common.Hash(block.BlockHash),
		ParentHash: common.Hash(block.ParentHash), BlockTime: block.BlockTime.Time, FinalityStatus: block.FinalityStatus,
	}, nil
}

func (adapter *Adapter) UpsertSyncState(ctx context.Context, state SyncState) (SyncState, error) {
	stored, err := adapter.queries.UpsertSyncState(ctx, sqlc.UpsertSyncStateParams(state))
	if err != nil {
		return SyncState{}, fmt.Errorf("upsert sync state: %w", err)
	}
	return SyncState(stored), nil
}

func (adapter *Adapter) GetSyncState(ctx context.Context, chainID int64, deploymentID string) (SyncState, error) {
	stored, err := adapter.queries.GetSyncState(ctx, sqlc.GetSyncStateParams{
		ChainID: chainID, DeploymentID: deploymentID,
	})
	if err != nil {
		return SyncState{}, fmt.Errorf("get sync state: %w", err)
	}
	return SyncState(stored), nil
}

func equalIndexedBlockIdentity(stored sqlc.IndexedBlock, attempted ledger.IndexedBlock) bool {
	return stored.ChainID == attempted.ChainID &&
		stored.BlockNumber == attempted.BlockNumber &&
		stored.BlockHash == sqlc.Hash(attempted.BlockHash) &&
		stored.ParentHash == sqlc.Hash(attempted.ParentHash) &&
		equalTimestamptz(stored.BlockTime, timestamptz(attempted.BlockTime))
}
