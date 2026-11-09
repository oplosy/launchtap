//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/social"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

var (
	profileW1 = common.HexToAddress("0x00000000000000000000000000000000000000b1")
	profileW2 = common.HexToAddress("0x00000000000000000000000000000000000000b2")
)

func newProfileStore(t *testing.T, ctx context.Context, database *Database) storepostgres.ProfileStore {
	t.Helper()
	return storepostgres.ProfileStore{Pool: openPool(t, ctx, database.URL)}
}

// ageUsernameChange moves the DID's last username change back by d on the database clock.
func ageUsernameChange(t *testing.T, ctx context.Context, database *Database, did string, d time.Duration) {
	t.Helper()
	mustExecSocial(t, ctx, database.DB, `
		UPDATE social_users SET username_changed_at = username_changed_at - make_interval(secs => $2)
		WHERE privy_did = $1`, did, d.Seconds())
}

func TestPutProfileClaimAndChangeInterval(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store := newProfileStore(t, ctx, database)
	alice := socialActor("did:alice", profileW1)

	empty, err := store.Get(ctx, alice)
	if err != nil || empty.Username != nil || len(empty.PublicWallets) != 0 || empty.NextChangeAt != nil {
		t.Fatalf("empty profile = %+v, %v", empty, err)
	}
	p, err := store.Put(ctx, alice, strPtr("alice"), []common.Address{profileW1})
	if err != nil {
		t.Fatalf("first set: %v", err)
	}
	if p.Username == nil || *p.Username != "alice" || len(p.PublicWallets) != 1 || p.PublicWallets[0] != profileW1 || p.NextChangeAt == nil {
		t.Fatalf("after first set = %+v", p)
	}
	// Saving the same name is not a change.
	if _, err := store.Put(ctx, alice, strPtr("alice"), []common.Address{profileW1}); err != nil {
		t.Fatalf("same name: %v", err)
	}
	rl := rateLimited(t, func() error { _, err := store.Put(ctx, alice, strPtr("alice2"), nil); return err }())
	if rl.RetryAfter < 6*24*time.Hour || rl.RetryAfter > social.UsernameChangeInterval {
		t.Fatalf("RetryAfter = %v", rl.RetryAfter)
	}
	// A wallets-only change is allowed inside the interval.
	if p, err = store.Put(ctx, alice, nil, []common.Address{}); err != nil || len(p.PublicWallets) != 0 || *p.Username != "alice" {
		t.Fatalf("wallets-only = %+v, %v", p, err)
	}
	ageUsernameChange(t, ctx, database, "did:alice", 8*24*time.Hour)
	if p, err = store.Put(ctx, alice, strPtr("alice2"), nil); err != nil || *p.Username != "alice2" {
		t.Fatalf("change after 8 days = %+v, %v", p, err)
	}

	// Errors: not linked, too many wallets, banned.
	if _, err := store.Put(ctx, alice, nil, []common.Address{profileW2}); !errors.Is(err, social.ErrWalletNotLinked) {
		t.Fatalf("unlinked err = %v", err)
	}
	many := []common.Address{profileW1, profileW2, socialHolder, socialStray, socialCreator, common.HexToAddress("0x00000000000000000000000000000000000000b9")}
	if _, err := store.Put(ctx, socialActor("did:alice", many...), nil, many); !errors.Is(err, social.ErrTooManyPublicWallets) {
		t.Fatalf("too many err = %v", err)
	}
	mustExecSocial(t, ctx, database.DB, `UPDATE social_users SET banned_at = now(), ban_reason = 'x' WHERE privy_did = 'did:alice'`)
	if _, err := store.Put(ctx, alice, nil, nil); !errors.Is(err, social.ErrBanned) {
		t.Fatalf("banned err = %v", err)
	}
	if p, err := store.Get(ctx, alice); err != nil || !p.Banned {
		t.Fatalf("banned profile = %+v, %v", p, err)
	}
	// Delete is allowed for a banned user and counts as a change.
	if err := store.Delete(ctx, alice); err != nil {
		t.Fatalf("delete banned: %v", err)
	}
	if p, err := store.Get(ctx, alice); err != nil || p.Username != nil || p.NextChangeAt == nil {
		t.Fatalf("after delete = %+v, %v", p, err)
	}
}

func TestUsernameRaceAndHold(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store := newProfileStore(t, ctx, database)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, did := range []string{"did:one", "did:two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = store.Put(ctx, socialActor(did), strPtr("race"), nil)
		}()
	}
	wg.Wait()
	okCount, taken := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			okCount++
		case errors.Is(err, social.ErrUsernameTaken):
			taken++
		default:
			t.Fatalf("race err = %v", err)
		}
	}
	if okCount != 1 || taken != 1 {
		t.Fatalf("race results ok=%d taken=%d", okCount, taken)
	}

	// The owner releases the name; it is held from other users but reclaimable by its owner.
	owner := "did:one"
	if errs[0] != nil {
		owner = "did:two"
	}
	if err := store.Delete(ctx, socialActor(owner)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(ctx, socialActor("did:three"), strPtr("race"), nil); !errors.Is(err, social.ErrUsernameTaken) {
		t.Fatalf("held name err = %v", err)
	}
	ageUsernameChange(t, ctx, database, owner, 8*24*time.Hour)
	if p, err := store.Put(ctx, socialActor(owner), strPtr("race"), nil); err != nil || *p.Username != "race" {
		t.Fatalf("owner reclaim = %+v, %v", p, err)
	}
	// After the 30-day hold expires the name is free again (once released).
	ageUsernameChange(t, ctx, database, owner, 8*24*time.Hour)
	if _, err := store.Put(ctx, socialActor(owner), strPtr("race_new"), nil); err != nil {
		t.Fatal(err)
	}
	mustExecSocial(t, ctx, database.DB, `UPDATE username_releases SET released_at = released_at - interval '31 days' WHERE username = 'race'`)
	if p, err := store.Put(ctx, socialActor("did:three"), strPtr("race"), nil); err != nil || *p.Username != "race" {
		t.Fatalf("after hold = %+v, %v", p, err)
	}
}

func TestPublicWalletTakeoverAndPrune(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store := newProfileStore(t, ctx, database)

	if _, err := store.Put(ctx, socialActor("did:a", profileW1, profileW2), strPtr("aaa"), []common.Address{profileW1, profileW2}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(ctx, socialActor("did:b", profileW1), strPtr("bbb"), []common.Address{profileW1}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM social_public_wallets WHERE wallet = $1 AND privy_did = 'did:b'`, profileW1.Bytes()); n != 1 {
		t.Fatalf("W1 owned by b rows = %d", n)
	}
	// A's next Put, with W1 no longer linked, keeps only W2.
	p, err := store.Put(ctx, socialActor("did:a", profileW2), nil, []common.Address{profileW2})
	if err != nil || len(p.PublicWallets) != 1 || p.PublicWallets[0] != profileW2 {
		t.Fatalf("a after takeover = %+v, %v", p, err)
	}
	// Delete unpublishes every wallet.
	if err := store.Delete(ctx, socialActor("did:a", profileW2)); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM social_public_wallets WHERE privy_did = 'did:a'`); n != 0 {
		t.Fatalf("a wallets after delete = %d", n)
	}
}

func TestWalletAndLeaderboardUsername(t *testing.T) {
	ctx, cancel := walletCtx(t)
	defer cancel()
	f := newWalletReaderFixture(t, ctx)
	f.seedWallets()
	f.totals(0xb1, 500, 2, 0)
	store := storepostgres.ProfileStore{Pool: f.pool}
	actor := func(did string, wallets ...common.Address) social.Actor {
		return social.Actor{Scope: social.Scope{ChainID: walletChainID, DeploymentID: walletReaderDeployment}, PrivyDID: did, Wallets: wallets}
	}
	published, unpublished := wantAddr(0xb1), wantAddr(walletW2)
	if _, err := store.Put(ctx, actor("did:w", published, unpublished), strPtr("whale"), []common.Address{published}); err != nil {
		t.Fatal(err)
	}

	username := func(addr common.Address) *string {
		t.Helper()
		v, err := f.reader.GetWallet(ctx, walletChainID, addr)
		if err != nil {
			t.Fatal(err)
		}
		return v.Username
	}
	board := func() map[common.Address]*string {
		t.Helper()
		page, err := f.reader.Leaderboard(ctx, walletChainID, 100)
		if err != nil {
			t.Fatal(err)
		}
		out := map[common.Address]*string{}
		for _, r := range page.Items {
			out[r.Address] = r.Username
		}
		return out
	}
	if got := username(published); got == nil || *got != "whale" {
		t.Fatalf("published wallet username = %v", got)
	}
	if got := username(unpublished); got != nil {
		t.Fatalf("unpublished wallet username = %q", *got)
	}
	if b := board(); b[published] == nil || *b[published] != "whale" || b[unpublished] != nil {
		t.Fatalf("leaderboard usernames = %v", b)
	}

	f.ledger.exec(`UPDATE social_users SET banned_at = now(), ban_reason = 'x' WHERE privy_did = 'did:w'`)
	if got := username(published); got != nil {
		t.Fatalf("banned user's username shown: %q", *got)
	}
	if b := board(); b[published] != nil {
		t.Fatal("banned user's username on leaderboard")
	}
	f.ledger.exec(`UPDATE social_users SET banned_at = NULL, ban_reason = NULL WHERE privy_did = 'did:w'`)
	if err := store.Delete(ctx, actor("did:w", published)); err != nil {
		t.Fatal(err)
	}
	if got := username(published); got != nil {
		t.Fatalf("username after delete = %q", *got)
	}
}
