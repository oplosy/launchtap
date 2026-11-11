//go:build integration

package postgrestest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/realtime"
	"github.com/Contictus/launchtap/backend/internal/social"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

var modScope = social.Scope{ChainID: socialChainID, DeploymentID: socialDep}

func newModerationStore(t *testing.T, ctx context.Context, database *Database) (storepostgres.ModerationStore, storepostgres.SocialStore) {
	t.Helper()
	pool := openPool(t, ctx, database.URL)
	return storepostgres.ModerationStore{Pool: pool}, storepostgres.SocialStore{Pool: pool}
}

// seedComment inserts a comment directly (the post rate limit is not under test here).
func seedComment(t *testing.T, ctx context.Context, database *Database, did string, token common.Address, body string, age time.Duration) int64 {
	t.Helper()
	var id int64
	err := database.DB.QueryRowContext(ctx, `
		INSERT INTO token_comments (chain_id, deployment_id, token_address, privy_did, wallet, author_role, body, created_at)
		VALUES ($1, $2, $3, $4, $5, 'holder', $6, now() - make_interval(secs => $7))
		RETURNING id`, socialChainID, socialDep, token.Bytes(), did, socialHolder.Bytes(), body, age.Seconds()).Scan(&id)
	if err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	return id
}

func seedReport(t *testing.T, ctx context.Context, database *Database, commentID int64, reporter, reason string, age time.Duration) {
	t.Helper()
	mustExecSocial(t, ctx, database.DB, `
		INSERT INTO comment_reports (comment_id, chain_id, deployment_id, reporter_did, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, now() - make_interval(secs => $6))`, commentID, socialChainID, socialDep, reporter, reason, age.Seconds())
}

func countRows(t *testing.T, ctx context.Context, database *Database, query string, args ...any) int {
	t.Helper()
	var n int
	if err := database.DB.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func openReportCount(t *testing.T, ctx context.Context, database *Database, commentID int64) int {
	return countRows(t, ctx, database, `SELECT count(*) FROM comment_reports WHERE comment_id = $1 AND resolved_at IS NULL`, commentID)
}

func listIDs(t *testing.T, ctx context.Context, store storepostgres.SocialStore, token common.Address) []int64 {
	t.Helper()
	page, err := store.List(ctx, modScope, token, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, len(page.Items))
	for _, c := range page.Items {
		ids = append(ids, c.ID)
	}
	return ids
}

// collectNotifies listens on api_refresh and returns a function that drains what arrived.
func collectNotifies(t *testing.T, ctx context.Context, store storepostgres.ModerationStore) func() []map[string]any {
	t.Helper()
	listenCtx, stop := context.WithCancel(ctx)
	t.Cleanup(stop)
	ready := make(chan struct{})
	payloads := make(chan []byte, 32)
	listenErr := make(chan error, 1)
	go func() {
		listenErr <- storepostgres.ListenAPIRefreshReady(listenCtx, store.Pool, ready, func(p []byte) { payloads <- p })
	}()
	select {
	case <-ready:
	case err := <-listenErr:
		t.Fatalf("listener: %v", err)
	}
	return func() []map[string]any {
		var out []map[string]any
		for {
			select {
			case p := <-payloads:
				var event map[string]any
				if err := json.Unmarshal(p, &event); err != nil {
					t.Fatal(err)
				}
				out = append(out, event)
			case <-time.After(700 * time.Millisecond):
				return out
			}
		}
	}
}

func TestModerateHideResolvesAndNotifies(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	mod, soc := newModerationStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	bad := seedComment(t, ctx, database, "did:bad", token, "buy now", 3*time.Minute)
	keep := seedComment(t, ctx, database, "did:good", token, "gm", 2*time.Minute)
	seedReport(t, ctx, database, bad, "did:r1", "spam", time.Minute)
	seedReport(t, ctx, database, bad, "did:r2", "scam", time.Minute)
	seedReport(t, ctx, database, keep, "did:r1", "other", time.Minute)
	drain := collectNotifies(t, ctx, mod)

	if err := mod.Hide(ctx, modScope, bad, "ops", "spam link"); err != nil {
		t.Fatal(err)
	}
	if ids := listIDs(t, ctx, soc, token); !slices.Equal(ids, []int64{keep}) {
		t.Fatalf("visible after hide = %v; want [%d]", ids, keep)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM comment_reports WHERE comment_id = $1 AND resolution = 'actioned' AND resolved_at IS NOT NULL`, bad); n != 2 {
		t.Fatalf("actioned reports = %d; want 2", n)
	}
	if n := openReportCount(t, ctx, database, keep); n != 1 {
		t.Fatalf("unrelated open reports = %d; want 1", n)
	}
	var action, operator, reason string
	var commentID int64
	if err := database.DB.QueryRowContext(ctx, `SELECT action, operator, reason, comment_id FROM moderation_actions`).Scan(&action, &operator, &reason, &commentID); err != nil {
		t.Fatalf("exactly one action row expected: %v", err)
	}
	if action != "hide_comment" || operator != "ops" || reason != "spam link" || commentID != bad {
		t.Fatalf("action row = %s %s %s %d", action, operator, reason, commentID)
	}
	events := drain()
	if len(events) != 1 || events[0]["type"] != "comment" || events[0]["token"] != token.Hex() || events[0]["deployment_id"] != socialDep {
		t.Fatalf("notifies = %v", events)
	}
	// The hide notification is a valid realtime comment hint, so open streams refetch comments.
	raw, err := json.Marshal(events[0])
	if err != nil {
		t.Fatal(err)
	}
	if hint, err := realtime.Decode(raw); err != nil || hint.Type != "comment" || hint.Token != token.Hex() || hint.ChainID != socialChainID {
		t.Fatalf("hide hint = %+v err=%v", hint, err)
	}

	if err := mod.Unhide(ctx, modScope, bad, "ops", "mistake"); err != nil {
		t.Fatal(err)
	}
	if ids := listIDs(t, ctx, soc, token); len(ids) != 2 {
		t.Fatalf("visible after unhide = %v", ids)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM moderation_actions WHERE action = 'unhide_comment'`); n != 1 {
		t.Fatalf("unhide action rows = %d", n)
	}
	if events := drain(); len(events) != 1 {
		t.Fatalf("unhide notifies = %v", events)
	}
}

func TestModerateResolveDismissesWithoutHiding(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	mod, soc := newModerationStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	id := seedComment(t, ctx, database, "did:a", token, "ok", time.Minute)
	seedReport(t, ctx, database, id, "did:r1", "abuse", time.Minute)
	seedReport(t, ctx, database, id, "did:r2", "abuse", time.Minute)

	n, err := mod.Resolve(ctx, modScope, id, social.ResolutionDismissed, "ops", "fine")
	if err != nil || n != 2 {
		t.Fatalf("resolve = %d, %v", n, err)
	}
	if got := openReportCount(t, ctx, database, id); got != 0 {
		t.Fatalf("open reports = %d", got)
	}
	if ids := listIDs(t, ctx, soc, token); !slices.Equal(ids, []int64{id}) {
		t.Fatalf("comment hidden by dismiss: %v", ids)
	}
	if n, err := mod.Resolve(ctx, modScope, id, social.ResolutionDismissed, "ops", "again"); err != nil || n != 0 {
		t.Fatalf("second resolve = %d, %v", n, err)
	}
}

func TestModerateBanHidesAllAndUnbanRestores(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	mod, soc := newModerationStore(t, ctx, database)
	t1 := seedSocialToken(t, ctx, database, 0x10)
	t2 := seedSocialToken(t, ctx, database, 0x20)
	a1 := seedComment(t, ctx, database, "did:spammer", t1, "one", 5*time.Minute)
	a2 := seedComment(t, ctx, database, "did:spammer", t2, "two", 4*time.Minute)
	seedComment(t, ctx, database, "did:good", t1, "fine", 3*time.Minute)
	seedReport(t, ctx, database, a1, "did:r1", "spam", time.Minute)
	seedReport(t, ctx, database, a2, "did:r2", "spam", time.Minute)
	name := "spammer"
	insertSocialUser(t, ctx, database.DB, socialDep, "did:spammer", &name, false)
	mustExecSocial(t, ctx, database.DB, `INSERT INTO social_public_wallets (chain_id, deployment_id, wallet, privy_did) VALUES ($1, $2, $3, 'did:spammer')`, socialChainID, socialDep, socialHolder.Bytes())
	drain := collectNotifies(t, ctx, mod)

	tokens, err := mod.BanCommentAuthor(ctx, modScope, a1, "ops", "scam spree")
	if err != nil {
		t.Fatal(err)
	}
	want := []common.Address{t1, t2}
	slices.SortFunc(want, func(a, b common.Address) int { return a.Cmp(b) })
	if !slices.Equal(tokens, want) {
		t.Fatalf("affected tokens = %v; want %v", tokens, want)
	}
	for _, token := range []common.Address{t1, t2} {
		for _, id := range listIDs(t, ctx, soc, token) {
			if id == a1 || id == a2 {
				t.Fatalf("banned author's comment %d still listed", id)
			}
		}
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM public_wallet_usernames WHERE username = 'spammer'`); n != 0 {
		t.Fatalf("banned username still public: %d rows", n)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM comment_reports WHERE resolution = 'actioned' AND resolved_at IS NOT NULL`); n != 2 {
		t.Fatalf("actioned reports = %d; want 2", n)
	}
	var reason string
	if err := database.DB.QueryRowContext(ctx, `SELECT ban_reason FROM social_users WHERE privy_did = 'did:spammer'`).Scan(&reason); err != nil || reason != "scam spree" {
		t.Fatalf("ban_reason = %q, %v", reason, err)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM moderation_actions WHERE action = 'ban_user' AND target_did = 'did:spammer' AND comment_id = $1`, a1); n != 1 {
		t.Fatalf("ban action rows = %d", n)
	}
	if events := drain(); len(events) != 2 {
		t.Fatalf("ban notifies = %v; want 2", events)
	}

	tokens, err = mod.Unban(ctx, modScope, "did:spammer", "ops", "appeal")
	if err != nil || !slices.Equal(tokens, want) {
		t.Fatalf("unban = %v, %v", tokens, err)
	}
	if ids := listIDs(t, ctx, soc, t1); !slices.Contains(ids, a1) {
		t.Fatalf("comment not restored on token 1: %v", ids)
	}
	if ids := listIDs(t, ctx, soc, t2); !slices.Contains(ids, a2) {
		t.Fatalf("comment not restored on token 2: %v", ids)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM public_wallet_usernames WHERE username = 'spammer'`); n != 1 {
		t.Fatalf("username not restored: %d rows", n)
	}
	if events := drain(); len(events) != 2 {
		t.Fatalf("unban notifies = %v; want 2", events)
	}
}

func TestModerateBanByDIDCreatesUserRowForCommenter(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	mod, soc := newModerationStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	id := seedComment(t, ctx, database, "did:norow", token, "hi", time.Minute)

	if _, err := mod.Ban(ctx, modScope, "did:norow", "ops", "r"); err != nil {
		t.Fatal(err)
	}
	if ids := listIDs(t, ctx, soc, token); slices.Contains(ids, id) {
		t.Fatal("comment of banned DID still listed")
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM social_users WHERE privy_did = 'did:norow' AND banned_at IS NOT NULL`); n != 1 {
		t.Fatalf("banned user rows = %d", n)
	}
}

func TestModerateUnknownTargets(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	mod, _ := newModerationStore(t, ctx, database)
	seedSocialToken(t, ctx, database, 0x10)
	drain := collectNotifies(t, ctx, mod)

	checks := map[string]error{}
	checks["hide"] = mod.Hide(ctx, modScope, 999, "ops", "r")
	checks["unhide"] = mod.Unhide(ctx, modScope, 999, "ops", "r")
	_, checks["resolve"] = mod.Resolve(ctx, modScope, 999, social.ResolutionActioned, "ops", "r")
	_, checks["ban comment"] = mod.BanCommentAuthor(ctx, modScope, 999, "ops", "r")
	_, checks["ban did"] = mod.Ban(ctx, modScope, "did:nobody", "ops", "r")
	_, checks["unban did"] = mod.Unban(ctx, modScope, "did:nobody", "ops", "r")
	for name, err := range checks {
		if !errors.Is(err, social.ErrNotFound) {
			t.Errorf("%s err = %v; want ErrNotFound", name, err)
		}
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM moderation_actions`); n != 0 {
		t.Fatalf("action rows after unknown targets = %d", n)
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM social_users`); n != 0 {
		t.Fatalf("user rows created for unknown DID = %d", n)
	}
	if events := drain(); len(events) != 0 {
		t.Fatalf("notifies for unknown targets: %v", events)
	}

	// A comment in another scope is not found either.
	token := common.BytesToAddress(addressBytes(0x10))
	id := seedComment(t, ctx, database, "did:a", token, "x", time.Minute)
	other := social.Scope{ChainID: socialChainID, DeploymentID: "dep-other"}
	if err := mod.Hide(ctx, other, id, "ops", "r"); !errors.Is(err, social.ErrNotFound) {
		t.Fatalf("cross-scope hide err = %v", err)
	}
}

func TestModerateRejectsInvalidActorFields(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	mod, _ := newModerationStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	id := seedComment(t, ctx, database, "did:a", token, "x", time.Minute)
	if err := mod.Hide(ctx, modScope, id, "", "r"); err == nil {
		t.Fatal("empty operator accepted")
	}
	if err := mod.Hide(ctx, modScope, id, "ops", ""); err == nil {
		t.Fatal("empty reason accepted")
	}
	if n := countRows(t, ctx, database, `SELECT count(*) FROM token_comments WHERE hidden_at IS NOT NULL`); n != 0 {
		t.Fatalf("hidden despite invalid input: %d", n)
	}
}

func TestModerateReportsListing(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	mod, _ := newModerationStore(t, ctx, database)
	t1 := seedSocialToken(t, ctx, database, 0x10)
	t2 := seedSocialToken(t, ctx, database, 0x20)
	older := seedComment(t, ctx, database, "did:a", t1, "older", 10*time.Minute)
	newer := seedComment(t, ctx, database, "did:b", t2, "newer\x1b", 9*time.Minute)
	quiet := seedComment(t, ctx, database, "did:c", t1, "no reports", 8*time.Minute)
	resolvedOnly := seedComment(t, ctx, database, "did:d", t1, "resolved", 7*time.Minute)
	_ = quiet
	seedReport(t, ctx, database, older, "did:r1", "spam", 5*time.Minute)
	seedReport(t, ctx, database, older, "did:r2", "scam", 4*time.Minute)
	seedReport(t, ctx, database, older, "did:r3", "spam", 3*time.Minute)
	seedReport(t, ctx, database, newer, "did:r1", "abuse", 2*time.Minute)
	seedReport(t, ctx, database, resolvedOnly, "did:r1", "other", time.Minute)
	mustExecSocial(t, ctx, database.DB, `UPDATE comment_reports SET resolved_at = now(), resolution = 'dismissed' WHERE comment_id = $1`, resolvedOnly)

	got, err := mod.OpenReports(ctx, modScope, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("listing = %+v; want 2 comments", got)
	}
	// Newest open report first: `newer` was reported 2 minutes ago, `older` last 3 minutes ago.
	if got[0].CommentID != newer || got[1].CommentID != older {
		t.Fatalf("order = %d,%d; want %d,%d", got[0].CommentID, got[1].CommentID, newer, older)
	}
	if got[1].Reports != 3 || !slices.Equal(got[1].Reasons, []social.ReportReason{social.ReasonScam, social.ReasonSpam}) {
		t.Fatalf("older summary = %+v", got[1])
	}
	if got[0].Reports != 1 || got[0].Token != t2 || got[0].AuthorDID != "did:b" || got[0].Body != "newer\x1b" || got[0].Wallet != socialHolder || got[0].CreatedAt.IsZero() {
		t.Fatalf("newer summary = %+v", got[0])
	}
	limited, err := mod.OpenReports(ctx, modScope, 1)
	if err != nil || len(limited) != 1 || limited[0].CommentID != newer {
		t.Fatalf("limited = %+v, %v", limited, err)
	}
	if other, err := mod.OpenReports(ctx, social.Scope{ChainID: socialChainID, DeploymentID: "dep-other"}, 10); err != nil || len(other) != 0 {
		t.Fatalf("other scope = %+v, %v", other, err)
	}
}
