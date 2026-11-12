//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/migrations"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Addresses of the frozen engine-2 fixture (chain/testdata/v2).
var (
	v2FixturePoolManager = common.HexToAddress("0x5c4a3C2CD1ffE6aAfDF62b64bb3E620C696c832E")
	v2FixtureLocker      = common.HexToAddress("0xc862c8F311ba99CfD883A479f966A8074956c07c")
	v2FixtureCreator     = common.HexToAddress("0x0000000000000000000000000000000000C0FFEE")
	v2FixtureRecipient   = common.HexToAddress("0x000000000000000000000000000000000000fEE2")
	v2FixtureTreasury    = common.HexToAddress("0x0000000000000000000000000000000000007000")
)

type v2ProjectionSnapshot struct{ tokens, reserves, pools, candles, feeBalances string }

func readV2ProjectionSnapshot(t *testing.T, ctx context.Context, db *sql.DB, chainID int64, token common.Address) v2ProjectionSnapshot {
	t.Helper()
	read := func(table, order string) string {
		var value string
		query := `SELECT coalesce(jsonb_agg(to_jsonb(row) ORDER BY ` + order + `)::text,'[]') FROM (SELECT * FROM ` + table + ` WHERE chain_id=$1 AND token_address=$2) AS row`
		if err := db.QueryRowContext(ctx, query, chainID, token[:]).Scan(&value); err != nil {
			t.Fatalf("read %s snapshot: %v", table, err)
		}
		return value
	}
	return v2ProjectionSnapshot{
		tokens:      read("tokens", "row.token_address"),
		reserves:    read("token_reserves", "row.token_address"),
		pools:       read("v4_pools", "row.pool_id"),
		candles:     read("candles", "row.interval, row.bucket_start_time"),
		feeBalances: read("creator_fee_balances", "row.account"),
	}
}

type feeBalance struct{ curveETH, lockerETH, lockerToken string }

func readFeeBalances(t *testing.T, ctx context.Context, db *sql.DB, chainID int64, token common.Address) map[common.Address]feeBalance {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT account, curve_eth_owed::text, locker_eth_owed::text, locker_token_owed::text FROM creator_fee_balances WHERE chain_id=$1 AND token_address=$2", chainID, token[:])
	if err != nil {
		t.Fatalf("read creator fee balances: %v", err)
	}
	defer func() { _ = rows.Close() }()
	balances := make(map[common.Address]feeBalance)
	for rows.Next() {
		var account []byte
		var balance feeBalance
		if err := rows.Scan(&account, &balance.curveETH, &balance.lockerETH, &balance.lockerToken); err != nil {
			t.Fatal(err)
		}
		balances[common.BytesToAddress(account)] = balance
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return balances
}

func assertRebuildMatchesIncremental(t *testing.T, ctx context.Context, database *Database, adapter *storepostgres.Adapter, chainID int64, token common.Address) {
	t.Helper()
	incremental := readV2ProjectionSnapshot(t, ctx, database.DB, chainID, token)
	if err := adapter.RebuildTokenProjections(ctx, chainID, token); err != nil {
		t.Fatalf("rebuild v2 token: %v", err)
	}
	if rebuilt := readV2ProjectionSnapshot(t, ctx, database.DB, chainID, token); !reflect.DeepEqual(incremental, rebuilt) {
		t.Fatalf("rebuild differs from incremental\nincremental=%+v\nrebuilt=%+v", incremental, rebuilt)
	}
}

// v4SpotPriceWAD mirrors the stored price formula independently: ETH per token, 1e18-scaled,
// for a pool whose currency0 is native ETH: 1e18 * 2^192 / sqrtPriceX96^2.
func v4SpotPriceWAD(sqrtPriceX96 *big.Int) *big.Int {
	numerator := new(big.Int).Lsh(big.NewInt(1_000_000_000_000_000_000), 192)
	return numerator.Div(numerator, new(big.Int).Mul(sqrtPriceX96, sqrtPriceX96))
}

func TestEngineV2Projections(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	adapter := storepostgres.NewAdapter(pool)

	const chainID int64 = 49030
	block := ledger.IndexedBlock{ChainID: chainID, BlockNumber: 9, BlockHash: common.Hash{0xc9}, ParentHash: common.Hash{0xc8}, BlockTime: time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC), FinalityStatus: "observed"}
	decoded := decodeV2Fixture(t, block, common.HexToAddress("0x0b0b"))
	if err := adapter.RegisterV4PoolManager(ctx, chainID, v2FixturePoolManager); err != nil {
		t.Fatalf("register pool manager: %v", err)
	}
	if err := adapter.RegisterV4PoolManager(ctx, chainID, common.HexToAddress("0xbad")); err == nil {
		t.Fatal("a second, different pool manager for the chain was accepted")
	}

	var launch chain.TokenLaunchedV2
	var swaps []chain.V4Swap
	var swapTransfers []chain.Transfer
	for index, log := range decoded {
		switch value := log.Value.(type) {
		case chain.TokenLaunchedV2:
			launch = value
		case chain.V4Swap:
			swaps = append(swaps, value)
			// The token leg of each swap is the Transfer logged right after it.
			swapTransfers = append(swapTransfers, decoded[index+1].Value.(chain.Transfer))
		}
	}
	token := launch.Token
	ingest := func(logs []chain.DecodedLog) {
		t.Helper()
		if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
			if _, err := adapter.UpsertIndexedBlock(ctx, block); err != nil {
				return err
			}
			return indexer.LedgerRouter{ChainID: chainID}.Apply(ctx, adapter, logs, map[int64]ledger.IndexedBlock{block.BlockNumber: block}, nil)
		}); err != nil {
			t.Fatalf("ingest fixture logs: %v", err)
		}
	}

	// Stage 1: through LpFeesCollected (log 28), before any claim.
	ingest(decoded[:29])

	balances := readFeeBalances(t, ctx, database.DB, chainID, token)
	want := map[common.Address]feeBalance{
		// Trades 5, 7 and 9 credit the creator; trade 13 follows FeeRecipientAccepted.
		v2FixtureCreator:   {curveETH: "1877549718661581", lockerETH: "0", lockerToken: "0"},
		v2FixtureRecipient: {curveETH: "20166853251173314", lockerETH: "999999999999999", lockerToken: "4999999999999999999999"},
		// The protocol keeps the locker remainder, odd wei included.
		v2FixtureTreasury: {curveETH: "0", lockerETH: "1000000000000000", lockerToken: "5000000000000000000000"},
	}
	if !reflect.DeepEqual(balances, want) {
		t.Fatalf("creator fee balances = %+v, want %+v", balances, want)
	}

	var source string
	var sqrtPrice string
	var ethReserveNull, tokenReserveNull bool
	if err := database.DB.QueryRowContext(ctx, "SELECT reserve_source, sqrt_price_x96::text, eth_reserve IS NULL, token_reserve IS NULL FROM token_reserves WHERE chain_id=$1 AND token_address=$2", chainID, token[:]).Scan(&source, &sqrtPrice, &ethReserveNull, &tokenReserveNull); err != nil {
		t.Fatalf("read v4 reserve: %v", err)
	}
	lastSwap := swaps[len(swaps)-1]
	if source != "v4" || sqrtPrice != lastSwap.SqrtPriceX96.String() || !ethReserveNull || !tokenReserveNull {
		t.Fatalf("reserve = %s sqrt %s nulls %t/%t, want v4 at the last swap's sqrt price %s", source, sqrtPrice, ethReserveNull, tokenReserveNull, lastSwap.SqrtPriceX96)
	}
	if err := adapter.RecomputeTokenStats(ctx, chainID, token); err != nil {
		t.Fatalf("recompute stats: %v", err)
	}
	var spotPrice string
	var liquidityNull bool
	var holderCount int
	if err := database.DB.QueryRowContext(ctx, "SELECT spot_price_eth_wad::text, liquidity_eth_wad IS NULL, holder_count FROM token_stats WHERE chain_id=$1 AND token_address=$2", chainID, token[:]).Scan(&spotPrice, &liquidityNull, &holderCount); err != nil {
		t.Fatalf("read stats: %v", err)
	}
	if spotPrice != v4SpotPriceWAD(lastSwap.SqrtPriceX96).String() || !liquidityNull {
		t.Fatalf("stats spot %s liquidity null %t, want %s and NULL liquidity", spotPrice, liquidityNull, v4SpotPriceWAD(lastSwap.SqrtPriceX96))
	}

	// Holders: the PoolManager and the locker hold tokens but are never listed, nor is the curve
	// (empty after graduation).
	holders, err := database.DB.QueryContext(ctx, "SELECT holder_address FROM holder_balances WHERE chain_id=$1 AND token_address=$2 AND balance > 0", chainID, token[:])
	if err != nil {
		t.Fatal(err)
	}
	held := make(map[common.Address]bool)
	for holders.Next() {
		var address []byte
		if err := holders.Scan(&address); err != nil {
			t.Fatal(err)
		}
		held[common.BytesToAddress(address)] = true
	}
	if err := holders.Close(); err != nil {
		t.Fatal(err)
	}
	if held[launch.Curve] {
		t.Fatalf("fixture precondition: the curve %s still holds tokens after graduation", launch.Curve.Hex())
	}
	for _, address := range []common.Address{v2FixturePoolManager, v2FixtureLocker} {
		if !held[address] {
			t.Fatalf("fixture precondition: %s holds no tokens", address.Hex())
		}
	}
	listed := listTokenHolders(t, ctx, pool, chainID, token)
	for _, address := range listed {
		if address == launch.Curve || address == v2FixturePoolManager || address == v2FixtureLocker {
			t.Fatalf("holder list includes excluded address %s: %v", address.Hex(), listed)
		}
	}
	if len(listed) != len(held)-2 || holderCount != len(listed) {
		t.Fatalf("listed holders %d, holder count %d, want %d", len(listed), holderCount, len(held)-2)
	}

	// market_trades: four curve trades, then the two V4 swaps, with positive volumes.
	trades, err := database.DB.QueryContext(ctx, "SELECT source, side_buy, gross_eth_volume::text, token_volume::text, spot_price_wad::text, encode(trader, 'hex') FROM market_trades WHERE chain_id=$1 AND token_address=$2 ORDER BY block_number, transaction_index, log_index", chainID, token[:])
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	dex := 0
	for trades.Next() {
		var source, ethVolume, tokenVolume, spot, trader string
		var buy bool
		if err := trades.Scan(&source, &buy, &ethVolume, &tokenVolume, &spot, &trader); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, source)
		if source != "dex" {
			continue
		}
		swap, transfer := swaps[dex], swapTransfers[dex]
		// A buy pays ETH into the pool (amount0 < 0) and receives tokens from the PoolManager.
		wantBuy := transfer.From == v2FixturePoolManager
		if buy != wantBuy || buy != (swap.Amount0.Sign() < 0) {
			t.Fatalf("swap %d side_buy %t, want %t (token leg from %s)", dex, buy, wantBuy, transfer.From.Hex())
		}
		if ethVolume != new(big.Int).Abs(swap.Amount0).String() || tokenVolume != transfer.Value.String() || tokenVolume != new(big.Int).Abs(swap.Amount1).String() {
			t.Fatalf("swap %d volumes eth %s token %s, want |amount0| %s and token leg %s", dex, ethVolume, tokenVolume, swap.Amount0, transfer.Value)
		}
		if spot != v4SpotPriceWAD(swap.SqrtPriceX96).String() || trader != "0000000000000000000000000000000000000b0b" {
			t.Fatalf("swap %d spot %s trader %s", dex, spot, trader)
		}
		dex++
	}
	if err := trades.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sources, []string{"curve", "curve", "curve", "curve", "dex", "dex"}) {
		t.Fatalf("market trade sources = %v", sources)
	}

	assertRebuildMatchesIncremental(t, ctx, database, adapter, chainID, token)

	// Stage 2: the claims empty every balance.
	ingest(decoded[29:])
	for account, balance := range readFeeBalances(t, ctx, database.DB, chainID, token) {
		if balance != (feeBalance{curveETH: "0", lockerETH: "0", lockerToken: "0"}) {
			t.Fatalf("balance of %s after claims = %+v, want zero", account.Hex(), balance)
		}
	}
	assertRebuildMatchesIncremental(t, ctx, database, adapter, chainID, token)

	// The engine-1 projection schema cannot hold these rows, so the migration's Down refuses (after
	// the V4 swap sender index, which rolls back freely).
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back the comment votes: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err != nil {
		t.Fatalf("roll back the V4 swap sender index: %v", err)
	}
	if _, err := migrations.Run(ctx, database.DB, migrations.CommandDown); err == nil || !strings.Contains(err.Error(), "engine v2 projection rows exist") {
		t.Fatalf("down with v2 projection rows error = %v, want refusal", err)
	}
}

func listTokenHolders(t *testing.T, ctx context.Context, pool *pgxpool.Pool, chainID int64, token common.Address) []common.Address {
	t.Helper()
	rows, err := sqlc.New(pool).ListTokenHolders(ctx, sqlc.ListTokenHoldersParams{ChainID: chainID, TokenAddress: sqlc.Address(token), PageSize: 100})
	if err != nil {
		t.Fatalf("list token holders: %v", err)
	}
	addresses := make([]common.Address, 0, len(rows))
	for _, row := range rows {
		addresses = append(addresses, common.Address(row.HolderAddress))
	}
	return addresses
}

// TestReadQuoteStateCarriesEngineV2Clock reads an engine-2 token's anti-snipe profile and the
// indexed head's block time, the quote's anti-snipe clock (pre-flight B5).
func TestReadQuoteStateCarriesEngineV2Clock(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)

	const chainID int64 = 49031
	const deployment = "engine-v2-quote-clock"
	launchedAt := time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC)
	block := ledger.IndexedBlock{ChainID: chainID, BlockNumber: 9, BlockHash: common.Hash{0xc9}, ParentHash: common.Hash{0xc8}, BlockTime: launchedAt, FinalityStatus: "observed"}
	decoded := decodeV2Fixture(t, block, common.HexToAddress("0x0b0b"))
	end := -1
	var launch chain.TokenLaunchedV2
	for index, log := range decoded {
		if value, ok := log.Value.(chain.TokenLaunchedV2); ok {
			launch, end = value, index+1
			break
		}
	}
	if end < 0 {
		t.Fatal("fixture has no TokenLaunchedV2")
	}
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
		if _, err := adapter.UpsertIndexedBlock(ctx, block); err != nil {
			return err
		}
		return indexer.LedgerRouter{ChainID: chainID}.Apply(ctx, adapter, decoded[:end], map[int64]ledger.IndexedBlock{block.BlockNumber: block}, nil)
	}); err != nil {
		t.Fatalf("ingest launch: %v", err)
	}
	head := launchedAt.Add(30 * time.Second)
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO sync_state(chain_id,deployment_id,observed_number,observed_hash,observed_at) VALUES($1,$2,9,$3,$4)`, chainID, deployment, block.BlockHash[:], head); err != nil {
		t.Fatal(err)
	}
	state, err := (storepostgres.TokenReader{Pool: pool, DeploymentID: deployment}).ReadQuoteState(ctx, chainID, launch.Token)
	if err != nil {
		t.Fatalf("read quote state: %v", err)
	}
	if state.Detail.EngineVersion != 2 || state.AntiSnipeStartBPS != launch.AntiSnipeStartBPS || state.AntiSnipeDurationSeconds != launch.AntiSnipeDurationSeconds {
		t.Fatalf("engine %d profile %d/%d, want 2 and %d/%d", state.Detail.EngineVersion, state.AntiSnipeStartBPS, state.AntiSnipeDurationSeconds, launch.AntiSnipeStartBPS, launch.AntiSnipeDurationSeconds)
	}
	if !state.Detail.LaunchTime.Equal(launchedAt) || !state.HeadTime.Equal(head) {
		t.Fatalf("launch %v head %v, want %v and %v", state.Detail.LaunchTime, state.HeadTime, launchedAt, head)
	}
}

// TestTokenDetailReadsEngineV2Facts reads a graduated engine-2 token's pool, fee recipient and
// locker balances through the detail read path, after the fixture's LpFeesCollected and before
// any claim.
func TestTokenDetailReadsEngineV2Facts(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)

	const chainID int64 = 49032
	const deployment = "engine-v2-detail"
	block := ledger.IndexedBlock{ChainID: chainID, BlockNumber: 9, BlockHash: common.Hash{0xc9}, ParentHash: common.Hash{0xc8}, BlockTime: time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC), FinalityStatus: "observed"}
	decoded := decodeV2Fixture(t, block, common.HexToAddress("0x0b0b"))
	var launch chain.TokenLaunchedV2
	var graduation chain.GraduatedV2
	for _, log := range decoded[:29] {
		switch value := log.Value.(type) {
		case chain.TokenLaunchedV2:
			launch = value
		case chain.GraduatedV2:
			graduation = value
		}
	}
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
		if _, err := adapter.UpsertIndexedBlock(ctx, block); err != nil {
			return err
		}
		return indexer.LedgerRouter{ChainID: chainID}.Apply(ctx, adapter, decoded[:29], map[int64]ledger.IndexedBlock{block.BlockNumber: block}, nil)
	}); err != nil {
		t.Fatalf("ingest fixture logs: %v", err)
	}
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO sync_state(chain_id,deployment_id,observed_number,observed_hash,observed_at) VALUES($1,$2,9,$3,$4)`, chainID, deployment, block.BlockHash[:], block.BlockTime); err != nil {
		t.Fatal(err)
	}
	detail, err := (storepostgres.TokenReader{Pool: pool, DeploymentID: deployment}).Get(ctx, chainID, launch.Token)
	if err != nil {
		t.Fatalf("read detail: %v", err)
	}
	facts := detail.V2
	if facts == nil {
		t.Fatal("engine-2 detail has no v2 facts")
	}
	if facts.AntiSnipeStartBPS != launch.AntiSnipeStartBPS || facts.AntiSnipeDurationSeconds != launch.AntiSnipeDurationSeconds || facts.LPCreatorShareBPS != launch.LPCreatorShareBPS {
		t.Fatalf("profile %+v, want the launch's", facts)
	}
	if facts.Pool == nil || facts.Pool.PoolID != graduation.PoolID || facts.Pool.PositionTokenID.Cmp(graduation.PositionTokenID) != 0 || facts.Pool.Hook != launch.Hook {
		t.Fatalf("pool %+v, want graduation pool %s #%s", facts.Pool, graduation.PoolID.Hex(), graduation.PositionTokenID)
	}
	// The fixture accepts the proposed recipient, so nothing is pending.
	if facts.FeeRecipient != v2FixtureRecipient || facts.PendingFeeRecipient != nil {
		t.Fatalf("fee recipient %s pending %v, want %s and none", facts.FeeRecipient.Hex(), facts.PendingFeeRecipient, v2FixtureRecipient.Hex())
	}
	// Ordered by account bytes: the treasury (0x…7000) sorts before the recipient (0x…fEE2).
	want := []token.LockerOwed{
		{Account: v2FixtureTreasury, ETH: big.NewInt(1000000000000000), Token: mustBig(t, "5000000000000000000000")},
		{Account: v2FixtureRecipient, ETH: big.NewInt(999999999999999), Token: mustBig(t, "4999999999999999999999")},
	}
	if len(facts.LockerOwed) != len(want) {
		t.Fatalf("locker owed %+v, want %+v", facts.LockerOwed, want)
	}
	for index, owed := range facts.LockerOwed {
		if owed.Account != want[index].Account || owed.ETH.Cmp(want[index].ETH) != 0 || owed.Token.Cmp(want[index].Token) != 0 {
			t.Fatalf("locker owed[%d] = %+v, want %+v", index, owed, want[index])
		}
	}
}

func mustBig(t *testing.T, value string) *big.Int {
	t.Helper()
	n, ok := new(big.Int).SetString(value, 10)
	if !ok {
		t.Fatalf("invalid integer %q", value)
	}
	return n
}
