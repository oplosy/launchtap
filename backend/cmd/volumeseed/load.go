package main

import (
	"context"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const copyBatchRows = 20_000

type copyTable struct {
	name    string
	columns []string
	rows    [][]any
}

// copySink streams the generated ledger into PostgreSQL with COPY inside one transaction. The
// ledger's foreign keys and engine triggers are deferred, so tables may be flushed in any order
// and are checked once at commit.
type copySink struct {
	ctx     context.Context
	tx      pgx.Tx
	chainID int64
	tables  map[string]*copyTable
}

var eventColumns = []string{"chain_id", "block_number", "block_hash", "block_time", "transaction_index", "tx_hash", "log_index"}

func newCopySink(ctx context.Context, tx pgx.Tx, chainID int64) *copySink {
	table := func(name string, columns ...string) *copyTable {
		return &copyTable{name: name, columns: columns}
	}
	withEvent := func(columns ...string) []string { return append(append([]string{}, eventColumns...), columns...) }
	s := &copySink{ctx: ctx, tx: tx, chainID: chainID, tables: map[string]*copyTable{}}
	for _, t := range []*copyTable{
		table("indexed_blocks", "chain_id", "block_number", "block_hash", "parent_hash", "block_time", "finality_status"),
		table("token_launches", withEvent("token_address", "curve_address", "creator", "lp_pair", "weth", "protocol_treasury",
			"engine_version", "name", "symbol", "total_supply", "virtual_eth", "virtual_token", "curve_tokens", "lp_tokens",
			"graduation_eth", "launch_fee_paid", "trade_fee_bps", "protocol_share_bps")...),
		table("trades", withEvent("token_address", "trader", "is_buy", "eth_gross", "eth_refund", "token_amount",
			"protocol_fee", "creator_fee", "new_eth_reserve", "new_token_reserve")...),
		table("transfers", withEvent("token_address", "from_address", "to_address", "value")...),
		table("graduations", withEvent("token_address", "lp_pair", "eth_to_pool", "tokens_to_pool", "lp_liquidity_burned")...),
		table("pool_syncs", withEvent("pair_address", "reserve0", "reserve1")...),
		table("pool_mints", withEvent("pair_address", "sender", "amount0", "amount1")...),
		table("pool_swaps", withEvent("pair_address", "sender", "amount0_in", "amount1_in", "amount0_out", "amount1_out", "to_address", "tx_from")...),
	} {
		s.tables[t.name] = t
	}
	return s
}

func numeric(v *big.Int) pgtype.Numeric { return pgtype.Numeric{Int: new(big.Int).Set(v), Valid: true} }

func (s *copySink) event(c coords, logIndex int32, values ...any) []any {
	return append([]any{s.chainID, c.BlockNumber, c.BlockHash[:], c.BlockTime, int32(0), c.TxHash[:], logIndex}, values...)
}

func (s *copySink) add(table string, row []any) error {
	t := s.tables[table]
	t.rows = append(t.rows, row)
	if len(t.rows) >= copyBatchRows {
		return s.flushTable(t)
	}
	return nil
}

func (s *copySink) flushTable(t *copyTable) error {
	if len(t.rows) == 0 {
		return nil
	}
	if _, err := s.tx.CopyFrom(s.ctx, pgx.Identifier{t.name}, t.columns, pgx.CopyFromRows(t.rows)); err != nil {
		return fmt.Errorf("copy %d rows into %s: %w", len(t.rows), t.name, err)
	}
	t.rows = t.rows[:0]
	return nil
}

func (s *copySink) flush() error {
	for _, t := range s.tables {
		if err := s.flushTable(t); err != nil {
			return err
		}
	}
	return nil
}

func (s *copySink) block(b block) error {
	return s.add("indexed_blocks", []any{s.chainID, b.Number, b.Hash[:], b.ParentHash[:], b.Time, "finalized"})
}

func (s *copySink) launch(r launchRow) error {
	return s.add("token_launches", s.event(r.coords, 1, r.Token[:], r.Curve[:], r.Creator[:], r.Pair[:], wethAddress[:], treasuryWallet[:],
		int32(1), r.Name, r.Symbol, numeric(totalSupply), numeric(virtualETH), numeric(virtualToken), numeric(curveTokens),
		numeric(lpTokens), numeric(graduationETH), numeric(launchFee), int32(tradeFeeBPS), int32(protocolShareBPS)))
}

func (s *copySink) trade(r tradeRow) error {
	return s.add("trades", s.event(r.coords, r.LogIndex, r.Token[:], r.Trader[:], r.IsBuy, numeric(r.ETHGross), numeric(new(big.Int)),
		numeric(r.TokenAmount), numeric(r.ProtocolFee), numeric(r.CreatorFee), numeric(r.NewETHReserve), numeric(r.NewTokenReserve)))
}

func (s *copySink) transfer(r transferRow) error {
	return s.add("transfers", s.event(r.coords, r.LogIndex, r.Token[:], r.From[:], r.To[:], numeric(r.Value)))
}

func (s *copySink) graduation(r graduationRow) error {
	return s.add("graduations", s.event(r.coords, 3, r.Token[:], r.Pair[:], numeric(r.ETHToPool), numeric(r.TokensToPool), numeric(r.LiquidityBurn)))
}

func (s *copySink) poolSync(r poolSyncRow) error {
	return s.add("pool_syncs", s.event(r.coords, r.LogIndex, r.Pair[:], numeric(r.Reserve0), numeric(r.Reserve1)))
}

func (s *copySink) poolMint(r poolMintRow) error {
	return s.add("pool_mints", s.event(r.coords, r.LogIndex, r.Pair[:], routerAddress[:], numeric(r.Amount0), numeric(r.Amount1)))
}

func (s *copySink) poolSwap(r poolSwapRow) error {
	return s.add("pool_swaps", s.event(r.coords, r.LogIndex, r.Pair[:], routerAddress[:], numeric(r.Amount0In), numeric(r.Amount1In),
		numeric(r.Amount0Out), numeric(r.Amount1Out), r.Trader[:], r.Trader[:]))
}
