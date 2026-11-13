//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/social"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
)

// Comment votes (docs/specs/2026-11-12-comment-votes-design.md).
func TestCommentVotesHolderGateScoreAndBans(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store, pool := newSocialStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	scope := social.Scope{ChainID: socialChainID, DeploymentID: socialDep}
	author := socialActor("did:author", socialHolder)
	comment, err := store.Post(ctx, author, token, socialHolder, "vote on me")
	if err != nil {
		t.Fatal(err)
	}
	creatorVoter := socialActor("did:voter-creator", socialCreator)
	holderVoter := socialActor("did:voter-holder", socialHolder, socialHolder)

	vote := func(a social.Actor, value int, wantScore int64) {
		t.Helper()
		got, err := store.Vote(ctx, a, comment.ID, value)
		if err != nil || got.Score != wantScore || got.Vote != value {
			t.Fatalf("%s votes %d: got %+v err=%v; want score %d", a.PrivyDID, value, got, err, wantScore)
		}
	}
	vote(creatorVoter, 1, 1)
	vote(holderVoter, 1, 2)
	vote(creatorVoter, -1, 0) // a changed vote replaces the old one
	vote(creatorVoter, -1, 0) // a repeat is a no-op

	if _, err := store.Vote(ctx, socialActor("did:stray", socialStray), comment.ID, 1); !errors.Is(err, social.ErrNotHolder) {
		t.Fatalf("non-holder err = %v", err)
	}
	vote(socialActor("did:stray", socialStray), 0, 0) // clearing needs no holding
	if _, err := store.Vote(ctx, author, comment.ID, 1); !errors.Is(err, social.ErrOwnVote) {
		t.Fatalf("own vote err = %v", err)
	}
	insertSocialUser(t, ctx, database.DB, socialDep, "did:banned", nil, true)
	if _, err := store.Vote(ctx, socialActor("did:banned", socialHolder), comment.ID, 1); !errors.Is(err, social.ErrBanned) {
		t.Fatalf("banned err = %v", err)
	}
	hidden := insertSocialComment(t, ctx, database.DB, token.Bytes(), "did:author", socialHolder.Bytes(), "hidden", true)
	if _, err := store.Vote(ctx, creatorVoter, hidden, 1); !errors.Is(err, social.ErrCommentNotFound) {
		t.Fatalf("hidden err = %v", err)
	}

	// A ban removes the voter's weight at once; clearing removes the vote.
	mustExecSocial(t, ctx, database.DB, `UPDATE social_users SET banned_at = now(), ban_reason = 'test' WHERE privy_did = 'did:voter-holder'`)
	page, err := store.List(ctx, scope, token, "", 50)
	if err != nil || len(page.Items) != 1 || page.Items[0].Score != -1 {
		t.Fatalf("list after ban = %+v err=%v; want score -1", page.Items, err)
	}
	vote(creatorVoter, 0, 0)

	// Best: recent posts by score, then newest; a post older than the window drops out.
	forumStore := storepostgres.ForumStore{Pool: pool}
	popular := insertSocialComment(t, ctx, database.DB, token.Bytes(), "did:author", socialHolder.Bytes(), "popular", false)
	old := insertSocialComment(t, ctx, database.DB, token.Bytes(), "did:author", socialHolder.Bytes(), "old", false)
	mustExecSocial(t, ctx, database.DB, `UPDATE token_comments SET created_at = now() - interval '8 days' WHERE id = $1`, old)
	for _, id := range []int64{popular, old} {
		if _, err := store.Vote(ctx, creatorVoter, id, 1); err != nil {
			t.Fatal(err)
		}
	}
	best, err := forumStore.BestPosts(ctx, scope, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(best) != 2 || best[0].ID != popular || best[0].Score != 1 || best[1].ID != comment.ID || best[1].Symbol != "TKN" {
		t.Fatalf("best = %+v", best)
	}
	feed, err := forumStore.Posts(ctx, scope, "", 10)
	if err != nil || len(feed.Items) != 3 || feed.Items[0].ID != old || feed.Items[0].Score != 1 {
		t.Fatalf("feed = %+v err=%v", feed.Items, err)
	}
}

func TestCommentVotesHourlyLimit(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store, _ := newSocialStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	voter := socialActor("did:busy", socialCreator)
	insertSocialUser(t, ctx, database.DB, socialDep, "did:author", nil, false)
	ids := make([]int64, 0, social.VotesPerHour+1)
	for range social.VotesPerHour + 1 {
		ids = append(ids, insertSocialComment(t, ctx, database.DB, token.Bytes(), "did:author", socialHolder.Bytes(), "c", false))
	}
	for _, id := range ids[:social.VotesPerHour] {
		mustExecSocial(t, ctx, database.DB, `INSERT INTO comment_votes (comment_id, chain_id, deployment_id, voter_did, value) VALUES ($1, $2, $3, 'did:busy', 1)`, id, socialChainID, socialDep)
	}
	_, err := store.Vote(ctx, voter, ids[social.VotesPerHour], 1)
	rl := rateLimited(t, err)
	if rl.RetryAfter < time.Second || rl.RetryAfter > time.Hour {
		t.Fatalf("RetryAfter = %s", rl.RetryAfter)
	}
	var n int
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM comment_votes WHERE voter_did = 'did:busy'`).Scan(&n); err != nil || n != social.VotesPerHour {
		t.Fatalf("votes = %d err=%v; the rejected vote must roll back", n, err)
	}
	// Repeating an existing vote and clearing one are never limited.
	if _, err := store.Vote(ctx, voter, ids[0], 1); err != nil {
		t.Fatalf("repeat at cap: %v", err)
	}
	if _, err := store.Vote(ctx, voter, ids[1], 0); err != nil {
		t.Fatalf("clear at cap: %v", err)
	}
}
