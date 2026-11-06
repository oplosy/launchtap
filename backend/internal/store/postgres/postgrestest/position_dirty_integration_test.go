//go:build integration

package postgrestest

import (
	"context"
	"encoding/hex"
	"math/big"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

type dirtyRow struct {
	wallet, token string
	generation    int64
}

func (f walletSchemaFixture) dirty(t *testing.T, ctx context.Context) map[string]dirtyRow {
	t.Helper()
	rows, err := f.database.DB.QueryContext(ctx, `SELECT wallet, token_address, generation FROM position_dirty WHERE chain_id = $1`, walletChainID)
	if err != nil {
		t.Fatalf("read position_dirty: %v", err)
	}
	defer func() { _ = rows.Close() }()
	result := map[string]dirtyRow{}
	for rows.Next() {
		var wallet, token []byte
		var row dirtyRow
		if err := rows.Scan(&wallet, &token, &row.generation); err != nil {
			t.Fatal(err)
		}
		row.wallet, row.token = hex.EncodeToString(wallet), hex.EncodeToString(token)
		result[row.wallet+"/"+row.token] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func keys(m map[string]dirtyRow) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func pairKey(wallet byte, token byte) string {
	return hex.EncodeToString(addressBytes(wallet)) + "/" + hex.EncodeToString(addressBytes(token))
}

func (f walletSchemaFixture) coordinates(txByte byte, logIndex int32) ledger.EventCoordinates {
	return ledger.EventCoordinates{ChainID: walletChainID, BlockNumber: 10, BlockHash: common.BytesToHash(f.hash), BlockTime: f.at, TxHash: common.BytesToHash(hashBytes(txByte)), LogIndex: logIndex}
}

func TestIngestMarksPositionPairs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	token := common.BytesToAddress(addressBytes(0x10))
	curve := common.BytesToAddress(addressBytes(0x41))
	trader := common.BytesToAddress(addressBytes(0xc1))
	holder := common.BytesToAddress(addressBytes(0xc2))
	swapper := common.BytesToAddress(addressBytes(0xc3))
	factory := common.BytesToAddress(addressBytes(0xc4))

	within := func(fn func(*storepostgres.Adapter) error) {
		t.Helper()
		if err := storepostgres.WithinTx(ctx, f.pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
			return fn(adapter.WithFactory(factory))
		}); err != nil {
			t.Fatal(err)
		}
	}
	trade := func(txByte byte, who common.Address) ledger.Trade {
		return ledger.Trade{EventCoordinates: f.coordinates(txByte, 1), Token: token, Trader: who, IsBuy: true,
			ETHGross: big.NewInt(10), ETHRefund: big.NewInt(0), TokenAmount: big.NewInt(5), ProtocolFee: big.NewInt(1), CreatorFee: big.NewInt(1),
			NewETHReserve: big.NewInt(11), NewTokenReserve: big.NewInt(99)}
	}

	// A curve trade marks (trader, token); the factory as trader is excluded.
	within(func(a *storepostgres.Adapter) error { _, err := a.IngestTrade(ctx, trade(0x71, trader)); return err })
	within(func(a *storepostgres.Adapter) error { _, err := a.IngestTrade(ctx, trade(0x72, factory)); return err })
	if got, want := keys(f.dirty(t, ctx)), []string{pairKey(0xc1, 0x10)}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("after trade dirty = %v, want %v", got, want)
	}

	// Mint zero->curve marks nothing; curve->wallet marks only the wallet.
	within(func(a *storepostgres.Adapter) error {
		_, err := a.IngestTransfer(ctx, ledger.Transfer{EventCoordinates: f.coordinates(0x73, 2), Token: token, From: common.Address{}, To: curve, Value: big.NewInt(1000)})
		return err
	})
	within(func(a *storepostgres.Adapter) error {
		_, err := a.IngestTransfer(ctx, ledger.Transfer{EventCoordinates: f.coordinates(0x74, 3), Token: token, From: curve, To: holder, Value: big.NewInt(5)})
		return err
	})
	got := f.dirty(t, ctx)
	if len(got) != 2 || got[pairKey(0xc1, 0x10)].wallet == "" || got[pairKey(0xc2, 0x10)].wallet == "" {
		t.Fatalf("after transfer dirty = %v, want trader and holder only", keys(got))
	}

	// A pool swap marks (tx_from, token); a NULL sender is a no-op.
	within(func(a *storepostgres.Adapter) error { _, err := a.IngestPoolSwap(ctx, f.swap(0x75, nil)); return err })
	if n := len(f.dirty(t, ctx)); n != 2 {
		t.Fatalf("NULL tx_from marked a pair: %d rows", n)
	}
	within(func(a *storepostgres.Adapter) error {
		_, err := a.IngestPoolSwap(ctx, f.swap(0x76, &swapper))
		return err
	})
	got = f.dirty(t, ctx)
	if len(got) != 3 || got[pairKey(0xc3, 0x10)].wallet == "" {
		t.Fatalf("after swap dirty = %v, want swapper added", keys(got))
	}
}

func TestReorgRemarksPositionPairs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	token := addressBytes(0x10)
	mustInsertBlock(t, ctx, f.database.DB, walletChainID, 11, hashBytes(0x22), f.hash, f.at.Add(time.Minute), "observed")
	// The fixture token is graduated, so the reorged branch carries a pool swap with a known sender.
	insertPoolSwap(t, ctx, f.database.DB, walletChainID, 11, hashBytes(0x22), f.at.Add(time.Minute), hashBytes(0x77), 0, f.pair.Bytes(), 0, 100, 20, 0)
	if _, err := f.database.DB.ExecContext(ctx, `UPDATE pool_swaps SET tx_from = $1 WHERE chain_id = $2 AND tx_hash = $3`, addressBytes(0x91), walletChainID, hashBytes(0x77)); err != nil {
		t.Fatal(err)
	}

	trader := common.BytesToAddress(addressBytes(0x91))
	if err := storepostgres.WithinTx(ctx, f.pool, func(ctx context.Context, a *storepostgres.Adapter) error {
		return a.MarkPositionsDirty(ctx, walletChainID, []indexer.PositionPair{{Wallet: trader, Token: common.BytesToAddress(token)}})
	}); err != nil {
		t.Fatal(err)
	}
	before := f.dirty(t, ctx)[pairKey(0x91, 0x10)]
	if before.wallet == "" {
		t.Fatal("seed mark missing")
	}

	if err := storepostgres.WithinTx(ctx, f.pool, func(ctx context.Context, a *storepostgres.Adapter) error {
		pairs, err := a.AffectedPositionPairsAbove(ctx, walletChainID, 10)
		if err != nil {
			return err
		}
		if len(pairs) != 1 || pairs[0].Wallet != trader {
			t.Fatalf("pairs = %v, want only the trader", pairs)
		}
		if err := a.DeleteCanonicalAbove(ctx, walletChainID, 10); err != nil {
			return err
		}
		return a.MarkPositionsDirty(ctx, walletChainID, pairs)
	}); err != nil {
		t.Fatal(err)
	}
	after := f.dirty(t, ctx)[pairKey(0x91, 0x10)]
	if after.generation <= before.generation {
		t.Fatalf("generation %d not higher than %d", after.generation, before.generation)
	}
}

func TestReorgRemarksPairsOfDeletedToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	token, curve, pair, weth := addressBytes(0x12), addressBytes(0x42), addressBytes(0x32), addressBytes(0x90)
	at := f.at.Add(time.Minute)
	mustInsertBlock(t, ctx, f.database.DB, walletChainID, 11, hashBytes(0x22), f.hash, at, "observed")
	insertProjectionLaunch(t, ctx, f.database.DB, walletChainID, 11, hashBytes(0x22), at, hashBytes(0x78), projectionLaunchFixture{token: token, curve: curve, pair: pair, weth: weth})
	insertProjectionTrade(t, ctx, f.database.DB, walletChainID, 11, hashBytes(0x22), at, hashBytes(0x79), 0, token, 10, 1, 99)
	insertTransfer(t, ctx, f.database.DB, walletChainID, 11, hashBytes(0x22), at, hashBytes(0x7c), 6, token, addressBytes(0), curve, 1000)
	insertTransfer(t, ctx, f.database.DB, walletChainID, 11, hashBytes(0x22), at, hashBytes(0x7a), 7, token, curve, addressBytes(0xd1), 5)
	insertPoolSwap(t, ctx, f.database.DB, walletChainID, 11, hashBytes(0x22), at, hashBytes(0x7b), 2, pair, 0, 100, 20, 0)
	if _, err := f.database.DB.ExecContext(ctx, `UPDATE pool_swaps SET tx_from = $1 WHERE chain_id = $2 AND tx_hash = $3`, addressBytes(0xd2), walletChainID, hashBytes(0x7b)); err != nil {
		t.Fatal(err)
	}
	callRebuild(t, ctx, f.database.DB, walletChainID, token)

	if err := storepostgres.WithinTx(ctx, f.pool, func(ctx context.Context, a *storepostgres.Adapter) error {
		pairs, err := a.AffectedPositionPairsAbove(ctx, walletChainID, 10)
		if err != nil {
			return err
		}
		if err := a.DeleteCanonicalAbove(ctx, walletChainID, 10); err != nil {
			return err
		}
		if err := a.RebuildTokenProjections(ctx, walletChainID, common.BytesToAddress(token)); err != nil {
			return err
		}
		return a.MarkPositionsDirty(ctx, walletChainID, pairs)
	}); err != nil {
		t.Fatal(err)
	}
	var tokenRows int
	if err := f.database.DB.QueryRowContext(ctx, `SELECT count(*) FROM tokens WHERE chain_id = $1 AND token_address = $2`, walletChainID, token).Scan(&tokenRows); err != nil || tokenRows != 0 {
		t.Fatalf("token row count = %d, %v; want 0", tokenRows, err)
	}
	want := []string{pairKey(0x91, 0x12), pairKey(0xd1, 0x12), pairKey(0xd2, 0x12)}
	sort.Strings(want)
	got := keys(f.dirty(t, ctx))
	if len(got) != len(want) {
		t.Fatalf("dirty = %v, want %v (curve/pair must be excluded)", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("dirty = %v, want %v", got, want)
		}
	}
}

// The launch mint Transfer(0 -> curve) is emitted by the token constructor, before TokenLaunched,
// so it is ingested while the token row does not exist yet (R4).
func TestLaunchMintDoesNotLeaveCurveDirty(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	database := NewMigrated(t)
	pool := openPool(t, ctx, database.URL)
	at := time.Date(2026, time.November, 6, 9, 0, 0, 0, time.UTC)
	hash := hashBytes(0x21)
	mustInsertBlock(t, ctx, database.DB, walletChainID, 10, hash, hashBytes(0x20), at, "safe")
	token, curve, pair := common.BytesToAddress(addressBytes(0x10)), common.BytesToAddress(addressBytes(0x41)), common.BytesToAddress(addressBytes(0x31))
	coords := func(txByte byte, logIndex int32) ledger.EventCoordinates {
		return ledger.EventCoordinates{ChainID: walletChainID, BlockNumber: 10, BlockHash: common.BytesToHash(hash), BlockTime: at, TxHash: common.BytesToHash(hashBytes(txByte)), LogIndex: logIndex}
	}
	mint := ledger.Transfer{EventCoordinates: coords(0x50, 0), Token: token, From: common.Address{}, To: curve, Value: big.NewInt(1000)}
	// A pair-bound transfer can also precede the launch row in the same block.
	toPair := ledger.Transfer{EventCoordinates: coords(0x50, 1), Token: token, From: common.Address{}, To: pair, Value: big.NewInt(1)}
	launch := ledger.TokenLaunch{EventCoordinates: coords(0x50, 2), Token: token, Curve: curve, Creator: common.BytesToAddress(addressBytes(0x55)), LPPair: pair, WETH: common.BytesToAddress(addressBytes(0x90)), ProtocolTreasury: common.BytesToAddress(addressBytes(0x56)), EngineVersion: 1, Name: "Token", Symbol: "TKN", TotalSupply: big.NewInt(1000000), VirtualETH: big.NewInt(10), VirtualToken: big.NewInt(1000000), CurveTokens: big.NewInt(800000), LPTokens: big.NewInt(200000), GraduationETH: big.NewInt(100), LaunchFeePaid: big.NewInt(1), TradeFeeBPS: 100, ProtocolShareBPS: 50}
	factory := common.BytesToAddress(addressBytes(0xc4))
	// One transaction, in log order, like the ledger router: the transfers' launch FK is deferred.
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
		adapter = adapter.WithFactory(factory)
		if _, err := adapter.IngestTransfer(ctx, mint); err != nil {
			return err
		}
		if _, err := adapter.IngestTransfer(ctx, toPair); err != nil {
			return err
		}
		_, err := adapter.IngestTokenLaunch(ctx, launch)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := database.DB.QueryContext(ctx, `SELECT wallet, token_address FROM position_dirty WHERE chain_id = $1`, walletChainID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var wallet, tokenAddress []byte
		if err := rows.Scan(&wallet, &tokenAddress); err != nil {
			t.Fatal(err)
		}
		t.Errorf("position_dirty holds %x/%x after launch, want none (curve and pair are protocol addresses)", wallet, tokenAddress)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestMarkPositionsDirtyDedupesPairs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	pair := indexer.PositionPair{Wallet: common.BytesToAddress(addressBytes(0x91)), Token: common.BytesToAddress(addressBytes(0x10))}
	if err := storepostgres.WithinTx(ctx, f.pool, func(ctx context.Context, a *storepostgres.Adapter) error {
		return a.MarkPositionsDirty(ctx, walletChainID, []indexer.PositionPair{pair, pair})
	}); err != nil {
		t.Fatalf("duplicate pairs: %v", err)
	}
	if got := keys(f.dirty(t, ctx)); len(got) != 1 {
		t.Fatalf("dirty = %v, want one row", got)
	}
}

func TestPositionExclusionLookupsUseIndexes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	tx, err := f.database.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"curve_address", "lp_pair"} {
		var plan string
		rows, err := tx.QueryContext(ctx, `EXPLAIN SELECT 1 FROM tokens WHERE chain_id = 48200 AND `+column+` = '\x00'::bytea`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan += line + "\n"
		}
		_ = rows.Close()
		if !strings.Contains(plan, "Index") || strings.Contains(plan, "Seq Scan") {
			t.Errorf("%s lookup plan:\n%s", column, plan)
		}
		t.Log(plan)
	}
}
