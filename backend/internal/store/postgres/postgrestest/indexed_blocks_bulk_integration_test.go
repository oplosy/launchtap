//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

func TestUpsertIndexedBlocksMatchesSingleRowRules(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	adapter := storepostgres.NewAdapter(pool)

	const chainID int64 = 49020
	blockTime := time.Date(2026, 11, 13, 12, 0, 0, 123456000, time.UTC)
	chunk := make([]ledger.IndexedBlock, 0, 3)
	parent := common.Hash{0xb0}
	for number := int64(10); number <= 12; number++ {
		hash := common.Hash{0xb0, byte(number)}
		chunk = append(chunk, ledger.IndexedBlock{ChainID: chainID, BlockNumber: number, BlockHash: hash, ParentHash: parent, BlockTime: blockTime.Add(time.Duration(number) * time.Second), FinalityStatus: "observed"})
		parent = hash
	}

	if result, err := adapter.UpsertIndexedBlocks(ctx, chunk); err != nil || !result.Changed {
		t.Fatalf("insert chunk = %+v, %v", result, err)
	}
	for _, block := range chunk {
		stored, err := adapter.GetIndexedBlockByNumber(ctx, chainID, block.BlockNumber)
		if err != nil {
			t.Fatalf("read block %d: %v", block.BlockNumber, err)
		}
		if stored.BlockHash != block.BlockHash || stored.ParentHash != block.ParentHash || !stored.BlockTime.Equal(block.BlockTime) || stored.FinalityStatus != "observed" {
			t.Fatalf("stored block %d = %+v, want %+v", block.BlockNumber, stored, block)
		}
	}

	// Same identity again: an idempotent rewrite that may move finality.
	for index := range chunk {
		chunk[index].FinalityStatus = "safe"
	}
	if result, err := adapter.UpsertIndexedBlocks(ctx, chunk); err != nil || !result.Changed {
		t.Fatalf("rewrite chunk = %+v, %v", result, err)
	}
	if stored, err := adapter.GetIndexedBlockByNumber(ctx, chainID, 11); err != nil || stored.FinalityStatus != "safe" {
		t.Fatalf("rewritten block 11 = %+v, %v", stored, err)
	}

	// A divergent identity names the exact block, like the single-row path.
	divergent := append([]ledger.IndexedBlock(nil), chunk...)
	divergent[1].ParentHash = common.Hash{0xff}
	_, err := adapter.UpsertIndexedBlocks(ctx, divergent)
	var conflict *storepostgres.InvariantConflictError
	if !errors.As(err, &conflict) || conflict.Key != "chain_id=49020 block_number=11" {
		t.Fatalf("divergent chunk error = %T %v, want conflict on block 11", err, err)
	}

	// A hash reused at a new height violates the per-chain hash key: reported for the range.
	reused := []ledger.IndexedBlock{{ChainID: chainID, BlockNumber: 13, BlockHash: chunk[2].BlockHash, ParentHash: chunk[2].BlockHash, BlockTime: blockTime, FinalityStatus: "observed"}}
	_, err = adapter.UpsertIndexedBlocks(ctx, reused)
	if !errors.As(err, &conflict) || conflict.Key != "chain_id=49020 block_number=13..13" {
		t.Fatalf("reused hash error = %T %v, want range conflict", err, err)
	}

	for name, blocks := range map[string][]ledger.IndexedBlock{
		"mixed chains": {chunk[0], {ChainID: chainID + 1, BlockNumber: 20, BlockHash: common.Hash{0xc1}, ParentHash: common.Hash{0xc0}, BlockTime: blockTime, FinalityStatus: "observed"}},
		"duplicate":    {chunk[0], chunk[0]},
	} {
		if _, err := adapter.UpsertIndexedBlocks(ctx, blocks); err == nil || errors.As(err, &conflict) {
			t.Fatalf("%s error = %v, want a validation error", name, err)
		}
	}
}
