package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/Contictus/launchtap/backend/internal/holder"
	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/Contictus/launchtap/backend/internal/stats"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/Contictus/launchtap/backend/internal/trading"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func finality(state SyncState, block int64) string {
	if state.FinalizedNumber.Valid && block <= state.FinalizedNumber.Int64 {
		return "finalized"
	}
	if state.SafeNumber.Valid && block <= state.SafeNumber.Int64 {
		return "safe"
	}
	return "provisional"
}

func (r TokenReader) Get(ctx context.Context, chainID int64, address common.Address) (token.Detail, error) {
	var out token.Detail
	err := withReadSnapshotBeginner(ctx, r.Pool, chainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		row, err := a.queries.GetTokenDetail(ctx, sqlc.GetTokenDetailParams{ChainID: chainID, TokenAddress: sqlc.Address(address)})
		if errors.Is(err, pgx.ErrNoRows) {
			return token.ErrNotFound
		}
		if err != nil {
			return err
		}
		out = token.Detail{
			Summary: token.Summary{
				Address: common.Address(row.TokenAddress), Name: row.Name, Symbol: row.Symbol, Phase: row.Phase,
				LaunchBlock: row.LaunchBlockNumber, LaunchTime: row.LaunchBlockTime.Time, TotalSupply: row.TotalSupply.BigInt(),
				MarketCapETH: row.MarketCapEthWad.BigInt(), Volume24hETH: row.Volume24hEthWad.BigInt(), HolderCount: row.HolderCount,
			},
			Curve: common.Address(row.CurveAddress), Pair: common.Address(row.LpPair),
			WETH: common.Address(row.Weth), Creator: common.Address(row.Creator), ProtocolTreasury: common.Address(row.ProtocolTreasury),
			EngineVersion:     uint16(row.EngineVersion),
			InitialVirtualETH: row.InitialVirtualEth.BigInt(), InitialVirtualToken: row.InitialVirtualToken.BigInt(),
			CurveTokens: row.CurveTokens.BigInt(), LPTokens: row.LpTokens.BigInt(), GraduationETH: row.GraduationEth.BigInt(),
			TradeFeeBPS: uint16(row.TradeFeeBps), ProtocolShareBPS: uint16(row.ProtocolShareBps), ReserveSource: row.ReserveSource,
			ETHReserve: row.EthReserve.BigInt(), TokenReserve: row.TokenReserve.BigInt(), RealCurveETH: numericBig(row.RealCurveEth),
			GraduationProgressBPS: row.GraduationProgressBps, ReserveBlock: row.ReserveBlockNumber,
			ReserveHash: common.Hash(row.ReserveBlockHash), Description: row.Description.String, ImageURL: row.ImageUrl.String,
			XURL: row.XUrl.String, TelegramURL: row.TelegramUrl.String, SpotPriceETH: row.SpotPriceEthWad.BigInt(),
			FDVETH: row.FdvEthWad.BigInt(), LiquidityETH: row.LiquidityEthWad.BigInt(), ATHPriceETH: row.AthPriceEthWad.BigInt(),
			ATHAt: row.AthAt.Time, PriceChange24hBPS: row.PriceChange24hBps,
		}
		out.Snapshot = s.Identity
		out.Finality = finality(s.State, s.Identity.BlockNumber)
		return nil
	})
	return out, err
}

func (r TokenReader) ReadQuoteState(ctx context.Context, chainID int64, address common.Address) (token.QuoteState, error) {
	var out token.QuoteState
	err := withReadSnapshotBeginner(ctx, r.Pool, chainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		row, err := a.queries.GetTokenQuoteState(ctx, sqlc.GetTokenQuoteStateParams{ChainID: chainID, TokenAddress: sqlc.Address(address)})
		if errors.Is(err, pgx.ErrNoRows) {
			return token.ErrNotFound
		}
		if err != nil {
			return err
		}
		out.Detail.Phase = row.Phase
		out.Detail.TotalSupply = row.TotalSupply.BigInt()
		out.Detail.CurveTokens = row.CurveTokens.BigInt()
		out.Detail.LPTokens = row.LpTokens.BigInt()
		out.Detail.GraduationETH = row.GraduationEth.BigInt()
		out.Detail.InitialVirtualETH = row.InitialVirtualEth.BigInt()
		out.Detail.InitialVirtualToken = row.InitialVirtualToken.BigInt()
		out.Detail.TradeFeeBPS = uint16(row.TradeFeeBps)
		out.Detail.ProtocolShareBPS = uint16(row.ProtocolShareBps)
		out.Detail.ETHReserve = row.EthReserve.BigInt()
		out.Detail.TokenReserve = row.TokenReserve.BigInt()
		out.Detail.ReserveBlock = row.ReserveBlockNumber
		out.Detail.ReserveHash = common.Hash(row.ReserveBlockHash)
		out.Detail.Snapshot = s.Identity
		out.Detail.Finality = finality(s.State, s.Identity.BlockNumber)
		// Accumulated fees only feed the curve's uint256 overflow guard, which no real ETH
		// balance can reach. Summing every trade per quote made quotes O(trades), so the
		// informational quote starts from zero accrued fees.
		out.ProtocolFees = new(big.Int)
		out.CreatorFees = new(big.Int)
		return nil
	})
	return out, err
}

type MarketReader struct {
	Pool         PoolReadBeginner
	DeploymentID string
}

func (r MarketReader) ListTrades(ctx context.Context, q trading.Query) (trading.Page, error) {
	if q.Limit < 1 || q.Limit > 100 {
		return trading.Page{}, fmt.Errorf("trade page size must be between 1 and 100")
	}
	var out trading.Page
	err := withReadSnapshotBeginner(ctx, r.Pool, q.ChainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		filters := cursorFilter(q.Token.Hex())
		exists, err := a.queries.TokenExists(ctx, sqlc.TokenExistsParams{ChainID: q.ChainID, TokenAddress: sqlc.Address(q.Token)})
		if err != nil {
			return err
		}
		if !exists {
			return token.ErrNotFound
		}
		args := sqlc.ListMarketTradesParams{ChainID: q.ChainID, TokenAddress: sqlc.Address(q.Token), PageSize: int32(q.Limit)}
		if q.Cursor != nil {
			if err := q.Cursor.ValidateRequest("trades", "newest", filters, "next", s.Identity, a.canonicalCursorCheck(ctx)); err != nil {
				return err
			}
			if len(q.Cursor.Key) != 3 {
				return pagination.ErrInvalidCursor
			}
			b, e := strconv.ParseInt(q.Cursor.Key[0], 10, 64)
			if e != nil {
				return pagination.ErrInvalidCursor
			}
			tx, e := strconv.ParseInt(q.Cursor.Key[1], 10, 32)
			if e != nil {
				return pagination.ErrInvalidCursor
			}
			li, e := strconv.ParseInt(q.Cursor.Key[2], 10, 32)
			if e != nil {
				return pagination.ErrInvalidCursor
			}
			args.AfterBlock = pgtype.Int8{Int64: b, Valid: true}
			args.AfterTransactionIndex = pgtype.Int4{Int32: int32(tx), Valid: true}
			args.AfterLogIndex = pgtype.Int4{Int32: int32(li), Valid: true}
		}
		rows, err := a.queries.ListMarketTrades(ctx, args)
		if err != nil {
			return err
		}
		for _, row := range rows {
			v := trading.Trade{Source: row.Source, Buy: row.SideBuy, ExecutionPrice: numericBig(row.ExecutionPriceWad), SpotPrice: numericBig(row.SpotPriceWad), ETHVolume: row.GrossEthVolume.BigInt(), TokenVolume: row.TokenVolume.BigInt(), BlockNumber: row.BlockNumber, TransactionIndex: row.TransactionIndex, TxHash: common.Hash(row.TxHash), LogIndex: row.LogIndex, Time: row.BlockTime.Time, Finality: row.Finality}
			if row.TraderHex != "" {
				x := common.HexToAddress("0x" + row.TraderHex)
				v.Trader = &x
			}
			out.Items = append(out.Items, v)
		}
		out.Snapshot = s.Identity
		out.Finality = finality(s.State, s.Identity.BlockNumber)
		if len(out.Items) == q.Limit {
			v := out.Items[len(out.Items)-1]
			out.NextCursor, _ = pagination.Encode(pagination.Cursor{Version: pagination.CurrentVersion, Snapshot: s.Identity, Endpoint: "trades", Sort: "newest", Filters: filters, Direction: "next", Key: []string{strconv.FormatInt(v.BlockNumber, 10), strconv.FormatInt(int64(v.TransactionIndex), 10), strconv.FormatInt(int64(v.LogIndex), 10)}})
		}
		return nil
	})
	return out, err
}

func (r MarketReader) ListHolders(ctx context.Context, q holder.Query) (holder.Page, error) {
	if q.Limit < 1 || q.Limit > 100 {
		return holder.Page{}, fmt.Errorf("holder page size must be between 1 and 100")
	}
	var out holder.Page
	err := withReadSnapshotBeginner(ctx, r.Pool, q.ChainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		filters := cursorFilter(q.Token.Hex())
		exists, err := a.queries.TokenExists(ctx, sqlc.TokenExistsParams{ChainID: q.ChainID, TokenAddress: sqlc.Address(q.Token)})
		if err != nil {
			return err
		}
		if !exists {
			return token.ErrNotFound
		}
		args := sqlc.ListTokenHoldersParams{ChainID: q.ChainID, TokenAddress: sqlc.Address(q.Token), PageSize: int32(q.Limit)}
		if q.Cursor != nil {
			if err := q.Cursor.ValidateRequest("holders", "balance", filters, "next", s.Identity, a.canonicalCursorCheck(ctx)); err != nil {
				return err
			}
			if len(q.Cursor.Key) != 2 || !common.IsHexAddress(q.Cursor.Key[1]) {
				return pagination.ErrInvalidCursor
			}
			balance, ok := new(big.Int).SetString(q.Cursor.Key[0], 10)
			if !ok || balance.Sign() < 0 {
				return pagination.ErrInvalidCursor
			}
			args.AfterBalance = pgtype.Numeric{Int: balance, Valid: true}
			args.AfterAddress = common.HexToAddress(q.Cursor.Key[1]).Bytes()
		}
		rows, err := a.queries.ListTokenHolders(ctx, args)
		if err != nil {
			return err
		}
		for _, row := range rows {
			out.Items = append(out.Items, holder.Balance{Address: common.Address(row.HolderAddress), Balance: row.Balance.BigInt(), FirstAcquiredBlock: row.FirstAcquiredBlockNumber.Int64})
		}
		out.Snapshot = s.Identity
		out.Finality = finality(s.State, s.Identity.BlockNumber)
		if len(out.Items) == q.Limit {
			v := out.Items[len(out.Items)-1]
			out.NextCursor, _ = pagination.Encode(pagination.Cursor{Version: pagination.CurrentVersion, Snapshot: s.Identity, Endpoint: "holders", Sort: "balance", Filters: filters, Direction: "next", Key: []string{v.Balance.String(), v.Address.Hex()}})
		}
		return nil
	})
	return out, err
}

type ProtocolReader struct {
	Pool         PoolReadBeginner
	DeploymentID string
}

func (r ProtocolReader) ReadProtocol(ctx context.Context, chainID int64) (stats.Protocol, error) {
	var out stats.Protocol
	err := withReadSnapshotBeginner(ctx, r.Pool, chainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		row, err := a.queries.GetProtocolStats(ctx, chainID)
		if errors.Is(err, pgx.ErrNoRows) {
			out.Volume24hETH = new(big.Int)
			out.VolumeAllTimeETH = new(big.Int)
		} else if err != nil {
			return err
		} else {
			out.Volume24hETH = row.Volume24hEthWad.BigInt()
			out.VolumeAllTimeETH = row.VolumeAllTimeEthWad.BigInt()
			out.Launches24h = int64(row.Launches24h)
			out.LaunchesAllTime = int64(row.LaunchesAllTime)
			out.Trades24h = int64(row.Trades24h)
			out.TradesAllTime = row.TradesAllTime
			out.Graduations24h = int64(row.Graduations24h)
			out.GraduationsAllTime = int64(row.GraduationsAllTime)
			out.UpdatedAt = row.UpdatedAt.Time
		}
		out.Snapshot = s.Identity
		out.Finality = finality(s.State, s.Identity.BlockNumber)
		return nil
	})
	return out, err
}

func (r ProtocolReader) ReadProtocolDaily(ctx context.Context, chainID int64, query stats.DailyQuery) (stats.DailyPage, error) {
	var out stats.DailyPage
	if query.Limit < 1 || query.Limit > 366 {
		return out, fmt.Errorf("protocol daily page size must be between 1 and 366")
	}
	from := time.Date(query.From.UTC().Year(), query.From.UTC().Month(), query.From.UTC().Day(), 0, 0, 0, 0, time.UTC)
	to := time.Date(query.To.UTC().Year(), query.To.UTC().Month(), query.To.UTC().Day(), 0, 0, 0, 0, time.UTC)
	if from.IsZero() || to.IsZero() || from.After(to) || to.Sub(from) > 366*24*time.Hour {
		return out, fmt.Errorf("protocol daily range must be 1 to 366 days")
	}
	err := withReadSnapshotBeginner(ctx, r.Pool, chainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		rows, err := a.queries.ListProtocolDaily(ctx, sqlc.ListProtocolDailyParams{
			ChainID:  chainID,
			FromDay:  pgtype.Date{Time: from, Valid: true},
			ToDay:    pgtype.Date{Time: to, Valid: true},
			PageSize: int32(query.Limit),
		})
		if err != nil {
			return err
		}
		out.Items = make([]stats.Daily, 0, len(rows))
		for _, row := range rows {
			out.Items = append(out.Items, stats.Daily{
				Day:         row.Day.Time.UTC(),
				VolumeETH:   row.VolumeEthWad.BigInt(),
				Launches:    int64(row.LaunchesCount),
				Trades:      int64(row.TradesCount),
				Graduations: int64(row.GraduationsCount),
			})
		}
		out.Snapshot = s.Identity
		out.Finality = finality(s.State, s.Identity.BlockNumber)
		return nil
	})
	return out, err
}

func numericBig(v pgtype.Numeric) *big.Int {
	n, _ := numericBigWithExponent(v, false)
	return n
}

// numericBigExact decodes a PostgreSQL NUMERIC without losing its exponent.
// Profile action amounts are NUMERIC(78,0), so a negative exponent is valid
// when it represents an exact integer (for example, 100e-2 = 1) and must
// reject a fractional value instead of truncating it silently.
func numericBigExact(v pgtype.Numeric) (*big.Int, error) {
	if !v.Valid || v.Int == nil {
		return nil, fmt.Errorf("numeric value is null")
	}
	if v.NaN || v.InfinityModifier != pgtype.Finite {
		return nil, fmt.Errorf("numeric value is not finite")
	}
	return numericBigWithExponent(v, true)
}

func numericBigWithExponent(v pgtype.Numeric, exact bool) (*big.Int, error) {
	if !v.Valid || v.Int == nil {
		return new(big.Int), nil
	}
	n := new(big.Int).Set(v.Int)
	if v.Exp > 0 {
		return n.Mul(n, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(v.Exp)), nil)), nil
	}
	if v.Exp == 0 {
		return n, nil
	}
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-v.Exp)), nil)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(n, divisor, remainder)
	if exact && remainder.Sign() != 0 {
		return nil, fmt.Errorf("numeric value is fractional")
	}
	return quotient, nil
}
