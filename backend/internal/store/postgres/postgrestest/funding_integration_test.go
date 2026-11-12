//go:build integration

package postgrestest

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/earlybuyer"
	"github.com/Contictus/launchtap/backend/internal/funding"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

// docs/specs/2026-11-12-funding-source-clusters-design.md acceptance (integration): the worker
// query picks only finalized, unresolved early wallets; the reader returns funding_checked and
// funding links, honouring the exclusion table and curves.
func TestFundingWorkerQueueAndReader(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	const chainID int64 = 46630
	const deployment = "funding-test"
	launchAt := time.Date(2026, 9, 9, 8, 0, 0, 0, time.UTC)
	tokenBytes, curve, zero := addressBytes(0x62), addressBytes(0x64), make([]byte, 20)
	w1, w2, w3, w4, w5 := addressBytes(0x71), addressBytes(0x72), addressBytes(0x73), addressBytes(0x74), addressBytes(0x75)
	blocks := []struct {
		number int64
		hash   byte
		at     time.Duration
	}{{100, 0xa0, 0}, {101, 0xa1, 10 * time.Second}, {102, 0xa2, 20 * time.Second}}
	parent := hashBytes(0x9f)
	for _, b := range blocks {
		mustInsertBlock(t, ctx, database.DB, chainID, b.number, hashBytes(b.hash), parent, launchAt.Add(b.at), "safe")
		parent = hashBytes(b.hash)
	}
	insertProjectionLaunch(t, ctx, database.DB, chainID, 100, hashBytes(0xa0), launchAt, hashBytes(0xb0),
		projectionLaunchFixture{token: tokenBytes, curve: curve, pair: addressBytes(0x65), weth: addressBytes(0x66)})
	transfer := func(number int64, tx byte, log int, from, to []byte, value int64) {
		b := blocks[number-100]
		insertTransfer(t, ctx, database.DB, chainID, number, hashBytes(b.hash), launchAt.Add(b.at), hashBytes(tx), log, tokenBytes, from, to, value)
	}
	transfer(100, 0xb0, 0, zero, curve, 1_000_000)
	// Separate buys, so no Level 1 link joins them.
	transfer(100, 0xb1, 1, curve, w1, 40)
	transfer(100, 0xb2, 2, curve, w2, 30)
	transfer(101, 0xb3, 0, curve, w3, 20)
	transfer(101, 0xb4, 1, curve, w4, 10)
	transfer(102, 0xb5, 0, curve, w5, 5) // above the finalized head
	if _, err := database.DB.ExecContext(ctx, `SELECT rebuild_token_projections($1,$2)`, chainID, tokenBytes); err != nil {
		t.Fatal(err)
	}
	store := storepostgres.FundingStore{Pool: pool, ChainID: chainID, DeploymentID: deployment}
	if _, known, err := store.FinalizedHead(ctx); err != nil || known {
		t.Fatalf("finalized head before sync_state: known %v, err %v", known, err)
	}
	at := launchAt.Add(20 * time.Second)
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO sync_state(chain_id,deployment_id,observed_number,observed_hash,observed_at,
		safe_number,safe_hash,safe_at,finalized_number,finalized_hash,finalized_at) VALUES($1,$2,102,$3,$5,102,$3,$5,101,$4,$5)`,
		chainID, deployment, hashBytes(0xa2), hashBytes(0xa1), at); err != nil {
		t.Fatal(err)
	}
	head, known, err := store.FinalizedHead(ctx)
	if err != nil || !known || head != 101 {
		t.Fatalf("finalized head %d known %v err %v", head, known, err)
	}
	addr := func(b []byte) common.Address { return common.BytesToAddress(b) }
	waiting, err := store.AwaitingWallets(ctx, head, 20)
	if err != nil {
		t.Fatal(err)
	}
	if want := []common.Address{addr(w1), addr(w2), addr(w3), addr(w4)}; !reflect.DeepEqual(waiting, want) {
		t.Fatalf("waiting %v, want %v (oldest first, w5 not final)", waiting, want)
	}
	if first, _ := store.AwaitingWallets(ctx, head, 1); len(first) != 1 || first[0] != addr(w1) {
		t.Fatalf("limit 1: %v", first)
	}

	shared, excluded := common.HexToAddress("0x00000000000000000000000000000000000000f1"), common.HexToAddress("0x00000000000000000000000000000000000000f2")
	save := func(w []byte, r funding.Result) {
		t.Helper()
		r.CheckedBlock = head
		if err := store.Save(ctx, addr(w), r); err != nil {
			t.Fatal(err)
		}
	}
	save(w1, funding.Result{Status: funding.StatusFunded, Funder: shared, Block: 50, TxHash: common.HexToHash("0x01")})
	save(w2, funding.Result{Status: funding.StatusFunded, Funder: shared, Block: 51, TxHash: common.HexToHash("0x02")})
	save(w3, funding.Result{Status: funding.StatusFunded, Funder: excluded, Block: 52, TxHash: common.HexToHash("0x03")})
	save(w4, funding.Result{Status: funding.StatusFunded, Funder: common.BytesToAddress(curve), Block: 53, TxHash: common.HexToHash("0x04")})
	// Rows are immutable: a second save keeps the first answer.
	save(w1, funding.Result{Status: funding.StatusUnfunded})
	if waiting, err := store.AwaitingWallets(ctx, head, 20); err != nil || len(waiting) != 0 {
		t.Fatalf("waiting after saving %v, %v", waiting, err)
	}
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO funding_excluded_funders(chain_id,address,label) VALUES($1,$2,'exchange')`, chainID, excluded.Bytes()); err != nil {
		t.Fatal(err)
	}
	// w3 and w4 would link through excluded and through the curve if those counted.
	if _, err := database.DB.ExecContext(ctx, `UPDATE wallet_funding SET funder = $1 WHERE wallet = $2`, excluded.Bytes(), w4); err != nil {
		t.Fatal(err)
	}

	reader := storepostgres.MarketReader{Pool: pool, DeploymentID: deployment}
	got, err := reader.ReadEarlyBuyers(ctx, earlybuyer.Query{ChainID: chainID, Token: addr(tokenBytes)})
	if err != nil {
		t.Fatal(err)
	}
	if got.EarlyWalletCount != 5 || got.FundingChecked != 4 || len(got.Clusters) != 1 {
		t.Fatalf("early %d checked %d clusters %+v", got.EarlyWalletCount, got.FundingChecked, got.Clusters)
	}
	c := got.Clusters[0]
	if c.WalletCount != 2 || c.Wallets[0].Address != addr(w1) || c.Wallets[1].Address != addr(w2) ||
		!reflect.DeepEqual(c.Links, []string{earlybuyer.LinkFundingSource}) {
		t.Fatalf("cluster %+v", c)
	}

	// A curve funder never links either.
	if _, err := database.DB.ExecContext(ctx, `DELETE FROM funding_excluded_funders`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.ExecContext(ctx, `UPDATE wallet_funding SET funder = $1 WHERE wallet = ANY($2::bytea[])`, curve, [][]byte{w3, w4}); err != nil {
		t.Fatal(err)
	}
	if got, err := reader.ReadEarlyBuyers(ctx, earlybuyer.Query{ChainID: chainID, Token: addr(tokenBytes)}); err != nil || len(got.Clusters) != 1 {
		t.Fatalf("curve funder: %+v, %v", got.Clusters, err)
	}
}
