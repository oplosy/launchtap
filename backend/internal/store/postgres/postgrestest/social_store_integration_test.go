//go:build integration

package postgrestest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/social"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// socialCreator is the creator written by insertProjectionLaunch.
	socialCreator = common.BytesToAddress(addressBytes(0x55))
	socialHolder  = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	socialStray   = common.HexToAddress("0x00000000000000000000000000000000000000a2")
)

func socialActor(did string, wallets ...common.Address) social.Actor {
	return social.Actor{Scope: social.Scope{ChainID: socialChainID, DeploymentID: socialDep}, PrivyDID: did, Wallets: wallets}
}

// seedSocialToken launches one token on the social chain and gives socialHolder a balance.
func seedSocialToken(t *testing.T, ctx context.Context, database *Database, tag byte) common.Address {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	block := int64(tag)
	mustInsertBlock(t, ctx, database.DB, socialChainID, block, hashBytes(0x70+tag), hashBytes(0x6f+tag), at, "observed")
	fixture := projectionLaunchFixture{token: addressBytes(tag), curve: addressBytes(tag + 1), pair: addressBytes(tag + 2), weth: addressBytes(tag + 3)}
	insertProjectionLaunch(t, ctx, database.DB, socialChainID, block, hashBytes(0x70+tag), at, hashBytes(0xc0+tag), fixture)
	callRebuild(t, ctx, database.DB, socialChainID, fixture.token)
	mustExecSocial(t, ctx, database.DB, `
		INSERT INTO holder_balances (chain_id, token_address, holder_address, balance, first_acquired_block_number)
		VALUES ($1, $2, $3, 5, $4)`, socialChainID, fixture.token, socialHolder.Bytes(), block)
	return common.BytesToAddress(fixture.token)
}

func newSocialStore(t *testing.T, ctx context.Context, database *Database) (storepostgres.SocialStore, *pgxpool.Pool) {
	t.Helper()
	pool := openPool(t, ctx, database.URL)
	return storepostgres.SocialStore{Pool: pool}, pool
}

func rateLimited(t *testing.T, err error) *social.RateLimitedError {
	t.Helper()
	var rl *social.RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v; want *RateLimitedError", err)
	}
	return rl
}

func TestPostQualifiesAndRecordsWallet(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store, _ := newSocialStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)

	holder, err := store.Post(ctx, socialActor("did:holder", socialHolder, socialHolder), token, socialHolder, "hello")
	if err != nil {
		t.Fatalf("holder post: %v", err)
	}
	if holder.Author.Role != social.RoleHolder || holder.Author.Wallet == nil || *holder.Author.Wallet != socialHolder || holder.Author.Username != nil || holder.Token != token || holder.Body != "hello" || holder.ID == 0 || holder.CreatedAt.IsZero() {
		t.Fatalf("holder comment = %+v", holder)
	}
	creator, err := store.Post(ctx, socialActor("did:creator", socialCreator), token, socialCreator, "gm")
	if err != nil || creator.Author.Role != social.RoleCreator {
		t.Fatalf("creator post = %+v err=%v", creator, err)
	}
	var wallet []byte
	var role string
	if err := database.DB.QueryRowContext(ctx, `SELECT wallet, author_role FROM token_comments WHERE id = $1`, holder.ID).Scan(&wallet, &role); err != nil {
		t.Fatal(err)
	}
	if common.BytesToAddress(wallet) != socialHolder || role != "holder" {
		t.Fatalf("stored wallet=%x role=%s", wallet, role)
	}

	// With a username the wallet is hidden until published; the post result follows DisplayAuthor.
	mustExecSocial(t, ctx, database.DB, `UPDATE social_users SET username = 'alice', username_changed_at = now() WHERE privy_did = 'did:holder'`)
	mustExecSocial(t, ctx, database.DB, `UPDATE token_comments SET created_at = now() - interval '1 hour'`)
	named, err := store.Post(ctx, socialActor("did:holder", socialHolder), token, socialHolder, "named")
	if err != nil || named.Author.Username == nil || *named.Author.Username != "alice" || named.Author.Wallet != nil {
		t.Fatalf("named post = %+v err=%v", named, err)
	}
	mustExecSocial(t, ctx, database.DB, `INSERT INTO social_public_wallets (chain_id, deployment_id, wallet, privy_did) VALUES ($1, $2, $3, 'did:holder')`, socialChainID, socialDep, socialHolder.Bytes())
	mustExecSocial(t, ctx, database.DB, `UPDATE token_comments SET created_at = now() - interval '1 hour'`)
	pub, err := store.Post(ctx, socialActor("did:holder", socialHolder), token, socialHolder, "published")
	if err != nil || pub.Author.Wallet == nil || *pub.Author.Wallet != socialHolder {
		t.Fatalf("published post = %+v err=%v", pub, err)
	}

	// R2: the stored read-time display equals social.DisplayAuthor for the same inputs.
	page, err := store.List(ctx, social.Scope{ChainID: socialChainID, DeploymentID: socialDep}, token, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range page.Items {
		if c.ID != pub.ID {
			continue
		}
		want := social.DisplayAuthor(pub.Author.Username, false, socialHolder, true, social.RoleHolder)
		if c.Author.Username == nil || *c.Author.Username != *want.Username || c.Author.Wallet == nil || *c.Author.Wallet != *want.Wallet || c.Author.Role != want.Role {
			t.Fatalf("listed author %+v != DisplayAuthor %+v", c.Author, want)
		}
	}
}

func TestPostRejectsUnlinkedNonHolderBannedUnknownToken(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store, _ := newSocialStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)

	if _, err := store.Post(ctx, socialActor("did:a", socialHolder), token, socialStray, "x"); !errors.Is(err, social.ErrWalletNotLinked) {
		t.Fatalf("unlinked err = %v", err)
	}
	if _, err := store.Post(ctx, socialActor("did:a", socialStray), token, socialStray, "x"); !errors.Is(err, social.ErrNotHolder) {
		t.Fatalf("non-holder err = %v", err)
	}
	if _, err := store.Post(ctx, socialActor("did:a", socialHolder), common.HexToAddress("0xff"), socialHolder, "x"); !errors.Is(err, social.ErrUnknownToken) {
		t.Fatalf("unknown token err = %v", err)
	}
	insertSocialUser(t, ctx, database.DB, socialDep, "did:banned", nil, true)
	if _, err := store.Post(ctx, socialActor("did:banned", socialHolder), token, socialHolder, "x"); !errors.Is(err, social.ErrBanned) {
		t.Fatalf("banned err = %v", err)
	}
	var n int
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM token_comments`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rejected posts left %d rows err=%v", n, err)
	}

	// R10: a post prunes published wallets that are no longer linked.
	mustExecSocial(t, ctx, database.DB, `INSERT INTO social_public_wallets (chain_id, deployment_id, wallet, privy_did) VALUES ($1, $2, $3, 'did:b'), ($1, $2, $4, 'did:b')`,
		socialChainID, socialDep, socialHolder.Bytes(), socialStray.Bytes())
	if _, err := store.Post(ctx, socialActor("did:b", socialHolder), token, socialHolder, "x"); err != nil {
		t.Fatal(err)
	}
	var kept []byte
	if err := database.DB.QueryRowContext(ctx, `SELECT wallet FROM social_public_wallets WHERE privy_did = 'did:b'`).Scan(&kept); err != nil || common.BytesToAddress(kept) != socialHolder {
		t.Fatalf("pruned wallets: kept=%x err=%v", kept, err)
	}
	// Pruning also happens when the post itself is then rejected for the wallet (empty link set).
	mustExecSocial(t, ctx, database.DB, `INSERT INTO social_public_wallets (chain_id, deployment_id, wallet, privy_did) VALUES ($1, $2, $3, 'did:c')`, socialChainID, socialDep, socialStray.Bytes())
	if _, err := store.Post(ctx, socialActor("did:c", socialHolder), token, socialHolder, "x"); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM social_public_wallets WHERE privy_did = 'did:c'`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("did:c published wallets = %d err=%v", left, err)
	}
}

func TestPostRateLimitInterval(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store, _ := newSocialStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	a := socialActor("did:a", socialHolder)
	if _, err := store.Post(ctx, a, token, socialHolder, "one"); err != nil {
		t.Fatal(err)
	}
	_, err := store.Post(ctx, a, token, socialHolder, "two")
	rl := rateLimited(t, err)
	if rl.RetryAfter <= 0 || rl.RetryAfter > social.CommentMinInterval {
		t.Fatalf("RetryAfter = %s; want in (0, 15s]", rl.RetryAfter)
	}
	// Another user is not limited.
	if _, err := store.Post(ctx, socialActor("did:b", socialHolder), token, socialHolder, "other"); err != nil {
		t.Fatalf("other user: %v", err)
	}
}

func TestPostRateLimitHourly(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store, _ := newSocialStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	mustExecSocial(t, ctx, database.DB, `
		INSERT INTO token_comments (chain_id, deployment_id, token_address, privy_did, wallet, author_role, body, created_at)
		SELECT $1, $2, $3, 'did:a', $4, 'holder', 'seed ' || g, now() - interval '20 minutes'
		FROM generate_series(1, 30) AS g`, socialChainID, socialDep, token.Bytes(), socialHolder.Bytes())
	_, err := store.Post(ctx, socialActor("did:a", socialHolder), token, socialHolder, "31st")
	rl := rateLimited(t, err)
	if rl.RetryAfter < 39*time.Minute || rl.RetryAfter > 40*time.Minute {
		t.Fatalf("RetryAfter = %s; want about 40m", rl.RetryAfter)
	}
	if rl.RetryAfter%time.Second != 0 {
		t.Fatalf("RetryAfter = %s; want whole seconds", rl.RetryAfter)
	}
}

func TestPostRateLimitConcurrent(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	store, _ := newSocialStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	a := socialActor("did:race", socialHolder)

	const workers = 10
	var wg sync.WaitGroup
	results := make(chan error, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.Post(ctx, a, token, socialHolder, "race")
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	ok, limited := 0, 0
	for err := range results {
		var rl *social.RateLimitedError
		switch {
		case err == nil:
			ok++
		case errors.As(err, &rl):
			limited++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || limited != workers-1 {
		t.Fatalf("ok=%d limited=%d; want 1 and %d", ok, limited, workers-1)
	}
	var n int
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM token_comments`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d err=%v", n, err)
	}
}

func TestRateLimitSurvivesNewStore(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	first, _ := newSocialStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	a := socialActor("did:a", socialHolder)
	if _, err := first.Post(ctx, a, token, socialHolder, "one"); err != nil {
		t.Fatal(err)
	}
	second, _ := newSocialStore(t, ctx, database)
	_, err := second.Post(ctx, a, token, socialHolder, "two")
	_ = rateLimited(t, err)
}

func TestReportIdempotentOwnHiddenAndLimit(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store, _ := newSocialStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	author := socialActor("did:author", socialHolder)
	comment, err := store.Post(ctx, author, token, socialHolder, "report me")
	if err != nil {
		t.Fatal(err)
	}
	reporter := socialActor("did:reporter", socialStray)
	countReports := func() int {
		var n int
		if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM comment_reports WHERE comment_id = $1`, comment.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := store.Report(ctx, reporter, comment.ID, social.ReasonSpam); err != nil {
		t.Fatalf("report: %v", err)
	}
	if err := store.Report(ctx, reporter, comment.ID, social.ReasonScam); err != nil {
		t.Fatalf("repeat report must be nil: %v", err)
	}
	if n := countReports(); n != 1 {
		t.Fatalf("reports = %d; want 1", n)
	}
	if err := store.Report(ctx, author, comment.ID, social.ReasonSpam); !errors.Is(err, social.ErrOwnComment) {
		t.Fatalf("own err = %v", err)
	}
	if err := store.Report(ctx, reporter, 999999, social.ReasonSpam); !errors.Is(err, social.ErrCommentNotFound) {
		t.Fatalf("missing err = %v", err)
	}
	insertSocialUser(t, ctx, database.DB, socialDep, "did:banned", nil, true)
	if err := store.Report(ctx, socialActor("did:banned"), comment.ID, social.ReasonSpam); !errors.Is(err, social.ErrBanned) {
		t.Fatalf("banned err = %v", err)
	}

	hidden := insertSocialComment(t, ctx, database.DB, token.Bytes(), "did:author", socialHolder.Bytes(), "hidden", true)
	if err := store.Report(ctx, reporter, hidden, social.ReasonSpam); !errors.Is(err, social.ErrCommentNotFound) {
		t.Fatalf("hidden err = %v", err)
	}

	// Hourly cap: the reporter already has 1 report; add 19 more (20 total), the 21st is rejected.
	ids := make([]int64, 0, 21)
	for i := 0; i < 21; i++ {
		ids = append(ids, insertSocialComment(t, ctx, database.DB, token.Bytes(), "did:author", socialHolder.Bytes(), "c", false))
	}
	for _, id := range ids[:19] {
		if err := store.Report(ctx, reporter, id, social.ReasonOther); err != nil {
			t.Fatalf("report %d: %v", id, err)
		}
	}
	err = store.Report(ctx, reporter, ids[19], social.ReasonOther)
	rl := rateLimited(t, err)
	if rl.RetryAfter < time.Second || rl.RetryAfter > time.Hour {
		t.Fatalf("RetryAfter = %s", rl.RetryAfter)
	}
	var n int
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM comment_reports WHERE reporter_did = 'did:reporter'`).Scan(&n); err != nil || n != 20 {
		t.Fatalf("reporter rows = %d err=%v; the rejected report must roll back", n, err)
	}
	// A repeat of an accepted report still succeeds at the cap.
	if err := store.Report(ctx, reporter, ids[0], social.ReasonSpam); err != nil {
		t.Fatalf("repeat at cap: %v", err)
	}
}

func TestListPagingAndReorgedToken(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	store, _ := newSocialStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)
	scope := social.Scope{ChainID: socialChainID, DeploymentID: socialDep}
	for i := 0; i < 45; i++ {
		insertSocialComment(t, ctx, database.DB, token.Bytes(), "did:a", socialHolder.Bytes(), "c", false)
	}

	seen := map[int64]bool{}
	var sizes []int
	cursor := ""
	for pages := 0; pages < 5; pages++ {
		page, err := store.List(ctx, scope, token, cursor, 20)
		if err != nil {
			t.Fatal(err)
		}
		if page.Count != 45 {
			t.Fatalf("count = %d; want 45", page.Count)
		}
		sizes = append(sizes, len(page.Items))
		for _, c := range page.Items {
			if seen[c.ID] {
				t.Fatalf("duplicate comment %d", c.ID)
			}
			seen[c.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(sizes) != 3 || sizes[0] != 20 || sizes[1] != 20 || sizes[2] != 5 || len(seen) != 45 {
		t.Fatalf("page sizes = %v seen=%d; want 20/20/5 and 45", sizes, len(seen))
	}
	if _, err := store.List(ctx, scope, token, "garbage", 20); !errors.Is(err, social.ErrInvalidCursor) {
		t.Fatalf("bad cursor err = %v", err)
	}
	// Exactly one full page has no next cursor.
	exact, err := store.List(ctx, scope, token, "", 45)
	if err != nil || len(exact.Items) != 45 || exact.NextCursor != "" {
		t.Fatalf("exact page = %d next=%q err=%v", len(exact.Items), exact.NextCursor, err)
	}

	// A reorg drops the token row: comments stay stored but the token is unknown; restoring
	// the row brings them back.
	conn, err := database.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `SET session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	// The row is moved to another address and back (tokens has generated columns, so it cannot
	// be re-inserted as a copy); dependents' FK triggers are skipped.
	moved := addressBytes(0xee)
	if _, err := conn.ExecContext(ctx, `UPDATE tokens SET token_address = $3 WHERE chain_id = $1 AND token_address = $2`, socialChainID, token.Bytes(), moved); err != nil {
		t.Fatalf("move token row: %v", err)
	}
	if _, err := store.List(ctx, scope, token, "", 20); !errors.Is(err, social.ErrUnknownToken) {
		t.Fatalf("list after reorg err = %v; want ErrUnknownToken", err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE tokens SET token_address = $2 WHERE chain_id = $1 AND token_address = $3`, socialChainID, token.Bytes(), moved); err != nil {
		t.Fatalf("restore token row: %v", err)
	}
	restored, err := store.List(ctx, scope, token, "", 20)
	if err != nil || len(restored.Items) != 20 || restored.Count != 45 {
		t.Fatalf("list after restore = %d count=%d err=%v", len(restored.Items), restored.Count, err)
	}
}

func TestPostSendsCommentNotify(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store, pool := newSocialStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)

	listenCtx, stop := context.WithCancel(ctx)
	defer stop()
	ready := make(chan struct{})
	payloads := make(chan []byte, 8)
	listenErr := make(chan error, 1)
	go func() {
		listenErr <- storepostgres.ListenAPIRefreshReady(listenCtx, pool, ready, func(p []byte) { payloads <- p })
	}()
	select {
	case <-ready:
	case err := <-listenErr:
		t.Fatalf("listener: %v", err)
	}

	a := socialActor("did:a", socialHolder)
	if _, err := store.Post(ctx, a, token, socialHolder, "ping"); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-payloads:
		var event map[string]any
		if err := json.Unmarshal(p, &event); err != nil {
			t.Fatal(err)
		}
		if event["type"] != "comment" || !strings.EqualFold(event["token"].(string), token.Hex()) || event["deployment_id"] != socialDep {
			t.Fatalf("payload = %s", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no comment notification after commit")
	}

	// A rejected (rolled-back) post sends nothing.
	if _, err := store.Post(ctx, a, token, socialHolder, "again"); err == nil {
		t.Fatal("second post should be rate limited")
	}
	select {
	case p := <-payloads:
		t.Fatalf("notification after rolled-back post: %s", p)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestEligibility(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	store, _ := newSocialStore(t, ctx, database)
	token := seedSocialToken(t, ctx, database, 0x10)

	got, err := store.Eligibility(ctx, socialActor("did:new", socialHolder, socialStray, socialCreator, socialHolder), token)
	if err != nil {
		t.Fatal(err)
	}
	roles := map[common.Address]social.Role{}
	for _, w := range got.Wallets {
		roles[w.Address] = w.Role
	}
	if len(got.Wallets) != 2 || roles[socialHolder] != social.RoleHolder || roles[socialCreator] != social.RoleCreator || got.Banned || got.Username != nil {
		t.Fatalf("eligibility = %+v", got)
	}
	if _, err := store.Eligibility(ctx, socialActor("did:new", socialHolder), common.HexToAddress("0xff")); !errors.Is(err, social.ErrUnknownToken) {
		t.Fatalf("unknown token err = %v", err)
	}
	insertSocialUser(t, ctx, database.DB, socialDep, "did:named", strPtr("bob"), false)
	got, err = store.Eligibility(ctx, socialActor("did:named", socialHolder), token)
	if err != nil || got.Username == nil || *got.Username != "bob" {
		t.Fatalf("named eligibility = %+v err=%v", got, err)
	}
	insertSocialUser(t, ctx, database.DB, socialDep, "did:banned", strPtr("carl"), true)
	got, err = store.Eligibility(ctx, socialActor("did:banned", socialHolder), token)
	if err != nil || !got.Banned || got.Username != nil {
		t.Fatalf("banned eligibility = %+v err=%v", got, err)
	}
	got, err = store.Eligibility(ctx, socialActor("did:none"), token)
	if err != nil || len(got.Wallets) != 0 {
		t.Fatalf("no wallets eligibility = %+v err=%v", got, err)
	}
}

// The following tests pin the Task 1 queries' behaviour that the store depends on.

func TestSocialRateStateQueries(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	q := sqlc.New(pool)
	token := seedSocialToken(t, ctx, database, 0x10)
	window := float64(time.Hour / time.Second)

	// No rows: counts are zero, timestamps NULL, db_now is set.
	c, err := q.CommentRateState(ctx, sqlc.CommentRateStateParams{WindowSeconds: window, ChainID: socialChainID, DeploymentID: socialDep, PrivyDid: "did:none"})
	if err != nil || c.CountInWindow != 0 || c.LastCreatedAt.Valid || c.OldestInWindow.Valid || !c.DbNow.Valid {
		t.Fatalf("empty comment state = %+v err=%v", c, err)
	}
	r, err := q.ReportRateState(ctx, sqlc.ReportRateStateParams{ChainID: socialChainID, DeploymentID: socialDep, ReporterDid: "did:none", WindowSeconds: window})
	if err != nil || r.CountInWindow != 0 || r.OldestInWindow.Valid || !r.DbNow.Valid {
		t.Fatalf("empty report state = %+v err=%v", r, err)
	}

	ins := func(ago string) int64 {
		var id int64
		if err := database.DB.QueryRowContext(ctx, `
			INSERT INTO token_comments (chain_id, deployment_id, token_address, privy_did, wallet, author_role, body, created_at)
			VALUES ($1, $2, $3, 'did:a', $4, 'holder', 'x', now() - $5::interval) RETURNING id`,
			socialChainID, socialDep, token.Bytes(), socialHolder.Bytes(), ago).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	old := ins("2 hours")
	ins("30 minutes")
	ins("10 minutes")
	c, err = q.CommentRateState(ctx, sqlc.CommentRateStateParams{WindowSeconds: window, ChainID: socialChainID, DeploymentID: socialDep, PrivyDid: "did:a"})
	if err != nil || c.CountInWindow != 2 {
		t.Fatalf("comment state = %+v err=%v; want 2 in window", c, err)
	}
	if got := c.DbNow.Time.Sub(c.OldestInWindow.Time); got < 29*time.Minute || got > 31*time.Minute {
		t.Fatalf("oldest in window is %s old; want ~30m (2h row excluded)", got)
	}
	if got := c.DbNow.Time.Sub(c.LastCreatedAt.Time); got < 9*time.Minute || got > 11*time.Minute {
		t.Fatalf("last comment is %s old; want ~10m", got)
	}
	// Other scope and other DID are not counted.
	other, err := q.CommentRateState(ctx, sqlc.CommentRateStateParams{WindowSeconds: window, ChainID: socialChainID, DeploymentID: "dep-b", PrivyDid: "did:a"})
	if err != nil || other.CountInWindow != 0 {
		t.Fatalf("other deployment state = %+v err=%v", other, err)
	}

	// Reports: one old, one recent.
	mustExecSocial(t, ctx, database.DB, `
		INSERT INTO comment_reports (comment_id, chain_id, deployment_id, reporter_did, reason, created_at)
		VALUES ($1, $2, $3, 'did:r', 'spam', now() - interval '2 hours')`, old, socialChainID, socialDep)
	recent := ins("1 minute")
	mustExecSocial(t, ctx, database.DB, `
		INSERT INTO comment_reports (comment_id, chain_id, deployment_id, reporter_did, reason, created_at)
		VALUES ($1, $2, $3, 'did:r', 'spam', now() - interval '5 minutes')`, recent, socialChainID, socialDep)
	r, err = q.ReportRateState(ctx, sqlc.ReportRateStateParams{ChainID: socialChainID, DeploymentID: socialDep, ReporterDid: "did:r", WindowSeconds: window})
	if err != nil || r.CountInWindow != 1 || !r.OldestInWindow.Valid {
		t.Fatalf("report state = %+v err=%v; want 1 in window", r, err)
	}
	if got := r.DbNow.Time.Sub(r.OldestInWindow.Time); got < 4*time.Minute || got > 6*time.Minute {
		t.Fatalf("oldest report is %s old; want ~5m", got)
	}
}

func TestSocialReportAndSubjectQueries(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	q := sqlc.New(pool)
	token := seedSocialToken(t, ctx, database, 0x10)
	wallet := socialHolder.Bytes()

	visible := insertSocialComment(t, ctx, database.DB, token.Bytes(), "did:author", wallet, "v", false)
	hidden := insertSocialComment(t, ctx, database.DB, token.Bytes(), "did:author", wallet, "h", true)
	insertSocialUser(t, ctx, database.DB, socialDep, "did:bannedauthor", nil, true)
	bannedComment := insertSocialComment(t, ctx, database.DB, token.Bytes(), "did:bannedauthor", wallet, "b", false)

	get := func(id int64, dep string) error {
		_, err := q.GetReportableComment(ctx, sqlc.GetReportableCommentParams{CommentID: id, ChainID: socialChainID, DeploymentID: dep})
		return err
	}
	row, err := q.GetReportableComment(ctx, sqlc.GetReportableCommentParams{CommentID: visible, ChainID: socialChainID, DeploymentID: socialDep})
	if err != nil || row.AuthorDid != "did:author" || common.Address(row.TokenAddress) != token {
		t.Fatalf("reportable = %+v err=%v", row, err)
	}
	for name, err := range map[string]error{
		"hidden":           get(hidden, socialDep),
		"banned author":    get(bannedComment, socialDep),
		"other deployment": get(visible, "dep-b"),
		"missing":          get(999999, socialDep),
	} {
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("%s err = %v; want ErrNoRows", name, err)
		}
	}

	// InsertReport: first insert 1 row, a repeat (even with another reason) 0 rows.
	params := sqlc.InsertReportParams{CommentID: visible, ChainID: socialChainID, DeploymentID: socialDep, ReporterDid: "did:r", Reason: "spam"}
	if n, err := q.InsertReport(ctx, params); err != nil || n != 1 {
		t.Fatalf("first report rows=%d err=%v", n, err)
	}
	params.Reason = "scam"
	if n, err := q.InsertReport(ctx, params); err != nil || n != 0 {
		t.Fatalf("repeat report rows=%d err=%v", n, err)
	}

	// EnsureSocialUser is idempotent and GetSocialUser reads the row; unknown DIDs have no row.
	if _, err := q.GetSocialUser(ctx, sqlc.GetSocialUserParams{ChainID: socialChainID, DeploymentID: socialDep, PrivyDid: "did:new"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unknown user err = %v", err)
	}
	ensure := sqlc.EnsureSocialUserParams{ChainID: socialChainID, DeploymentID: socialDep, PrivyDid: "did:new"}
	for i := 0; i < 2; i++ {
		if err := q.EnsureSocialUser(ctx, ensure); err != nil {
			t.Fatal(err)
		}
	}
	user, err := q.GetSocialUser(ctx, sqlc.GetSocialUserParams{ChainID: socialChainID, DeploymentID: socialDep, PrivyDid: "did:new"})
	if err != nil || user.Username.Valid || user.UsernameChangedAt.Valid || user.BannedAt.Valid {
		t.Fatalf("new user = %+v err=%v", user, err)
	}
	var users int
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM social_users WHERE privy_did = 'did:new'`).Scan(&users); err != nil || users != 1 {
		t.Fatalf("users = %d err=%v", users, err)
	}

	// LockSocialSubject: a second transaction on the same subject waits for the first, a
	// different subject does not.
	tx1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx1.Rollback(ctx) }()
	lock := sqlc.LockSocialSubjectParams{ChainID: socialChainID, DeploymentID: socialDep, SubjectKey: "did:lock"}
	if err := sqlc.New(tx1).LockSocialSubject(ctx, lock); err != nil {
		t.Fatal(err)
	}
	tryLock := func(key string) (acquired bool) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var got bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('social' || chr(31) || $1::text || chr(31) || $2::text || chr(31) || $3::text, 0))`,
			"48023", socialDep, key).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if tryLock("did:lock") {
		t.Fatal("same subject lock must be held by the first transaction")
	}
	if !tryLock("did:other") {
		t.Fatal("different subject must not block")
	}
}
