//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"testing"
	"time"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

func TestProfileReaderAggregatesActionsAcrossLinkedWallets(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URLAs(t, APIRole))

	const chainID int64 = 46630
	const deployment = "profile-read-test"
	at := time.Date(2026, 9, 9, 8, 0, 0, 0, time.UTC)
	blockHash := hashBytes(0x71)
	mustInsertBlock(t, ctx, database.DB, chainID, 100, blockHash, hashBytes(0x70), at, "safe")

	tokenWithBothActions := addressBytes(0x10)
	tokenWithRefundOnly := addressBytes(0x20)
	insertProjectionLaunch(t, ctx, database.DB, chainID, 100, blockHash, at, hashBytes(0x11), projectionLaunchFixture{token: tokenWithBothActions, curve: addressBytes(0x30), pair: addressBytes(0x40), weth: addressBytes(0x50)})
	insertProjectionLaunch(t, ctx, database.DB, chainID, 100, blockHash, at, hashBytes(0x21), projectionLaunchFixture{token: tokenWithRefundOnly, curve: addressBytes(0x31), pair: addressBytes(0x41), weth: addressBytes(0x51)})
	insertProjectionTrade(t, ctx, database.DB, chainID, 100, blockHash, at, hashBytes(0x12), 0, tokenWithBothActions, 10, 20, 900000)
	insertProjectionTrade(t, ctx, database.DB, chainID, 100, blockHash, at, hashBytes(0x13), 0, tokenWithBothActions, 11, 31, 899000)
	insertRefundCredit(t, ctx, database.DB, chainID, 100, blockHash, at, hashBytes(0x14), tokenWithBothActions, addressBytes(0x60), 7)
	insertRefundCredit(t, ctx, database.DB, chainID, 100, blockHash, at, hashBytes(0x15), tokenWithBothActions, addressBytes(0x60), 3)
	insertRefundCredit(t, ctx, database.DB, chainID, 100, blockHash, at, hashBytes(0x22), tokenWithRefundOnly, addressBytes(0x60), 5)
	for _, token := range [][]byte{tokenWithBothActions, tokenWithRefundOnly} {
		if _, err := database.DB.ExecContext(ctx, `SELECT rebuild_token_projections($1,$2)`, chainID, token); err != nil {
			t.Fatalf("rebuild token projection %x: %v", token, err)
		}
	}
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO sync_state(chain_id, deployment_id, observed_number, observed_hash, observed_at, safe_number, safe_hash, safe_at)
		VALUES ($1, $2, 100, $3, $4, 100, $3, $4)
	`, chainID, deployment, blockHash, at); err != nil {
		t.Fatal(err)
	}

	reader := storepostgres.ProfileReader{Pool: pool, DeploymentID: deployment}
	creator := common.BytesToAddress(addressBytes(0x55))
	refundWallet := common.BytesToAddress(addressBytes(0x60))
	page, err := reader.List(ctx, chainID, []common.Address{creator, refundWallet, creator, refundWallet})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("profile action count = %d, want 2: %+v", len(page.Items), page.Items)
	}
	if page.Items[0].Token != common.BytesToAddress(tokenWithBothActions) || page.Items[1].Token != common.BytesToAddress(tokenWithRefundOnly) {
		t.Fatalf("profile action order = %v, want token addresses ascending", []common.Address{page.Items[0].Token, page.Items[1].Token})
	}
	if page.Items[0].CreatorFees.String() != "2" || page.Items[0].Refund.String() != "10" {
		t.Fatalf("both-actions token values = creator fees %s, refund %s; want 2, 10", page.Items[0].CreatorFees, page.Items[0].Refund)
	}
	if page.Items[1].CreatorFees.Sign() != 0 || page.Items[1].Refund.String() != "5" {
		t.Fatalf("refund-only token values = creator fees %s, refund %s; want 0, 5", page.Items[1].CreatorFees, page.Items[1].Refund)
	}

	repeated, err := reader.List(ctx, chainID, []common.Address{creator, refundWallet})
	if err != nil {
		t.Fatal(err)
	}
	if len(repeated.Items) != len(page.Items) || repeated.Items[0].Token != page.Items[0].Token || repeated.Items[1].Token != page.Items[1].Token {
		t.Fatalf("profile action ordering is not deterministic: first=%v repeated=%v", page.Items, repeated.Items)
	}
}

func insertRefundCredit(t testing.TB, ctx context.Context, database *sql.DB, chainID, blockNumber int64, blockHash []byte, blockTime time.Time, txHash []byte, token, account []byte, amount int64) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
		INSERT INTO refund_credits (
			chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
			token_address, account, amount
		) VALUES ($1, $2, $3, $4, 0, $5, 0, $6, $7, $8)
	`, chainID, blockNumber, blockHash, blockTime, txHash, token, account, amount)
	if err != nil {
		t.Fatalf("insert refund credit: %v", err)
	}
}
