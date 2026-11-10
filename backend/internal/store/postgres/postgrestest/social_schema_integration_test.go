//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	socialChainID int64 = 48023
	socialDep           = "dep-a"
)

func execSocial(t testing.TB, ctx context.Context, db *sql.DB, query string, args ...any) error {
	t.Helper()
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func mustExecSocial(t testing.TB, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	if err := execSocial(t, ctx, db, query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func assertSocialConstraint(t testing.TB, err error, code, constraint string) {
	t.Helper()
	assertPostgresConstraint(t, err, code, constraint)
}

func insertSocialUser(t testing.TB, ctx context.Context, db *sql.DB, dep, did string, username *string, banned bool) {
	t.Helper()
	var changed any
	if username != nil {
		changed = time.Now().UTC()
	}
	var bannedAt, banReason any
	if banned {
		bannedAt, banReason = time.Now().UTC(), "test"
	}
	mustExecSocial(t, ctx, db, `
		INSERT INTO social_users (chain_id, deployment_id, privy_did, username, username_changed_at, banned_at, ban_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, socialChainID, dep, did, username, changed, bannedAt, banReason)
}

func insertSocialComment(t testing.TB, ctx context.Context, db *sql.DB, token []byte, did string, wallet []byte, body string, hidden bool) int64 {
	t.Helper()
	var hiddenAt, hiddenReason any
	if hidden {
		hiddenAt, hiddenReason = time.Now().UTC(), "test"
	}
	var id int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO token_comments (chain_id, deployment_id, token_address, privy_did, wallet, author_role, body, hidden_at, hidden_reason)
		VALUES ($1, $2, $3, $4, $5, 'holder', $6, $7, $8) RETURNING id`,
		socialChainID, socialDep, token, did, wallet, body, hiddenAt, hiddenReason).Scan(&id); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	return id
}

func strPtr(s string) *string { return &s }

func TestSocialMigrationConstraints(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	db := database.DB

	for _, name := range []string{"Bad", "ab"} {
		err := execSocial(t, ctx, db, `
			INSERT INTO social_users (chain_id, deployment_id, privy_did, username, username_changed_at)
			VALUES ($1, $2, 'did:a', $3, now())`, socialChainID, socialDep, name)
		assertSocialConstraint(t, err, "23514", "social_users_username_format")
	}

	token, wallet := addressBytes(0x21), addressBytes(0x22)
	err := execSocial(t, ctx, db, `
		INSERT INTO token_comments (chain_id, deployment_id, token_address, privy_did, wallet, author_role, body)
		VALUES ($1, $2, $3, 'did:a', $4, 'holder', $5)`, socialChainID, socialDep, token, wallet, strings.Repeat("ç", 501))
	assertSocialConstraint(t, err, "23514", "token_comments_body_length")
	// 500 code points (1000 bytes) is accepted: the limit counts characters.
	mustExecSocial(t, ctx, db, `
		INSERT INTO token_comments (chain_id, deployment_id, token_address, privy_did, wallet, author_role, body)
		VALUES ($1, $2, $3, 'did:a', $4, 'holder', $5)`, socialChainID, socialDep, token, wallet, strings.Repeat("ç", 500))

	err = execSocial(t, ctx, db, `
		INSERT INTO token_comments (chain_id, deployment_id, token_address, privy_did, wallet, author_role, body)
		VALUES ($1, $2, $3, 'did:a', $4, 'admin', 'x')`, socialChainID, socialDep, token, wallet)
	assertSocialConstraint(t, err, "23514", "token_comments_author_role_valid")

	err = execSocial(t, ctx, db, `
		INSERT INTO comment_reports (comment_id, chain_id, deployment_id, reporter_did, reason)
		VALUES (999, $1, $2, 'did:b', 'spam')`, socialChainID, socialDep)
	assertSocialConstraint(t, err, "23503", "comment_reports_comment_fk")
}

func TestSocialUsernameUniqueness(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	db := database.DB

	insertSocialUser(t, ctx, db, "dep-a", "did:a", strPtr("alice"), false)
	err := execSocial(t, ctx, db, `
		INSERT INTO social_users (chain_id, deployment_id, privy_did, username, username_changed_at)
		VALUES ($1, 'dep-a', 'did:b', 'alice', now())`, socialChainID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("duplicate username err = %v; want unique violation", err)
	}
	insertSocialUser(t, ctx, db, "dep-b", "did:b", strPtr("alice"), false)
}

func TestListTokenCommentsDisplayRule(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	db := database.DB
	q := sqlc.New(openPool(t, ctx, database.URL))
	token := addressBytes(0x31)
	w1, w2, w3, w4, w5 := addressBytes(0x41), addressBytes(0x42), addressBytes(0x43), addressBytes(0x44), addressBytes(0x45)

	// u1: no username. u2: username, wallet not published. u3: username, wallet published.
	// u4: username, wallet published, but the comment was posted from another wallet. u5: banned.
	insertSocialUser(t, ctx, db, socialDep, "did:1", nil, false)
	insertSocialUser(t, ctx, db, socialDep, "did:2", strPtr("bob"), false)
	insertSocialUser(t, ctx, db, socialDep, "did:3", strPtr("carol"), false)
	insertSocialUser(t, ctx, db, socialDep, "did:4", strPtr("dave"), false)
	insertSocialUser(t, ctx, db, socialDep, "did:5", strPtr("eve"), true)
	mustExecSocial(t, ctx, db, `INSERT INTO social_public_wallets (chain_id, deployment_id, wallet, privy_did) VALUES ($1, $2, $3, 'did:3')`, socialChainID, socialDep, w3)
	mustExecSocial(t, ctx, db, `INSERT INTO social_public_wallets (chain_id, deployment_id, wallet, privy_did) VALUES ($1, $2, $3, 'did:4')`, socialChainID, socialDep, addressBytes(0x99))
	id1 := insertSocialComment(t, ctx, db, token, "did:1", w1, "c1", false)
	id2 := insertSocialComment(t, ctx, db, token, "did:2", w2, "c2", false)
	id3 := insertSocialComment(t, ctx, db, token, "did:3", w3, "c3", false)
	id4 := insertSocialComment(t, ctx, db, token, "did:4", w4, "c4", false)
	insertSocialComment(t, ctx, db, token, "did:5", w5, "c5", false)

	rows, err := q.ListTokenComments(ctx, sqlc.ListTokenCommentsParams{
		ChainID: socialChainID, DeploymentID: socialDep, TokenAddress: sqlc.Address(token), RowLimit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	type view struct {
		username string
		wallet   []byte
	}
	got := map[int64]view{}
	var order []int64
	for _, r := range rows {
		got[r.ID] = view{r.DisplayUsername.String, r.DisplayWallet}
		order = append(order, r.ID)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d; want 4 (banned author excluded): %+v", len(rows), rows)
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] <= order[i] {
			t.Fatalf("order = %v; want id DESC", order)
		}
	}
	want := map[int64]view{
		id1: {"", w1},      // no username: wallet shown
		id2: {"bob", nil},  // username, wallet unpublished: wallet hidden
		id3: {"carol", w3}, // username, wallet published: both
		id4: {"dave", nil}, // published a different wallet: posting wallet hidden
	}
	for id, w := range want {
		g := got[id]
		if g.username != w.username || string(g.wallet) != string(w.wallet) {
			t.Fatalf("comment %d display = %+v; want %+v", id, g, w)
		}
	}
}

func TestListTokenCommentsHidesHidden(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	db := database.DB
	q := sqlc.New(openPool(t, ctx, database.URL))
	token := addressBytes(0x32)
	insertSocialComment(t, ctx, db, token, "did:1", addressBytes(0x41), "visible", false)
	insertSocialComment(t, ctx, db, token, "did:1", addressBytes(0x41), "hidden", true)

	rows, err := q.ListTokenComments(ctx, sqlc.ListTokenCommentsParams{
		ChainID: socialChainID, DeploymentID: socialDep, TokenAddress: sqlc.Address(token), RowLimit: 50,
	})
	if err != nil || len(rows) != 1 || rows[0].Body != "visible" {
		t.Fatalf("rows = %+v err=%v; want only the visible comment", rows, err)
	}
	count, err := q.CountVisibleComments(ctx, sqlc.CountVisibleCommentsParams{
		ChainID: socialChainID, DeploymentID: socialDep, TokenAddress: sqlc.Address(token),
	})
	if err != nil || count != 1 {
		t.Fatalf("count = %d err=%v; want 1", count, err)
	}
	before := pgtype.Int8{Int64: rows[0].ID, Valid: true}
	older, err := q.ListTokenComments(ctx, sqlc.ListTokenCommentsParams{
		ChainID: socialChainID, DeploymentID: socialDep, TokenAddress: sqlc.Address(token), BeforeID: before, RowLimit: 50,
	})
	if err != nil || len(older) != 0 {
		t.Fatalf("older = %+v err=%v; want none", older, err)
	}
}

func TestQualifyWallets(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	q := sqlc.New(openPool(t, ctx, database.URL))
	tokens := seedWatchlistTokens(t, ctx, database, 1, 0x10)
	// seeded creator is addressBytes(0x55); the chain id is the watchlist test chain.
	token := tokens[0].Bytes()
	creator, holder, empty, both := addressBytes(0x55), addressBytes(0x70), addressBytes(0x71), addressBytes(0x72)
	for _, h := range [][]byte{holder, both} {
		mustExecSocial(t, ctx, database.DB, `
			INSERT INTO holder_balances (chain_id, token_address, holder_address, balance, first_acquired_block_number)
			VALUES ($1, $2, $3, 5, 1)`, watchlistChainID, token, h)
	}
	mustExecSocial(t, ctx, database.DB, `
		INSERT INTO holder_balances (chain_id, token_address, holder_address, balance, first_acquired_block_number)
		VALUES ($1, $2, $3, 5, 1) ON CONFLICT DO NOTHING`, watchlistChainID, token, creator)

	run := func(wallets ...[]byte) map[string]string {
		rows, err := q.QualifyWallets(ctx, sqlc.QualifyWalletsParams{
			ChainID: watchlistChainID, DeploymentID: socialDep, TokenAddress: sqlc.Address(token), Wallets: wallets,
		})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, r := range rows {
			out[string(r.Wallet)] = r.Role
		}
		return out
	}

	// Creator holding a positive balance still reports creator.
	if got := run(creator, holder, empty); len(got) != 2 || got[string(creator)] != "creator" || got[string(holder)] != "holder" {
		t.Fatalf("with balances = %v", got)
	}
	// Creator with a zero balance is still the creator.
	mustExecSocial(t, ctx, database.DB, `
		UPDATE holder_balances SET balance = 0, first_acquired_block_number = NULL
		WHERE chain_id = $1 AND token_address = $2 AND holder_address = $3`, watchlistChainID, token, creator)
	if got := run(creator, empty); len(got) != 1 || got[string(creator)] != "creator" {
		t.Fatalf("creator zero balance = %v", got)
	}
	// A holder who drops to zero is absent.
	mustExecSocial(t, ctx, database.DB, `
		UPDATE holder_balances SET balance = 0, first_acquired_block_number = NULL
		WHERE chain_id = $1 AND token_address = $2 AND holder_address = $3`, watchlistChainID, token, both)
	if got := run(both); len(got) != 0 {
		t.Fatalf("zero-balance holder = %v; want absent", got)
	}
}
