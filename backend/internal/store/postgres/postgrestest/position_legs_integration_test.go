//go:build integration

package postgrestest

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"
	"time"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
)

// readLegs returns ListPositionLegs for (wallet, 0x10) as "block/txIndex delta paid received".
func readLegs(t *testing.T, ctx context.Context, f walletSchemaFixture, wallet byte) []string {
	t.Helper()
	rows, err := sqlc.New(f.pool).ListPositionLegs(ctx, sqlc.ListPositionLegsParams{ChainID: walletChainID, TokenAddress: positionToken, Wallet: addressBytes(wallet)})
	if err != nil {
		t.Fatalf("list position legs: %v", err)
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		values := make([]string, 3)
		for j, number := range []driver.Valuer{row.LegTokenDelta, row.LegEthPaid, row.LegEthReceived} {
			value, err := number.Value()
			if err != nil {
				t.Fatalf("leg numeric: %v", err)
			}
			values[j] = fmt.Sprint(value)
		}
		out[i] = fmt.Sprintf("%d/%d %s", row.LegBlockNumber, row.LegTransactionIndex, strings.Join(values, " "))
	}
	return out
}

func assertLegs(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("legs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// M1: WETH sorts below the token, so WETH is the pair's token0 (token_is_token0 = false) and the
// ETH side of each swap is amount0.
func TestPositionLegsWETHAsToken0(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	database := NewMigrated(t)
	pool := openPool(t, ctx, database.URL)
	at := time.Date(2026, time.November, 6, 9, 0, 0, 0, time.UTC)
	hash := hashBytes(0x21)
	mustInsertBlock(t, ctx, database.DB, walletChainID, 10, hash, hashBytes(0x20), at, "safe")
	pair, weth := addressBytes(0x31), addressBytes(0x05)
	insertProjectionLaunch(t, ctx, database.DB, walletChainID, 10, hash, at, hashBytes(0x30), projectionLaunchFixture{token: positionToken, curve: addressBytes(0x41), pair: pair, weth: weth})
	insertProjectionGraduation(t, ctx, database.DB, walletChainID, 10, hash, at, hashBytes(0x31), 2, positionToken, pair)
	insertPoolSync(t, ctx, database.DB, walletChainID, 10, hash, at, hashBytes(0x7f), 3, pair, 1200, 200)
	callRebuild(t, ctx, database.DB, walletChainID, positionToken)
	var tokenIsToken0 bool
	if err := database.DB.QueryRowContext(ctx, `SELECT token_is_token0 FROM tokens WHERE chain_id = $1 AND token_address = $2`, walletChainID, positionToken).Scan(&tokenIsToken0); err != nil || tokenIsToken0 {
		t.Fatalf("token_is_token0 = %t, %v; the fixture needs WETH as token0", tokenIsToken0, err)
	}
	f := walletSchemaFixture{database: database, pool: pool, reader: storepostgres.FeedReader{Pool: pool}, pair: common.BytesToAddress(pair), at: at, hash: hash}
	l := positionLedger{t: t, ctx: ctx, f: f}
	l.block(11, "safe")
	l.block(12, "safe")
	// Buy: 300 WETH (token0) in, 50 tokens (token1) out to 0xc1.
	l.swap(11, 0, 0x91, 0, 300, 0, 0, 50, 0xc1, 0xc1)
	l.transfer(11, 0, 0x91, 1, 0x31, 0xc1, 50)
	// Sell: 20 tokens (token1) in, 40 WETH (token0) out.
	l.transfer(12, 0, 0x92, 0, 0xc1, 0x31, 20)
	l.swap(12, 0, 0x92, 1, 0, 20, 40, 0, 0xc1, 0xc1)

	assertLegs(t, readLegs(t, ctx, f, 0xc1), "11/0 50 300 0", "12/0 -20 0 40")
	l.mark(0xc1)
	drainPositions(t, ctx, f)
	got, ok := readPosition(t, ctx, f.database.DB, 0xc1, 0x10)
	if want := wantPosition(mustCompute(t, leg(11, 0, 50, 300, 0), leg(12, 0, -20, 0, 40))); !ok || got != want {
		t.Fatalf("position = %+v (present %t), want %+v", got, ok, want)
	}
}

// M2: several events of one transaction form exactly one leg — a curve buy and sell, two pool
// swaps, a curve trade plus a swap whose tokens go to someone else, and a swap-only leg.
func TestPositionLegsMultiEventTransactions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newCurvePositionFixture(t, ctx)
	l := positionLedger{t: t, ctx: ctx, f: f}
	for block := int64(11); block <= 14; block++ {
		l.block(block, "safe")
	}
	// tx 0x81: curve buy of 500 tokens for 1000 gross, then a curve sell of 200 for 400 gross with
	// 2 + 3 fees (395 received), in one transaction.
	l.trade(11, 0, 0x81, 0, 0xc1, true, 1000, 500, 5, 5)
	l.transfer(11, 0, 0x81, 1, 0x41, 0xc1, 500)
	l.trade(11, 0, 0x81, 2, 0xc1, false, 400, 200, 2, 3)
	l.transfer(11, 0, 0x81, 3, 0xc1, 0x41, 200)
	// Graduation in block 12 (tx 0x8f) opens the pair.
	insertProjectionGraduation(l.t, l.ctx, l.f.database.DB, walletChainID, 12, l.blockHash(12), l.blockTime(12), hashBytes(0x8f), 0, positionToken, l.f.pair.Bytes())
	insertPoolSync(l.t, l.ctx, l.f.database.DB, walletChainID, 12, l.blockHash(12), l.blockTime(12), hashBytes(0x8f), 1, l.f.pair.Bytes(), 200, 1200)
	callRebuild(l.t, l.ctx, l.f.database.DB, walletChainID, positionToken)
	// tx 0x87: a curve buy of 250 for 600 gross and, in the same tx, a pool buy of 25 tokens for
	// 40 WETH delivered to 0xc2 (no transfer for 0xc1 from the swap).
	l.trade(12, 1, 0x87, 2, 0xc1, true, 600, 250, 3, 3)
	l.transfer(12, 1, 0x87, 3, 0x41, 0xc1, 250)
	l.swap(12, 1, 0x87, 4, 0, 40, 25, 0, 0xc2, 0xc1)
	l.transfer(12, 1, 0x87, 5, 0x31, 0xc2, 25)
	// tx 0x84: two swaps — a sell of 100 tokens for 150 WETH, then a buy of 5 tokens for 10 WETH.
	l.transfer(13, 0, 0x84, 0, 0xc1, 0x31, 100)
	l.swap(13, 0, 0x84, 1, 100, 0, 0, 150, 0xc1, 0xc1)
	l.swap(13, 0, 0x84, 2, 0, 10, 5, 0, 0xc1, 0xc1)
	l.transfer(13, 0, 0x84, 3, 0x31, 0xc1, 5)
	// tx 0x88: a swap-only leg — 0xc1 pays 40 WETH, 0xc2 receives the tokens.
	l.swap(14, 0, 0x88, 0, 0, 40, 25, 0, 0xc2, 0xc1)
	l.transfer(14, 0, 0x88, 1, 0x31, 0xc2, 25)

	assertLegs(t, readLegs(t, ctx, f, 0xc1),
		"11/0 300 1000 395",
		"12/1 250 640 0",
		"13/0 -95 10 150",
		"14/0 0 40 0",
	)
	// The recipient's legs carry tokens only.
	assertLegs(t, readLegs(t, ctx, f, 0xc2), "12/1 25 0 0", "14/0 25 0 0")

	l.mark(0xc1, 0xc2)
	drainPositions(t, ctx, f)
	got, ok := readPosition(t, ctx, f.database.DB, 0xc1, 0x10)
	want := wantPosition(mustCompute(t, leg(11, 0, 300, 1000, 395), leg(12, 1, 250, 640, 0), leg(13, 0, -95, 10, 150), leg(14, 0, 0, 40, 0)))
	if !ok || got != want {
		t.Fatalf("position = %+v (present %t), want %+v", got, ok, want)
	}
}
