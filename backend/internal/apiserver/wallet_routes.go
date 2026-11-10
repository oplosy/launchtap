package apiserver

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"time"

	"github.com/Contictus/launchtap/backend/internal/wallet"
	"github.com/danielgtaylor/huma/v2"
	"github.com/ethereum/go-ethereum/common"
)

const (
	defaultWalletPage  = 50
	defaultLeaderboard = 100
)

// WalletRoutes serves the public wallet page and the realized-PnL leaderboard. Rate limiting and the
// read snapshot are the same as for the other public reads.
type WalletRoutes struct {
	Reader  wallet.Reader
	ChainID int64
}

type walletInput struct {
	Address string `path:"address"`
}

type walletPageInput struct {
	Address string `path:"address"`
	Cursor  string `query:"cursor"`
	Limit   int    `query:"limit" minimum:"1" maximum:"100" default:"50" doc:"Page size, 1-100."`
}

type walletPositionsInput struct {
	Address string `path:"address"`
	Cursor  string `query:"cursor" doc:"Closed mode only; supplying it with closed=false is rejected with 422."`
	Closed  bool   `query:"closed" doc:"List closed positions (balance 0) newest first, paginated. When false (default), all open positions are returned in one response ordered by current value, capped at 500 (see truncated)."`
	Limit   int    `query:"limit" minimum:"1" maximum:"100" default:"50" doc:"Page size, 1-100. Applies to closed mode only; open mode returns one capped page."`
}

type leaderboardInput struct {
	Window string `query:"window" doc:"Ranking period: all (default, all-time realized PnL), 7d or 30d (realized PnL booked in the last 7 or 30 days). Any other value is a 400 invalid_window."`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"100" doc:"Maximum number of wallets, 1-100. Only wallets with a positive realized PnL are ranked, so fewer may be returned."`
}

// walletDTO amounts are wei decimal strings; the PnL fields are signed.
type walletDTO struct {
	Snapshot            snapshotDTO `json:"snapshot"`
	Address             string      `json:"address"`
	RealizedPnLWei      string      `json:"realized_pnl_wei"`
	UnrealizedPnLWei    string      `json:"unrealized_pnl_wei"`
	PortfolioValueWei   string      `json:"portfolio_value_wei"`
	HeldCount           int64       `json:"held_count"`
	CreatedCount        int64       `json:"created_count"`
	TokensTraded        int64       `json:"tokens_traded"`
	FirstBlock          *int64      `json:"first_block"`
	LastBlock           *int64      `json:"last_block"`
	AttributionComplete bool        `json:"attribution_complete"`
	Username            *string     `json:"username" nullable:"true" doc:"Username published on this wallet, or null."`
}

type walletOutput struct{ Body walletDTO }

type walletPositionDTO struct {
	Token            string  `json:"token"`
	Name             string  `json:"name"`
	Symbol           string  `json:"symbol"`
	Phase            string  `json:"phase"`
	Balance          string  `json:"balance"`
	AverageCostWad   *string `json:"average_cost_wad"`
	RemainingCostWei string  `json:"remaining_cost_wei"`
	RealizedPnLWei   string  `json:"realized_pnl_wei"`
	CurrentValueWei  string  `json:"current_value_wei"`
	UnrealizedPnLWei string  `json:"unrealized_pnl_wei"`
	TransferredIn    string  `json:"transferred_in"`
	TradeCount       int64   `json:"trade_count"`
	LastBlock        int64   `json:"last_block"`
}

type walletPositionsBody struct {
	Snapshot   snapshotDTO         `json:"snapshot"`
	Items      []walletPositionDTO `json:"items"`
	Truncated  bool                `json:"truncated" doc:"Open mode only: true when the wallet has more open positions than the 500 returned."`
	NextCursor string              `json:"next_cursor,omitempty"`
}

type walletPositionsOutput struct{ Body walletPositionsBody }

// walletTradeDTO is the tradeDTO shape plus the token traded; the list spans many tokens. The fields
// are spelled out because huma does not flatten an unexported embedded struct into the schema.
type walletTradeDTO struct {
	Token            string  `json:"token"`
	Name             string  `json:"name"`
	Symbol           string  `json:"symbol"`
	Source           string  `json:"source"`
	Trader           *string `json:"trader"`
	Side             string  `json:"side"`
	ExecutionPrice   string  `json:"execution_price"`
	SpotPrice        string  `json:"spot_price"`
	ETHVolume        string  `json:"eth_volume"`
	TokenVolume      string  `json:"token_volume"`
	BlockNumber      int64   `json:"block_number"`
	TransactionIndex int32   `json:"transaction_index"`
	TxHash           string  `json:"tx_hash"`
	LogIndex         int32   `json:"log_index"`
	Time             string  `json:"time"`
	Finality         string  `json:"finality"`
}

type walletTradesBody struct {
	Snapshot   snapshotDTO      `json:"snapshot"`
	Items      []walletTradeDTO `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type walletTradesOutput struct{ Body walletTradesBody }

type walletCreatedDTO struct {
	Token        string `json:"token"`
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	Phase        string `json:"phase"`
	MarketCapWei string `json:"market_cap_wei"`
	LaunchBlock  int64  `json:"launch_block"`
}

type walletCreatedBody struct {
	Snapshot   snapshotDTO        `json:"snapshot"`
	Items      []walletCreatedDTO `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type walletCreatedOutput struct{ Body walletCreatedBody }

type leaderboardItemDTO struct {
	Rank           int     `json:"rank"`
	Address        string  `json:"address"`
	RealizedPnLWei string  `json:"realized_pnl_wei"`
	TokensTraded   int64   `json:"tokens_traded"`
	Username       *string `json:"username" nullable:"true" doc:"Username published on this wallet, or null."`
}

type leaderboardBody struct {
	Snapshot snapshotDTO          `json:"snapshot"`
	Items    []leaderboardItemDTO `json:"items"`
}

type leaderboardOutput struct{ Body leaderboardBody }

func (r WalletRoutes) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "getWallet", Method: http.MethodGet, Path: "/wallets/{address}", Tags: []string{"wallets"}}, r.get)
	huma.Register(api, huma.Operation{OperationID: "listWalletPositions", Method: http.MethodGet, Path: "/wallets/{address}/positions", Tags: []string{"wallets"}}, r.positions)
	huma.Register(api, huma.Operation{OperationID: "listWalletTrades", Method: http.MethodGet, Path: "/wallets/{address}/trades", Tags: []string{"wallets"}}, r.trades)
	huma.Register(api, huma.Operation{OperationID: "listWalletCreated", Method: http.MethodGet, Path: "/wallets/{address}/created", Tags: []string{"wallets"}}, r.created)
	huma.Register(api, huma.Operation{OperationID: "getLeaderboard", Method: http.MethodGet, Path: "/leaderboard", Tags: []string{"stats"}}, r.leaderboard)
}

func walletAddress(value string) (common.Address, error) {
	if !common.IsHexAddress(value) {
		return common.Address{}, huma.Error422UnprocessableEntity("invalid wallet address")
	}
	return common.HexToAddress(value), nil
}

func walletReadError(ctx context.Context, err error) error {
	if errors.Is(err, wallet.ErrCursorMode) {
		return apiProblem(http.StatusUnprocessableEntity, "cursor_mode_mismatch", "Cursor belongs to the other positions mode")
	}
	return mapReadError(ctx, err)
}

func pageLimit(limit, fallback int) int {
	if limit == 0 {
		return fallback
	}
	return limit
}

func stampDTO(s wallet.Stamp) snapshotDTO { return snapDTO(s.Snapshot, s.Finality) }

func (r WalletRoutes) get(ctx context.Context, in *walletInput) (*walletOutput, error) {
	a, err := walletAddress(in.Address)
	if err != nil {
		return nil, err
	}
	v, err := r.Reader.GetWallet(ctx, r.ChainID, a)
	if err != nil {
		return nil, walletReadError(ctx, err)
	}
	return &walletOutput{Body: walletDTO{
		Snapshot: stampDTO(v.Stamp), Address: v.Address.Hex(), RealizedPnLWei: decimal(v.RealizedPnL),
		UnrealizedPnLWei: decimal(v.UnrealizedPnL), PortfolioValueWei: decimal(v.PortfolioValue),
		HeldCount: v.HeldCount, CreatedCount: v.CreatedCount, TokensTraded: v.TokensTraded,
		FirstBlock: v.FirstBlock, LastBlock: v.LastBlock, AttributionComplete: v.AttributionComplete,
		Username: v.Username,
	}}, nil
}

var wad = big.NewInt(1_000_000_000_000_000_000)

// averageCost is remaining_cost * 1e18 / balance, floored; nil when the balance is 0.
func averageCost(remaining, balance *big.Int) *string {
	if balance == nil || balance.Sign() == 0 {
		return nil
	}
	s := new(big.Int).Quo(new(big.Int).Mul(remaining, wad), balance).String()
	return &s
}

func (r WalletRoutes) positions(ctx context.Context, in *walletPositionsInput) (*walletPositionsOutput, error) {
	a, err := walletAddress(in.Address)
	if err != nil {
		return nil, err
	}
	if !in.Closed && in.Cursor != "" {
		return nil, apiProblem(http.StatusUnprocessableEntity, "cursor_not_supported", "Open positions are returned in one response; cursor applies only with closed=true")
	}
	cur, err := cursor(in.Cursor)
	if err != nil {
		return nil, err
	}
	page, err := r.Reader.ListPositions(ctx, wallet.PositionsQuery{ChainID: r.ChainID, Wallet: a, Closed: in.Closed, Limit: pageLimit(in.Limit, defaultWalletPage), Cursor: cur})
	if err != nil {
		return nil, walletReadError(ctx, err)
	}
	body := walletPositionsBody{Snapshot: stampDTO(page.Stamp), Items: make([]walletPositionDTO, 0, len(page.Items)), Truncated: page.Truncated, NextCursor: page.NextCursor}
	for _, p := range page.Items {
		body.Items = append(body.Items, walletPositionDTO{
			Token: p.Token.Hex(), Name: p.Name, Symbol: p.Symbol, Phase: p.Phase, Balance: decimal(p.Balance),
			AverageCostWad: averageCost(p.RemainingCost, p.Balance), RemainingCostWei: decimal(p.RemainingCost),
			RealizedPnLWei: decimal(p.RealizedPnL), CurrentValueWei: decimal(p.CurrentValue),
			UnrealizedPnLWei: decimal(new(big.Int).Sub(p.CurrentValue, p.RemainingCost)),
			TransferredIn:    decimal(p.TransferredIn), TradeCount: p.TradeCount, LastBlock: p.LastBlock,
		})
	}
	return &walletPositionsOutput{Body: body}, nil
}

func (r WalletRoutes) trades(ctx context.Context, in *walletPageInput) (*walletTradesOutput, error) {
	a, err := walletAddress(in.Address)
	if err != nil {
		return nil, err
	}
	cur, err := cursor(in.Cursor)
	if err != nil {
		return nil, err
	}
	page, err := r.Reader.ListTrades(ctx, wallet.TradesQuery{ChainID: r.ChainID, Wallet: a, Limit: pageLimit(in.Limit, defaultWalletPage), Cursor: cur})
	if err != nil {
		return nil, walletReadError(ctx, err)
	}
	body := walletTradesBody{Snapshot: stampDTO(page.Stamp), Items: make([]walletTradeDTO, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, v := range page.Items {
		trader, side := tradeParty(v.Trader, v.Buy)
		body.Items = append(body.Items, walletTradeDTO{Token: v.Token.Hex(), Name: v.Name, Symbol: v.Symbol,
			Source: v.Source, Trader: trader, Side: side, ExecutionPrice: decimal(v.ExecutionPrice), SpotPrice: decimal(v.SpotPrice),
			ETHVolume: decimal(v.ETHVolume), TokenVolume: decimal(v.TokenVolume), BlockNumber: v.BlockNumber,
			TransactionIndex: v.TransactionIndex, TxHash: v.TxHash.Hex(), LogIndex: v.LogIndex,
			Time: v.Time.UTC().Format(time.RFC3339Nano), Finality: v.Finality,
		})
	}
	return &walletTradesOutput{Body: body}, nil
}

func (r WalletRoutes) created(ctx context.Context, in *walletPageInput) (*walletCreatedOutput, error) {
	a, err := walletAddress(in.Address)
	if err != nil {
		return nil, err
	}
	cur, err := cursor(in.Cursor)
	if err != nil {
		return nil, err
	}
	page, err := r.Reader.ListCreated(ctx, wallet.CreatedQuery{ChainID: r.ChainID, Wallet: a, Limit: pageLimit(in.Limit, defaultWalletPage), Cursor: cur})
	if err != nil {
		return nil, walletReadError(ctx, err)
	}
	body := walletCreatedBody{Snapshot: stampDTO(page.Stamp), Items: make([]walletCreatedDTO, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, c := range page.Items {
		body.Items = append(body.Items, walletCreatedDTO{Token: c.Token.Hex(), Name: c.Name, Symbol: c.Symbol, Phase: c.Phase, MarketCapWei: decimal(c.MarketCap), LaunchBlock: c.LaunchBlock})
	}
	return &walletCreatedOutput{Body: body}, nil
}

var leaderboardWindows = map[string]wallet.LeaderboardWindow{
	"": wallet.WindowAll, "all": wallet.WindowAll, "7d": wallet.Window7d, "30d": wallet.Window30d,
}

func (r WalletRoutes) leaderboard(ctx context.Context, in *leaderboardInput) (*leaderboardOutput, error) {
	window, ok := leaderboardWindows[in.Window]
	if !ok {
		return nil, apiProblem(http.StatusBadRequest, "invalid_window", "Unsupported leaderboard window")
	}
	page, err := r.Reader.Leaderboard(ctx, r.ChainID, window, pageLimit(in.Limit, defaultLeaderboard))
	if err != nil {
		return nil, walletReadError(ctx, err)
	}
	body := leaderboardBody{Snapshot: stampDTO(page.Stamp), Items: make([]leaderboardItemDTO, 0, len(page.Items))}
	for _, v := range page.Items {
		body.Items = append(body.Items, leaderboardItemDTO{Rank: v.Rank, Address: v.Address.Hex(), RealizedPnLWei: decimal(v.RealizedPnL), TokensTraded: v.TokensTraded, Username: v.Username})
	}
	return &leaderboardOutput{Body: body}, nil
}
