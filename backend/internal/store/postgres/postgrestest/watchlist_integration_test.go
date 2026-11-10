//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/ethereum/go-ethereum/common"
)

const watchlistChainID int64 = 48022

// seedWatchlistTokens launches n tokens (address tag base+4i) in consecutive blocks.
func seedWatchlistTokens(t *testing.T, ctx context.Context, database *Database, n int, base byte) []common.Address {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	out := make([]common.Address, 0, n)
	for i := 0; i < n; i++ {
		tag := base + byte(4*i)
		block := int64(i + 1)
		mustInsertBlock(t, ctx, database.DB, watchlistChainID, block, hashBytes(byte(0x40+i)), hashBytes(byte(0x3f+i)), at, "observed")
		fixture := projectionLaunchFixture{token: addressBytes(tag), curve: addressBytes(tag + 1), pair: addressBytes(tag + 2), weth: addressBytes(tag + 3)}
		insertProjectionLaunch(t, ctx, database.DB, watchlistChainID, block, hashBytes(byte(0x40+i)), at, hashBytes(byte(0xc0+i)), fixture)
		callRebuild(t, ctx, database.DB, watchlistChainID, fixture.token)
		out = append(out, common.BytesToAddress(fixture.token))
	}
	return out
}

func TestWatchlistAddListRemove(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store := storepostgres.WatchlistStore{Pool: openPool(t, ctx, database.URL)}
	tokens := seedWatchlistTokens(t, ctx, database, 2, 0x10)
	owner := watchlist.Owner{ChainID: watchlistChainID, DeploymentID: "dep-a", PrivyDID: "did:privy:a"}

	for _, tok := range tokens {
		if err := store.Add(ctx, owner, tok); err != nil {
			t.Fatalf("add: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := store.Add(ctx, owner, tokens[0]); err != nil {
		t.Fatalf("re-add must be idempotent: %v", err)
	}
	if err := store.Add(ctx, owner, common.HexToAddress("0x00000000000000000000000000000000000000ff")); !errors.Is(err, watchlist.ErrUnknownToken) {
		t.Fatalf("unknown token err = %v", err)
	}
	entries, err := store.List(ctx, owner)
	if err != nil || len(entries) != 2 {
		t.Fatalf("list = %v err=%v", entries, err)
	}
	if entries[0].Token != tokens[1] || entries[1].Token != tokens[0] {
		t.Fatalf("order = %v %v; want newest first", entries[0].Token, entries[1].Token)
	}
	if s := entries[0].Summary; s == nil || s.Name != "Token" || s.Symbol != "TKN" || s.TotalSupply == nil || s.TotalSupply.Int64() != 1000000 || s.Address != tokens[1] {
		t.Fatalf("summary = %+v", s)
	}
	if err := store.Remove(ctx, owner, tokens[0]); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(ctx, owner, tokens[0]); err != nil {
		t.Fatalf("re-remove must be idempotent: %v", err)
	}
	entries, err = store.List(ctx, owner)
	if err != nil || len(entries) != 1 || entries[0].Token != tokens[1] {
		t.Fatalf("list after remove = %v err=%v", entries, err)
	}
}

func TestWatchlistLimitConcurrent(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	store := storepostgres.WatchlistStore{Pool: openPool(t, ctx, database.URL)}
	tokens := seedWatchlistTokens(t, ctx, database, 5, 0x10)
	owner := watchlist.Owner{ChainID: watchlistChainID, DeploymentID: "dep-a", PrivyDID: "did:privy:full"}
	// 199 pre-existing entries (their token rows need not exist: the table has no FK).
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO watchlist_entries (chain_id, deployment_id, privy_did, token_address)
		SELECT $1, $2, $3, decode(lpad(to_hex(g), 40, '0'), 'hex') FROM generate_series(1000, 1198) AS g
	`, owner.ChainID, owner.DeploymentID, owner.PrivyDID); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, len(tokens))
	for i, tok := range tokens {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = store.Add(ctx, owner, tok)
		}()
	}
	wg.Wait()
	ok, full := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, watchlist.ErrFull):
			full++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || full != 4 {
		t.Fatalf("ok=%d full=%d; want 1 and 4", ok, full)
	}
	var count int
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM watchlist_entries WHERE privy_did = $1`, owner.PrivyDID).Scan(&count); err != nil || count != watchlist.MaxEntries {
		t.Fatalf("count=%d err=%v; want %d", count, err, watchlist.MaxEntries)
	}
	// A full watchlist still accepts a repeat of an existing entry.
	for i, err := range errs {
		if err == nil {
			if again := store.Add(ctx, owner, tokens[i]); again != nil {
				t.Fatalf("idempotent add on full list: %v", again)
			}
		}
	}
}

func TestWatchlistUnavailableToken(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store := storepostgres.WatchlistStore{Pool: openPool(t, ctx, database.URL)}
	tokens := seedWatchlistTokens(t, ctx, database, 1, 0x10)
	owner := watchlist.Owner{ChainID: watchlistChainID, DeploymentID: "dep-a", PrivyDID: "did:privy:gone"}
	if err := store.Add(ctx, owner, tokens[0]); err != nil {
		t.Fatal(err)
	}
	// Simulate a reorg dropping the token row: skip the FK triggers of its dependents.
	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatalf("disable FK triggers: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tokens WHERE chain_id = $1 AND token_address = $2`, watchlistChainID, tokens[0].Bytes()); err != nil {
		t.Fatalf("delete token row: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(ctx, owner)
	if err != nil || len(entries) != 1 {
		t.Fatalf("list = %v err=%v", entries, err)
	}
	if entries[0].Token != tokens[0] || entries[0].Summary != nil {
		t.Fatalf("entry = %+v; want the address with a nil summary", entries[0])
	}
}

func TestWatchlistIsolatedByDeployment(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store := storepostgres.WatchlistStore{Pool: openPool(t, ctx, database.URL)}
	tokens := seedWatchlistTokens(t, ctx, database, 1, 0x10)
	a := watchlist.Owner{ChainID: watchlistChainID, DeploymentID: "dep-a", PrivyDID: "did:privy:same"}
	b := watchlist.Owner{ChainID: watchlistChainID, DeploymentID: "dep-b", PrivyDID: "did:privy:same"}
	other := watchlist.Owner{ChainID: watchlistChainID, DeploymentID: "dep-a", PrivyDID: "did:privy:other"}
	if err := store.Add(ctx, a, tokens[0]); err != nil {
		t.Fatal(err)
	}
	for name, o := range map[string]watchlist.Owner{"other deployment": b, "other user": other} {
		entries, err := store.List(ctx, o)
		if err != nil || len(entries) != 0 {
			t.Fatalf("%s sees %v err=%v", name, entries, err)
		}
	}
	if err := store.Remove(ctx, b, tokens[0]); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(ctx, a)
	if err != nil || len(entries) != 1 {
		t.Fatalf("removal from another deployment affected this one: %v err=%v", entries, err)
	}
}

// Spec C3 AC4: the watch count follows inserts and deletes for one chain and deployment only.
func TestCountWatchersScopedToDeployment(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	store := storepostgres.WatchlistStore{Pool: pool}
	token := common.BytesToAddress(addressBytes(0x77))
	insert := func(chainID int64, deployment, did string) {
		t.Helper()
		if _, err := database.DB.ExecContext(ctx, `INSERT INTO watchlist_entries (chain_id, deployment_id, privy_did, token_address) VALUES ($1, $2, $3, $4)`,
			chainID, deployment, did, token.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int64 {
		t.Helper()
		n, err := store.CountWatchers(ctx, 46630, "dep", token)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(); n != 0 {
		t.Fatalf("empty count = %d", n)
	}
	insert(46630, "dep", "did:a")
	insert(46630, "dep", "did:b")
	insert(46630, "other-dep", "did:c")
	insert(1, "dep", "did:d")
	if n := count(); n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
	if err := store.Remove(ctx, watchlist.Owner{ChainID: 46630, DeploymentID: "dep", PrivyDID: "did:a"}, token); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Fatalf("count after remove = %d, want 1", n)
	}
}
