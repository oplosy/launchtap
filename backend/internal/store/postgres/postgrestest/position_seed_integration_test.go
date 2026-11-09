//go:build integration

package postgrestest

import (
	"context"
	"sort"
	"testing"
	"time"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

// I1: ledger history written before the position queue existed has no position_dirty rows. The
// seed queues every (wallet, token) pair of it, never an excluded address (R4), and is safe to
// rerun; draining the queue then yields the same positions as a fresh compute.
func TestSeedPositionDirtyQueuesExistingHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newCurvePositionFixture(t, ctx)
	l := positionLedger{t: t, ctx: ctx, f: f}
	seedTradingLedger(l)
	factory := common.BytesToAddress(addressBytes(0xfa))
	// Transfer-only holder: 0xc7 never traded, it was given tokens.
	l.transfer(14, 2, 0x8a, 4, 0xc5, 0xc7, 5)
	// Curve-only trader: 0xc9's buy delivered the tokens elsewhere, so it has no transfer row.
	l.trade(12, 2, 0x8b, 2, 0xc9, true, 60, 4, 1, 1)
	l.transfer(12, 2, 0x8b, 3, 0x41, 0xc8, 4)
	// The factory is excluded even when it holds tokens.
	l.transfer(14, 4, 0x8c, 5, 0xc5, 0xfa, 1)
	// A second token makes the seed span more than one chunk: its curve funds 0xca, which passes
	// tokens on to 0xcb. Its curve (0x42) and pair (0x32) are excluded like the first token's.
	secondToken := addressBytes(0x11)
	insertProjectionLaunch(t, ctx, f.database.DB, walletChainID, 10, f.hash, f.at, hashBytes(0x33), projectionLaunchFixture{token: secondToken, curve: addressBytes(0x42), pair: addressBytes(0x32), weth: addressBytes(0x90)})
	insertTransfer(t, ctx, f.database.DB, walletChainID, 10, f.hash, f.at, hashBytes(0x34), 1, secondToken, addressBytes(0), addressBytes(0x42), 1000000)
	insertTransfer(t, ctx, f.database.DB, walletChainID, 10, f.hash, f.at, hashBytes(0x35), 1, secondToken, addressBytes(0x42), addressBytes(0xca), 7)
	insertTransfer(t, ctx, f.database.DB, walletChainID, 11, l.blockHash(11), l.blockTime(11), hashBytes(0x36), 1, secondToken, addressBytes(0xca), addressBytes(0xcb), 3)
	callRebuild(t, ctx, f.database.DB, walletChainID, secondToken)
	// Pre-feature state: nothing queued, nothing computed.
	l.exec(`DELETE FROM position_dirty WHERE chain_id = $1`, walletChainID)

	adapter := storepostgres.NewAdapter(f.pool).WithFactory(factory)
	wantKeys := []string{}
	for _, wallet := range []byte{0xc1, 0xc5, 0xc6, 0xc7, 0xc8, 0xc9} {
		wantKeys = append(wantKeys, common.Bytes2Hex(addressBytes(wallet))+"/"+common.Bytes2Hex(positionToken))
	}
	for _, wallet := range []byte{0xca, 0xcb} {
		wantKeys = append(wantKeys, common.Bytes2Hex(addressBytes(wallet))+"/"+common.Bytes2Hex(secondToken))
	}
	sort.Strings(wantKeys)

	dry, err := adapter.SeedPositionDirty(ctx, walletChainID, true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Pairs != int64(len(wantKeys)) || dry.Marked != 0 || dry.Tokens != 2 {
		t.Fatalf("dry run = %+v, want %d pairs over 2 token chunks and nothing marked", dry, len(wantKeys))
	}
	if rows := f.dirty(t, ctx); len(rows) != 0 {
		t.Fatalf("dry run wrote position_dirty rows: %v", keys(rows))
	}

	for run := 0; run < 2; run++ {
		seed, err := adapter.SeedPositionDirty(ctx, walletChainID, false)
		if err != nil {
			t.Fatal(err)
		}
		if seed.Pairs != int64(len(wantKeys)) || seed.Marked != int64(len(wantKeys)) || seed.Tokens != 2 {
			t.Fatalf("seed run %d = %+v, want %d pairs marked over 2 token chunks", run, seed, len(wantKeys))
		}
		got := keys(f.dirty(t, ctx))
		sort.Strings(got)
		if len(got) != len(wantKeys) {
			t.Fatalf("seed run %d queued %v, want %v", run, got, wantKeys)
		}
		for i := range got {
			if got[i] != wantKeys[i] {
				t.Fatalf("seed run %d queued %v, want %v (zero address, curve, pair and factory excluded)", run, got, wantKeys)
			}
		}
	}

	drainPositions(t, ctx, f)
	trader, ok := readPosition(t, ctx, f.database.DB, 0xc1, 0x10)
	if want := wantPosition(mustCompute(t, leg(11, 0, 500, 1000, 0), leg(12, 1, -200, 0, 395), leg(14, 0, -100, 0, 150))); !ok || trader != want {
		t.Fatalf("seeded trader position = %+v (present %t), want %+v", trader, ok, want)
	}
	holder, ok := readPosition(t, ctx, f.database.DB, 0xc7, 0x10)
	if want := wantPosition(mustCompute(t, leg(14, 2, 5, 0, 0))); !ok || holder != want {
		t.Fatalf("seeded transfer-only holder = %+v (present %t), want %+v", holder, ok, want)
	}
	curveOnly, ok := readPosition(t, ctx, f.database.DB, 0xc9, 0x10)
	if want := wantPosition(mustCompute(t, leg(12, 2, 0, 60, 0))); !ok || curveOnly != want {
		t.Fatalf("seeded curve-only trader = %+v (present %t), want %+v", curveOnly, ok, want)
	}
	for _, excluded := range []byte{0x00, 0x41, 0x31, 0x42, 0x32, 0xfa} {
		if row, ok := readPosition(t, ctx, f.database.DB, excluded, 0x10); ok {
			t.Fatalf("excluded address %#x got a position %+v", excluded, row)
		}
	}
}

// The seed pages tokens with a keyset cursor; pages smaller than the token count must still
// cover every token exactly once.
func TestSeedPositionDirtyPagesTokens(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newCurvePositionFixture(t, ctx)
	tokens := [][]byte{positionToken}
	insertTransfer(t, ctx, f.database.DB, walletChainID, 10, f.hash, f.at, hashBytes(0x37), 2, positionToken, addressBytes(0x41), addressBytes(0xd0), 1)
	for i, b := range []byte{0x11, 0x12} {
		token := addressBytes(b)
		tokens = append(tokens, token)
		insertProjectionLaunch(t, ctx, f.database.DB, walletChainID, 10, f.hash, f.at, hashBytes(0x40+byte(i)), projectionLaunchFixture{token: token, curve: addressBytes(0x50 + byte(i)), pair: addressBytes(0x60 + byte(i)), weth: addressBytes(0x90)})
		insertTransfer(t, ctx, f.database.DB, walletChainID, 10, f.hash, f.at, hashBytes(0x48+byte(i)), 0, token, addressBytes(0), addressBytes(0x50+byte(i)), 1000)
		insertTransfer(t, ctx, f.database.DB, walletChainID, 10, f.hash, f.at, hashBytes(0x48+byte(i)), 1, token, addressBytes(0x50+byte(i)), addressBytes(0xd1+byte(i)), 1)
		callRebuild(t, ctx, f.database.DB, walletChainID, token)
	}
	if _, err := f.database.DB.ExecContext(ctx, `DELETE FROM position_dirty WHERE chain_id = $1`, walletChainID); err != nil {
		t.Fatal(err)
	}
	seed, err := storepostgres.NewAdapter(f.pool).SeedPositionDirtyPaged(ctx, walletChainID, false, 2)
	if err != nil {
		t.Fatal(err)
	}
	if seed.Tokens != 3 || seed.Pairs != 3 || seed.Marked != 3 {
		t.Fatalf("seed = %+v, want 3 tokens and 3 pairs over two pages", seed)
	}
	rows := f.dirty(t, ctx)
	for i, wallet := range []byte{0xd0, 0xd1, 0xd2} {
		if rows[common.Bytes2Hex(addressBytes(wallet))+"/"+common.Bytes2Hex(tokens[i])].wallet == "" {
			t.Fatalf("queued %v, missing wallet %#x of token %d", keys(rows), wallet, i)
		}
	}
}
