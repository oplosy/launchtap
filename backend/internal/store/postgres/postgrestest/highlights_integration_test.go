//go:build integration

package postgrestest

import (
	"bytes"
	"context"
	"database/sql"
	"testing"
	"time"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

const highlightsChainID int64 = 46630

type highlightsToken struct {
	address, curve, pair, weth []byte
}

func newHighlightsToken(tag byte) highlightsToken {
	return highlightsToken{address: addressBytes(tag), curve: addressBytes(tag + 1), pair: addressBytes(tag + 2), weth: addressBytes(tag + 3)}
}

func (h highlightsToken) isToken0() bool { return bytes.Compare(h.address, h.weth) < 0 }

// insertHighlightsLaunch creates a launch block and the launch event for one token.
func insertHighlightsLaunch(t *testing.T, ctx context.Context, db *sql.DB, block int64, at time.Time, tok highlightsToken) {
	t.Helper()
	mustInsertBlock(t, ctx, db, highlightsChainID, block, hashBytes(byte(block)), hashBytes(byte(block-1)), at, "safe")
	insertProjectionLaunch(t, ctx, db, highlightsChainID, block, hashBytes(byte(block)), at, hashBytes(byte(0x80+block)),
		projectionLaunchFixture{token: tok.address, curve: tok.curve, pair: tok.pair, weth: tok.weth})
}

// insertHighlightsTrade stores a curve trade in its own block with a chosen trader and side.
func insertHighlightsTrade(t *testing.T, ctx context.Context, db *sql.DB, block int64, at time.Time, tok highlightsToken, trader byte, isBuy bool, gross int64) {
	t.Helper()
	mustInsertBlock(t, ctx, db, highlightsChainID, block, hashBytes(byte(block)), hashBytes(byte(block-1)), at, "safe")
	if _, err := db.ExecContext(ctx, `
		INSERT INTO trades (
			chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
			token_address, trader, is_buy, eth_gross, eth_refund, token_amount,
			protocol_fee, creator_fee, new_eth_reserve, new_token_reserve
		) VALUES ($1, $2, $3, $4, 0, $5, 0, $6, $7, $8, $9, 0, 1000, 0, 0, $10, 800000)
	`, highlightsChainID, block, hashBytes(byte(block)), at, hashBytes(byte(0x80+block)), tok.address, addressBytes(trader), isBuy, gross, 10+gross); err != nil {
		t.Fatalf("insert trade in block %d: %v", block, err)
	}
}

func insertHighlightsSync(t *testing.T, ctx context.Context, db *sql.DB, deployment string, block int64, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO sync_state(chain_id,deployment_id,observed_number,observed_hash,observed_at,safe_number,safe_hash,safe_at) VALUES($1,$2,$3,$4,$5,$3,$4,$5)`,
		highlightsChainID, deployment, block, hashBytes(byte(block)), at); err != nil {
		t.Fatalf("insert sync state: %v", err)
	}
}

func openHighlightsPool(t *testing.T, ctx context.Context, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestHighlightsTrending(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool := openHighlightsPool(t, ctx, database.URL)
	const deployment = "highlights-trending-test"
	now := time.Now().UTC().Truncate(time.Second)
	windowStart := now.Add(-60 * time.Minute)
	db := database.DB

	a, b, c, d, e, f := newHighlightsToken(0x10), newHighlightsToken(0x20), newHighlightsToken(0x30), newHighlightsToken(0x40), newHighlightsToken(0x60), newHighlightsToken(0x70)
	for i, tok := range []highlightsToken{a, b, c, d, e, f} {
		insertHighlightsLaunch(t, ctx, db, int64(i+1), now.Add(-3*time.Hour), tok)
	}
	// A: one buyer, 10. Latest trade of the tie below.
	insertHighlightsTrade(t, ctx, db, 11, now.Add(-5*time.Minute), a, 0xb1, true, 10)
	// E: one buyer, 10, older than A's trade, so it loses the score tie.
	insertHighlightsTrade(t, ctx, db, 12, now.Add(-20*time.Minute), e, 0xb2, true, 10)
	// B: curve buyers b3 (twice) and b4, plus one DEX buyer: 3 distinct buyers, volume 4.
	insertHighlightsTrade(t, ctx, db, 13, now.Add(-30*time.Minute), b, 0xb3, true, 1)
	insertHighlightsTrade(t, ctx, db, 14, now.Add(-28*time.Minute), b, 0xb3, true, 1)
	insertHighlightsTrade(t, ctx, db, 16, now.Add(-25*time.Minute), b, 0xb4, true, 1)
	mustInsertBlock(t, ctx, db, highlightsChainID, 15, hashBytes(15), hashBytes(14), now.Add(-10*time.Minute), "safe")
	if b.isToken0() {
		insertPoolSwap(t, ctx, db, highlightsChainID, 15, hashBytes(15), now.Add(-10*time.Minute), hashBytes(0x8f), 0, b.pair, 0, 1, 1000, 0)
	} else {
		insertPoolSwap(t, ctx, db, highlightsChainID, 15, hashBytes(15), now.Add(-10*time.Minute), hashBytes(0x8f), 0, b.pair, 1, 0, 0, 1000)
	}
	// C: only a trade 61 minutes old, outside the window.
	insertHighlightsTrade(t, ctx, db, 17, now.Add(-61*time.Minute), c, 0xb5, true, 50)
	// F: volume in the window but only sells, so no buyer: must not be listed.
	insertHighlightsTrade(t, ctx, db, 18, now.Add(-15*time.Minute), f, 0xb7, false, 40)
	// D: a trade in a block that is reorged away below.
	insertHighlightsTrade(t, ctx, db, 30, now.Add(-time.Minute), d, 0xb6, true, 100)
	for _, tok := range []highlightsToken{a, b, c, d, e, f} {
		callRebuild(t, ctx, db, highlightsChainID, tok.address)
	}
	insertHighlightsSync(t, ctx, db, deployment, 20, now)
	mustInsertBlock(t, ctx, db, highlightsChainID, 20, hashBytes(20), hashBytes(19), now, "safe")

	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, tx *storepostgres.Adapter) error {
		if err := tx.DeleteCanonicalAbove(ctx, highlightsChainID, 29); err != nil {
			return err
		}
		return tx.RebuildTokenProjections(ctx, highlightsChainID, common.BytesToAddress(d.address))
	}); err != nil {
		t.Fatalf("reorg block 30: %v", err)
	}

	reader := storepostgres.HighlightsReader{Pool: pool, DeploymentID: deployment}
	got, err := reader.Highlights(ctx, highlightsChainID, windowStart, 20)
	if err != nil {
		t.Fatalf("highlights: %v", err)
	}
	if len(got.Trending) != 3 {
		t.Fatalf("trending = %d tokens, want 3 (B, A, E): %+v", len(got.Trending), got.Trending)
	}
	for i, want := range []struct {
		tok     highlightsToken
		volume  int64
		buyers  int64
		comment string
	}{{b, 4, 3, "highest score 4*3 incl. DEX buyer"}, {a, 10, 1, "score 10, latest trade wins the tie"}, {e, 10, 1, "score 10, older trade"}} {
		row := got.Trending[i]
		if row.Address != common.BytesToAddress(want.tok.address) || row.WindowVolumeETH.Int64() != want.volume || row.DistinctBuyers != want.buyers {
			t.Fatalf("trending[%d] = %s volume=%s buyers=%d; want %x volume=%d buyers=%d (%s)", i, row.Address, row.WindowVolumeETH, row.DistinctBuyers, want.tok.address, want.volume, want.buyers, want.comment)
		}
	}
	for _, row := range got.Trending {
		if row.Address == common.BytesToAddress(c.address) {
			t.Fatal("token C (trade outside the window) must be absent")
		}
		if row.Address == common.BytesToAddress(f.address) {
			t.Fatal("token F (sell-only, zero buyers) must be absent")
		}
		if row.Address == common.BytesToAddress(d.address) {
			t.Fatal("token D (reorged trade) must be absent")
		}
	}
	if !got.WindowStart.Equal(windowStart) {
		t.Fatalf("window start = %s, want %s", got.WindowStart, windowStart)
	}
	if got.Snapshot.BlockNumber != 20 || got.Finality != "safe" {
		t.Fatalf("snapshot block=%d finality=%q, want 20/safe", got.Snapshot.BlockNumber, got.Finality)
	}

	limited, err := reader.Highlights(ctx, highlightsChainID, windowStart, 1)
	if err != nil || len(limited.Trending) != 1 || limited.Trending[0].Address != common.BytesToAddress(b.address) {
		t.Fatalf("limit 1 = %+v err=%v, want only B", limited.Trending, err)
	}
	// The window start is inclusive of a trade exactly at the boundary and excludes older ones.
	atBoundary, err := reader.Highlights(ctx, highlightsChainID, now.Add(-61*time.Minute), 20)
	if err != nil {
		t.Fatal(err)
	}
	foundC := false
	for _, row := range atBoundary.Trending {
		foundC = foundC || row.Address == common.BytesToAddress(c.address)
	}
	if !foundC {
		t.Fatal("token C must appear when its trade is exactly at the window start")
	}
	if _, err := reader.Highlights(ctx, highlightsChainID, windowStart, 0); err == nil {
		t.Fatal("limit 0 must be rejected")
	}
}

func TestHighlightsKingOfTheHill(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool := openHighlightsPool(t, ctx, database.URL)
	const deployment = "highlights-koth-test"
	now := time.Now().UTC().Truncate(time.Second)
	db := database.DB

	low, high, graduated := newHighlightsToken(0x10), newHighlightsToken(0x20), newHighlightsToken(0x30)
	for i, tok := range []highlightsToken{low, high, graduated} {
		insertHighlightsLaunch(t, ctx, db, int64(i+1), now.Add(-3*time.Hour), tok)
	}
	// graduation_eth is 100 and virtual_eth is 10: reserves 60 / 90 / 109 give 5000 / 8000 / 9900 bps.
	insertProjectionTrade(t, ctx, db, highlightsChainID, 11, hashBytesAfter(t, ctx, db, 11, now), now.Add(-2*time.Hour), hashBytes(0xa1), 0, low.address, 50, 60, 700000)
	insertProjectionTrade(t, ctx, db, highlightsChainID, 12, hashBytesAfter(t, ctx, db, 12, now), now.Add(-2*time.Hour), hashBytes(0xa2), 0, high.address, 80, 90, 700000)
	insertProjectionTrade(t, ctx, db, highlightsChainID, 13, hashBytesAfter(t, ctx, db, 13, now), now.Add(-2*time.Hour), hashBytes(0xa3), 0, graduated.address, 99, 109, 700000)
	insertProjectionGraduation(t, ctx, db, highlightsChainID, 14, hashBytesAfter(t, ctx, db, 14, now), now.Add(-2*time.Hour), hashBytes(0xa4), 1, graduated.address, graduated.pair)
	for _, tok := range []highlightsToken{low, high, graduated} {
		callRebuild(t, ctx, db, highlightsChainID, tok.address)
	}
	insertHighlightsSync(t, ctx, db, deployment, 14, now)

	reader := storepostgres.HighlightsReader{Pool: pool, DeploymentID: deployment}
	got, err := reader.Highlights(ctx, highlightsChainID, now.Add(-time.Hour), 20)
	if err != nil {
		t.Fatalf("highlights: %v", err)
	}
	if got.King == nil {
		t.Fatal("king = nil, want the highest-progress curve token")
	}
	if got.King.Address != common.BytesToAddress(high.address) || got.King.GraduationProgressBPS != 8000 || got.King.RemainingETH.Int64() != 20 {
		t.Fatalf("king = %s progress=%d remaining=%s; want %x 8000 20 (graduated token at 9900 must be excluded)", got.King.Address, got.King.GraduationProgressBPS, got.King.RemainingETH, high.address)
	}
	if got.King.Phase != "curve" {
		t.Fatalf("king phase = %q", got.King.Phase)
	}
	if len(got.Trending) != 0 {
		t.Fatalf("no trade falls in the window, trending = %+v", got.Trending)
	}
}

func TestHighlightsKingOfTheHillAbsent(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool := openHighlightsPool(t, ctx, database.URL)
	const deployment = "highlights-no-koth-test"
	now := time.Now().UTC().Truncate(time.Second)
	db := database.DB

	graduated := newHighlightsToken(0x30)
	insertHighlightsLaunch(t, ctx, db, 1, now.Add(-3*time.Hour), graduated)
	insertProjectionTrade(t, ctx, db, highlightsChainID, 2, hashBytesAfter(t, ctx, db, 2, now), now.Add(-2*time.Hour), hashBytes(0xa3), 0, graduated.address, 99, 109, 700000)
	insertProjectionGraduation(t, ctx, db, highlightsChainID, 3, hashBytesAfter(t, ctx, db, 3, now), now.Add(-2*time.Hour), hashBytes(0xa4), 1, graduated.address, graduated.pair)
	callRebuild(t, ctx, db, highlightsChainID, graduated.address)
	insertHighlightsSync(t, ctx, db, deployment, 3, now)

	got, err := (storepostgres.HighlightsReader{Pool: pool, DeploymentID: deployment}).Highlights(ctx, highlightsChainID, now.Add(-time.Hour), 20)
	if err != nil {
		t.Fatalf("highlights: %v", err)
	}
	if got.King != nil {
		t.Fatalf("king = %+v, want nil when no curve token exists", got.King)
	}
	if got.Trending == nil {
		t.Fatal("trending must be an empty slice, not nil")
	}
	var _ token.HighlightsReader = storepostgres.HighlightsReader{}
}

// hashBytesAfter inserts the block for a hand-built event and returns its hash.
func hashBytesAfter(t *testing.T, ctx context.Context, db *sql.DB, block int64, at time.Time) []byte {
	t.Helper()
	mustInsertBlock(t, ctx, db, highlightsChainID, block, hashBytes(byte(block)), hashBytes(byte(block-1)), at.Add(-2*time.Hour), "safe")
	return hashBytes(byte(block))
}
