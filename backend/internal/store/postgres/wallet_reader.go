package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/Contictus/launchtap/backend/internal/trading"
	"github.com/Contictus/launchtap/backend/internal/wallet"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	walletPositionsEndpoint = "wallet_positions"
	walletTradesEndpoint    = "wallet_trades"
	walletCreatedEndpoint   = "wallet_created"
	positionsModeClosed     = "closed"
)

// WalletReader serves the public wallet page and leaderboard reads. Every method reads inside one
// read snapshot, so a response never mixes two blocks.
type WalletReader struct {
	Pool         *pgxpool.Pool
	DeploymentID string
	// OpenPositionCap overrides wallet.MaxOpenPositions when positive (tests).
	OpenPositionCap int
}

var _ wallet.Reader = WalletReader{}

func checkWalletLimit(limit int) error {
	if limit < 1 || limit > 100 {
		return fmt.Errorf("wallet page size must be between 1 and 100")
	}
	return nil
}

func walletStamp(s ReadSnapshot) wallet.Stamp {
	return wallet.Stamp{Snapshot: s.Identity, Finality: finality(s.State, s.Identity.BlockNumber)}
}

// GetWallet returns the wallet card. A wallet without a wallet_totals row has no activity and reads as zeros.
func (r WalletReader) GetWallet(ctx context.Context, chainID int64, address common.Address) (wallet.Overview, error) {
	out := wallet.Overview{Address: address, RealizedPnL: new(big.Int), UnrealizedPnL: new(big.Int), PortfolioValue: new(big.Int)}
	err := withReadSnapshotBeginner(ctx, r.Pool, chainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		out.Stamp = walletStamp(s)
		totals, err := a.queries.GetWalletTotals(ctx, sqlc.GetWalletTotalsParams{ChainID: chainID, Wallet: sqlc.Address(address)})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		default:
			if out.RealizedPnL, err = signedBigInt("wallet realized pnl", totals.RealizedPnl); err != nil {
				return err
			}
			first, last := totals.FirstBlockNumber, totals.LastBlockNumber
			out.HeldCount, out.TokensTraded, out.FirstBlock, out.LastBlock = totals.HeldCount, totals.TokensTraded, &first, &last
		}
		valuation, err := a.queries.GetWalletValuation(ctx, sqlc.GetWalletValuationParams{ChainID: chainID, Wallet: sqlc.Address(address)})
		if err != nil {
			return err
		}
		out.PortfolioValue = numericBig(valuation.PortfolioValue)
		out.UnrealizedPnL = new(big.Int).Sub(out.PortfolioValue, numericBig(valuation.OpenCost))
		if out.CreatedCount, err = a.queries.CountWalletCreated(ctx, sqlc.CountWalletCreatedParams{ChainID: chainID, Wallet: sqlc.Address(address)}); err != nil {
			return err
		}
		out.AttributionComplete, err = a.queries.WalletAttributionComplete(ctx, chainID)
		return err
	})
	return out, err
}

func walletCursorFilter(address common.Address) string { return cursorFilter(address.Hex()) }

// keyAddress parses the token address component of a keyset cursor.
func keyAddress(value string) ([]byte, error) {
	if !common.IsHexAddress(value) {
		return nil, pagination.ErrInvalidCursor
	}
	return common.HexToAddress(value).Bytes(), nil
}

func keyBlock(value string) (int64, error) {
	block, err := strconv.ParseInt(value, 10, 64)
	if err != nil || block < 0 {
		return 0, pagination.ErrInvalidCursor
	}
	return block, nil
}

// ListPositions returns open positions (balance > 0) in one page ordered by live value, largest first,
// capped at the reader's open-position cap with Truncated set beyond it, or closed positions
// (balance 0) newest first with a keyset cursor. Open mode has no cursor and ignores Limit: its order
// is a live value that reserve changes move between requests, so no stable keyset exists. A cursor in
// open mode, or one issued for the other mode, is wallet.ErrCursorMode.
func (r WalletReader) ListPositions(ctx context.Context, q wallet.PositionsQuery) (wallet.PositionsPage, error) {
	if !q.Closed && q.Cursor != nil {
		return wallet.PositionsPage{}, wallet.ErrCursorMode
	}
	if q.Closed {
		if err := checkWalletLimit(q.Limit); err != nil {
			return wallet.PositionsPage{}, err
		}
	}
	openCap := r.OpenPositionCap
	if openCap <= 0 {
		openCap = wallet.MaxOpenPositions
	}
	var out wallet.PositionsPage
	err := withReadSnapshotBeginner(ctx, r.Pool, q.ChainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		out.Stamp = walletStamp(s)
		out.Items = []wallet.Position{}
		if !q.Closed {
			rows, err := a.queries.ListWalletOpenPositions(ctx, sqlc.ListWalletOpenPositionsParams{
				ChainID: q.ChainID, Wallet: sqlc.Address(q.Wallet), PageSize: int32(openCap + 1),
			})
			if err != nil {
				return err
			}
			if len(rows) > openCap {
				rows, out.Truncated = rows[:openCap], true
			}
			for _, row := range rows {
				realized, err := signedBigInt("position realized pnl", row.RealizedPnl)
				if err != nil {
					return err
				}
				out.Items = append(out.Items, wallet.Position{
					Token: common.Address(row.TokenAddress), Name: row.Name, Symbol: row.Symbol, Phase: row.Phase,
					Balance: row.Balance.BigInt(), RemainingCost: row.RemainingCost.BigInt(), RealizedPnL: realized,
					CurrentValue: numericBig(row.CurrentValue), TransferredIn: row.TransferredIn.BigInt(), TradeCount: row.TradeCount, LastBlock: row.LastBlockNumber,
				})
			}
			return nil
		}
		filters := walletCursorFilter(q.Wallet)
		var afterBlock pgtype.Int8
		var afterAddress []byte
		if q.Cursor != nil {
			if q.Cursor.Endpoint == walletPositionsEndpoint && q.Cursor.Sort != positionsModeClosed {
				return wallet.ErrCursorMode
			}
			if err := q.Cursor.ValidateRequest(walletPositionsEndpoint, positionsModeClosed, filters, "next", s.Identity, a.canonicalCursorCheck(ctx)); err != nil {
				return err
			}
			if len(q.Cursor.Key) != 2 {
				return pagination.ErrInvalidCursor
			}
			var err error
			if afterAddress, err = keyAddress(q.Cursor.Key[1]); err != nil {
				return err
			}
			block, err := keyBlock(q.Cursor.Key[0])
			if err != nil {
				return err
			}
			afterBlock = pgtype.Int8{Int64: block, Valid: true}
		}
		rows, err := a.queries.ListWalletClosedPositions(ctx, sqlc.ListWalletClosedPositionsParams{
			ChainID: q.ChainID, Wallet: sqlc.Address(q.Wallet), AfterBlock: afterBlock, AfterAddress: afterAddress, PageSize: int32(q.Limit),
		})
		if err != nil {
			return err
		}
		var lastKey []string
		for _, row := range rows {
			realized, err := signedBigInt("position realized pnl", row.RealizedPnl)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, wallet.Position{
				Token: common.Address(row.TokenAddress), Name: row.Name, Symbol: row.Symbol, Phase: row.Phase,
				Balance: row.Balance.BigInt(), RemainingCost: row.RemainingCost.BigInt(), RealizedPnL: realized,
				CurrentValue: new(big.Int), TransferredIn: row.TransferredIn.BigInt(), TradeCount: row.TradeCount, LastBlock: row.LastBlockNumber,
			})
			lastKey = []string{strconv.FormatInt(row.LastBlockNumber, 10), common.Address(row.TokenAddress).Hex()}
		}
		if len(out.Items) == q.Limit {
			out.NextCursor, _ = pagination.Encode(pagination.Cursor{Version: pagination.CurrentVersion, Snapshot: s.Identity, Endpoint: walletPositionsEndpoint, Sort: positionsModeClosed, Filters: filters, Direction: "next", Key: lastKey})
		}
		return nil
	})
	return out, err
}

// ListTrades returns the wallet's market trades, newest first: curve trades by trader and pool swaps
// by transaction sender (market_trades.trader).
func (r WalletReader) ListTrades(ctx context.Context, q wallet.TradesQuery) (wallet.TradesPage, error) {
	if err := checkWalletLimit(q.Limit); err != nil {
		return wallet.TradesPage{}, err
	}
	var out wallet.TradesPage
	err := withReadSnapshotBeginner(ctx, r.Pool, q.ChainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		filters := walletCursorFilter(q.Wallet)
		args := sqlc.ListWalletTradesParams{ChainID: q.ChainID, Wallet: q.Wallet.Bytes(), PageSize: int32(q.Limit)}
		if q.Cursor != nil {
			if err := q.Cursor.ValidateRequest(walletTradesEndpoint, "newest", filters, "next", s.Identity, a.canonicalCursorCheck(ctx)); err != nil {
				return err
			}
			if len(q.Cursor.Key) != 3 {
				return pagination.ErrInvalidCursor
			}
			block, err := keyBlock(q.Cursor.Key[0])
			if err != nil {
				return err
			}
			txIndex, err := strconv.ParseInt(q.Cursor.Key[1], 10, 32)
			if err != nil {
				return pagination.ErrInvalidCursor
			}
			logIndex, err := strconv.ParseInt(q.Cursor.Key[2], 10, 32)
			if err != nil {
				return pagination.ErrInvalidCursor
			}
			args.AfterBlock = pgtype.Int8{Int64: block, Valid: true}
			args.AfterTransactionIndex = pgtype.Int4{Int32: int32(txIndex), Valid: true}
			args.AfterLogIndex = pgtype.Int4{Int32: int32(logIndex), Valid: true}
		}
		rows, err := a.queries.ListWalletTrades(ctx, args)
		if err != nil {
			return err
		}
		out.Stamp = walletStamp(s)
		out.Items = make([]wallet.Trade, 0, len(rows))
		trader := q.Wallet
		for _, row := range rows {
			out.Items = append(out.Items, wallet.Trade{
				Token: common.Address(row.TokenAddress), Name: row.Name, Symbol: row.Symbol,
				Trade: trading.Trade{
					Source: row.Source, Trader: &trader, Buy: row.SideBuy, ExecutionPrice: numericBig(row.ExecutionPriceWad), SpotPrice: numericBig(row.SpotPriceWad),
					ETHVolume: row.GrossEthVolume.BigInt(), TokenVolume: row.TokenVolume.BigInt(), BlockNumber: row.BlockNumber,
					TransactionIndex: row.TransactionIndex, TxHash: common.Hash(row.TxHash), LogIndex: row.LogIndex, Time: row.BlockTime.Time, Finality: row.Finality,
				},
			})
		}
		if len(out.Items) == q.Limit {
			last := out.Items[len(out.Items)-1]
			out.NextCursor, _ = pagination.Encode(pagination.Cursor{Version: pagination.CurrentVersion, Snapshot: s.Identity, Endpoint: walletTradesEndpoint, Sort: "newest", Filters: filters, Direction: "next",
				Key: []string{strconv.FormatInt(last.BlockNumber, 10), strconv.FormatInt(int64(last.TransactionIndex), 10), strconv.FormatInt(int64(last.LogIndex), 10)}})
		}
		return nil
	})
	return out, err
}

// ListCreated returns the tokens the wallet launched, newest launch first.
func (r WalletReader) ListCreated(ctx context.Context, q wallet.CreatedQuery) (wallet.CreatedPage, error) {
	if err := checkWalletLimit(q.Limit); err != nil {
		return wallet.CreatedPage{}, err
	}
	var out wallet.CreatedPage
	err := withReadSnapshotBeginner(ctx, r.Pool, q.ChainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		filters := walletCursorFilter(q.Wallet)
		args := sqlc.ListWalletCreatedParams{ChainID: q.ChainID, Wallet: sqlc.Address(q.Wallet), PageSize: int32(q.Limit)}
		if q.Cursor != nil {
			if err := q.Cursor.ValidateRequest(walletCreatedEndpoint, "newest", filters, "next", s.Identity, a.canonicalCursorCheck(ctx)); err != nil {
				return err
			}
			if len(q.Cursor.Key) != 2 {
				return pagination.ErrInvalidCursor
			}
			block, err := keyBlock(q.Cursor.Key[0])
			if err != nil {
				return err
			}
			address, err := keyAddress(q.Cursor.Key[1])
			if err != nil {
				return err
			}
			args.AfterBlock, args.AfterAddress = pgtype.Int8{Int64: block, Valid: true}, address
		}
		rows, err := a.queries.ListWalletCreated(ctx, args)
		if err != nil {
			return err
		}
		out.Stamp = walletStamp(s)
		out.Items = make([]wallet.Created, 0, len(rows))
		for _, row := range rows {
			out.Items = append(out.Items, wallet.Created{
				Token: common.Address(row.TokenAddress), Name: row.Name, Symbol: row.Symbol, Phase: row.Phase,
				MarketCap: row.MarketCapEthWad.BigInt(), LaunchBlock: row.LaunchBlockNumber,
			})
		}
		if len(out.Items) == q.Limit {
			last := out.Items[len(out.Items)-1]
			out.NextCursor, _ = pagination.Encode(pagination.Cursor{Version: pagination.CurrentVersion, Snapshot: s.Identity, Endpoint: walletCreatedEndpoint, Sort: "newest", Filters: filters, Direction: "next",
				Key: []string{strconv.FormatInt(last.LaunchBlock, 10), last.Token.Hex()}})
		}
		return nil
	})
	return out, err
}

// Leaderboard ranks wallets with a positive realized PnL (highest first, ties by address),
// excluding the zero address and every curve and pair address. It may return fewer than limit.
func (r WalletReader) Leaderboard(ctx context.Context, chainID int64, limit int) (wallet.LeaderboardPage, error) {
	if err := checkWalletLimit(limit); err != nil {
		return wallet.LeaderboardPage{}, err
	}
	var out wallet.LeaderboardPage
	err := withReadSnapshotBeginner(ctx, r.Pool, chainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		rows, err := a.queries.ListLeaderboard(ctx, sqlc.ListLeaderboardParams{ChainID: chainID, PageSize: int32(limit)})
		if err != nil {
			return err
		}
		out.Stamp = walletStamp(s)
		out.Items = make([]wallet.Ranked, 0, len(rows))
		for i, row := range rows {
			realized, err := signedBigInt("wallet realized pnl", row.RealizedPnl)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, wallet.Ranked{Rank: i + 1, Address: common.Address(row.Wallet), RealizedPnL: realized, TokensTraded: row.TokensTraded})
		}
		return nil
	})
	return out, err
}
