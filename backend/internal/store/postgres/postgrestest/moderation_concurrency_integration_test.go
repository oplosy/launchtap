//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/social"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pendingModeration opens a transaction that has applied a moderation change but not committed,
// mirroring what Hide and Ban do before they commit.
func pendingModeration(t *testing.T, ctx context.Context, pool *pgxpool.Pool, statement string, args ...any) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(ctx, statement, args...); err != nil {
		t.Fatalf("pending moderation: %v", err)
	}
	return tx
}

// reportBlockedBy starts a report while tx holds an uncommitted moderation change, checks that the
// report waits for it, commits tx and returns the report's result.
func reportBlockedBy(t *testing.T, ctx context.Context, soc storepostgres.SocialStore, tx pgx.Tx, commentID int64) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- soc.Report(ctx, socialActor("did:reporter"), commentID, social.ReasonSpam)
	}()
	select {
	case err := <-done:
		t.Fatalf("report did not wait for the in-flight moderation change (err=%v)", err)
	case <-time.After(1500 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("report still blocked after the moderation commit")
		return nil
	}
}

func TestReportWaitsForInFlightHideAndBan(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	_, soc := newModerationStore(t, ctx, database)
	pool := openPool(t, ctx, database.URL)
	token := seedSocialToken(t, ctx, database, 0x10)
	insertSocialUser(t, ctx, database.DB, socialDep, "did:author", nil, false)

	hidden := seedComment(t, ctx, database, "did:author", token, "to hide", time.Minute)
	tx := pendingModeration(t, ctx, pool, `UPDATE token_comments SET hidden_at = now(), hidden_reason = 'x' WHERE id = $1`, hidden)
	if err := reportBlockedBy(t, ctx, soc, tx, hidden); !errors.Is(err, social.ErrCommentNotFound) {
		t.Fatalf("report vs hide err = %v; want ErrCommentNotFound", err)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM comment_reports WHERE comment_id = $1`, hidden); n != 0 {
		t.Fatalf("reports on hidden comment = %d", n)
	}

	banned := seedComment(t, ctx, database, "did:author", token, "to ban", time.Minute)
	tx = pendingModeration(t, ctx, pool, `UPDATE social_users SET banned_at = now(), ban_reason = 'x' WHERE privy_did = 'did:author'`)
	if err := reportBlockedBy(t, ctx, soc, tx, banned); !errors.Is(err, social.ErrCommentNotFound) {
		t.Fatalf("report vs ban err = %v; want ErrCommentNotFound", err)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM comment_reports WHERE comment_id = $1`, banned); n != 0 {
		t.Fatalf("reports on banned author's comment = %d", n)
	}
}

func TestModerateScopesAreIsolated(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	mod, soc := newModerationStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	idA := seedComment(t, ctx, database, "did:both", token, "in a", 2*time.Minute)
	var idB int64
	if err := database.DB.QueryRowContext(ctx, `
		INSERT INTO token_comments (chain_id, deployment_id, token_address, privy_did, wallet, author_role, body)
		VALUES ($1, 'dep-b', $2, 'did:both', $3, 'holder', 'in b') RETURNING id`, socialChainID, token.Bytes(), socialHolder.Bytes()).Scan(&idB); err != nil {
		t.Fatal(err)
	}
	seedReport(t, ctx, database, idA, "did:r1", "spam", time.Minute)
	mustExecSocial(t, ctx, database.DB, `
		INSERT INTO comment_reports (comment_id, chain_id, deployment_id, reporter_did, reason)
		VALUES ($1, $2, 'dep-b', 'did:r1', 'spam')`, idB, socialChainID)
	mustExecSocial(t, ctx, database.DB, `
		INSERT INTO social_users (chain_id, deployment_id, privy_did, banned_at, ban_reason)
		VALUES ($1, 'dep-b', 'did:both', now(), 'b-reason')`, socialChainID)
	drain := collectNotifies(t, ctx, mod)
	scopeB := social.Scope{ChainID: socialChainID, DeploymentID: "dep-b"}

	// Foreign-scope ids are not found.
	if err := mod.Unhide(ctx, scopeB, idA, "ops", "r"); !errors.Is(err, social.ErrNotFound) {
		t.Fatalf("cross-scope unhide err = %v", err)
	}
	if _, err := mod.Resolve(ctx, scopeB, idA, social.ResolutionActioned, "ops", "r"); !errors.Is(err, social.ErrNotFound) {
		t.Fatalf("cross-scope resolve err = %v", err)
	}
	if _, err := mod.BanCommentAuthor(ctx, scopeB, idA, "ops", "r"); !errors.Is(err, social.ErrNotFound) {
		t.Fatalf("cross-scope ban-by-comment err = %v", err)
	}

	// Ban, then re-ban, in scope A: the first reason and time are kept and scope B is untouched.
	if _, err := mod.Ban(ctx, modScope, "did:both", "ops", "first"); err != nil {
		t.Fatal(err)
	}
	var firstAt time.Time
	if err := database.DB.QueryRowContext(ctx, `SELECT banned_at FROM social_users WHERE deployment_id = $1 AND privy_did = 'did:both'`, socialDep).Scan(&firstAt); err != nil {
		t.Fatal(err)
	}
	if _, err := mod.Ban(ctx, modScope, "did:both", "ops", "second"); err != nil {
		t.Fatal(err)
	}
	var reason string
	var secondAt time.Time
	if err := database.DB.QueryRowContext(ctx, `SELECT ban_reason, banned_at FROM social_users WHERE deployment_id = $1 AND privy_did = 'did:both'`, socialDep).Scan(&reason, &secondAt); err != nil {
		t.Fatal(err)
	}
	if reason != "first" || !secondAt.Equal(firstAt) {
		t.Fatalf("re-ban changed state: reason=%q at=%v first=%v", reason, secondAt, firstAt)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM comment_reports WHERE deployment_id = 'dep-b' AND resolved_at IS NULL`); n != 1 {
		t.Fatalf("scope-B open reports after scope-A ban = %d; want 1", n)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM comment_reports WHERE deployment_id = $1 AND resolved_at IS NULL`, socialDep); n != 0 {
		t.Fatalf("scope-A open reports = %d; want 0", n)
	}
	for _, e := range drain() {
		if e["deployment_id"] != socialDep {
			t.Fatalf("notify for another scope: %v", e)
		}
	}

	// Unban in scope A leaves scope B's ban in place.
	if _, err := mod.Unban(ctx, modScope, "did:both", "ops", "appeal"); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM social_users WHERE deployment_id = 'dep-b' AND privy_did = 'did:both' AND banned_at IS NOT NULL AND ban_reason = 'b-reason'`); n != 1 {
		t.Fatal("scope-B ban altered by scope-A unban")
	}
	if ids := listIDs(t, ctx, soc, token); !slices.Contains(ids, idA) {
		t.Fatalf("scope-A comment not restored: %v", ids)
	}

	// Re-hide keeps the first reason.
	if err := mod.Hide(ctx, modScope, idA, "ops", "one"); err != nil {
		t.Fatal(err)
	}
	if err := mod.Hide(ctx, modScope, idA, "ops", "two"); err != nil {
		t.Fatal(err)
	}
	var hiddenReason string
	if err := database.DB.QueryRowContext(ctx, `SELECT hidden_reason FROM token_comments WHERE id = $1`, idA).Scan(&hiddenReason); err != nil || hiddenReason != "one" {
		t.Fatalf("hidden_reason = %q, %v", hiddenReason, err)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM token_comments WHERE id = $1 AND hidden_at IS NULL`, idB); n != 1 {
		t.Fatal("scope-B comment hidden by scope-A hide")
	}
}
