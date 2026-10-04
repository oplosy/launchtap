//go:build integration

package postgrestest

import (
	"context"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/candle"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/pagination"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A +05:30 session zone shifts local hour and day boundaries away from UTC.
const nonUTCSessionZone = "Asia/Kolkata"

func TestAnalyticsCountEachTradeOnceAcrossCandleIntervals(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPoolInZone(t, ctx, database.URL, nonUTCSessionZone)
	fixture := newProjectionDifferentialFixture()
	ingestProjectionChunk(t, ctx, pool, fixture.chunks[0])

	now := time.Now().UTC()
	athAt := now.Add(-2 * time.Hour)
	for index, trade := range []struct {
		at    time.Time
		gross int64
	}{{athAt, 50}, {now.Add(-30 * time.Minute), 10}} {
		ingestProjectionChunk(t, ctx, pool, tradeChunk(fixture, int64(index+2), trade.at, trade.gross))
	}

	var dayStart time.Time
	if err := database.DB.QueryRowContext(ctx, `
		SELECT bucket_start_time FROM candles
		WHERE chain_id = $1 AND token_address = $2 AND interval = '1d'
		ORDER BY bucket_start_time LIMIT 1
	`, fixture.chainID, fixture.token[:]).Scan(&dayStart); err != nil {
		t.Fatalf("read daily candle: %v", err)
	}
	if want := athAt.Truncate(24 * time.Hour); !dayStart.Equal(want) {
		t.Fatalf("daily candle starts at %s under a %s session; want UTC midnight %s", dayStart.UTC(), nonUTCSessionZone, want)
	}
	before := readProjectionSnapshot(t, ctx, database, fixture.chainID, fixture.token)
	adapter := storepostgres.NewAdapter(pool)
	if err := adapter.RebuildTokenProjections(ctx, fixture.chainID, fixture.token); err != nil {
		t.Fatalf("rebuild under non-UTC session: %v", err)
	}
	if after := readProjectionSnapshot(t, ctx, database, fixture.chainID, fixture.token); before != after {
		t.Fatalf("rebuild under %s session differs from incremental candles\nincremental=%+v\nrebuilt=%+v", nonUTCSessionZone, before, after)
	}

	if err := adapter.RecomputeTokenStats(ctx, fixture.chainID, fixture.token); err != nil {
		t.Fatalf("recompute token stats: %v", err)
	}
	var volume string
	var gotATHAt time.Time
	if err := database.DB.QueryRowContext(ctx, `
		SELECT volume_24h_eth_wad::TEXT, ath_at FROM token_stats WHERE chain_id = $1 AND token_address = $2
	`, fixture.chainID, fixture.token[:]).Scan(&volume, &gotATHAt); err != nil {
		t.Fatalf("read token stats: %v", err)
	}
	if volume != "60" {
		t.Fatalf("volume_24h = %s; want 60 (each trade counted once, not once per candle interval)", volume)
	}
	if want := athAt.Truncate(time.Minute); !gotATHAt.Equal(want) {
		t.Fatalf("ath_at = %s; want the ATH trade's minute %s", gotATHAt.UTC(), want)
	}

	if err := adapter.RecomputeProtocolAggregates(ctx, fixture.chainID); err != nil {
		t.Fatalf("recompute protocol aggregates: %v", err)
	}
	var dailyTrades, trades24h, tradesAllTime int64
	if err := database.DB.QueryRowContext(ctx, `
		SELECT (SELECT COALESCE(sum(trades_count), 0) FROM protocol_daily WHERE chain_id = $1),
		       trades_24h, trades_all_time
		FROM protocol_stats WHERE chain_id = $1
	`, fixture.chainID).Scan(&dailyTrades, &trades24h, &tradesAllTime); err != nil {
		t.Fatalf("read protocol aggregates: %v", err)
	}
	if dailyTrades != 2 || trades24h != 2 || tradesAllTime != 2 {
		t.Fatalf("protocol trades daily/24h/all-time = %d/%d/%d; want 2/2/2", dailyTrades, trades24h, tradesAllTime)
	}
}

func TestAggregatedCandlesUseUTCGroupStartsAndPageWholeGroups(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPoolInZone(t, ctx, database.URL, nonUTCSessionZone)
	fixture := newProjectionDifferentialFixture()
	launch := fixture.chunks[0]
	ingestProjectionChunk(t, ctx, pool, launch)

	const deployment = "aggregated-candles-test"
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO sync_state (chain_id, deployment_id, observed_number, observed_hash, observed_at)
		VALUES ($1, $2, 1, $3, $4)
	`, fixture.chainID, deployment, launch.block.BlockHash[:], launch.block.BlockTime); err != nil {
		t.Fatalf("insert sync state: %v", err)
	}
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	// The first 6h group has no 00:00 bucket; its row must still start at 00:00.
	for _, hour := range []int{1, 3, 7} {
		if _, err := database.DB.ExecContext(ctx, `
			INSERT INTO candles (
				chain_id, token_address, interval, bucket_start_time,
				open_price_wad, high_price_wad, low_price_wad, close_price_wad, trade_count
			) VALUES ($1, $2, '1h', $3, $4, $4, $4, $4, 1)
		`, fixture.chainID, fixture.token[:], day.Add(time.Duration(hour)*time.Hour), hour); err != nil {
			t.Fatalf("insert hourly candle %02d:00: %v", hour, err)
		}
	}

	query := candle.Query{ChainID: fixture.chainID, Token: fixture.token, Interval: "6h", From: day, To: day.Add(24 * time.Hour), Limit: 1}
	first, err := storepostgres.ReadAggregatedCandles(ctx, pool, fixture.chainID, deployment, query)
	if err != nil {
		t.Fatalf("read first aggregated page: %v", err)
	}
	if len(first.Items) != 1 || !first.Items[0].Start.Equal(day) || first.Items[0].TradeCount != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %+v; want one 00:00 UTC group with 2 trades and a next cursor", first)
	}
	cursor, err := pagination.Decode(first.NextCursor)
	if err != nil {
		t.Fatalf("decode next cursor: %v", err)
	}
	query.Cursor = &cursor
	second, err := storepostgres.ReadAggregatedCandles(ctx, pool, fixture.chainID, deployment, query)
	if err != nil {
		t.Fatalf("read second aggregated page: %v", err)
	}
	if len(second.Items) != 1 || !second.Items[0].Start.Equal(day.Add(6*time.Hour)) || second.Items[0].TradeCount != 1 {
		t.Fatalf("second page = %+v; want only the 06:00 UTC group, not a partial repeat of 00:00", second)
	}
}

func openPoolInZone(t testing.TB, ctx context.Context, databaseURL, zone string) *pgxpool.Pool {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	values := parsed.Query()
	values.Set("timezone", zone)
	parsed.RawQuery = values.Encode()
	return openPool(t, ctx, parsed.String())
}

func ingestProjectionChunk(t testing.TB, ctx context.Context, pool *pgxpool.Pool, chunk projectionChunk) {
	t.Helper()
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
		if _, err := adapter.UpsertIndexedBlock(ctx, chunk.block); err != nil {
			return err
		}
		for _, ingest := range chunk.events {
			if _, err := ingest(ctx, adapter); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("ingest block %d: %v", chunk.block.BlockNumber, err)
	}
}

func tradeChunk(fixture projectionDifferentialFixture, number int64, at time.Time, gross int64) projectionChunk {
	block := ledger.IndexedBlock{ChainID: fixture.chainID, BlockNumber: number, BlockHash: common.Hash{31: byte(number)}, ParentHash: common.Hash{31: byte(number - 1)}, BlockTime: at, FinalityStatus: "observed"}
	trade := ledger.Trade{EventCoordinates: ledger.EventCoordinates{ChainID: fixture.chainID, BlockNumber: number, BlockHash: block.BlockHash, BlockTime: at, TxHash: common.Hash{31: byte(number)}}, Token: fixture.token, Trader: common.Address{19: 7}, IsBuy: true, ETHGross: big.NewInt(gross), ETHRefund: big.NewInt(0), TokenAmount: big.NewInt(1), ProtocolFee: big.NewInt(0), CreatorFee: big.NewInt(0), NewETHReserve: big.NewInt(100), NewTokenReserve: big.NewInt(90)}
	return projectionChunk{block: block, events: []func(context.Context, *storepostgres.Adapter) (ledger.InsertResult, error){func(ctx context.Context, adapter *storepostgres.Adapter) (ledger.InsertResult, error) {
		return adapter.IngestTrade(ctx, trade)
	}}}
}
