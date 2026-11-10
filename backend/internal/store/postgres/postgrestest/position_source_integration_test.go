//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/position"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

// positionLedger writes raw ledger rows for the wallet-schema fixture token (0x10, curve 0x41,
// pair 0x31, WETH 0x90; the token is token0, so WETH is the pair's token1).
type positionLedger struct {
	t   *testing.T
	ctx context.Context
	f   walletSchemaFixture
}

var positionToken = addressBytes(0x10)

func (l positionLedger) blockHash(block int64) []byte { return hashBytes(byte(0x21 + block - 10)) }

func (l positionLedger) blockTime(block int64) time.Time {
	return l.f.at.Add(time.Duration(block-10) * time.Minute)
}

// block inserts block n (> 10) on top of the fixture's block 10.
func (l positionLedger) block(n int64, status string) {
	l.t.Helper()
	mustInsertBlock(l.t, l.ctx, l.f.database.DB, walletChainID, n, l.blockHash(n), l.blockHash(n-1), l.blockTime(n), status)
}

func (l positionLedger) exec(query string, args ...any) {
	l.t.Helper()
	if _, err := l.f.database.DB.ExecContext(l.ctx, query, args...); err != nil {
		l.t.Fatalf("exec %q: %v", strings.Fields(query)[2], err)
	}
}

func (l positionLedger) trade(block int64, txIndex int, txHash byte, logIndex int, trader byte, isBuy bool, ethGross, tokenAmount, protocolFee, creatorFee int64) {
	l.t.Helper()
	l.exec(`INSERT INTO trades (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
		token_address, trader, is_buy, eth_gross, eth_refund, token_amount, protocol_fee, creator_fee, new_eth_reserve, new_token_reserve)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 0, $12, $13, $14, 1000, 1000)`,
		walletChainID, block, l.blockHash(block), l.blockTime(block), txIndex, hashBytes(txHash), logIndex,
		positionToken, addressBytes(trader), isBuy, ethGross, tokenAmount, protocolFee, creatorFee)
}

func (l positionLedger) transfer(block int64, txIndex int, txHash byte, logIndex int, from, to byte, value int64) {
	l.t.Helper()
	l.exec(`INSERT INTO transfers (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
		token_address, from_address, to_address, value) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		walletChainID, block, l.blockHash(block), l.blockTime(block), txIndex, hashBytes(txHash), logIndex,
		positionToken, addressBytes(from), addressBytes(to), value)
}

func (l positionLedger) swap(block int64, txIndex int, txHash byte, logIndex int, amount0In, amount1In, amount0Out, amount1Out int64, to, txFrom byte) {
	l.t.Helper()
	l.exec(`INSERT INTO pool_swaps (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
		pair_address, sender, amount0_in, amount1_in, amount0_out, amount1_out, to_address, tx_from)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		walletChainID, block, l.blockHash(block), l.blockTime(block), txIndex, hashBytes(txHash), logIndex,
		l.f.pair.Bytes(), addressBytes(0xa1), amount0In, amount1In, amount0Out, amount1Out, addressBytes(to), addressBytes(txFrom))
}

func (l positionLedger) mark(wallets ...byte) {
	l.t.Helper()
	pairs := make([]indexer.PositionPair, len(wallets))
	for i, wallet := range wallets {
		pairs[i] = indexer.PositionPair{Wallet: common.BytesToAddress(addressBytes(wallet)), Token: common.BytesToAddress(positionToken)}
	}
	if err := storepostgres.WithinTx(l.ctx, l.f.pool, func(ctx context.Context, a *storepostgres.Adapter) error {
		return a.MarkPositionsDirty(ctx, walletChainID, pairs)
	}); err != nil {
		l.t.Fatal(err)
	}
}

// drainPositions runs the position source until position_dirty is empty.
func drainPositions(t *testing.T, ctx context.Context, f walletSchemaFixture) {
	t.Helper()
	source := storepostgres.PositionSource{Adapter: storepostgres.NewAdapter(f.pool), ChainID: walletChainID}
	for round := 0; ; round++ {
		if round > 10 {
			t.Fatal("position_dirty did not drain")
		}
		claims, err := source.Claim(ctx, "position-test", 64)
		if err != nil {
			t.Fatalf("claim positions: %v", err)
		}
		if len(claims) == 0 {
			return
		}
		for _, claim := range claims {
			if err := source.Compute(ctx, claim); err != nil {
				t.Fatalf("compute position %x/%x: %v", claim.Wallet, claim.Token, err)
			}
			if completed, err := source.Complete(ctx, claim, "position-test"); err != nil || !completed {
				t.Fatalf("complete position %x/%x = %t, %v", claim.Wallet, claim.Token, completed, err)
			}
		}
	}
}

type positionRow struct {
	balance, remainingCost, realizedPnL, transferredIn, ethPaid, ethReceived string
	tradeCount, firstBlock, lastBlock                                        int64
}

func readPosition(t *testing.T, ctx context.Context, db *sql.DB, wallet, token byte) (positionRow, bool) {
	t.Helper()
	var row positionRow
	err := db.QueryRowContext(ctx, `SELECT balance::text, remaining_cost::text, realized_pnl::text, transferred_in::text,
		eth_paid::text, eth_received::text, trade_count, first_block_number, last_block_number
		FROM wallet_positions WHERE chain_id = $1 AND wallet = $2 AND token_address = $3`,
		walletChainID, addressBytes(wallet), addressBytes(token)).Scan(&row.balance, &row.remainingCost, &row.realizedPnL,
		&row.transferredIn, &row.ethPaid, &row.ethReceived, &row.tradeCount, &row.firstBlock, &row.lastBlock)
	if err == sql.ErrNoRows {
		return positionRow{}, false
	}
	if err != nil {
		t.Fatalf("read wallet position: %v", err)
	}
	return row, true
}

func wantPosition(p position.Position) positionRow {
	return positionRow{balance: p.Balance.String(), remainingCost: p.RemainingCost.String(), realizedPnL: p.RealizedPnL.String(),
		transferredIn: p.TransferredIn.String(), ethPaid: p.ETHPaid.String(), ethReceived: p.ETHReceived.String(),
		tradeCount: p.TradeCount, firstBlock: p.FirstBlock, lastBlock: p.LastBlock}
}

type totalsRow struct {
	realizedPnL                                    string
	tokensTraded, heldCount, firstBlock, lastBlock int64
}

func readTotals(t *testing.T, ctx context.Context, db *sql.DB, wallet byte) (totalsRow, bool) {
	t.Helper()
	var row totalsRow
	err := db.QueryRowContext(ctx, `SELECT realized_pnl::text, tokens_traded, held_count, first_block_number, last_block_number
		FROM wallet_totals WHERE chain_id = $1 AND wallet = $2`, walletChainID, addressBytes(wallet)).
		Scan(&row.realizedPnL, &row.tokensTraded, &row.heldCount, &row.firstBlock, &row.lastBlock)
	if err == sql.ErrNoRows {
		return totalsRow{}, false
	}
	if err != nil {
		t.Fatalf("read wallet totals: %v", err)
	}
	return row, true
}

func leg(block int64, txIndex int32, delta, paid, received int64) position.Leg {
	return position.Leg{Block: block, TxIndex: txIndex, TokenDelta: big.NewInt(delta), ETHPaid: big.NewInt(paid), ETHReceived: big.NewInt(received)}
}

func mustCompute(t *testing.T, legs ...position.Leg) position.Position {
	t.Helper()
	p, err := position.Compute(legs)
	if err != nil {
		t.Fatalf("compute expected position: %v", err)
	}
	return p
}

// newCurvePositionFixture launches the wallet-schema token in block 10 without graduating it, so
// curve trades can follow; seedTradingLedger graduates it in block 13.
func newCurvePositionFixture(t *testing.T, ctx context.Context) walletSchemaFixture {
	t.Helper()
	database := NewMigrated(t)
	pool := openPool(t, ctx, database.URL)
	at := time.Date(2026, time.November, 6, 9, 0, 0, 0, time.UTC)
	hash := hashBytes(0x21)
	mustInsertBlock(t, ctx, database.DB, walletChainID, 10, hash, hashBytes(0x20), at, "safe")
	pair := addressBytes(0x31)
	insertProjectionLaunch(t, ctx, database.DB, walletChainID, 10, hash, at, hashBytes(0x30), projectionLaunchFixture{token: positionToken, curve: addressBytes(0x41), pair: pair, weth: addressBytes(0x90)})
	// The launch mint funds the curve.
	insertTransfer(t, ctx, database.DB, walletChainID, 10, hash, at, hashBytes(0x2f), 1, positionToken, addressBytes(0), addressBytes(0x41), 1000000)
	callRebuild(t, ctx, database.DB, walletChainID, positionToken)
	return walletSchemaFixture{database: database, pool: pool, reader: storepostgres.FeedReader{Pool: pool}, pair: common.BytesToAddress(pair), at: at, hash: hash}
}

// seedTradingLedger writes a curve buy, a curve sell and (after graduation) a pool-swap sell for
// wallet 0xc1, plus unrelated activity the loader must ignore.
func seedTradingLedger(l positionLedger) {
	for block := int64(11); block <= 14; block++ {
		l.block(block, "safe")
	}
	// tx 0x81: curve buy of 500 tokens for 1000 wei gross (fees included).
	l.trade(11, 0, 0x81, 0, 0xc1, true, 1000, 500, 5, 5)
	l.transfer(11, 0, 0x81, 1, 0x41, 0xc1, 500)
	// tx 0x82: another trader's buy, never part of 0xc1's legs.
	l.trade(11, 1, 0x82, 2, 0xc5, true, 70, 30, 1, 1)
	l.transfer(11, 1, 0x82, 3, 0x41, 0xc5, 30)
	// tx 0x83: curve sell of 200 tokens, 400 gross, 2 + 3 fees -> 395 received.
	l.trade(12, 1, 0x83, 0, 0xc1, false, 400, 200, 2, 3)
	l.transfer(12, 1, 0x83, 1, 0xc1, 0x41, 200)
	// Graduation in block 13 opens the pair.
	insertProjectionGraduation(l.t, l.ctx, l.f.database.DB, walletChainID, 13, l.blockHash(13), l.blockTime(13), hashBytes(0x8f), 0, positionToken, l.f.pair.Bytes())
	insertPoolSync(l.t, l.ctx, l.f.database.DB, walletChainID, 13, l.blockHash(13), l.blockTime(13), hashBytes(0x8f), 1, l.f.pair.Bytes(), 200, 1200)
	callRebuild(l.t, l.ctx, l.f.database.DB, walletChainID, positionToken)
	// tx 0x84: pool sell of 100 tokens (token0 in) for 150 WETH (token1 out), sent by 0xc1.
	l.transfer(14, 0, 0x84, 0, 0xc1, 0x31, 100)
	l.swap(14, 0, 0x84, 1, 100, 0, 0, 150, 0xc1, 0xc1)
	// tx 0x85: a swap sent by someone else whose tokens leave nothing for 0xc1.
	l.swap(14, 1, 0x85, 2, 0, 10, 5, 0, 0xc6, 0xc6)
	l.transfer(14, 1, 0x85, 3, 0x31, 0xc6, 5)
}

func TestPositionRecomputeMatchesLedger(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newCurvePositionFixture(t, ctx)
	l := positionLedger{t: t, ctx: ctx, f: f}
	seedTradingLedger(l)
	l.mark(0xc1)
	drainPositions(t, ctx, f)

	want := mustCompute(t, leg(11, 0, 500, 1000, 0), leg(12, 1, -200, 0, 395), leg(14, 0, -100, 0, 150))
	got, ok := readPosition(t, ctx, f.database.DB, 0xc1, 0x10)
	if !ok {
		t.Fatal("wallet_positions row missing")
	}
	if got != wantPosition(want) {
		t.Fatalf("position = %+v, want %+v", got, wantPosition(want))
	}
	totals, ok := readTotals(t, ctx, f.database.DB, 0xc1)
	wantTotals := totalsRow{realizedPnL: want.RealizedPnL.String(), tokensTraded: 1, heldCount: 1, firstBlock: 11, lastBlock: 14}
	if !ok || totals != wantTotals {
		t.Fatalf("totals = %+v (present %t), want %+v", totals, ok, wantTotals)
	}
}

// Review Focus 2: the swap sender pays, another address receives the tokens.
func TestPositionRecipientDiffersFromSender(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	l := positionLedger{t: t, ctx: ctx, f: f}
	l.block(11, "safe")
	// Buy: 300 WETH (token1) in, 50 tokens (token0) out to 0xc2; the transaction is sent by 0xc1.
	l.swap(11, 0, 0x91, 0, 0, 300, 50, 0, 0xc2, 0xc1)
	l.transfer(11, 0, 0x91, 1, 0x31, 0xc2, 50)
	l.mark(0xc1, 0xc2)
	drainPositions(t, ctx, f)

	sender, ok := readPosition(t, ctx, f.database.DB, 0xc1, 0x10)
	wantSender := wantPosition(mustCompute(t, leg(11, 0, 0, 300, 0)))
	if !ok || sender != wantSender || sender.balance != "0" || sender.realizedPnL != "-300" {
		t.Fatalf("sender = %+v (present %t), want %+v", sender, ok, wantSender)
	}
	recipient, ok := readPosition(t, ctx, f.database.DB, 0xc2, 0x10)
	wantRecipient := wantPosition(mustCompute(t, leg(11, 0, 50, 0, 0)))
	if !ok || recipient != wantRecipient || recipient.transferredIn != "50" || recipient.remainingCost != "0" {
		t.Fatalf("recipient = %+v (present %t), want %+v", recipient, ok, wantRecipient)
	}
	if totals, _ := readTotals(t, ctx, f.database.DB, 0xc2); totals.tokensTraded != 0 || totals.heldCount != 1 {
		t.Fatalf("recipient totals = %+v, want tokens_traded 0, held_count 1", totals)
	}
}

// Review Focus 3 (P2): a dirty pair whose ledger is empty deletes its position, and the totals
// row too once the wallet has no positions left.
func TestPositionEmptyLegsDeletesRow(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	l := positionLedger{t: t, ctx: ctx, f: f}
	for _, token := range []byte{0x10, 0x12} {
		l.exec(`INSERT INTO wallet_positions (chain_id, wallet, token_address, balance, remaining_cost, realized_pnl, transferred_in,
			eth_paid, eth_received, trade_count, first_block_number, last_block_number) VALUES ($1, $2, $3, 5, 5, -7, 0, 5, 0, 1, 10, 10)`,
			walletChainID, addressBytes(0xc1), addressBytes(token))
	}
	l.exec(`INSERT INTO wallet_totals (chain_id, wallet, realized_pnl, tokens_traded, held_count, first_block_number, last_block_number)
		VALUES ($1, $2, -14, 2, 2, 10, 10)`, walletChainID, addressBytes(0xc1))

	l.mark(0xc1)
	drainPositions(t, ctx, f)
	if _, ok := readPosition(t, ctx, f.database.DB, 0xc1, 0x10); ok {
		t.Fatal("position with no legs was not deleted")
	}
	totals, ok := readTotals(t, ctx, f.database.DB, 0xc1)
	if want := (totalsRow{realizedPnL: "-7", tokensTraded: 1, heldCount: 1, firstBlock: 10, lastBlock: 10}); !ok || totals != want {
		t.Fatalf("totals = %+v (present %t), want %+v from the remaining position", totals, ok, want)
	}

	// The second token has no ledger either (it does not even exist): its recompute removes the
	// wallet's last position and with it the totals row.
	if err := storepostgres.WithinTx(ctx, f.pool, func(ctx context.Context, a *storepostgres.Adapter) error {
		return a.MarkPositionsDirty(ctx, walletChainID, []indexer.PositionPair{{Wallet: common.BytesToAddress(addressBytes(0xc1)), Token: common.BytesToAddress(addressBytes(0x12))}})
	}); err != nil {
		t.Fatal(err)
	}
	drainPositions(t, ctx, f)
	if _, ok := readPosition(t, ctx, f.database.DB, 0xc1, 0x12); ok {
		t.Fatal("last position was not deleted")
	}
	if totals, ok := readTotals(t, ctx, f.database.DB, 0xc1); ok {
		t.Fatalf("totals = %+v, want deleted with the last position", totals)
	}
}

func dumpPositionProjection(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var out strings.Builder
	for _, query := range []string{
		`SELECT encode(wallet, 'hex'), encode(token_address, 'hex'), balance::text, remaining_cost::text, realized_pnl::text,
			transferred_in::text, eth_paid::text, eth_received::text, trade_count::text, first_block_number::text, last_block_number::text
		 FROM wallet_positions WHERE chain_id = $1 ORDER BY wallet, token_address`,
		`SELECT encode(wallet, 'hex'), '', realized_pnl::text, tokens_traded::text, held_count::text, first_block_number::text,
			last_block_number::text, '', '', '', ''
		 FROM wallet_totals WHERE chain_id = $1 ORDER BY wallet`,
		`SELECT encode(wallet, 'hex'), encode(token_address, 'hex'), block_number::text, transaction_index::text,
			to_char(block_time AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS'), realized_pnl::text, '', '', '', '', ''
		 FROM wallet_realized_legs WHERE chain_id = $1 ORDER BY wallet, token_address, block_number, transaction_index`,
	} {
		rows, err := db.QueryContext(ctx, query, walletChainID)
		if err != nil {
			t.Fatalf("dump projection: %v", err)
		}
		for rows.Next() {
			values := make([]string, 11)
			pointers := make([]any, len(values))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintln(&out, strings.Join(values, "|"))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		out.WriteString("--\n")
	}
	return out.String()
}

// Spec AC3 (P3): after a reorg drops the last trade, the drained projection equals a fresh
// recompute of every pair over the remaining ledger.
func TestReorgRecomputeEqualsFresh(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newCurvePositionFixture(t, ctx)
	l := positionLedger{t: t, ctx: ctx, f: f}
	seedTradingLedger(l)
	// The last trade: 0xc1 buys again on the pool in an unsafe block (40 WETH in, 25 tokens out),
	// and 0xc2 receives a gift.
	l.block(15, "observed")
	l.swap(15, 0, 0x86, 0, 0, 40, 25, 0, 0xc1, 0xc1)
	l.transfer(15, 0, 0x86, 1, 0x31, 0xc1, 25)
	l.transfer(15, 1, 0x87, 2, 0xc1, 0xc2, 25)
	l.mark(0xc1, 0xc2, 0xc5, 0xc6)
	drainPositions(t, ctx, f)
	beforeReorg := dumpPositionProjection(t, ctx, f.database.DB)

	if err := storepostgres.WithinTx(ctx, f.pool, func(ctx context.Context, a *storepostgres.Adapter) error {
		pairs, err := a.AffectedPositionPairsAbove(ctx, walletChainID, 14)
		if err != nil {
			return err
		}
		if err := a.DeleteCanonicalAbove(ctx, walletChainID, 14); err != nil {
			return err
		}
		return a.MarkPositionsDirty(ctx, walletChainID, pairs)
	}); err != nil {
		t.Fatal(err)
	}
	drainPositions(t, ctx, f)
	afterReorg := dumpPositionProjection(t, ctx, f.database.DB)
	if afterReorg == beforeReorg {
		t.Fatal("reorg recompute left the projection unchanged; the fixture does not exercise the reorg")
	}
	if _, ok := readPosition(t, ctx, f.database.DB, 0xc2, 0x10); ok {
		t.Fatal("0xc2's only activity was reorged out, its position must be gone")
	}

	l.exec(`DELETE FROM wallet_positions WHERE chain_id = $1`, walletChainID)
	l.exec(`DELETE FROM wallet_totals WHERE chain_id = $1`, walletChainID)
	l.exec(`INSERT INTO position_dirty (chain_id, wallet, token_address, generation)
		SELECT DISTINCT $1::bigint, pair.wallet, pair.token_address, nextval('position_dirty_generation_seq') FROM (
			SELECT to_address AS wallet, token_address FROM transfers WHERE chain_id = $1
			UNION SELECT from_address, token_address FROM transfers WHERE chain_id = $1
			UNION SELECT trader, token_address FROM trades WHERE chain_id = $1
			UNION SELECT swap.tx_from, token.token_address FROM pool_swaps AS swap
				JOIN tokens AS token ON token.chain_id = swap.chain_id AND token.lp_pair = swap.pair_address
				WHERE swap.chain_id = $1 AND swap.tx_from IS NOT NULL
		) AS pair
		WHERE pair.wallet NOT IN ($2, $3, $4)`, walletChainID, addressBytes(0), addressBytes(0x41), addressBytes(0x31))
	drainPositions(t, ctx, f)
	if fresh := dumpPositionProjection(t, ctx, f.database.DB); fresh != afterReorg {
		t.Fatalf("reorg recompute differs from a fresh recompute:\nreorg:\n%s\nfresh:\n%s", afterReorg, fresh)
	}
}

// Claim, Complete and stale-claim recovery mirror aggregation_dirty: a lapsed lease can be taken
// over, and neither a stale owner nor a superseded generation completes the newer work.
func TestPositionDirtyLeaseGuardsStaleCompletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newWalletSchemaFixture(t, ctx)
	l := positionLedger{t: t, ctx: ctx, f: f}
	l.mark(0xc1)
	source := storepostgres.PositionSource{Adapter: storepostgres.NewAdapter(f.pool), ChainID: walletChainID}

	first, err := source.Claim(ctx, "worker-a", 8)
	if err != nil || len(first) != 1 || first[0].Wallet != [20]byte(addressBytes(0xc1)) || first[0].Token != [20]byte(positionToken) {
		t.Fatalf("first claim = %+v, %v", first, err)
	}
	if again, err := source.Claim(ctx, "worker-b", 8); err != nil || len(again) != 0 {
		t.Fatalf("live lease was reclaimed: %+v, %v", again, err)
	}
	if other, err := (storepostgres.PositionSource{Adapter: storepostgres.NewAdapter(f.pool), ChainID: walletChainID + 1}).Claim(ctx, "worker-x", 8); err != nil || len(other) != 0 {
		t.Fatalf("another chain's worker claimed this chain's pair: %+v, %v", other, err)
	}
	l.exec(`UPDATE position_dirty SET claimed_at = now() - interval '31 seconds' WHERE chain_id = $1`, walletChainID)
	second, err := source.Claim(ctx, "worker-b", 8)
	if err != nil || len(second) != 1 {
		t.Fatalf("expired lease not reclaimed: %+v, %v", second, err)
	}
	if completed, err := source.Complete(ctx, first[0], "worker-a"); err != nil || completed {
		t.Fatalf("stale owner completion = %t, %v; want false", completed, err)
	}
	l.mark(0xc1)
	if completed, err := source.Complete(ctx, second[0], "worker-b"); err != nil || completed {
		t.Fatalf("superseded generation completion = %t, %v; want false", completed, err)
	}
	third, err := source.Claim(ctx, "worker-c", 8)
	if err != nil || len(third) != 1 || third[0].Generation <= second[0].Generation {
		t.Fatalf("newer generation claim = %+v, %v", third, err)
	}
	if completed, err := source.Complete(ctx, third[0], "worker-c"); err != nil || !completed {
		t.Fatalf("current completion = %t, %v; want true", completed, err)
	}
	if n := len(f.dirty(t, ctx)); n != 0 {
		t.Fatalf("position_dirty rows = %d, want 0", n)
	}
}

// Spec C3 AC2: a recompute writes exactly the non-zero realized legs with their block times, and
// their sum equals the position's realized PnL; recomputing to no legs deletes them.
func TestRealizedLegsMatchLedger(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	f := newCurvePositionFixture(t, ctx)
	l := positionLedger{t: t, ctx: ctx, f: f}
	seedTradingLedger(l)
	l.mark(0xc1)
	drainPositions(t, ctx, f)

	want := mustCompute(t, leg(11, 0, 500, 1000, 0), leg(12, 1, -200, 0, 395), leg(14, 0, -100, 0, 150))
	type legRow struct {
		block   int64
		txIndex int32
		at      time.Time
		pnl     string
	}
	readLegs := func() []legRow {
		rows, err := f.database.DB.QueryContext(ctx, `SELECT block_number, transaction_index, block_time, realized_pnl::text
			FROM wallet_realized_legs WHERE chain_id = $1 AND wallet = $2 AND token_address = $3 ORDER BY block_number, transaction_index`,
			walletChainID, addressBytes(0xc1), positionToken)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []legRow
		for rows.Next() {
			var r legRow
			if err := rows.Scan(&r.block, &r.txIndex, &r.at, &r.pnl); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	got := readLegs()
	wantRows := []legRow{
		{12, 1, l.blockTime(12), want.LegRealized[1].String()},
		{14, 0, l.blockTime(14), want.LegRealized[2].String()},
	}
	if want.LegRealized[0].Sign() != 0 || len(got) != len(wantRows) {
		t.Fatalf("legs = %+v, want %+v (buy leg realized %s)", got, wantRows, want.LegRealized[0])
	}
	sum := new(big.Int)
	for i := range wantRows {
		if got[i].block != wantRows[i].block || got[i].txIndex != wantRows[i].txIndex || !got[i].at.Equal(wantRows[i].at) || got[i].pnl != wantRows[i].pnl {
			t.Fatalf("leg %d = %+v, want %+v", i, got[i], wantRows[i])
		}
		v, _ := new(big.Int).SetString(got[i].pnl, 10)
		sum.Add(sum, v)
	}
	if sum.Cmp(want.RealizedPnL) != 0 {
		t.Fatalf("sum of legs = %s, realized = %s", sum, want.RealizedPnL)
	}

	// A pair with no ledger at all (wallet 0xc9 never touched the token) loses stale leg rows on
	// recompute, like TestPositionEmptyLegsDeletesRow does for its position row.
	l.exec(`INSERT INTO wallet_realized_legs (chain_id, wallet, token_address, block_number, transaction_index, block_time, realized_pnl)
		VALUES ($1, $2, $3, 12, 0, $4, 77)`, walletChainID, addressBytes(0xc9), positionToken, l.blockTime(12))
	l.mark(0xc9)
	drainPositions(t, ctx, f)
	var stale int
	if err := f.database.DB.QueryRowContext(ctx, `SELECT count(*) FROM wallet_realized_legs WHERE chain_id = $1 AND wallet = $2`,
		walletChainID, addressBytes(0xc9)).Scan(&stale); err != nil || stale != 0 {
		t.Fatalf("stale legs after empty recompute = %d (%v)", stale, err)
	}
	// 0xc1's legs are untouched by another pair's recompute.
	if rest := readLegs(); len(rest) != len(wantRows) {
		t.Fatalf("legs after another pair's recompute = %+v", rest)
	}
}
