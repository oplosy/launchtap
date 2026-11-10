//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/forum"
	"github.com/Contictus/launchtap/backend/internal/social"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
)

func forumIDs(page forum.PostPage) []int64 {
	out := make([]int64, len(page.Items))
	for i, p := range page.Items {
		out[i] = p.ID
	}
	return out
}

func communitySummary(items []forum.Community) []string {
	out := make([]string, len(items))
	for i, c := range items {
		out[i] = c.Token.Hex()[38:] + ":" + string(rune('0'+c.CommentCount))
	}
	return out
}

// Spec P0-C5 AC1-AC2.
func TestForumFeedAndCommunities(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	store := storepostgres.ForumStore{Pool: pool}
	scope := social.Scope{ChainID: socialChainID, DeploymentID: socialDep}
	a := seedSocialToken(t, ctx, database, 0x10)
	b := seedSocialToken(t, ctx, database, 0x20)
	c := seedSocialToken(t, ctx, database, 0x30)

	insertSocialUser(t, ctx, database.DB, socialDep, "did:named", strPtr("alice"), false)
	insertSocialUser(t, ctx, database.DB, socialDep, "did:banned", nil, true)
	wallet := socialHolder.Bytes()

	a1 := insertSocialComment(t, ctx, database.DB, a.Bytes(), "did:plain", wallet, "a1", false)
	b1 := insertSocialComment(t, ctx, database.DB, b.Bytes(), "did:named", wallet, "b1", false)
	insertSocialComment(t, ctx, database.DB, a.Bytes(), "did:plain", wallet, "hidden", true)
	insertSocialComment(t, ctx, database.DB, b.Bytes(), "did:banned", wallet, "banned", false)
	a2 := insertSocialComment(t, ctx, database.DB, a.Bytes(), "did:plain", wallet, "a2", false)
	c1 := insertSocialComment(t, ctx, database.DB, c.Bytes(), "did:plain", wallet, "c1", false)
	// Another deployment's comment on the same token never appears.
	mustExecSocial(t, ctx, database.DB, `
		INSERT INTO token_comments (chain_id, deployment_id, token_address, privy_did, wallet, author_role, body)
		VALUES ($1, 'dep-b', $2, 'did:plain', $3, 'holder', 'other deployment')`, socialChainID, a.Bytes(), wallet)
	// c1 is older than the hot window.
	mustExecSocial(t, ctx, database.DB, `UPDATE token_comments SET created_at = now() - interval '25 hours' WHERE id = $1`, c1)

	first, err := store.Posts(ctx, scope, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{c1, a2}; !slices.Equal(forumIDs(first), want) || first.NextCursor == "" {
		t.Fatalf("first page = %v next=%q, want %v", forumIDs(first), first.NextCursor, want)
	}
	if first.Items[0].Token != c || first.Items[0].Symbol == "" || first.Items[0].Name == "" {
		t.Fatalf("first post = %+v", first.Items[0])
	}
	second, err := store.Posts(ctx, scope, first.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{b1, a1}; !slices.Equal(forumIDs(second), want) || second.NextCursor != "" {
		t.Fatalf("second page = %v next=%q, want %v", forumIDs(second), second.NextCursor, want)
	}
	// C2 display rule: a username hides an unpublished wallet; no username shows the wallet.
	named, plain := second.Items[0].Author, second.Items[1].Author
	if named.Username == nil || *named.Username != "alice" || named.Wallet != nil {
		t.Fatalf("named author = %+v", named)
	}
	if plain.Username != nil || plain.Wallet == nil || *plain.Wallet != socialHolder {
		t.Fatalf("plain author = %+v", plain)
	}
	if _, err := store.Posts(ctx, scope, "garbage", 2); !errors.Is(err, social.ErrInvalidCursor) {
		t.Fatalf("bad cursor err = %v", err)
	}

	hot, err := store.Communities(ctx, scope, forum.SortHot, 10)
	if err != nil {
		t.Fatal(err)
	}
	// a: 2 visible in 24 h; b: 1 (the banned author's does not count); c: none in 24 h.
	if want := []string{a.Hex()[38:] + ":2", b.Hex()[38:] + ":1"}; !slices.Equal(communitySummary(hot), want) {
		t.Fatalf("hot = %v, want %v", communitySummary(hot), want)
	}
	top, err := store.Communities(ctx, scope, forum.SortTop, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Ties (b and c with one each) break on the newest comment: c1 was inserted after b1.
	if want := []string{a.Hex()[38:] + ":2", c.Hex()[38:] + ":1", b.Hex()[38:] + ":1"}; !slices.Equal(communitySummary(top), want) {
		t.Fatalf("top = %v, want %v", communitySummary(top), want)
	}
	if limited, err := store.Communities(ctx, scope, forum.SortTop, 1); err != nil || len(limited) != 1 {
		t.Fatalf("limited = %v err=%v", communitySummary(limited), err)
	}

	// A reorg removing token a's row drops its comments from the feed and the rankings.
	conn, err := database.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `SET session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE tokens SET token_address = $3 WHERE chain_id = $1 AND token_address = $2`, socialChainID, a.Bytes(), addressBytes(0xee)); err != nil {
		t.Fatalf("move token row: %v", err)
	}
	all, err := store.Posts(ctx, scope, "", 10)
	if want := []int64{c1, b1}; err != nil || !slices.Equal(forumIDs(all), want) {
		t.Fatalf("after reorg = %v err=%v, want %v", forumIDs(all), err, want)
	}
	if hot, err := store.Communities(ctx, scope, forum.SortHot, 10); err != nil || len(hot) != 1 || hot[0].Token != b {
		t.Fatalf("hot after reorg = %v err=%v", communitySummary(hot), err)
	}
}
