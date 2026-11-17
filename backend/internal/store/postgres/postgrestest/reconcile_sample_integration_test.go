//go:build integration

package postgrestest

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
)

// The reconciliation sample adds a rotating sample over every token to the recently traded ones,
// so a token without a token_reserves row is still compared (security-audit run-4).
func TestReconcileSampleRotatesOverTokensWithoutReserves(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	const chainID int64 = 48011
	blockTime := time.Date(2026, time.November, 17, 12, 0, 0, 0, time.UTC)
	mustInsertBlock(t, ctx, database.DB, chainID, 1, hashBytes(0xd1), hashBytes(0xd0), blockTime, "observed")
	tokens := [][]byte{addressBytes(0x61), addressBytes(0x62), addressBytes(0x63)}
	for index, token := range tokens {
		insertProjectionLaunch(t, ctx, database.DB, chainID, 1, hashBytes(0xd1), blockTime, hashBytes(byte(0xe1+index)), projectionLaunchFixture{token: token, curve: addressBytes(byte(0x71 + index)), pair: addressBytes(byte(0x81 + index)), weth: addressBytes(0x50)})
		callRebuild(t, ctx, database.DB, chainID, token)
	}
	// Only the first token has reserves: the others never traded.
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO token_reserves (
			chain_id, token_address, reserve_source, eth_reserve, token_reserve,
			source_block_number, source_block_hash, source_block_time, source_tx_hash, source_log_index
		) VALUES ($1, $2, 'curve', 10, 1000000, 1, $3, $4, $5, 0)
		ON CONFLICT (chain_id, token_address) DO NOTHING
	`, chainID, tokens[0], hashBytes(0xd1), blockTime, hashBytes(0xe1)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.ExecContext(ctx, `DELETE FROM token_reserves WHERE chain_id = $1 AND token_address <> $2`, chainID, tokens[0]); err != nil {
		t.Fatal(err)
	}
	pool := openPool(t, ctx, database.URL)
	queries := sqlc.New(pool)
	sample := func(start []byte) [][]byte {
		t.Helper()
		rows, err := queries.ListReconcileTokens(ctx, sqlc.ListReconcileTokensParams{ChainID: chainID, RowLimit: 1, RotationStart: start})
		if err != nil {
			t.Fatal(err)
		}
		var got [][]byte
		for _, row := range rows {
			got = append(got, row.TokenAddress[:])
			if !bytes.Equal(row.TokenAddress[:], tokens[0]) && row.ReserveSource != "" {
				t.Fatalf("token without reserves reported source %q", row.ReserveSource)
			}
		}
		return got
	}
	assertSample := func(name string, got [][]byte, want ...[]byte) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: sample = %x, want %x", name, got, want)
		}
		for index := range want {
			if !bytes.Equal(got[index], want[index]) {
				t.Fatalf("%s: sample = %x, want %x", name, got, want)
			}
		}
	}
	assertSample("start at the third token", sample(tokens[2]), tokens[0], tokens[2])
	assertSample("start past every token wraps", sample(addressBytes(0xff)), tokens[0])
	assertSample("start at the second token", sample(tokens[1]), tokens[0], tokens[1])
}
