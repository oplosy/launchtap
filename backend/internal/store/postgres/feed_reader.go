package postgres

import (
	"errors"
	"time"

	"context"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/feed"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FeedReader serves the live-feed tail queries across all tokens of a chain.
type FeedReader struct {
	Pool *pgxpool.Pool
	// DeploymentID scopes the read snapshot of RecentTradesSnapshot; the tail queries ignore it.
	DeploymentID string
}

var _ feed.Reader = FeedReader{}

func checkFeedLimit(limit int) error {
	if limit < 1 || limit > 1000 {
		return fmt.Errorf("feed page size must be between 1 and 1000")
	}
	return nil
}

func (r FeedReader) TradesAfter(ctx context.Context, chainID int64, after feed.Cursor, limit int) ([]feed.Trade, error) {
	if err := checkFeedLimit(limit); err != nil {
		return nil, err
	}
	rows, err := sqlc.New(r.Pool).ListMarketTradesAfter(ctx, sqlc.ListMarketTradesAfterParams{
		ChainID: chainID, AfterBlock: after.Block, AfterTransactionIndex: after.TxIndex, AfterLogIndex: after.LogIndex, PageSize: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]feed.Trade, 0, len(rows))
	for _, row := range rows {
		out = append(out, feedTrade(row.TokenAddress, row.Name, row.Symbol, row.Source, row.TraderHex, row.SideBuy, row.GrossEthVolume, row.TokenVolume,
			row.BlockNumber, row.TransactionIndex, row.TxHash, row.LogIndex, row.BlockTime.Time, row.Finality))
	}
	return out, nil
}

func (r FeedReader) RecentTrades(ctx context.Context, chainID int64, limit int) ([]feed.Trade, error) {
	if err := checkFeedLimit(limit); err != nil {
		return nil, err
	}
	rows, err := sqlc.New(r.Pool).ListRecentMarketTrades(ctx, sqlc.ListRecentMarketTradesParams{ChainID: chainID, PageSize: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]feed.Trade, 0, len(rows))
	for _, row := range rows {
		out = append(out, feedTrade(row.TokenAddress, row.Name, row.Symbol, row.Source, row.TraderHex, row.SideBuy, row.GrossEthVolume, row.TokenVolume,
			row.BlockNumber, row.TransactionIndex, row.TxHash, row.LogIndex, row.BlockTime.Time, row.Finality))
	}
	return out, nil
}

// RecentTradesSnapshot returns the newest market trades with the canonical read snapshot they were read at.
func (r FeedReader) RecentTradesSnapshot(ctx context.Context, chainID int64, limit int) (feed.RecentPage, error) {
	if err := checkFeedLimit(limit); err != nil {
		return feed.RecentPage{}, err
	}
	var out feed.RecentPage
	err := withReadSnapshotBeginner(ctx, r.Pool, chainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		rows, err := a.queries.ListRecentMarketTrades(ctx, sqlc.ListRecentMarketTradesParams{ChainID: chainID, PageSize: int32(limit)})
		if err != nil {
			return err
		}
		out.Items = make([]feed.Trade, 0, len(rows))
		for _, row := range rows {
			out.Items = append(out.Items, feedTrade(row.TokenAddress, row.Name, row.Symbol, row.Source, row.TraderHex, row.SideBuy, row.GrossEthVolume, row.TokenVolume,
				row.BlockNumber, row.TransactionIndex, row.TxHash, row.LogIndex, row.BlockTime.Time, row.Finality))
		}
		out.Snapshot = s.Identity
		out.Finality = finality(s.State, s.Identity.BlockNumber)
		return nil
	})
	return out, err
}

func feedTrade(token sqlc.Address, name, symbol, source, traderHex string, buy bool, eth, tokens sqlc.Uint256,
	block int64, txIndex int32, txHash sqlc.Hash, logIndex int32, at time.Time, finality string) feed.Trade {
	trade := feed.Trade{
		Token: common.Address(token), Name: name, Symbol: symbol, Source: source, Buy: buy,
		ETHVolume: eth.BigInt(), TokenVolume: tokens.BigInt(),
		Block: block, TxIndex: txIndex, TxHash: common.Hash(txHash), LogIndex: logIndex, Time: at, Finality: finality,
	}
	if traderHex != "" {
		trader := common.HexToAddress("0x" + traderHex)
		trade.Trader = &trader
	}
	return trade
}

func (r FeedReader) GraduationsAfter(ctx context.Context, chainID int64, after feed.Cursor, limit int) ([]feed.Graduation, error) {
	if err := checkFeedLimit(limit); err != nil {
		return nil, err
	}
	rows, err := sqlc.New(r.Pool).ListGraduationsAfter(ctx, sqlc.ListGraduationsAfterParams{
		ChainID: chainID, AfterBlock: after.Block, AfterTransactionIndex: after.TxIndex, AfterLogIndex: after.LogIndex, PageSize: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]feed.Graduation, 0, len(rows))
	for _, row := range rows {
		out = append(out, feed.Graduation{
			Token: common.Address(row.TokenAddress), Pair: addressOrZero(row.LpPair), Name: row.Name, Symbol: row.Symbol,
			TxHash: common.Hash(row.TxHash), Block: row.BlockNumber, TxIndex: row.TransactionIndex, LogIndex: row.LogIndex,
		})
	}
	return out, nil
}

func (r FeedReader) LaunchesAfter(ctx context.Context, chainID int64, after feed.Cursor, limit int) ([]feed.Launch, error) {
	if err := checkFeedLimit(limit); err != nil {
		return nil, err
	}
	rows, err := sqlc.New(r.Pool).ListLaunchesAfter(ctx, sqlc.ListLaunchesAfterParams{
		ChainID: chainID, AfterBlock: after.Block, AfterTransactionIndex: after.TxIndex, AfterLogIndex: after.LogIndex, PageSize: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]feed.Launch, 0, len(rows))
	for _, row := range rows {
		out = append(out, feed.Launch{
			Token: common.Address(row.TokenAddress), Creator: common.Address(row.Creator), Name: row.Name, Symbol: row.Symbol,
			TxHash: common.Hash(row.TxHash), Block: row.BlockNumber, TxIndex: row.TransactionIndex, LogIndex: row.LogIndex,
		})
	}
	return out, nil
}

func (r FeedReader) Newest(ctx context.Context, chainID int64) (trade, graduation, launch feed.Cursor, err error) {
	row, err := sqlc.New(r.Pool).GetNewestFeedCursors(ctx, chainID)
	if err != nil {
		return feed.Cursor{}, feed.Cursor{}, feed.Cursor{}, err
	}
	return feed.Cursor{Block: row.TradeBlock, TxIndex: row.TradeTransactionIndex, LogIndex: row.TradeLogIndex},
		feed.Cursor{Block: row.GraduationBlock, TxIndex: row.GraduationTransactionIndex, LogIndex: row.GraduationLogIndex},
		feed.Cursor{Block: row.LaunchBlock, TxIndex: row.LaunchTransactionIndex, LogIndex: row.LaunchLogIndex}, nil
}

func (r FeedReader) Watermarks(ctx context.Context, chainID int64, deploymentID string) (feed.Watermarks, error) {
	row, err := sqlc.New(r.Pool).GetSyncWatermarks(ctx, sqlc.GetSyncWatermarksParams{ChainID: chainID, DeploymentID: deploymentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return feed.Watermarks{}, nil
	}
	if err != nil {
		return feed.Watermarks{}, err
	}
	return feed.Watermarks{Safe: row.SafeNumber, Finalized: row.FinalizedNumber}, nil
}
