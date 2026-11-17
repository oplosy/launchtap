package apiserver

import (
	"context"
	"net/http"
	"time"

	"github.com/Contictus/launchtap/backend/internal/candle"
	"github.com/danielgtaylor/huma/v2"
	"github.com/ethereum/go-ethereum/common"
)

type CandleRoutes struct {
	Reader  candle.Reader
	ChainID int64
}
type candleInput struct {
	Token    string `path:"token"`
	Interval string `query:"interval"`
	From     string `query:"from"`
	To       string `query:"to"`
	Limit    int    `query:"limit" minimum:"1" maximum:"100"`
	Cursor   string `query:"cursor"`
}
type candleOutput struct{ Body candleBody }
type candleBody struct {
	Items      []candleDTO `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
	Snapshot   snapshotDTO `json:"snapshot"`
}
type candleDTO struct {
	Start       string `json:"start"`
	Open        string `json:"open"`
	High        string `json:"high"`
	Low         string `json:"low"`
	Close       string `json:"close"`
	ETHVolume   string `json:"eth_volume"`
	TokenVolume string `json:"token_volume"`
	TradeCount  int64  `json:"trade_count"`
}

func (r CandleRoutes) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "listCandles", Method: http.MethodGet, Path: "/tokens/{token}/candles", Tags: []string{"market"}}, r.handle)
}
func (r CandleRoutes) handle(ctx context.Context, in *candleInput) (*candleOutput, error) {
	if r.Reader == nil {
		return nil, huma.Error503ServiceUnavailable("candle reader unavailable")
	}
	if !common.IsHexAddress(in.Token) {
		return nil, apiProblem(http.StatusBadRequest, "invalid_address", "invalid token address")
	}
	interval := in.Interval
	if interval == "" {
		interval = "1h"
	}
	if interval != "1m" && interval != "5m" && interval != "1h" && interval != "1d" && interval != "6h" && interval != "all" {
		return nil, apiProblem(http.StatusBadRequest, "invalid_interval", "unsupported interval")
	}
	limit := in.Limit
	if limit == 0 {
		limit = 20
	}
	from, to := time.Time{}, time.Now().UTC()
	var err error
	if in.From != "" {
		from, err = time.Parse(time.RFC3339, in.From)
		if err != nil {
			return nil, apiProblem(http.StatusBadRequest, "invalid_range", "invalid from")
		}
	}
	if in.To != "" {
		to, err = time.Parse(time.RFC3339, in.To)
		if err != nil {
			return nil, apiProblem(http.StatusBadRequest, "invalid_range", "invalid to")
		}
	}
	cur, e := cursor(in.Cursor)
	if e != nil {
		return nil, e
	}
	p, err := r.Reader.List(ctx, candle.Query{ChainID: r.ChainID, Token: common.HexToAddress(in.Token), Interval: interval, From: from, To: to, Limit: limit, Cursor: cur})
	if err != nil {
		return nil, mapReadError(ctx, err)
	}
	out := candleOutput{Body: candleBody{Items: make([]candleDTO, 0, len(p.Items)), NextCursor: p.NextCursor, Snapshot: snapDTO(p.Snapshot, p.Finality)}}
	for _, v := range p.Items {
		out.Body.Items = append(out.Body.Items, candleDTO{Start: v.Start.UTC().Format(time.RFC3339Nano), Open: decimal(v.Open), High: decimal(v.High), Low: decimal(v.Low), Close: decimal(v.Close), ETHVolume: decimal(v.ETHVolume), TokenVolume: decimal(v.TokenVolume), TradeCount: v.TradeCount})
	}
	return &out, nil
}
