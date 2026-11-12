//go:build integration

package postgrestest

import (
	"context"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ingestV2Fixture registers the fixture PoolManager and ingests the v2 fixture through
// LpFeesCollected (log 28); it returns the launched token.
func ingestV2Fixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, chainID int64) common.Address {
	t.Helper()
	adapter := storepostgres.NewAdapter(pool)
	block := ledger.IndexedBlock{ChainID: chainID, BlockNumber: 9, BlockHash: common.Hash{0xc9}, ParentHash: common.Hash{0xc8}, BlockTime: time.Now().UTC().Truncate(time.Second), FinalityStatus: "observed"}
	decoded := decodeV2Fixture(t, block, common.HexToAddress("0x0b0b"))
	if err := adapter.RegisterV4PoolManager(ctx, chainID, v2FixturePoolManager); err != nil {
		t.Fatalf("register pool manager: %v", err)
	}
	var token common.Address
	for _, log := range decoded {
		if launch, ok := log.Value.(chain.TokenLaunchedV2); ok {
			token = launch.Token
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
	return token
}

func numericString(t *testing.T, value pgtype.Numeric) string {
	t.Helper()
	raw, err := value.Value()
	if err != nil {
		t.Fatal(err)
	}
	if raw == nil {
		return "NULL"
	}
	return raw.(string)
}

// A V4 swap is a priced position leg of its tx_from, like a v1 pool swap: the swapper's ETH
// delta (amount0) is the ETH paid or received.
func TestPositionLegsIncludeV4Swaps(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	const chainID int64 = 49032
	token := ingestV2Fixture(t, ctx, pool, chainID)

	var trader []byte
	if err := database.DB.QueryRowContext(ctx, `SELECT tx_from FROM v4_swaps WHERE chain_id = $1 AND tx_from IS NOT NULL ORDER BY block_number, log_index LIMIT 1`, chainID).Scan(&trader); err != nil {
		t.Fatalf("fixture V4 swapper: %v", err)
	}
	var wantPaid, wantReceived string
	if err := database.DB.QueryRowContext(ctx, `
		SELECT (COALESCE((SELECT sum(eth_gross) FROM trades WHERE chain_id = $1 AND token_address = $2 AND trader = $3 AND is_buy), 0)
		      + COALESCE((SELECT sum(-amount0_delta) FROM v4_swaps WHERE chain_id = $1 AND tx_from = $3 AND amount0_delta < 0), 0))::text,
		       (COALESCE((SELECT sum(eth_gross - protocol_fee - creator_fee) FROM trades WHERE chain_id = $1 AND token_address = $2 AND trader = $3 AND NOT is_buy), 0)
		      + COALESCE((SELECT sum(amount0_delta) FROM v4_swaps WHERE chain_id = $1 AND tx_from = $3 AND amount0_delta > 0), 0))::text`,
		chainID, token[:], trader).Scan(&wantPaid, &wantReceived); err != nil {
		t.Fatal(err)
	}
	rows, err := sqlc.New(pool).ListPositionLegs(ctx, sqlc.ListPositionLegsParams{ChainID: chainID, TokenAddress: token[:], Wallet: trader})
	if err != nil {
		t.Fatalf("list position legs: %v", err)
	}
	gotPaid, gotReceived := "0", "0"
	for _, row := range rows {
		if err := pool.QueryRow(ctx, `SELECT ($1::numeric + $2::numeric)::text, ($3::numeric + $4::numeric)::text`, gotPaid, numericString(t, row.LegEthPaid), gotReceived, numericString(t, row.LegEthReceived)).Scan(&gotPaid, &gotReceived); err != nil {
			t.Fatal(err)
		}
	}
	if gotPaid != wantPaid || gotReceived != wantReceived {
		t.Fatalf("legs of %x: paid %s received %s, want %s / %s (curve trades plus V4 swaps)", trader, gotPaid, gotReceived, wantPaid, wantReceived)
	}
}

// A graduated engine-2 holding is valued at the V4 pool's spot price, not at zero.
func TestWalletValuationUsesV4SpotPrice(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	const chainID int64 = 49033
	token := ingestV2Fixture(t, ctx, pool, chainID)

	holder := common.HexToAddress("0x00000000000000000000000000000000000000d8")
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO wallet_positions (chain_id, wallet, token_address, balance, remaining_cost, realized_pnl, transferred_in, eth_paid, eth_received, trade_count, first_block_number, last_block_number)
		VALUES ($1, $2, $3, 1000000000000000000000, 1, 0, 0, 1, 0, 1, 9, 9)`, chainID, holder[:], token[:]); err != nil {
		t.Fatalf("seed position: %v", err)
	}
	var want string
	if err := database.DB.QueryRowContext(ctx, `
		SELECT div(1000000000000000000000 * v4_spot_price_wad(sqrt_price_x96), 1000000000000000000)::text
		FROM token_reserves WHERE chain_id = $1 AND token_address = $2 AND reserve_source = 'v4'`, chainID, token[:]).Scan(&want); err != nil {
		t.Fatalf("v4 reserve: %v", err)
	}
	queries := sqlc.New(pool)
	valuation, err := queries.GetWalletValuation(ctx, sqlc.GetWalletValuationParams{ChainID: chainID, Wallet: sqlc.Address(holder)})
	if err != nil {
		t.Fatal(err)
	}
	if got := numericString(t, valuation.PortfolioValue); got != want || want == "0" {
		t.Fatalf("portfolio value = %s, want %s (balance at the V4 spot price)", got, want)
	}
	open, err := queries.ListWalletOpenPositions(ctx, sqlc.ListWalletOpenPositionsParams{ChainID: chainID, Wallet: sqlc.Address(holder), PageSize: 10})
	if err != nil || len(open) != 1 {
		t.Fatalf("open positions = %d, %v", len(open), err)
	}
	if got := numericString(t, open[0].CurrentValue); got != want {
		t.Fatalf("open position value = %s, want %s", got, want)
	}
}

// The PoolManager and the locker hold engine-2 tokens as infrastructure; they never rank.
func TestLeaderboardExcludesV4Infrastructure(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)
	const chainID int64 = 49034
	token := ingestV2Fixture(t, ctx, pool, chainID)

	trader := common.HexToAddress("0x00000000000000000000000000000000000000d9")
	for _, wallet := range []common.Address{v2FixturePoolManager, v2FixtureLocker, trader} {
		if _, err := database.DB.ExecContext(ctx, `
			INSERT INTO wallet_totals (chain_id, wallet, realized_pnl, tokens_traded, held_count, first_block_number, last_block_number)
			VALUES ($1, $2, 5, 1, 0, 9, 9)`, chainID, wallet[:]); err != nil {
			t.Fatalf("seed totals: %v", err)
		}
		if _, err := database.DB.ExecContext(ctx, `
			INSERT INTO wallet_realized_legs (chain_id, wallet, token_address, block_number, transaction_index, block_time, realized_pnl)
			VALUES ($1, $2, $3, 9, 0, now(), 5)`, chainID, wallet[:], token[:]); err != nil {
			t.Fatalf("seed realized leg: %v", err)
		}
	}
	queries := sqlc.New(pool)
	ranked, err := queries.ListLeaderboard(ctx, sqlc.ListLeaderboardParams{ChainID: chainID, DeploymentID: "v2-positions", PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 1 || common.Address(ranked[0].Wallet) != trader {
		t.Fatalf("leaderboard = %d rows, want only %s", len(ranked), trader)
	}
	windowed, err := queries.ListLeaderboardWindow(ctx, sqlc.ListLeaderboardWindowParams{ChainID: chainID, DeploymentID: "v2-positions", PageSize: 10, WindowDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(windowed) != 1 || common.BytesToAddress(windowed[0].Wallet) != trader {
		t.Fatalf("windowed leaderboard = %d rows, want only %s", len(windowed), trader)
	}
}
