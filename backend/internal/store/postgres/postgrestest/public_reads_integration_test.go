//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/Contictus/launchtap/backend/internal/trading"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPublicReadsUseOneCanonicalSnapshot(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	const chainID int64 = 46630
	const deployment = "api-read-test"
	at := time.Date(2026, 9, 9, 8, 0, 0, 0, time.UTC)
	blockHash := hashBytes(0x61)
	tokenBytes := addressBytes(0x62)
	mustInsertBlock(t, ctx, database.DB, chainID, 100, blockHash, hashBytes(0x60), at, "safe")
	insertProjectionLaunch(t, ctx, database.DB, chainID, 100, blockHash, at, hashBytes(0x63), projectionLaunchFixture{token: tokenBytes, curve: addressBytes(0x64), pair: addressBytes(0x65), weth: addressBytes(0x66)})
	insertProjectionTrade(t, ctx, database.DB, chainID, 100, blockHash, at, hashBytes(0x67), 1, tokenBytes, 100, 110, 900000)
	if _, err := database.DB.ExecContext(ctx, `SELECT rebuild_token_projections($1,$2)`, chainID, tokenBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO sync_state(chain_id,deployment_id,observed_number,observed_hash,observed_at,safe_number,safe_hash,safe_at) VALUES($1,$2,100,$3,$4,100,$3,$4)`, chainID, deployment, blockHash, at); err != nil {
		t.Fatal(err)
	}
	tokens := storepostgres.TokenReader{Pool: pool, DeploymentID: deployment}
	detail, err := tokens.Get(ctx, chainID, common.BytesToAddress(tokenBytes))
	if err != nil {
		t.Fatal(err)
	}
	if detail.Snapshot.BlockNumber != 100 || detail.Finality != "safe" || detail.ETHReserve.String() != "110" {
		t.Fatalf("detail=%+v", detail)
	}
	for _, sort := range []string{"newest", "oldest", "market_cap", "volume_24h"} {
		page, err := tokens.List(ctx, token.ListQuery{ChainID: chainID, Phase: "curve", Sort: sort, Limit: 20})
		if err != nil {
			t.Fatalf("list tokens sorted by %s: %v", sort, err)
		}
		if len(page.Items) != 1 || page.Items[0].Address != common.BytesToAddress(tokenBytes) || page.Finality != "safe" {
			t.Fatalf("page sorted by %s = %+v", sort, page)
		}
	}
	for search, want := range map[string]int{"tok": 1, "%": 0, "_oken": 0, `\`: 0} {
		page, err := tokens.List(ctx, token.ListQuery{ChainID: chainID, Phase: "curve", Sort: "newest", Search: search, Limit: 20})
		if err != nil || len(page.Items) != want {
			t.Fatalf("search %q items=%d err=%v, want %d", search, len(page.Items), err, want)
		}
	}
	one, err := tokens.List(ctx, token.ListQuery{ChainID: chainID, Phase: "curve", Sort: "newest", Limit: 1})
	if err != nil || one.NextCursor == "" {
		t.Fatalf("first cursor page=%+v err=%v", one, err)
	}
	decoded, err := pagination.Decode(one.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tokens.List(ctx, token.ListQuery{ChainID: chainID, Phase: "graduated", Sort: "newest", Limit: 1, Cursor: &decoded})
	if !errors.Is(err, pagination.ErrInvalidCursor) {
		t.Fatalf("changed phase accepted: %v", err)
	}
	trades, err := (storepostgres.MarketReader{Pool: pool, DeploymentID: deployment}).ListTrades(ctx, trading.Query{ChainID: chainID, Token: common.BytesToAddress(tokenBytes), Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(trades.Items) != 1 || trades.Items[0].ETHVolume.String() != "100" {
		t.Fatalf("trades=%+v", trades)
	}

	// Tip advancement keeps a canonical cursor usable; removing its block (a reorg) does not.
	nextHash := hashBytes(0x68)
	mustInsertBlock(t, ctx, database.DB, chainID, 101, nextHash, blockHash, at.Add(time.Second), "observed")
	if _, err := database.DB.ExecContext(ctx, `UPDATE sync_state SET observed_number=101, observed_hash=$3 WHERE chain_id=$1 AND deployment_id=$2`, chainID, deployment, nextHash); err != nil {
		t.Fatal(err)
	}
	advanced, err := tokens.List(ctx, token.ListQuery{ChainID: chainID, Phase: "curve", Sort: "newest", Limit: 1, Cursor: &decoded})
	if err != nil {
		t.Fatalf("cursor from an older canonical block was rejected: %v", err)
	}
	if advanced.Snapshot.BlockNumber != 101 || len(advanced.Items) != 0 {
		t.Fatalf("advanced page=%+v", advanced)
	}
	orphaned := decoded
	orphaned.Snapshot.BlockHash = [32]byte(hashBytes(0x69))
	if _, err := tokens.List(ctx, token.ListQuery{ChainID: chainID, Phase: "curve", Sort: "newest", Limit: 1, Cursor: &orphaned}); !errors.Is(err, pagination.ErrCursorInvalidated) {
		t.Fatalf("cursor from an orphaned block was accepted: %v", err)
	}
}
