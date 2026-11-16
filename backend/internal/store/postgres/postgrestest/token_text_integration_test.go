//go:build integration

package postgrestest

import (
	"context"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

// A launch name or symbol that Postgres cannot store as given (NUL, invalid UTF-8, or longer than
// the lower(name) btree index accepts) must still commit, and replaying the same event must match
// the stored row instead of reporting divergence. Otherwise one launch stops the whole indexer.
func TestTokenLaunchWithUnstorableTextCommitsAndReplays(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)

	const chainID int64 = 49011
	blockTime := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	blockHash := common.Hash{0xb1}
	if _, err := storepostgres.NewAdapter(pool).UpsertIndexedBlock(ctx, ledger.IndexedBlock{ChainID: chainID, BlockNumber: 100, BlockHash: blockHash, ParentHash: common.Hash{0xb0}, BlockTime: blockTime, FinalityStatus: "observed"}); err != nil {
		t.Fatal(err)
	}
	launch := func(index int32, name, symbol string) ledger.TokenLaunch {
		one := big.NewInt(1)
		return ledger.TokenLaunch{
			EventCoordinates: ledger.EventCoordinates{ChainID: chainID, BlockNumber: 100, BlockHash: blockHash, BlockTime: blockTime, TransactionIndex: index, TxHash: common.Hash{31: byte(index)}, LogIndex: 0},
			Token:            common.Address{19: byte(0x10 + index)}, Curve: common.Address{19: byte(0x40 + index)}, Creator: common.Address{19: 0x70},
			LPPair: common.Address{19: byte(0x90 + index)}, WETH: common.Address{19: 0xa0}, ProtocolTreasury: common.Address{19: 0xa1}, EngineVersion: 1,
			Name: name, Symbol: symbol, TotalSupply: big.NewInt(1000), VirtualETH: one, VirtualToken: one, CurveTokens: one, LPTokens: one,
			GraduationETH: one, LaunchFeePaid: one, TradeFeeBPS: 100, ProtocolShareBPS: 50,
		}
	}
	events := []ledger.TokenLaunch{
		launch(1, "A\x00B", "S\x00"),
		launch(2, "A\xffB", "S\xff"),
		launch(3, strings.Repeat("N", 5000), strings.Repeat("S", 5000)),
	}
	for _, event := range events {
		for attempt := range 2 { // the second attempt is a replay of the same event
			if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
				_, err := adapter.IngestTokenLaunch(ctx, event)
				return err
			}); err != nil {
				t.Fatalf("launch %d attempt %d: %v", event.TransactionIndex, attempt, err)
			}
		}
	}
	var stored []string
	rows, err := database.DB.QueryContext(ctx, `SELECT name FROM tokens WHERE chain_id = $1 ORDER BY token_address`, chainID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		stored = append(stored, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 || stored[0] != "A�B" || stored[1] != "A�B" || len(stored[2]) != storepostgres.MaxStoredTextBytes {
		t.Fatalf("stored names = %q (last length %d)", stored[:min(2, len(stored))], len(stored[len(stored)-1]))
	}
}
