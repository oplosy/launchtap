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
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTokenListAgeCursorStable(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	const chainID int64 = 46630
	const deployment = "age-filter-test"
	now := time.Now().UTC().Truncate(time.Second)

	seed := func(block int64, tag byte, launchedAt time.Time) []byte {
		blockHash := hashBytes(tag)
		tokenBytes := addressBytes(tag + 1)
		mustInsertBlock(t, ctx, database.DB, chainID, block, blockHash, hashBytes(tag-1), launchedAt, "safe")
		insertProjectionLaunch(t, ctx, database.DB, chainID, block, blockHash, launchedAt, hashBytes(tag+2), projectionLaunchFixture{token: tokenBytes, curve: addressBytes(tag + 3), pair: addressBytes(tag + 4), weth: addressBytes(tag + 5)})
		if _, err := database.DB.ExecContext(ctx, `SELECT rebuild_token_projections($1,$2)`, chainID, tokenBytes); err != nil {
			t.Fatal(err)
		}
		return tokenBytes
	}
	oldTok := seed(100, 0x10, now.Add(-30*time.Hour))
	twoH := seed(101, 0x20, now.Add(-2*time.Hour))
	oneH := seed(102, 0x30, now.Add(-1*time.Hour))
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO sync_state(chain_id,deployment_id,observed_number,observed_hash,observed_at,safe_number,safe_hash,safe_at) VALUES($1,$2,102,$3,$4,102,$3,$4)`, chainID, deployment, hashBytes(0x30), now); err != nil {
		t.Fatal(err)
	}
	reader := storepostgres.TokenReader{Pool: pool, DeploymentID: deployment}
	query := func(age string, limit int, cur *pagination.Cursor) (token.Page, error) {
		return reader.List(ctx, token.ListQuery{ChainID: chainID, Phase: "curve", Sort: "newest", Limit: limit, Age: age, Now: now, Cursor: cur})
	}

	// age=all still sees everything.
	all, err := query("all", 20, nil)
	if err != nil || len(all.Items) != 3 {
		t.Fatalf("all items=%d err=%v", len(all.Items), err)
	}
	if c, _ := pagination.Decode(all.NextCursor); all.NextCursor != "" && c.Since != "" {
		t.Fatalf("all cursor must not carry since: %+v", c)
	}

	p1, err := query("24h", 1, nil)
	if err != nil || len(p1.Items) != 1 || p1.Items[0].Address != common.BytesToAddress(oneH) || p1.NextCursor == "" {
		t.Fatalf("page 1=%+v err=%v", p1, err)
	}
	c1, err := pagination.Decode(p1.NextCursor)
	if err != nil || c1.Since != now.Add(-24*time.Hour).Format(time.RFC3339) {
		t.Fatalf("cursor since=%q err=%v", c1.Since, err)
	}

	// A token launched after page 1 must not leak into page 2, and the window must not slide.
	later := now.Add(10 * time.Minute)
	seed(103, 0x40, later)
	if _, err := database.DB.ExecContext(ctx, `UPDATE sync_state SET observed_number=103, observed_hash=$3, safe_number=103, safe_hash=$3 WHERE chain_id=$1 AND deployment_id=$2`, chainID, deployment, hashBytes(0x40)); err != nil {
		t.Fatal(err)
	}
	laterQuery := func(cur *pagination.Cursor) (token.Page, error) {
		return reader.List(ctx, token.ListQuery{ChainID: chainID, Phase: "curve", Sort: "newest", Limit: 1, Age: "24h", Now: later.Add(23 * time.Hour), Cursor: cur})
	}
	p2, err := laterQuery(&c1)
	if err != nil || len(p2.Items) != 1 || p2.Items[0].Address != common.BytesToAddress(twoH) {
		t.Fatalf("page 2=%+v err=%v", p2, err)
	}
	c2, err := pagination.Decode(p2.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	p3, err := laterQuery(&c2)
	if err != nil || len(p3.Items) != 0 || p3.NextCursor != "" {
		t.Fatalf("page 3=%+v err=%v (the 30h token must stay filtered)", p3, err)
	}
	_ = oldTok

	// A cursor minted for age=24h is rejected for age=7d and for age=all.
	for _, age := range []string{"7d", "all"} {
		if _, err := query(age, 1, &c1); !errors.Is(err, pagination.ErrInvalidCursor) {
			t.Fatalf("24h cursor accepted for age=%s: %v", age, err)
		}
	}
	// A malformed since is the invalid-cursor problem.
	bad := c1
	bad.Since = "not-a-time"
	if _, err := query("24h", 1, &bad); !errors.Is(err, pagination.ErrInvalidCursor) {
		t.Fatalf("malformed since accepted: %v", err)
	}
}

func TestTokenListAgeCutoffIsInclusive(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	const chainID int64 = 46630
	const deployment = "age-boundary-test"
	now := time.Now().UTC().Truncate(time.Second)
	launchedAt := now.Add(-24 * time.Hour) // exactly at the cutoff
	blockHash := hashBytes(0x51)
	tokenBytes := addressBytes(0x52)
	mustInsertBlock(t, ctx, database.DB, chainID, 100, blockHash, hashBytes(0x50), launchedAt, "safe")
	insertProjectionLaunch(t, ctx, database.DB, chainID, 100, blockHash, launchedAt, hashBytes(0x53), projectionLaunchFixture{token: tokenBytes, curve: addressBytes(0x54), pair: addressBytes(0x55), weth: addressBytes(0x56)})
	if _, err := database.DB.ExecContext(ctx, `SELECT rebuild_token_projections($1,$2)`, chainID, tokenBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO sync_state(chain_id,deployment_id,observed_number,observed_hash,observed_at,safe_number,safe_hash,safe_at) VALUES($1,$2,100,$3,$4,100,$3,$4)`, chainID, deployment, blockHash, now); err != nil {
		t.Fatal(err)
	}
	page, err := (storepostgres.TokenReader{Pool: pool, DeploymentID: deployment}).List(ctx, token.ListQuery{ChainID: chainID, Phase: "curve", Sort: "newest", Limit: 20, Age: "24h", Now: now})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("token exactly at the cutoff must be included: items=%d err=%v", len(page.Items), err)
	}
}
