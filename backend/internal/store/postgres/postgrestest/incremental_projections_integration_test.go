//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

func TestIncrementalProjectionsMatchRebuildAcrossChunkSplits(t *testing.T) {
	patterns := [][]int{{1, 1, 1, 1}, {2, 2}, {1, 2, 1}, {4}}
	for _, pattern := range patterns {
		t.Run(fmt.Sprint(pattern), func(t *testing.T) { runProjectionDifferential(t, pattern) })
	}
}

type projectionSnapshot struct{ tokens, reserves, holders, candles string }

func runProjectionDifferential(t *testing.T, pattern []int) {
	t.Helper()
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	fixture := newProjectionDifferentialFixture()

	nextBlock := 0
	for _, chunkSize := range pattern {
		if nextBlock+chunkSize > len(fixture.chunks) {
			t.Fatalf("invalid chunk pattern %v", pattern)
		}
		if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
			for _, chunk := range fixture.chunks[nextBlock : nextBlock+chunkSize] {
				if _, err := adapter.UpsertIndexedBlock(ctx, chunk.block); err != nil {
					return err
				}
				for _, ingest := range chunk.events {
					if _, err := ingest(ctx, adapter); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("apply chunks %d..%d: %v", nextBlock, nextBlock+chunkSize, err)
		}
		nextBlock += chunkSize

		incremental := readProjectionSnapshot(t, ctx, database, fixture.chainID, fixture.token)
		generation := readDirtyGeneration(t, ctx, database, fixture.chainID, fixture.token)
		adapter := storepostgres.NewAdapter(pool)
		if err := adapter.RebuildTokenProjections(ctx, fixture.chainID, fixture.token); err != nil {
			t.Fatalf("rebuild after chunk %d: %v", nextBlock, err)
		}
		rebuilt := readProjectionSnapshot(t, ctx, database, fixture.chainID, fixture.token)
		if rebuiltGeneration := readDirtyGeneration(t, ctx, database, fixture.chainID, fixture.token); rebuiltGeneration <= generation {
			t.Fatalf("rebuild did not advance dirty generation: %d -> %d", generation, rebuiltGeneration)
		}
		if !reflect.DeepEqual(incremental, rebuilt) {
			t.Fatalf("incremental snapshot after chunk %d differs from rebuild\nincremental=%+v\nrebuilt=%+v", nextBlock, incremental, rebuilt)
		}
	}
	if nextBlock != len(fixture.chunks) {
		t.Fatalf("pattern %v applied %d chunks, want %d", pattern, nextBlock, len(fixture.chunks))
	}

	beforeReplay := readProjectionSnapshot(t, ctx, database, fixture.chainID, fixture.token)
	generationBeforeReplay := readDirtyGeneration(t, ctx, database, fixture.chainID, fixture.token)
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
		for _, chunk := range fixture.chunks {
			for _, ingest := range chunk.events {
				if _, err := ingest(ctx, adapter); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("replay canonical events: %v", err)
	}
	if afterReplay := readProjectionSnapshot(t, ctx, database, fixture.chainID, fixture.token); !reflect.DeepEqual(beforeReplay, afterReplay) {
		t.Fatalf("idempotent replay changed projections\nbefore=%+v\nafter=%+v", beforeReplay, afterReplay)
	}
	if got := readDirtyGeneration(t, ctx, database, fixture.chainID, fixture.token); got != generationBeforeReplay {
		t.Fatalf("replay changed dirty generation: %d -> %d", generationBeforeReplay, got)
	}
}

type projectionChunk struct {
	block  ledger.IndexedBlock
	events []func(context.Context, *storepostgres.Adapter) (ledger.InsertResult, error)
}
type projectionDifferentialFixture struct {
	chainID int64
	token   common.Address
	chunks  []projectionChunk
}

func newProjectionDifferentialFixture() projectionDifferentialFixture {
	const chainID int64 = 49011
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	address := func(last byte) common.Address { return common.Address{19: last} }
	amount := func(value int64) *big.Int { return big.NewInt(value) }
	token, curve, pair, weth, creator, treasury := address(1), address(2), address(3), address(4), address(5), address(6)
	coordinates := func(block int64, tx, log int32, at time.Time) ledger.EventCoordinates {
		return ledger.EventCoordinates{ChainID: chainID, BlockNumber: block, BlockHash: common.Hash{31: byte(block)}, BlockTime: at, TransactionIndex: tx, TxHash: common.Hash{30: byte(tx)}, LogIndex: log}
	}
	block := func(number int64, at time.Time) ledger.IndexedBlock {
		return ledger.IndexedBlock{ChainID: chainID, BlockNumber: number, BlockHash: common.Hash{31: byte(number)}, ParentHash: common.Hash{31: byte(number - 1)}, BlockTime: at, FinalityStatus: "observed"}
	}
	transferBeforeLaunch := ledger.Transfer{EventCoordinates: coordinates(1, 1, 0, base), Token: token, To: curve, Value: amount(100)}
	launch := ledger.TokenLaunch{EventCoordinates: coordinates(1, 1, 1, base), Token: token, Curve: curve, Creator: creator, LPPair: pair, WETH: weth, ProtocolTreasury: treasury, EngineVersion: 1, Name: "Pons", Symbol: "PONS", TotalSupply: amount(100), VirtualETH: amount(10), VirtualToken: amount(100), CurveTokens: amount(80), LPTokens: amount(20), GraduationETH: amount(20), LaunchFeePaid: amount(1), TradeFeeBPS: 100, ProtocolShareBPS: 5000}
	tradeOne := ledger.Trade{EventCoordinates: coordinates(2, 2, 0, base.Add(10*time.Second)), Token: token, Trader: address(7), IsBuy: true, ETHGross: amount(10), ETHRefund: amount(0), TokenAmount: amount(10), ProtocolFee: amount(1), CreatorFee: amount(1), NewETHReserve: amount(20), NewTokenReserve: amount(90)}
	transferToHolder := ledger.Transfer{EventCoordinates: coordinates(2, 2, 1, base.Add(10*time.Second)), Token: token, From: curve, To: address(8), Value: amount(100)}
	tradeTwo := ledger.Trade{EventCoordinates: coordinates(3, 3, 0, base.Add(20*time.Second)), Token: token, Trader: address(7), IsBuy: true, ETHGross: amount(11), ETHRefund: amount(0), TokenAmount: amount(11), ProtocolFee: amount(1), CreatorFee: amount(1), NewETHReserve: amount(31), NewTokenReserve: amount(79)}
	transferReacquire := ledger.Transfer{EventCoordinates: coordinates(3, 3, 1, base.Add(20*time.Second)), Token: token, To: address(8), Value: amount(5)}
	// On chain, pair.mint emits the opening Sync before the curve emits Graduated.
	openingSync := ledger.PoolSync{EventCoordinates: coordinates(3, 3, 2, base.Add(20*time.Second)), Pair: pair, Reserve0: amount(20), Reserve1: amount(20)}
	graduation := ledger.Graduation{EventCoordinates: coordinates(3, 3, 3, base.Add(20*time.Second)), Token: token, LPPair: pair, ETHToPool: amount(20), TokensToPool: amount(20), LPLiquidityBurned: amount(1)}
	syncOne := ledger.PoolSync{EventCoordinates: coordinates(4, 4, 0, base.Add(time.Minute)), Pair: pair, Reserve0: amount(20), Reserve1: amount(31)}
	swapOne := ledger.PoolSwap{EventCoordinates: coordinates(4, 4, 1, base.Add(time.Minute)), Pair: pair, Sender: address(9), Amount0In: amount(1), Amount1In: amount(2), Amount0Out: amount(3), Amount1Out: amount(4), To: address(10)}
	syncTwo := ledger.PoolSync{EventCoordinates: coordinates(4, 4, 2, base.Add(time.Minute)), Pair: pair, Reserve0: amount(30), Reserve1: amount(40)}
	swapTwo := ledger.PoolSwap{EventCoordinates: coordinates(4, 4, 3, base.Add(time.Minute)), Pair: pair, Sender: address(9), Amount0In: amount(2), Amount1In: amount(3), Amount0Out: amount(4), Amount1Out: amount(5), To: address(10)}
	fixture := projectionDifferentialFixture{chainID: chainID, token: token, chunks: []projectionChunk{
		{block: block(1, base), events: []func(context.Context, *storepostgres.Adapter) (ledger.InsertResult, error){func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestTransfer(ctx, transferBeforeLaunch)
		}, func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestTokenLaunch(ctx, launch)
		}}},
		{block: block(2, base.Add(10*time.Second)), events: []func(context.Context, *storepostgres.Adapter) (ledger.InsertResult, error){func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestTrade(ctx, tradeOne)
		}, func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestTransfer(ctx, transferToHolder)
		}}},
		{block: block(3, base.Add(20*time.Second)), events: []func(context.Context, *storepostgres.Adapter) (ledger.InsertResult, error){func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestTrade(ctx, tradeTwo)
		}, func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestTransfer(ctx, transferReacquire)
		}, func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestPoolSync(ctx, openingSync)
		}, func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestGraduation(ctx, graduation)
		}}},
		{block: block(4, base.Add(time.Minute)), events: []func(context.Context, *storepostgres.Adapter) (ledger.InsertResult, error){func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestPoolSync(ctx, syncOne)
		}, func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestPoolSwap(ctx, swapOne)
		}, func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestPoolSync(ctx, syncTwo)
		}, func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestPoolSwap(ctx, swapTwo)
		}}},
	}}
	// Preserve intermediate zero crossings, self-transfers, burns, and zero rows.
	for i, transfer := range []ledger.Transfer{
		{Token: token, From: address(8), To: address(8), Value: amount(100)},
		{Token: token, From: address(8), Value: amount(100)},
		{Token: token, From: address(11), To: address(12), Value: amount(0)},
	} {
		transfer.EventCoordinates = coordinates(2, 2, int32(i+2), base.Add(10*time.Second))
		fixture.chunks[1].events = append(fixture.chunks[1].events, func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
			return a.IngestTransfer(ctx, transfer)
		})
	}
	zeroFill := tradeOne
	zeroFill.LogIndex = 5
	zeroFill.TokenAmount = amount(0)
	fixture.chunks[1].events = append(fixture.chunks[1].events, func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
		return a.IngestTrade(ctx, zeroFill)
	})
	unpaired := swapOne
	unpaired.EventCoordinates = coordinates(3, 5, 0, base.Add(20*time.Second))
	fixture.chunks[2].events = append(fixture.chunks[2].events, func(ctx context.Context, a *storepostgres.Adapter) (ledger.InsertResult, error) {
		return a.IngestPoolSwap(ctx, unpaired)
	})
	return fixture
}

func readDirtyGeneration(t testing.TB, ctx context.Context, database *Database, chainID int64, token common.Address) int64 {
	t.Helper()
	var generation int64
	if err := database.DB.QueryRowContext(ctx, `SELECT generation FROM aggregation_dirty WHERE chain_id=$1 AND token_address=$2`, chainID, token[:]).Scan(&generation); err != nil || generation <= 0 {
		t.Fatalf("read dirty generation: %d, %v", generation, err)
	}
	return generation
}

func TestIncrementalCandleBucketEdgesMatchRebuild(t *testing.T) {
	database := NewMigrated(t)
	ctx := t.Context()
	pool := openPool(t, ctx, database.URL)
	fixture := newProjectionDifferentialFixture()
	launch := fixture.chunks[0]
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, a *storepostgres.Adapter) error {
		if _, err := a.UpsertIndexedBlock(ctx, launch.block); err != nil {
			return err
		}
		for _, ingest := range launch.events {
			if _, err := ingest(ctx, a); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var number int64 = 1
	for _, interval := range []time.Duration{time.Minute, 5 * time.Minute, time.Hour, 24 * time.Hour} {
		for _, offset := range []time.Duration{-time.Microsecond, 0, time.Microsecond} {
			number++
			at := launch.block.BlockTime.Add(interval + offset)
			block := ledger.IndexedBlock{ChainID: fixture.chainID, BlockNumber: number, BlockHash: common.Hash{31: byte(number)}, ParentHash: common.Hash{31: byte(number - 1)}, BlockTime: at, FinalityStatus: "observed"}
			trade := ledger.Trade{EventCoordinates: ledger.EventCoordinates{ChainID: fixture.chainID, BlockNumber: number, BlockHash: block.BlockHash, BlockTime: at, TxHash: common.Hash{31: byte(number)}}, Token: fixture.token, Trader: common.Address{19: 7}, IsBuy: true, ETHGross: big.NewInt(number), ETHRefund: big.NewInt(0), TokenAmount: big.NewInt(1), ProtocolFee: big.NewInt(0), CreatorFee: big.NewInt(0), NewETHReserve: big.NewInt(100), NewTokenReserve: big.NewInt(90)}
			if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, a *storepostgres.Adapter) error {
				if _, err := a.UpsertIndexedBlock(ctx, block); err != nil {
					return err
				}
				_, err := a.IngestTrade(ctx, trade)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			before := readProjectionSnapshot(t, ctx, database, fixture.chainID, fixture.token)
			if err := storepostgres.NewAdapter(pool).RebuildTokenProjections(ctx, fixture.chainID, fixture.token); err != nil {
				t.Fatal(err)
			}
			if after := readProjectionSnapshot(t, ctx, database, fixture.chainID, fixture.token); before != after {
				t.Fatalf("bucket edge %s %s differs: before=%+v after=%+v", interval, offset, before, after)
			}
		}
	}
}

func TestIncrementalNegativeBalanceAbortsEvent(t *testing.T) {
	database := NewMigrated(t)
	ctx := t.Context()
	pool := openPool(t, ctx, database.URL)
	fixture := newProjectionDifferentialFixture()
	chunk := fixture.chunks[0]
	err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, a *storepostgres.Adapter) error {
		if _, err := a.UpsertIndexedBlock(ctx, chunk.block); err != nil {
			return err
		}
		for _, ingest := range chunk.events {
			if _, err := ingest(ctx, a); err != nil {
				return err
			}
		}
		_, err := a.IngestTransfer(ctx, ledger.Transfer{EventCoordinates: ledger.EventCoordinates{ChainID: fixture.chainID, BlockNumber: 1, BlockHash: chunk.block.BlockHash, BlockTime: chunk.block.BlockTime, TxHash: common.Hash{31: 99}, LogIndex: 2}, Token: fixture.token, From: common.Address{19: 2}, To: common.Address{19: 8}, Value: big.NewInt(101)})
		return err
	})
	var negative *storepostgres.NegativeBalanceError
	if !errors.As(err, &negative) {
		t.Fatalf("overspend error=%v, want NegativeBalanceError", err)
	}
	var count int
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM transfers WHERE chain_id=$1`, fixture.chainID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback transfers=%d: %v", count, err)
	}
}

func readProjectionSnapshot(t testing.TB, ctx context.Context, database *Database, chainID int64, token common.Address) projectionSnapshot {
	t.Helper()
	read := func(query string) string {
		var value string
		if err := database.DB.QueryRowContext(ctx, query, chainID, token[:]).Scan(&value); err != nil {
			t.Fatalf("read projection snapshot: %v", err)
		}
		return value
	}
	return projectionSnapshot{
		tokens:   read(`SELECT coalesce(jsonb_agg(to_jsonb(row) ORDER BY row.token_address)::text,'[]') FROM (SELECT * FROM tokens WHERE chain_id=$1 AND token_address=$2) AS row`),
		reserves: read(`SELECT coalesce(jsonb_agg(to_jsonb(row) ORDER BY row.token_address)::text,'[]') FROM (SELECT * FROM token_reserves WHERE chain_id=$1 AND token_address=$2) AS row`),
		holders:  read(`SELECT coalesce(jsonb_agg(to_jsonb(row) ORDER BY row.holder_address)::text,'[]') FROM (SELECT * FROM holder_balances WHERE chain_id=$1 AND token_address=$2) AS row`),
		candles:  read(`SELECT coalesce(jsonb_agg(to_jsonb(row) ORDER BY row.interval,row.bucket_start_time)::text,'[]') FROM (SELECT * FROM candles WHERE chain_id=$1 AND token_address=$2) AS row`),
	}
}
