//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/swapbackfill"
	"github.com/ethereum/go-ethereum/common"
)

type mapSenders map[common.Hash]common.Address

func (m mapSenders) TransactionSenders(_ context.Context, hashes []common.Hash) (map[common.Hash]common.Address, error) {
	out := map[common.Hash]common.Address{}
	for _, hash := range hashes {
		if sender, ok := m[hash]; ok {
			out[hash] = sender
		}
	}
	return out, nil
}

func (f walletSchemaFixture) backfill(ctx context.Context, senders mapSenders) (swapbackfill.Summary, error) {
	return swapbackfill.Run(ctx, swapbackfill.Deps{
		Store:   swapbackfill.PostgresStore{Pool: f.pool, ChainID: walletChainID, Factory: common.BytesToAddress(addressBytes(0xc4))},
		Senders: senders,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sleep:   func(context.Context) error { return nil },
	}, swapbackfill.Options{Batch: 1})
}

func (f walletSchemaFixture) nullSwaps(t *testing.T, ctx context.Context) int {
	t.Helper()
	var n int
	if err := f.database.DB.QueryRowContext(ctx, `SELECT count(*) FROM pool_swaps WHERE chain_id = $1 AND tx_from IS NULL`, walletChainID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBackfillFillsAndMarks(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	for _, tx := range []byte{0x61, 0x62} {
		if err := f.ingest(t, ctx, f.swap(tx, nil)); err != nil {
			t.Fatal(err)
		}
	}
	senderA, senderB := common.BytesToAddress(addressBytes(0xc5)), common.BytesToAddress(addressBytes(0xc6))
	summary, err := f.backfill(ctx, mapSenders{common.BytesToHash(hashBytes(0x61)): senderA, common.BytesToHash(hashBytes(0x62)): senderB})
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if summary.Updated != 2 || f.nullSwaps(t, ctx) != 0 {
		t.Fatalf("summary = %+v, null swaps = %d", summary, f.nullSwaps(t, ctx))
	}
	var stored []byte
	if err := f.database.DB.QueryRowContext(ctx, `SELECT tx_from FROM pool_swaps WHERE chain_id = $1 AND tx_hash = $2`, walletChainID, hashBytes(0x61)).Scan(&stored); err != nil || string(stored) != string(senderA.Bytes()) {
		t.Fatalf("tx_from = %x, %v; want %x", stored, err, senderA.Bytes())
	}
	dirty := f.dirty(t, ctx)
	if dirty[pairKey(0xc5, 0x10)].wallet == "" || dirty[pairKey(0xc6, 0x10)].wallet == "" {
		t.Fatalf("dirty = %v, want both senders marked", keys(dirty))
	}
}

func TestBackfillResumable(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	if err := f.ingest(t, ctx, f.swap(0x61, nil)); err != nil {
		t.Fatal(err)
	}
	senders := mapSenders{common.BytesToHash(hashBytes(0x61)): common.BytesToAddress(addressBytes(0xc5))}
	if _, err := f.backfill(ctx, senders); err != nil {
		t.Fatal(err)
	}
	before := f.dirty(t, ctx)[pairKey(0xc5, 0x10)]
	summary, err := f.backfill(ctx, senders)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Listed != 0 || summary.Updated != 0 {
		t.Fatalf("second run summary = %+v, want nothing", summary)
	}
	if after := f.dirty(t, ctx)[pairKey(0xc5, 0x10)]; after.generation != before.generation {
		t.Fatalf("second run re-marked the pair: generation %d -> %d", before.generation, after.generation)
	}
}

func TestBackfillSkipsUnresolved(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	for _, tx := range []byte{0x61, 0x62} {
		if err := f.ingest(t, ctx, f.swap(tx, nil)); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := f.backfill(ctx, mapSenders{common.BytesToHash(hashBytes(0x61)): common.BytesToAddress(addressBytes(0xc5))})
	if !errors.Is(err, swapbackfill.ErrSkipped) {
		t.Fatalf("err = %v, want ErrSkipped", err)
	}
	if summary.Updated != 1 || summary.Skipped != 1 || f.nullSwaps(t, ctx) != 1 {
		t.Fatalf("summary = %+v, null swaps = %d", summary, f.nullSwaps(t, ctx))
	}
	var stored []byte
	if err := f.database.DB.QueryRowContext(ctx, `SELECT tx_from FROM pool_swaps WHERE chain_id = $1 AND tx_hash = $2`, walletChainID, hashBytes(0x62)).Scan(&stored); err != nil || stored != nil {
		t.Fatalf("unresolved swap tx_from = %x, %v; want NULL", stored, err)
	}
}

// One scheduler pass, bounded by the indexer's committed safe head, fills a swap ingested with
// a NULL sender and marks its (sender, token) pair dirty; a swap above the safe head is left for
// a later pass.
func TestSchedulerPassFillsSwapsAtOrBelowSafeHead(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	const deploymentID = "wallet-backfill-test"
	store := swapbackfill.PostgresStore{Pool: f.pool, ChainID: walletChainID, Factory: common.BytesToAddress(addressBytes(0xc4)), DeploymentID: deploymentID}
	if _, ok, err := store.SafeBlock(ctx); err != nil || ok {
		t.Fatalf("SafeBlock() before any sync state = %t, %v; want no safe head", ok, err)
	}
	unsafeHash := hashBytes(0x22)
	mustInsertBlock(t, ctx, f.database.DB, walletChainID, 11, unsafeHash, f.hash, f.at.Add(time.Second), "observed")
	if err := f.ingest(t, ctx, f.swap(0x61, nil)); err != nil {
		t.Fatal(err)
	}
	above := f.swap(0x62, nil)
	above.BlockNumber, above.BlockHash, above.BlockTime = 11, common.BytesToHash(unsafeHash), f.at.Add(time.Second)
	if err := f.ingest(t, ctx, above); err != nil {
		t.Fatal(err)
	}
	safe := ledger.IndexedBlock{ChainID: walletChainID, BlockNumber: 10, BlockHash: common.BytesToHash(f.hash), BlockTime: f.at}
	observed := ledger.IndexedBlock{ChainID: walletChainID, BlockNumber: 11, BlockHash: common.BytesToHash(unsafeHash), BlockTime: f.at.Add(time.Second)}
	if err := storepostgres.NewAdapter(f.pool).WriteState(ctx, indexer.State{ChainID: walletChainID, DeploymentID: deploymentID, Observed: &observed, Safe: &safe, Finalized: &safe}); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := store.SafeBlock(ctx); err != nil || !ok || got != 10 {
		t.Fatalf("SafeBlock() = %d, %t, %v; want 10", got, ok, err)
	}

	senderA, senderB := common.BytesToAddress(addressBytes(0xc5)), common.BytesToAddress(addressBytes(0xc6))
	var filled, failures int
	var pending []int64
	scheduler := swapbackfill.Scheduler{
		Deps: swapbackfill.Deps{
			Store:   store,
			Senders: mapSenders{common.BytesToHash(hashBytes(0x61)): senderA, common.BytesToHash(hashBytes(0x62)): senderB},
			Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
			Sleep:   func(context.Context) error { return nil },
		},
		Interval: time.Minute, Batch: swapbackfill.DefaultBatch, MaxTransactions: 100, SafeBlock: store.SafeBlock,
		Backlog: store.CountUnattributed, OnBacklog: func(n int64) { pending = append(pending, n) },
		OnFilled: func(n int) { filled += n }, OnError: func() { failures++ },
	}
	if before, err := store.CountUnattributed(ctx, 10); err != nil || before != 1 {
		t.Fatalf("CountUnattributed(10) before the pass = %d, %v; want 1 (the unsafe swap is not counted)", before, err)
	}
	scheduler.Pass(ctx)
	if len(pending) != 1 || pending[0] != 0 {
		t.Fatalf("pending after the pass = %v, want [0]", pending)
	}

	if filled != 1 || failures != 0 {
		t.Fatalf("filled = %d, failures = %d; want 1, 0", filled, failures)
	}
	var stored []byte
	if err := f.database.DB.QueryRowContext(ctx, `SELECT tx_from FROM pool_swaps WHERE chain_id = $1 AND tx_hash = $2`, walletChainID, hashBytes(0x61)).Scan(&stored); err != nil || string(stored) != string(senderA.Bytes()) {
		t.Fatalf("safe swap tx_from = %x, %v; want %x", stored, err, senderA.Bytes())
	}
	if err := f.database.DB.QueryRowContext(ctx, `SELECT tx_from FROM pool_swaps WHERE chain_id = $1 AND tx_hash = $2`, walletChainID, hashBytes(0x62)).Scan(&stored); err != nil || stored != nil {
		t.Fatalf("swap above the safe head tx_from = %x, %v; want NULL", stored, err)
	}
	dirty := f.dirty(t, ctx)
	if dirty[pairKey(0xc5, 0x10)].wallet == "" {
		t.Fatalf("dirty = %v, want the filled sender's pair marked", keys(dirty))
	}
	if dirty[pairKey(0xc6, 0x10)].wallet != "" {
		t.Fatalf("dirty = %v, the unsafe swap's sender must not be marked", keys(dirty))
	}
}
