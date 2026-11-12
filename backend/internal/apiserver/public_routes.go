package apiserver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Contictus/launchtap/backend/internal/earlybuyer"
	"github.com/Contictus/launchtap/backend/internal/holder"
	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/Contictus/launchtap/backend/internal/stats"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/Contictus/launchtap/backend/internal/trading"
	"github.com/danielgtaylor/huma/v2"
	"github.com/ethereum/go-ethereum/common"
)

type PublicRoutes struct {
	Tokens interface {
		token.Reader
		token.QuoteReader
	}
	Market interface {
		trading.Reader
		holder.Reader
	}
	// EarlyBuyers serves the early-buyer clusters read; nil answers 503.
	EarlyBuyers   earlybuyer.Reader
	Protocol      stats.Reader
	ProtocolDaily stats.DailyReader
	ChainID       int64
}
type addressInput struct {
	Token string `path:"token"`
}
type snapshotDTO struct {
	ChainID     int64  `json:"chain_id"`
	BlockNumber int64  `json:"as_of_block"`
	BlockHash   string `json:"as_of_block_hash"`
	Finality    string `json:"finality"`
}
type tokenDetailDTO struct {
	Snapshot              snapshotDTO `json:"snapshot"`
	Address               string      `json:"address"`
	Curve                 string      `json:"curve"`
	Pair                  string      `json:"pair"`
	WETH                  string      `json:"weth"`
	Creator               string      `json:"creator"`
	ProtocolTreasury      string      `json:"protocol_treasury"`
	Name                  string      `json:"name"`
	Symbol                string      `json:"symbol"`
	Phase                 string      `json:"phase"`
	EngineVersion         uint16      `json:"engine_version"`
	TotalSupply           string      `json:"total_supply"`
	InitialVirtualETH     string      `json:"initial_virtual_eth"`
	InitialVirtualToken   string      `json:"initial_virtual_token"`
	CurveTokens           string      `json:"curve_tokens"`
	LPTokens              string      `json:"lp_tokens"`
	GraduationETH         string      `json:"graduation_eth"`
	TradeFeeBPS           uint16      `json:"trade_fee_bps"`
	ProtocolShareBPS      uint16      `json:"protocol_share_bps"`
	ReserveSource         string      `json:"reserve_source"`
	ETHReserve            string      `json:"eth_reserve"`
	TokenReserve          string      `json:"token_reserve"`
	RealCurveETH          string      `json:"real_curve_eth"`
	GraduationProgressBPS int32       `json:"graduation_progress_bps"`
	ReserveBlock          int64       `json:"reserve_block"`
	ReserveHash           string      `json:"reserve_hash"`
	Description           string      `json:"description"`
	ImageURL              string      `json:"image_url"`
	XURL                  string      `json:"x_url"`
	TelegramURL           string      `json:"telegram_url"`
	SpotPriceETH          string      `json:"spot_price_eth"`
	MarketCapETH          string      `json:"market_cap_eth"`
	FDVETH                string      `json:"fdv_eth"`
	LiquidityETH          string      `json:"liquidity_eth"`
	ATHPriceETH           string      `json:"ath_price_eth"`
	Volume24hETH          string      `json:"volume_24h_eth"`
	ATHAt                 string      `json:"ath_at"`
	PriceChange24hBPS     int64       `json:"price_change_24h_bps" minimum:"-9007199254740991" maximum:"9007199254740991"`
	HolderCount           int64       `json:"holder_count"`
	LaunchBlock           int64       `json:"launch_block" doc:"Block of the launch transaction; holders first acquiring in it were bundled with the launch."`
	LaunchTime            string      `json:"launch_time"`
	V2                    *tokenV2DTO `json:"v2" doc:"Engine-2 launch, pool and fee facts; null for engine 1."`
}

// Huma marks an object schema nullable through the struct's `_` field, not the referencing field.
type tokenV2DTO struct {
	_                   struct{}        `nullable:"true"`
	AntiSnipe           antiSnipeDTO    `json:"anti_snipe"`
	LPCreatorShareBPS   uint16          `json:"lp_creator_share_bps"`
	Pool                *v2PoolDTO      `json:"pool" doc:"The graduated Uniswap V4 pool; null before graduation."`
	FeeRecipient        string          `json:"fee_recipient"`
	PendingFeeRecipient *string         `json:"pending_fee_recipient" nullable:"true"`
	LockerOwed          []lockerOwedDTO `json:"locker_owed" doc:"What the LP fee locker owes each account, by account."`
}
type antiSnipeDTO struct {
	StartBPS        uint16 `json:"start_bps"`
	DurationSeconds uint32 `json:"duration_seconds"`
	EndsAt          string `json:"ends_at" doc:"RFC 3339 time at which the anti-snipe tax reaches zero."`
}
type v2PoolDTO struct {
	_               struct{} `nullable:"true"`
	PoolID          string   `json:"pool_id"`
	PositionTokenID string   `json:"position_token_id"`
	Hook            string   `json:"hook"`
}
type lockerOwedDTO struct {
	Account string `json:"account"`
	ETH     string `json:"eth"`
	Token   string `json:"token"`
}
type detailOutput struct{ Body tokenDetailDTO }
type pageInput struct {
	Token  string `path:"token"`
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100"`
}
type tradeDTO struct {
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
type tradesBody struct {
	Snapshot   snapshotDTO `json:"snapshot"`
	Items      []tradeDTO  `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
}
type tradesOutput struct{ Body tradesBody }
type holderDTO struct {
	Address            string `json:"address"`
	Balance            string `json:"balance"`
	FirstAcquiredBlock int64  `json:"first_acquired_block"`
}
type holdersBody struct {
	Snapshot   snapshotDTO `json:"snapshot"`
	Items      []holderDTO `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
}
type holdersOutput struct{ Body holdersBody }
type protocolDTO struct {
	Snapshot           snapshotDTO `json:"snapshot"`
	Volume24hETH       string      `json:"volume_24h_eth"`
	VolumeAllTimeETH   string      `json:"volume_all_time_eth"`
	Launches24h        int64       `json:"launches_24h"`
	LaunchesAllTime    int64       `json:"launches_all_time"`
	Trades24h          int64       `json:"trades_24h"`
	TradesAllTime      int64       `json:"trades_all_time"`
	Graduations24h     int64       `json:"graduations_24h"`
	GraduationsAllTime int64       `json:"graduations_all_time"`
	UpdatedAt          string      `json:"updated_at"`
}
type protocolOutput struct{ Body protocolDTO }
type protocolDailyInput struct {
	From  string `query:"from"`
	To    string `query:"to"`
	Limit int    `query:"limit" minimum:"1" maximum:"366"`
}
type protocolDailyDTO struct {
	Day         string `json:"day"`
	VolumeETH   string `json:"volume_eth"`
	Launches    int64  `json:"launches"`
	Trades      int64  `json:"trades"`
	Graduations int64  `json:"graduations"`
}
type protocolDailyBody struct {
	Snapshot snapshotDTO        `json:"snapshot"`
	Items    []protocolDailyDTO `json:"items"`
}
type protocolDailyOutput struct{ Body protocolDailyBody }

func (r PublicRoutes) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "getToken", Method: http.MethodGet, Path: "/tokens/{token}", Tags: []string{"tokens"}}, r.detail)
	huma.Register(api, huma.Operation{OperationID: "listTrades", Method: http.MethodGet, Path: "/tokens/{token}/trades", Tags: []string{"market"}}, r.trades)
	huma.Register(api, huma.Operation{OperationID: "listHolders", Method: http.MethodGet, Path: "/tokens/{token}/holders", Tags: []string{"market"}}, r.holders)
	huma.Register(api, huma.Operation{OperationID: "getEarlyBuyers", Method: http.MethodGet, Path: "/tokens/{token}/early-buyers", Tags: []string{"market"}, Description: "Wallets that received tokens from the curve within the early window after launch, grouped by same-transaction deliveries and token transfers between them."}, r.earlyBuyers)
	huma.Register(api, huma.Operation{OperationID: "getProtocolStats", Method: http.MethodGet, Path: "/stats/protocol", Tags: []string{"stats"}}, r.protocol)
	huma.Register(api, huma.Operation{OperationID: "listProtocolDaily", Method: http.MethodGet, Path: "/stats/protocol/daily", Tags: []string{"stats"}}, r.protocolDaily)
}
func address(value string) (common.Address, error) {
	if !common.IsHexAddress(value) {
		return common.Address{}, huma.Error400BadRequest("invalid token address")
	}
	return common.HexToAddress(value), nil
}
func mapReadError(ctx context.Context, err error) error {
	if errors.Is(err, pagination.ErrSnapshotUnavailable) {
		return apiProblem(http.StatusServiceUnavailable, "snapshot_unavailable", "Canonical read snapshot is not available yet")
	}
	if errors.Is(err, token.ErrNotFound) {
		return apiProblem(http.StatusNotFound, "token_not_found", "Token not found")
	}
	if errors.Is(err, pagination.ErrCursorInvalidated) {
		return apiProblem(http.StatusConflict, "cursor_invalidated", "Cursor snapshot is no longer canonical")
	}
	if errors.Is(err, pagination.ErrInvalidCursor) {
		return apiProblem(http.StatusBadRequest, "invalid_cursor", "Cursor is invalid")
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return apiRequestProblem(ctx, http.StatusInternalServerError, "internal_error", "Request failed")
}
func apiProblem(status int, code, detail string) error {
	return &huma.ErrorModel{Type: "urn:launchpad:problem:" + code, Title: http.StatusText(status), Status: status, Detail: detail}
}
func apiRequestProblem(ctx context.Context, status int, code, detail string) error {
	err := &huma.ErrorModel{Type: "urn:launchpad:problem:" + code, Title: http.StatusText(status), Status: status, Detail: detail}
	if id := RequestID(ctx); id != "" {
		err.Instance = "urn:launchpad:request:" + id
	}
	return err
}
func snapDTO(s pagination.Snapshot, f string) snapshotDTO {
	return snapshotDTO{ChainID: s.ChainID, BlockNumber: s.BlockNumber, BlockHash: common.Hash(s.BlockHash).Hex(), Finality: f}
}
func (r PublicRoutes) detail(ctx context.Context, in *addressInput) (*detailOutput, error) {
	a, e := address(in.Token)
	if e != nil {
		return nil, e
	}
	v, e := r.Tokens.Get(ctx, r.ChainID, a)
	if e != nil {
		return nil, mapReadError(ctx, e)
	}
	d := tokenDetailDTO{Snapshot: snapDTO(v.Snapshot, v.Finality), Address: v.Address.Hex(), Curve: v.Curve.Hex(), Pair: v.Pair.Hex(), WETH: v.WETH.Hex(), Creator: v.Creator.Hex(), ProtocolTreasury: v.ProtocolTreasury.Hex(), Name: v.Name, Symbol: v.Symbol, Phase: v.Phase, EngineVersion: v.EngineVersion, TotalSupply: decimal(v.TotalSupply), InitialVirtualETH: decimal(v.InitialVirtualETH), InitialVirtualToken: decimal(v.InitialVirtualToken), CurveTokens: decimal(v.CurveTokens), LPTokens: decimal(v.LPTokens), GraduationETH: decimal(v.GraduationETH), TradeFeeBPS: v.TradeFeeBPS, ProtocolShareBPS: v.ProtocolShareBPS, ReserveSource: v.ReserveSource, ETHReserve: decimal(v.ETHReserve), TokenReserve: decimal(v.TokenReserve), RealCurveETH: decimal(v.RealCurveETH), GraduationProgressBPS: v.GraduationProgressBPS, ReserveBlock: v.ReserveBlock, ReserveHash: v.ReserveHash.Hex(), Description: v.Description, ImageURL: v.ImageURL, XURL: v.XURL, TelegramURL: v.TelegramURL, SpotPriceETH: decimal(v.SpotPriceETH), MarketCapETH: decimal(v.MarketCapETH), FDVETH: decimal(v.FDVETH), LiquidityETH: decimal(v.LiquidityETH), ATHPriceETH: decimal(v.ATHPriceETH), Volume24hETH: decimal(v.Volume24hETH), ATHAt: v.ATHAt.UTC().Format(time.RFC3339Nano), PriceChange24hBPS: v.PriceChange24hBPS, HolderCount: v.HolderCount, LaunchBlock: v.LaunchBlock, LaunchTime: v.LaunchTime.UTC().Format("2006-01-02T15:04:05.000000Z"), V2: v2DTO(v)}
	return &detailOutput{Body: d}, nil
}
func cursor(value string) (*pagination.Cursor, error) {
	if value == "" {
		return nil, nil
	}
	c, e := pagination.Decode(value)
	if e != nil {
		return nil, apiProblem(http.StatusBadRequest, "invalid_cursor", "Cursor is invalid")
	}
	return &c, nil
}
func (r PublicRoutes) trades(ctx context.Context, in *pageInput) (*tradesOutput, error) {
	a, e := address(in.Token)
	if e != nil {
		return nil, e
	}
	cur, e := cursor(in.Cursor)
	if e != nil {
		return nil, e
	}
	limit := in.Limit
	if limit == 0 {
		limit = 20
	}
	p, e := r.Market.ListTrades(ctx, trading.Query{ChainID: r.ChainID, Token: a, Limit: limit, Cursor: cur})
	if e != nil {
		return nil, mapReadError(ctx, e)
	}
	body := tradesBody{Snapshot: snapDTO(p.Snapshot, p.Finality), Items: make([]tradeDTO, 0, len(p.Items)), NextCursor: p.NextCursor}
	for _, v := range p.Items {
		trader, side := tradeParty(v.Trader, v.Buy)
		body.Items = append(body.Items, tradeDTO{Source: v.Source, Trader: trader, Side: side, ExecutionPrice: decimal(v.ExecutionPrice), SpotPrice: decimal(v.SpotPrice), ETHVolume: decimal(v.ETHVolume), TokenVolume: decimal(v.TokenVolume), BlockNumber: v.BlockNumber, TransactionIndex: v.TransactionIndex, TxHash: v.TxHash.Hex(), LogIndex: v.LogIndex, Time: v.Time.UTC().Format(time.RFC3339Nano), Finality: v.Finality})
	}
	return &tradesOutput{Body: body}, nil
}
func (r PublicRoutes) holders(ctx context.Context, in *pageInput) (*holdersOutput, error) {
	a, e := address(in.Token)
	if e != nil {
		return nil, e
	}
	cur, e := cursor(in.Cursor)
	if e != nil {
		return nil, e
	}
	limit := in.Limit
	if limit == 0 {
		limit = 20
	}
	p, e := r.Market.ListHolders(ctx, holder.Query{ChainID: r.ChainID, Token: a, Limit: limit, Cursor: cur})
	if e != nil {
		return nil, mapReadError(ctx, e)
	}
	body := holdersBody{Snapshot: snapDTO(p.Snapshot, p.Finality), Items: make([]holderDTO, 0, len(p.Items)), NextCursor: p.NextCursor}
	for _, v := range p.Items {
		body.Items = append(body.Items, holderDTO{Address: v.Address.Hex(), Balance: decimal(v.Balance), FirstAcquiredBlock: v.FirstAcquiredBlock})
	}
	return &holdersOutput{Body: body}, nil
}

type earlyWalletDTO struct {
	Address string `json:"address"`
	Balance string `json:"balance"`
}
type earlyClusterDTO struct {
	WalletCount     int              `json:"wallet_count"`
	Balance         string           `json:"balance" doc:"What every wallet of the cluster holds now."`
	Links           []string         `json:"links" doc:"Link kinds present: same_transaction, token_transfer."`
	IncludesCreator bool             `json:"includes_creator"`
	Wallets         []earlyWalletDTO `json:"wallets" doc:"At most 20 wallets, largest balance first."`
}
type earlyBuyersBody struct {
	Snapshot         snapshotDTO       `json:"snapshot"`
	WindowSeconds    int               `json:"window_seconds"`
	WindowOpen       bool              `json:"window_open" doc:"The early window had not ended at the snapshot block."`
	EarlyWalletCount int               `json:"early_wallet_count"`
	EarlyBalance     string            `json:"early_balance" doc:"What every early wallet holds now."`
	ClusterCount     int               `json:"cluster_count"`
	Clusters         []earlyClusterDTO `json:"clusters" doc:"At most 10 clusters, largest combined balance first."`
}
type earlyBuyersOutput struct{ Body earlyBuyersBody }

func (r PublicRoutes) earlyBuyers(ctx context.Context, in *addressInput) (*earlyBuyersOutput, error) {
	a, e := address(in.Token)
	if e != nil {
		return nil, e
	}
	if r.EarlyBuyers == nil {
		return nil, apiProblem(http.StatusServiceUnavailable, "early_buyers_unavailable", "Early-buyer reads are not configured")
	}
	v, e := r.EarlyBuyers.ReadEarlyBuyers(ctx, earlybuyer.Query{ChainID: r.ChainID, Token: a})
	if e != nil {
		return nil, mapReadError(ctx, e)
	}
	body := earlyBuyersBody{Snapshot: snapDTO(v.Snapshot, v.Finality), WindowSeconds: earlybuyer.WindowSeconds, WindowOpen: v.WindowOpen, EarlyWalletCount: v.EarlyWalletCount, EarlyBalance: decimal(v.EarlyBalance), ClusterCount: v.ClusterCount, Clusters: make([]earlyClusterDTO, 0, len(v.Clusters))}
	for _, c := range v.Clusters {
		dto := earlyClusterDTO{WalletCount: c.WalletCount, Balance: decimal(c.Balance), Links: append([]string{}, c.Links...), IncludesCreator: c.IncludesCreator, Wallets: make([]earlyWalletDTO, 0, len(c.Wallets))}
		for _, w := range c.Wallets {
			dto.Wallets = append(dto.Wallets, earlyWalletDTO{Address: w.Address.Hex(), Balance: decimal(w.Balance)})
		}
		body.Clusters = append(body.Clusters, dto)
	}
	return &earlyBuyersOutput{Body: body}, nil
}
func (r PublicRoutes) protocol(ctx context.Context, _ *struct{}) (*protocolOutput, error) {
	v, e := r.Protocol.ReadProtocol(ctx, r.ChainID)
	if e != nil {
		return nil, mapReadError(ctx, e)
	}
	return &protocolOutput{Body: protocolDTO{Snapshot: snapDTO(v.Snapshot, v.Finality), Volume24hETH: decimal(v.Volume24hETH), VolumeAllTimeETH: decimal(v.VolumeAllTimeETH), Launches24h: v.Launches24h, LaunchesAllTime: v.LaunchesAllTime, Trades24h: v.Trades24h, TradesAllTime: v.TradesAllTime, Graduations24h: v.Graduations24h, GraduationsAllTime: v.GraduationsAllTime, UpdatedAt: v.UpdatedAt.UTC().Format(time.RFC3339Nano)}}, nil
}

func (r PublicRoutes) protocolDaily(ctx context.Context, in *protocolDailyInput) (*protocolDailyOutput, error) {
	if r.ProtocolDaily == nil {
		return nil, apiProblem(http.StatusServiceUnavailable, "stats_unavailable", "Protocol daily statistics are unavailable")
	}
	from, to, err := dailyRange(in.From, in.To)
	if err != nil {
		return nil, apiProblem(http.StatusBadRequest, "invalid_range", err.Error())
	}
	limit := in.Limit
	if limit == 0 {
		limit = 366
	}
	v, err := r.ProtocolDaily.ReadProtocolDaily(ctx, r.ChainID, stats.DailyQuery{From: from, To: to, Limit: limit})
	if err != nil {
		return nil, mapReadError(ctx, err)
	}
	body := protocolDailyBody{Snapshot: snapDTO(v.Snapshot, v.Finality), Items: make([]protocolDailyDTO, 0, len(v.Items))}
	for _, item := range v.Items {
		body.Items = append(body.Items, protocolDailyDTO{Day: item.Day.UTC().Format("2006-01-02"), VolumeETH: decimal(item.VolumeETH), Launches: item.Launches, Trades: item.Trades, Graduations: item.Graduations})
	}
	return &protocolDailyOutput{Body: body}, nil
}

func dailyRange(fromValue, toValue string) (time.Time, time.Time, error) {
	const layout = "2006-01-02"
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -29)
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if fromValue != "" {
		parsed, err := time.Parse(layout, fromValue)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("from must be an ISO date (YYYY-MM-DD)")
		}
		from = parsed
	}
	if toValue != "" {
		parsed, err := time.Parse(layout, toValue)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("to must be an ISO date (YYYY-MM-DD)")
		}
		to = parsed
	}
	if from.After(to) || to.Sub(from) > 365*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("date range must be ordered and no longer than 366 days")
	}
	return from, to, nil
}

// tradeParty maps a trade's optional trader and its side to their JSON forms; shared by every trade DTO.
func tradeParty(trader *common.Address, buy bool) (*string, string) {
	var hex *string
	if trader != nil {
		x := trader.Hex()
		hex = &x
	}
	if buy {
		return hex, "buy"
	}
	return hex, "sell"
}

func v2DTO(v token.Detail) *tokenV2DTO {
	if v.V2 == nil {
		return nil
	}
	facts := v.V2
	out := &tokenV2DTO{
		AntiSnipe: antiSnipeDTO{
			StartBPS:        facts.AntiSnipeStartBPS,
			DurationSeconds: facts.AntiSnipeDurationSeconds,
			EndsAt:          v.LaunchTime.Add(time.Duration(facts.AntiSnipeDurationSeconds) * time.Second).UTC().Format(time.RFC3339),
		},
		LPCreatorShareBPS: facts.LPCreatorShareBPS,
		FeeRecipient:      facts.FeeRecipient.Hex(),
		LockerOwed:        make([]lockerOwedDTO, 0, len(facts.LockerOwed)),
	}
	if facts.Pool != nil {
		out.Pool = &v2PoolDTO{PoolID: facts.Pool.PoolID.Hex(), PositionTokenID: decimal(facts.Pool.PositionTokenID), Hook: facts.Pool.Hook.Hex()}
	}
	if facts.PendingFeeRecipient != nil {
		pending := facts.PendingFeeRecipient.Hex()
		out.PendingFeeRecipient = &pending
	}
	for _, owed := range facts.LockerOwed {
		out.LockerOwed = append(out.LockerOwed, lockerOwedDTO{Account: owed.Account.Hex(), ETH: decimal(owed.ETH), Token: decimal(owed.Token)})
	}
	return out
}
