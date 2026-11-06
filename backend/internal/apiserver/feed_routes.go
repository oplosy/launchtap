package apiserver

import (
	"context"
	"net/http"
	"time"

	"github.com/Contictus/launchtap/backend/internal/feed"
	"github.com/Contictus/launchtap/backend/internal/realtime"
	"github.com/danielgtaylor/huma/v2"
)

const (
	defaultRecentTrades = 50
	maxRecentTrades     = 100
)

// tradeFeedItem is one market trade across all tokens. It is the payload of the `trade` SSE event and the
// item type of GET /v1/trades/recent. Amounts are decimal strings: wei for eth_volume, token base units
// for token_volume.
type tradeFeedItem struct {
	ChainID          int64   `json:"chain_id"`
	DeploymentID     string  `json:"deployment_id"`
	Token            string  `json:"token"`
	Name             string  `json:"name"`
	Symbol           string  `json:"symbol"`
	Source           string  `json:"source"`
	Trader           *string `json:"trader"`
	Side             string  `json:"side"`
	ETHVolume        string  `json:"eth_volume"`
	TokenVolume      string  `json:"token_volume"`
	BlockNumber      int64   `json:"block_number"`
	TransactionIndex int32   `json:"transaction_index"`
	TxHash           string  `json:"tx_hash"`
	LogIndex         int32   `json:"log_index"`
	Time             string  `json:"time"`
	Finality         string  `json:"finality"`
}

type graduationFeedEvent struct {
	ChainID      int64  `json:"chain_id"`
	DeploymentID string `json:"deployment_id"`
	Token        string `json:"token"`
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	Pair         string `json:"pair"`
	TxHash       string `json:"tx_hash"`
	BlockNumber  int64  `json:"block_number"`
}

type newTokenFeedEvent struct {
	ChainID      int64  `json:"chain_id"`
	DeploymentID string `json:"deployment_id"`
	Token        string `json:"token"`
	Creator      string `json:"creator"`
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	TxHash       string `json:"tx_hash"`
	BlockNumber  int64  `json:"block_number"`
}

type burstItem struct {
	Token string `json:"token"`
	Count int    `json:"count"`
}

type tradeBurstFeedEvent struct {
	ChainID      int64       `json:"chain_id"`
	DeploymentID string      `json:"deployment_id"`
	WindowStart  string      `json:"window_start"`
	WindowEnd    string      `json:"window_end"`
	Items        []burstItem `json:"items"`
}

type finalityFeedEvent struct {
	ChainID        int64  `json:"chain_id"`
	DeploymentID   string `json:"deployment_id"`
	SafeBlock      int64  `json:"safe_block"`
	FinalizedBlock int64  `json:"finalized_block"`
}

func feedTradeItem(chainID int64, deploymentID string, t feed.Trade) tradeFeedItem {
	var trader *string
	if t.Trader != nil {
		hex := t.Trader.Hex()
		trader = &hex
	}
	side := "sell"
	if t.Buy {
		side = "buy"
	}
	return tradeFeedItem{
		ChainID: chainID, DeploymentID: deploymentID, Token: t.Token.Hex(), Name: t.Name, Symbol: t.Symbol,
		Source: t.Source, Trader: trader, Side: side, ETHVolume: decimal(t.ETHVolume), TokenVolume: decimal(t.TokenVolume),
		BlockNumber: t.Block, TransactionIndex: t.TxIndex, TxHash: t.TxHash.Hex(), LogIndex: t.LogIndex,
		Time: t.Time.UTC().Format(time.RFC3339Nano), Finality: t.Finality,
	}
}

// feedPayload maps a hub feed event to its SSE DTO; ok is false when the event carries no payload.
func (r EventRoutes) feedPayload(event realtime.Event) (payload any, ok bool) {
	scope := deployment(event.DeploymentID, r.DeploymentID)
	switch event.Type {
	case "trade":
		if event.Trade == nil {
			return nil, false
		}
		return feedTradeItem(event.ChainID, scope, *event.Trade), true
	case "graduation":
		g := event.Graduation
		if g == nil {
			return nil, false
		}
		return graduationFeedEvent{ChainID: event.ChainID, DeploymentID: scope, Token: g.Token.Hex(), Name: g.Name, Symbol: g.Symbol, Pair: g.Pair.Hex(), TxHash: g.TxHash.Hex(), BlockNumber: g.Block}, true
	case "new_token":
		l := event.Launch
		if l == nil {
			return nil, false
		}
		return newTokenFeedEvent{ChainID: event.ChainID, DeploymentID: scope, Token: l.Token.Hex(), Creator: l.Creator.Hex(), Name: l.Name, Symbol: l.Symbol, TxHash: l.TxHash.Hex(), BlockNumber: l.Block}, true
	case "trade_burst":
		items := make([]burstItem, 0, len(event.Burst))
		for _, b := range event.Burst {
			items = append(items, burstItem{Token: b.Token, Count: b.Count})
		}
		return tradeBurstFeedEvent{ChainID: event.ChainID, DeploymentID: scope, WindowStart: event.BurstStart.UTC().Format(time.RFC3339Nano), WindowEnd: event.BurstEnd.UTC().Format(time.RFC3339Nano), Items: items}, true
	case "finality":
		return finalityFeedEvent{ChainID: event.ChainID, DeploymentID: scope, SafeBlock: event.SafeBlock, FinalizedBlock: event.FinalizedBlock}, true
	}
	return nil, false
}

// FeedRoutes serves the global recent-trades snapshot that seeds the live strip.
type FeedRoutes struct {
	Reader interface {
		RecentTradesSnapshot(ctx context.Context, chainID int64, limit int) (feed.RecentPage, error)
	}
	ChainID      int64
	DeploymentID string
}

type recentTradesInput struct {
	Limit int `query:"limit" minimum:"1" maximum:"100" doc:"Number of trades, 1-100; defaults to 50."`
}

type recentTradesBody struct {
	Snapshot snapshotDTO     `json:"snapshot"`
	Items    []tradeFeedItem `json:"items"`
}

type recentTradesOutput struct{ Body recentTradesBody }

func (r FeedRoutes) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "listRecentTrades", Method: http.MethodGet, Path: "/trades/recent", Tags: []string{"market"}}, r.recent)
}

func (r FeedRoutes) recent(ctx context.Context, in *recentTradesInput) (*recentTradesOutput, error) {
	limit := in.Limit
	if limit == 0 {
		limit = defaultRecentTrades
	}
	if limit < 1 || limit > maxRecentTrades {
		return nil, huma.Error422UnprocessableEntity("limit must be between 1 and 100")
	}
	page, err := r.Reader.RecentTradesSnapshot(ctx, r.ChainID, limit)
	if err != nil {
		return nil, mapReadError(ctx, err)
	}
	body := recentTradesBody{Snapshot: snapDTO(page.Snapshot, page.Finality), Items: make([]tradeFeedItem, 0, len(page.Items))}
	for _, t := range page.Items {
		body.Items = append(body.Items, feedTradeItem(r.ChainID, r.DeploymentID, t))
	}
	return &recentTradesOutput{Body: body}, nil
}
