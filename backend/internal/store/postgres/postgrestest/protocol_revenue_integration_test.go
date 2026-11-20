//go:build integration

package postgrestest

import (
	"context"
	"testing"
	"time"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Protocol revenue sums every ETH flow credited to the protocol per engine
// (docs/specs/2026-11-12-protocol-revenue-design.md), read from the protocol_daily_revenue rollup
// the stats worker refreshes (docs/specs/2026-11-16-protocol-revenue-rollup-preflight.md).
func TestProtocolRevenueSumsEachSourcePerEngine(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	const chainID int64 = 52011
	const deployment = "protocol-revenue-test"
	reader := storepostgres.ProtocolReader{Pool: pool, DeploymentID: deployment}
	base := time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)
	mustInsertBlock(t, ctx, database.DB, chainID, 1, hashBytes(0x01), hashBytes(0x00), base, "safe")
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO sync_state(chain_id,deployment_id,observed_number,observed_hash,observed_at,safe_number,safe_hash,safe_at) VALUES($1,$2,1,$3,$4,1,$3,$4)`, chainID, deployment, hashBytes(0x01), base); err != nil {
		t.Fatal(err)
	}

	adapter := storepostgres.NewAdapter(pool)
	refresh := func() {
		t.Helper()
		if err := adapter.RefreshProtocolAggregates(ctx, chainID); err != nil {
			t.Fatalf("refresh protocol aggregates: %v", err)
		}
	}
	refresh()
	empty, err := reader.ReadProtocolRevenue(ctx, chainID)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Total.Sign() != 0 || empty.TradeFees.Total.Sign() != 0 || empty.Snapshot.BlockNumber != 1 {
		t.Fatalf("empty revenue %+v", empty)
	}

	// The events land in block 2, a day later, so the incremental refresh rebuilds only that day.
	next := base.Add(24 * time.Hour)
	mustInsertBlock(t, ctx, database.DB, chainID, 2, hashBytes(0x02), hashBytes(0x01), next, "safe")
	at := func(tx byte, log int) eventCoordinates {
		return eventCoordinates{chainID, 2, hashBytes(0x02), next, 0, hashBytes(tx), log}
	}
	insert := func(table string, coordinates eventCoordinates, payload ...column) {
		t.Helper()
		if err := insertLedgerRow(ctx, database.DB, table, coordinates, payload...); err != nil {
			t.Fatalf("insert %s: %v", table, err)
		}
	}
	trade := func(token []byte, protocolFee, snipeTax int64) []column {
		return []column{
			{"token_address", token}, {"trader", addressBytes(0xc1)}, {"is_buy", true},
			{"eth_gross", 100}, {"eth_refund", 0}, {"token_amount", 1_000}, {"protocol_fee", protocolFee},
			{"creator_fee", 1}, {"new_eth_reserve", 10}, {"new_token_reserve", 999_000}, {"snipe_tax", snipeTax},
		}
	}
	v1, v2 := addressBytes(0x31), addressBytes(0x32)
	insert("token_launches", at(0x31, 0), launchPayload(v1, 1, addressBytes(0xb3), addressBytes(0xb4))...) // launch fee 1
	insert("token_launches", at(0x32, 0), v2Launch(v2)...)                                                 // launch fee 1, LP creator share 50%
	insert("trades", at(0x33, 0), trade(v1, 4, 0)...)
	insert("trades", at(0x34, 0), trade(v2, 6, 9)...)
	insert("graduations", at(0x35, 0), v2Graduation(v2, hashBytes(0x35))...) // ETH remainder 3
	// Odd amounts: the creator gets floor(7/2)=3 and floor(11/2)=5; the protocol 4 and 6.
	insert("lp_fee_collections", at(0x36, 0), column{"token_address", v2}, column{"eth_amount", 7}, column{"token_amount", 2})
	insert("lp_fee_collections", at(0x36, 1), column{"token_address", v2}, column{"eth_amount", 11}, column{"token_amount", 0})

	if stale, err := reader.ReadProtocolRevenue(ctx, chainID); err != nil || stale.Total.Sign() != 0 {
		t.Fatalf("revenue before the refresh = %+v, %v; want the rollup unchanged", stale, err)
	}
	refresh()
	assertRevenue(t, ctx, reader, chainID, "incremental refresh")
	// A reorg's full rebuild and the migration's backfill produce the same rollup.
	if err := adapter.RecomputeProtocolAggregates(ctx, chainID); err != nil {
		t.Fatalf("full protocol aggregate rebuild: %v", err)
	}
	assertRevenue(t, ctx, reader, chainID, "full rebuild")
	// 00041 indexer engine deployments, 00040 indexer heartbeat, 00039 service roles, 00038 audit follow-ups, 00037 candle ATH index, 00036 revenue rollup.
	for range 6 {
		if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
			t.Fatalf("roll back to before the revenue rollup: %v", err)
		}
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandUp); err != nil {
		t.Fatalf("re-apply the revenue rollup migration: %v", err)
	}
	assertRevenue(t, ctx, reader, chainID, "migration backfill")
}

func assertRevenue(t *testing.T, ctx context.Context, reader storepostgres.ProtocolReader, chainID int64, stage string) {
	t.Helper()
	got, err := reader.ReadProtocolRevenue(ctx, chainID)
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, got [3]int64, want [3]int64) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: %s = %v, want %v", stage, name, got, want)
		}
	}
	for _, c := range []struct {
		name string
		src  [3]int64
		want [3]int64
	}{
		{"launch fees", [3]int64{got.LaunchFees.Engine1.Int64(), got.LaunchFees.Engine2.Int64(), got.LaunchFees.Total.Int64()}, [3]int64{1, 1, 2}},
		{"trade fees", [3]int64{got.TradeFees.Engine1.Int64(), got.TradeFees.Engine2.Int64(), got.TradeFees.Total.Int64()}, [3]int64{4, 6, 10}},
		{"snipe tax", [3]int64{got.SnipeTax.Engine1.Int64(), got.SnipeTax.Engine2.Int64(), got.SnipeTax.Total.Int64()}, [3]int64{0, 9, 9}},
		{"graduation remainder", [3]int64{got.GraduationRemainder.Engine1.Int64(), got.GraduationRemainder.Engine2.Int64(), got.GraduationRemainder.Total.Int64()}, [3]int64{0, 3, 3}},
		{"lp fees", [3]int64{got.LPFees.Engine1.Int64(), got.LPFees.Engine2.Int64(), got.LPFees.Total.Int64()}, [3]int64{0, 10, 10}},
	} {
		check(c.name, c.src, c.want)
	}
	if got.Total.Int64() != 34 || got.Finality != "safe" {
		t.Fatalf("%s: total %s finality %q", stage, got.Total, got.Finality)
	}
}
