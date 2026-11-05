//go:build integration

package postgrestest

import (
	"context"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
)

func TestListWatchlistReturnsEntriesWhoseTokenRowIsMissing(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	queries := sqlc.New(pool)

	const chainID int64 = 48021
	const deployment, did = "launchpad-test", "did:privy:watcher"
	now := time.Now().UTC().Truncate(time.Second)
	blockHash := hashBytes(0xa1)
	token, curve, pair, weth := addressBytes(0xa2), addressBytes(0xa3), addressBytes(0xa4), addressBytes(0xa5)
	orphan := addressBytes(0xa6)
	mustInsertBlock(t, ctx, database.DB, chainID, 1, blockHash, hashBytes(0xa0), now.Add(-time.Hour), "observed")
	insertProjectionLaunch(t, ctx, database.DB, chainID, 1, blockHash, now.Add(-time.Hour), hashBytes(0xa7), projectionLaunchFixture{token: token, curve: curve, pair: pair, weth: weth})
	callRebuild(t, ctx, database.DB, chainID, token)

	for _, address := range [][]byte{token, orphan} {
		var addr sqlc.Address
		copy(addr[:], address)
		rows, err := queries.InsertWatchlistEntry(ctx, sqlc.InsertWatchlistEntryParams{ChainID: chainID, DeploymentID: deployment, PrivyDid: did, TokenAddress: addr})
		if err != nil || rows != 1 {
			t.Fatalf("insert watchlist entry rows=%d err=%v", rows, err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	list, err := queries.ListWatchlist(ctx, sqlc.ListWatchlistParams{ChainID: chainID, DeploymentID: deployment, PrivyDid: did, RowLimit: 10})
	if err != nil {
		t.Fatalf("list watchlist with an orphaned entry: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("rows = %d; want 2", len(list))
	}
	// Newest entry first: the orphan was inserted last.
	if list[0].TokenAvailable || string(list[0].WatchedTokenAddress[:]) != string(orphan) {
		t.Fatalf("first row = %+v; want the orphan with token_available=false", list[0])
	}
	if !list[1].TokenAvailable || string(list[1].WatchedTokenAddress[:]) != string(token) || !list[1].Name.Valid {
		t.Fatalf("second row = %+v; want the existing token with token_available=true", list[1])
	}
}
