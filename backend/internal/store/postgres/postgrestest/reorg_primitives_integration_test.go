//go:build integration

package postgrestest

import (
	"context"
	"testing"
	"time"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

func TestReorgPrimitivesDeleteEventsBeforeBlocksAndKeepMetadata(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URLAs(t, IndexerRole))
	const chainID int64 = 49012
	base := time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC)
	for number := int64(1); number <= 3; number++ {
		mustInsertBlock(t, ctx, database.DB, chainID, number, hashBytes(byte(number)), hashBytes(byte(number-1)), base.Add(time.Duration(number)*time.Minute), "observed")
	}
	token, curve, pair, weth := addressBytes(1), addressBytes(2), addressBytes(3), addressBytes(4)
	insertProjectionLaunch(t, ctx, database.DB, chainID, 1, hashBytes(1), base.Add(time.Minute), hashBytes(11), projectionLaunchFixture{token: token, curve: curve, pair: pair, weth: weth})
	insertProjectionTrade(t, ctx, database.DB, chainID, 2, hashBytes(2), base.Add(2*time.Minute), hashBytes(12), 0, token, 10, 1, 99)
	insertProjectionGraduation(t, ctx, database.DB, chainID, 3, hashBytes(3), base.Add(3*time.Minute), hashBytes(13), 0, token, pair)
	insertPoolSync(t, ctx, database.DB, chainID, 3, hashBytes(3), base.Add(3*time.Minute), hashBytes(14), 1, pair, 50, 60)
	callRebuild(t, ctx, database.DB, chainID, token)
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO token_metadata (chain_id, token_address, description, updated_at) VALUES ($1,$2,'kept',now())`, chainID, token); err != nil {
		t.Fatalf("insert metadata: %v", err)
	}

	adapter := storepostgres.NewAdapter(pool)
	ancestor, err := adapter.FindCommonAncestor(ctx, chainID, []common.Hash{common.Hash(hashBytes(3)), common.Hash(hashBytes(1))})
	if err != nil || ancestor.BlockNumber != 3 {
		t.Fatalf("highest candidate ancestor = %+v, %v; want block 3", ancestor, err)
	}
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, tx *storepostgres.Adapter) error {
		affected, err := tx.AffectedTokensAbove(ctx, chainID, 1)
		if err != nil {
			return err
		}
		if len(affected) != 1 || affected[0] != common.Address(token) {
			t.Fatalf("affected tokens = %v, want %x", affected, token)
		}
		if err := tx.DeleteCanonicalAbove(ctx, chainID, 1); err != nil {
			return err
		}
		for _, affectedToken := range affected {
			if err := tx.RebuildTokenProjections(ctx, chainID, affectedToken); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("rollback canonical branch: %v", err)
	}

	for _, table := range []string{"trades", "graduations", "pool_syncs"} {
		var count int
		if err := database.DB.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE chain_id=$1", chainID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows = %d, want 0", table, count)
		}
	}
	var blocks int
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM indexed_blocks WHERE chain_id=$1`, chainID).Scan(&blocks); err != nil || blocks != 1 {
		t.Fatalf("surviving blocks = %d, %v; want 1", blocks, err)
	}
	var phase, metadata string
	if err := database.DB.QueryRowContext(ctx, `SELECT phase FROM tokens WHERE chain_id=$1 AND token_address=$2`, chainID, token).Scan(&phase); err != nil || phase != "curve" {
		t.Fatalf("token phase = %q, %v; want curve", phase, err)
	}
	if err := database.DB.QueryRowContext(ctx, `SELECT description FROM token_metadata WHERE chain_id=$1 AND token_address=$2`, chainID, token).Scan(&metadata); err != nil || metadata != "kept" {
		t.Fatalf("metadata = %q, %v; want kept", metadata, err)
	}
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, tx *storepostgres.Adapter) error {
		if err := tx.DeleteCanonicalAbove(ctx, chainID, 0); err != nil {
			return err
		}
		return tx.RebuildTokenProjections(ctx, chainID, common.Address(token))
	}); err != nil {
		t.Fatalf("rollback launch itself: %v", err)
	}
	for _, table := range []string{"token_launches", "tokens", "token_reserves", "holder_balances", "candles", "token_stats", "aggregation_dirty", "indexed_blocks"} {
		var count int
		if err := database.DB.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE chain_id=$1", chainID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("orphan %s count=%d: %v", table, count, err)
		}
	}
	if err := database.DB.QueryRowContext(ctx, `SELECT description FROM token_metadata WHERE chain_id=$1 AND token_address=$2`, chainID, token).Scan(&metadata); err != nil || metadata != "kept" {
		t.Fatalf("orphan metadata = %q, %v; want kept", metadata, err)
	}
	mustInsertBlock(t, ctx, database.DB, chainID, 4, hashBytes(4), hashBytes(3), base.Add(4*time.Minute), "observed")
	insertProjectionLaunch(t, ctx, database.DB, chainID, 4, hashBytes(4), base.Add(4*time.Minute), hashBytes(21), projectionLaunchFixture{token: token, curve: curve, pair: pair, weth: weth})
	callRebuild(t, ctx, database.DB, chainID, token)
	if err := database.DB.QueryRowContext(ctx, `SELECT metadata.description FROM token_metadata metadata JOIN tokens USING(chain_id,token_address) WHERE chain_id=$1 AND token_address=$2`, chainID, token).Scan(&metadata); err != nil || metadata != "kept" {
		t.Fatalf("reattached metadata: %q %v", metadata, err)
	}
}
