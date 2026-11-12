//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/earlybuyer"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Early-buyer clusters read the curve deliveries inside the 60 s window, the token transfers
// between those wallets and their current balances in one snapshot
// (docs/specs/2026-11-12-early-buyer-clusters-design.md).
func TestEarlyBuyersGroupDeliveriesInsideTheWindow(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	const chainID int64 = 46630
	const deployment = "early-buyers-test"
	launchAt := time.Date(2026, 9, 9, 8, 0, 0, 0, time.UTC)
	tokenBytes, curve, zero := addressBytes(0x62), addressBytes(0x64), make([]byte, 20)
	creator := addressBytes(0x55) // insertProjectionLaunch's creator
	w1, w2, w3, late1, late2 := addressBytes(0x71), addressBytes(0x72), addressBytes(0x73), addressBytes(0x75), addressBytes(0x76)

	blocks := []struct {
		number int64
		hash   byte
		at     time.Duration
	}{{100, 0xa0, 0}, {101, 0xa1, 30 * time.Second}, {103, 0xa3, 120 * time.Second}, {104, 0xa4, 130 * time.Second}}
	parent := hashBytes(0x9f)
	for _, b := range blocks {
		mustInsertBlock(t, ctx, database.DB, chainID, b.number, hashBytes(b.hash), parent, launchAt.Add(b.at), "safe")
		parent = hashBytes(b.hash)
	}
	block := func(number int64) ([]byte, time.Time) {
		for _, b := range blocks {
			if b.number == number {
				return hashBytes(b.hash), launchAt.Add(b.at)
			}
		}
		t.Fatalf("no block %d", number)
		return nil, time.Time{}
	}
	launchHash, launchTime := block(100)
	launchTx := hashBytes(0xb0)
	insertProjectionLaunch(t, ctx, database.DB, chainID, 100, launchHash, launchTime, launchTx, projectionLaunchFixture{token: tokenBytes, curve: curve, pair: addressBytes(0x65), weth: addressBytes(0x66)})
	transfer := func(number int64, tx byte, log int, from, to []byte, value int64) {
		hash, at := block(number)
		insertTransfer(t, ctx, database.DB, chainID, number, hash, at, hashBytes(tx), log, tokenBytes, from, to, value)
	}
	transfer(100, 0xb0, 0, zero, curve, 1_000_000) // the supply mint (insertTransfer orders by log index within a block)
	// One transaction in the launch block delivers to the creator and two wallets.
	transfer(100, 0xb1, 1, curve, creator, 30)
	transfer(100, 0xb1, 2, curve, w1, 100)
	transfer(100, 0xb1, 3, curve, w2, 50)
	// A separate buy 30 s later; that wallet later sends everything to w1.
	transfer(101, 0xb2, 0, curve, w3, 20)
	transfer(104, 0xb4, 0, w3, w1, 20)
	// Two recipients of one transaction 120 s after launch: outside the window.
	transfer(103, 0xb3, 0, curve, late1, 10)
	transfer(103, 0xb3, 1, curve, late2, 10)
	if _, err := database.DB.ExecContext(ctx, `SELECT rebuild_token_projections($1,$2)`, chainID, tokenBytes); err != nil {
		t.Fatal(err)
	}
	// A second token launched at block 103: at the snapshot (block 104, 10 s later) its window is open.
	otherToken := addressBytes(0x82)
	otherHash, otherTime := block(103)
	insertProjectionLaunch(t, ctx, database.DB, chainID, 103, otherHash, otherTime, hashBytes(0xc0), projectionLaunchFixture{token: otherToken, curve: addressBytes(0x84), pair: addressBytes(0x85), weth: addressBytes(0x66)})
	if _, err := database.DB.ExecContext(ctx, `SELECT rebuild_token_projections($1,$2)`, chainID, otherToken); err != nil {
		t.Fatal(err)
	}
	snapshotHash, snapshotTime := block(104)
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO sync_state(chain_id,deployment_id,observed_number,observed_hash,observed_at,safe_number,safe_hash,safe_at) VALUES($1,$2,104,$3,$4,104,$3,$4)`, chainID, deployment, snapshotHash, snapshotTime); err != nil {
		t.Fatal(err)
	}

	reader := storepostgres.MarketReader{Pool: pool, DeploymentID: deployment}
	got, err := reader.ReadEarlyBuyers(ctx, earlybuyer.Query{ChainID: chainID, Token: common.BytesToAddress(tokenBytes)})
	if err != nil {
		t.Fatal(err)
	}
	if got.WindowOpen || got.Snapshot.BlockNumber != 104 || got.Finality != "safe" {
		t.Fatalf("window open %v, snapshot %+v, finality %q", got.WindowOpen, got.Snapshot, got.Finality)
	}
	// Early: creator, w1, w2 (launch block) and w3 (30 s); late1 and late2 are outside the window.
	if got.EarlyWalletCount != 4 || got.EarlyBalance.Int64() != 200 || got.ClusterCount != 1 || len(got.Clusters) != 1 {
		t.Fatalf("early %d / %s, clusters %d: %+v", got.EarlyWalletCount, got.EarlyBalance, got.ClusterCount, got.Clusters)
	}
	c := got.Clusters[0]
	var wallets []common.Address
	var balances []int64
	for _, w := range c.Wallets {
		wallets = append(wallets, w.Address)
		balances = append(balances, w.Balance.Int64())
	}
	wantWallets := []common.Address{common.BytesToAddress(w1), common.BytesToAddress(w2), common.BytesToAddress(creator), common.BytesToAddress(w3)}
	if !reflect.DeepEqual(wallets, wantWallets) || !reflect.DeepEqual(balances, []int64{120, 50, 30, 0}) {
		t.Fatalf("wallets %v balances %v", wallets, balances)
	}
	if c.WalletCount != 4 || c.Balance.Int64() != 200 || !c.IncludesCreator ||
		!reflect.DeepEqual(c.Links, []string{earlybuyer.LinkSameTransaction, earlybuyer.LinkTokenTransfer}) {
		t.Fatalf("cluster %+v", c)
	}

	open, err := reader.ReadEarlyBuyers(ctx, earlybuyer.Query{ChainID: chainID, Token: common.BytesToAddress(otherToken)})
	if err != nil {
		t.Fatal(err)
	}
	if !open.WindowOpen || open.EarlyWalletCount != 0 || len(open.Clusters) != 0 {
		t.Fatalf("other token %+v", open)
	}

	_, err = reader.ReadEarlyBuyers(ctx, earlybuyer.Query{ChainID: chainID, Token: common.BytesToAddress(addressBytes(0x99))})
	if !errors.Is(err, token.ErrNotFound) {
		t.Fatalf("unknown token err = %v", err)
	}
}
