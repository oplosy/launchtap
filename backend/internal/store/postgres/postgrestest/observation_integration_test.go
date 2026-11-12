//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/observation"
	"github.com/Contictus/launchtap/backend/internal/pagination"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestCanonicalObservationUnionAndScope exercises every event source in the
// HTTP transaction lookup query against PostgreSQL. It also verifies finality
// propagation, snapshot availability precedence, and canonical disappearance
// after rollback.
func TestCanonicalObservationUnionAndSnapshotAvailability(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	adapter := storepostgres.NewAdapter(pool)
	const chainID int64 = 49020
	const deploymentID = "observation-v1"
	when := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	blockHash := common.Hash{31: 0x20}
	mustInsertBlock(t, ctx, database.DB, chainID, 100, blockHash[:], hashBytes(0x19), when, "finalized")
	launchHash := common.Hash{31: 0x01}
	token, curve, creator, pair := addressBytes(1), addressBytes(2), addressBytes(3), addressBytes(4)
	coords := func(hash common.Hash, log int32, number int64, h common.Hash) ledger.EventCoordinates {
		return ledger.EventCoordinates{ChainID: chainID, BlockNumber: number, BlockHash: h, BlockTime: when, TransactionIndex: log, TxHash: hash, LogIndex: log}
	}
	launch := ledger.TokenLaunch{EventCoordinates: coords(launchHash, 1, 100, blockHash), Token: common.BytesToAddress(token), Curve: common.BytesToAddress(curve), Creator: common.BytesToAddress(creator), LPPair: common.BytesToAddress(pair), WETH: common.BytesToAddress(addressBytes(5)), ProtocolTreasury: common.BytesToAddress(addressBytes(6)), EngineVersion: 1, Name: "Observation", Symbol: "OBS", TotalSupply: big.NewInt(100), VirtualETH: big.NewInt(10), VirtualToken: big.NewInt(90), CurveTokens: big.NewInt(80), LPTokens: big.NewInt(20), GraduationETH: big.NewInt(20), LaunchFeePaid: big.NewInt(1)}
	if _, err := adapter.InsertTokenLaunch(ctx, launch); err != nil {
		t.Fatal(err)
	}
	tradeHash := common.Hash{31: 0x02}
	if _, err := adapter.InsertTrade(ctx, ledger.Trade{EventCoordinates: coords(tradeHash, 2, 100, blockHash), Token: common.BytesToAddress(token), Trader: common.BytesToAddress(addressBytes(7)), IsBuy: true, ETHGross: big.NewInt(2), ETHRefund: new(big.Int), TokenAmount: big.NewInt(3), ProtocolFee: big.NewInt(1), CreatorFee: big.NewInt(1), NewETHReserve: big.NewInt(12), NewTokenReserve: big.NewInt(87)}); err != nil {
		t.Fatal(err)
	}
	claimHash := common.Hash{31: 0x03}
	if _, err := adapter.InsertCreatorFeeClaim(ctx, ledger.CreatorFeeClaim{EventCoordinates: coords(claimHash, 3, 100, blockHash), Token: common.BytesToAddress(token), Creator: common.BytesToAddress(creator), Amount: big.NewInt(1)}); err != nil {
		t.Fatal(err)
	}
	refundHash := common.Hash{31: 0x04}
	if _, err := adapter.InsertRefundClaim(ctx, ledger.RefundClaim{EventCoordinates: coords(refundHash, 4, 100, blockHash), Token: common.BytesToAddress(token), Account: common.BytesToAddress(creator), Amount: big.NewInt(1)}); err != nil {
		t.Fatal(err)
	}
	swapHash := common.Hash{31: 0x05}
	if _, err := adapter.InsertPoolSwap(ctx, ledger.PoolSwap{EventCoordinates: coords(swapHash, 5, 100, blockHash), Pair: common.BytesToAddress(pair), Sender: common.BytesToAddress(creator), Amount0In: big.NewInt(1), Amount1In: new(big.Int), Amount0Out: new(big.Int), Amount1Out: big.NewInt(1), To: common.BytesToAddress(creator)}); err != nil {
		t.Fatal(err)
	}
	finalHash := storepostgres.Hash(blockHash)
	syncState := storepostgres.SyncState{
		ChainID: chainID, DeploymentID: deploymentID,
		ObservedNumber: pgtype.Int8{Int64: 100, Valid: true}, ObservedHash: &finalHash,
		ObservedAt: pgtype.Timestamptz{Time: when, Valid: true},
		SafeNumber: pgtype.Int8{Int64: 100, Valid: true}, SafeHash: &finalHash,
		SafeAt:          pgtype.Timestamptz{Time: when, Valid: true},
		FinalizedNumber: pgtype.Int8{Int64: 100, Valid: true}, FinalizedHash: &finalHash,
		FinalizedAt: pgtype.Timestamptz{Time: when, Valid: true},
	}
	if _, err := adapter.UpsertSyncState(ctx, syncState); err != nil {
		t.Fatal(err)
	}
	reader := storepostgres.ObservationReader{Pool: pool}
	for hash, kind := range map[common.Hash]string{launchHash: "token_launch", tradeHash: "trade", claimHash: "creator_fee_claim", refundHash: "refund_claim", swapHash: "router_swap"} {
		observation, err := reader.Get(ctx, chainID, deploymentID, hash)
		if err != nil {
			t.Fatalf("lookup %s: %v", kind, err)
		}
		if len(observation.Events) != 1 || observation.Events[0].Kind != kind || observation.Events[0].Finality != "finalized" {
			t.Fatalf("lookup %s = %+v", kind, observation.Events)
		}
	}
	if _, err := reader.Get(ctx, chainID, "other-deployment", tradeHash); !errors.Is(err, pagination.ErrSnapshotUnavailable) {
		t.Fatalf("lookup without deployment snapshot error = %v, want unavailable snapshot", err)
	}
	missingHash := common.Hash{31: 0xff}
	if _, err := reader.Get(ctx, chainID, deploymentID, missingHash); !errors.Is(err, observation.ErrNotFound) {
		t.Fatalf("lookup for absent transaction under an initialized snapshot error = %v, want not found", err)
	}
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, tx *storepostgres.Adapter) error {
		return tx.DeleteCanonicalAbove(ctx, chainID, 99)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Get(ctx, chainID, deploymentID, tradeHash); err == nil {
		t.Fatal("reorged trade remained observable")
	}
}

// TestCanonicalObservationIncludesV4Swaps covers the engine-2 pool trade: a Universal Router
// swap emits only the PoolManager Swap event, so without it the browser waits on a graduated
// v2 trade forever (Plan v2-C Task 9). The token comes from the graduation ledger row, never
// from the rebuildable pool projection.
func TestCanonicalObservationIncludesV4Swaps(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	adapter := storepostgres.NewAdapter(pool)
	const chainID int64 = 49021
	const deploymentID = "observation-v2"
	when := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	blockHash := common.Hash{31: 0x30}
	mustInsertBlock(t, ctx, database.DB, chainID, 100, blockHash[:], hashBytes(0x29), when, "safe")
	at := func(tx byte, log int) eventCoordinates {
		return eventCoordinates{chainID, 100, blockHash[:], when, log, hashBytes(tx), log}
	}
	token, poolID := addressBytes(0x31), hashBytes(0x32)
	if err := insertLedgerRow(ctx, database.DB, "token_launches", at(0x33, 0), v2Launch(token)...); err != nil {
		t.Fatal(err)
	}
	if err := insertLedgerRow(ctx, database.DB, "graduations", at(0x34, 1), v2Graduation(token, poolID)...); err != nil {
		t.Fatal(err)
	}
	if err := insertLedgerRow(ctx, database.DB, "v4_swaps", at(0x35, 2), v4Swap(poolID)...); err != nil {
		t.Fatal(err)
	}
	safeHash := storepostgres.Hash(blockHash)
	if _, err := adapter.UpsertSyncState(ctx, storepostgres.SyncState{
		ChainID: chainID, DeploymentID: deploymentID,
		ObservedNumber: pgtype.Int8{Int64: 100, Valid: true}, ObservedHash: &safeHash,
		ObservedAt: pgtype.Timestamptz{Time: when, Valid: true},
		SafeNumber: pgtype.Int8{Int64: 100, Valid: true}, SafeHash: &safeHash,
		SafeAt: pgtype.Timestamptz{Time: when, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	reader := storepostgres.ObservationReader{Pool: pool}
	got, err := reader.Get(ctx, chainID, deploymentID, common.BytesToHash(hashBytes(0x35)))
	if err != nil {
		t.Fatalf("lookup v4 swap: %v", err)
	}
	if len(got.Events) != 1 {
		t.Fatalf("v4 swap events = %+v, want one", got.Events)
	}
	event := got.Events[0]
	if event.Kind != "v4_swap" || event.Finality != "safe" || event.Token == nil ||
		*event.Token != common.BytesToAddress(token) || event.Pair != nil {
		t.Fatalf("v4 swap event = %+v", event)
	}
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, tx *storepostgres.Adapter) error {
		return tx.DeleteCanonicalAbove(ctx, chainID, 99)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Get(ctx, chainID, deploymentID, common.BytesToHash(hashBytes(0x35))); !errors.Is(err, observation.ErrNotFound) {
		t.Fatalf("reorged v4 swap lookup error = %v, want not found", err)
	}
}
