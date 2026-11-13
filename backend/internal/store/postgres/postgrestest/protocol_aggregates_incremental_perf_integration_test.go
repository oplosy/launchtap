//go:build integration

package postgrestest

import (
	"context"
	"os"
	"testing"
	"time"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
)

const (
	incrementalPerfTradeCount = 200_000
	incrementalPerfDays       = 200
	incrementalPerfSamples    = 5
)

// TestProtocolAggregatesIncrementalPerf is an opt-in PostgreSQL 18 measurement of the steady
// state: history spread over incrementalPerfDays days, then one new block per refresh. It compares
// the full rebuild with the incremental refresh and checks their outputs match. Run with
// LAUNCHPAD_AGGREGATION_PERF=1:
// go test -tags=integration ./internal/store/postgres/postgrestest -run '^TestProtocolAggregatesIncrementalPerf$' -count=1 -v
func TestProtocolAggregatesIncrementalPerf(t *testing.T) {
	if os.Getenv(aggregationPerfEnv) != "1" {
		t.Skipf("set %s=1 to run the 200k-row incremental aggregation benchmark", aggregationPerfEnv)
	}

	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	adapter := storepostgres.NewAdapter(pool)
	const chainID int64 = 48015
	now := time.Now().UTC().Truncate(time.Second)
	start := now.AddDate(0, 0, -incrementalPerfDays)

	fixture := newIncrementalFixture(t, ctx, database, chainID, start)
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO indexed_blocks (chain_id, block_number, block_hash, parent_hash, block_time, finality_status)
		SELECT $1, 1000 + value,
		       decode(lpad(to_hex(value::bigint), 64, '0'), 'hex'),
		       decode(lpad(to_hex((value - 1)::bigint), 64, '0'), 'hex'),
		       $2::timestamptz + (value * ($3::timestamptz - $2::timestamptz) / ($4::integer + 1)), 'observed'
		FROM generate_series(1, $4::integer) AS generated(value)
	`, chainID, start, now.Add(-2*time.Hour), incrementalPerfTradeCount); err != nil {
		t.Fatalf("seed blocks: %v", err)
	}
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO trades (
			chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
			token_address, trader, is_buy, eth_gross, eth_refund, token_amount,
			protocol_fee, creator_fee, new_eth_reserve, new_token_reserve
		)
		SELECT block.chain_id, block.block_number, block.block_hash, block.block_time, 0,
		       decode(lpad(to_hex((1000000 + block.block_number)::bigint), 64, '0'), 'hex'),
		       0, $2, decode('0000000000000000000000000000000000000001', 'hex'), TRUE,
		       1000000000000000, 0, 1000000000000000000, 0, 0, 10, 1000
		FROM indexed_blocks AS block
		WHERE block.chain_id = $1 AND block.block_number > 1000
	`, chainID, fixture.token); err != nil {
		t.Fatalf("seed trades: %v", err)
	}
	if _, err := database.DB.ExecContext(ctx, `ANALYZE`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	fixture.parent = hashBytes(0) // the new blocks below do not chain from the generated ones

	full := make([]time.Duration, 0, incrementalPerfSamples)
	incremental := make([]time.Duration, 0, incrementalPerfSamples)
	next := int64(1000 + incrementalPerfTradeCount + 1)
	for sample := range incrementalPerfSamples {
		started := time.Now()
		if err := adapter.RecomputeProtocolAggregates(ctx, chainID); err != nil {
			t.Fatalf("sample %d full rebuild: %v", sample, err)
		}
		full = append(full, time.Since(started))

		fixture.addTrade(next, now.Add(-time.Hour), 5)
		next++
		started = time.Now()
		if err := adapter.RefreshProtocolAggregates(ctx, chainID); err != nil {
			t.Fatalf("sample %d incremental refresh: %v", sample, err)
		}
		incremental = append(incremental, time.Since(started))
		refreshed := snapshotProtocolAggregates(t, ctx, database.DB, chainID)
		if err := adapter.RecomputeProtocolAggregates(ctx, chainID); err != nil {
			t.Fatalf("sample %d parity rebuild: %v", sample, err)
		}
		if rebuilt := snapshotProtocolAggregates(t, ctx, database.DB, chainID); rebuilt != refreshed {
			t.Fatalf("sample %d incremental output differs from full rebuild", sample)
		}
	}
	t.Logf("trades=%d days=%d full_ms=%v full_median_ms=%d incremental_ms=%v incremental_median_ms=%d output_parity=true",
		incrementalPerfTradeCount, incrementalPerfDays,
		durationMillis(full), medianDuration(full).Milliseconds(),
		durationMillis(incremental), medianDuration(incremental).Milliseconds())
}
