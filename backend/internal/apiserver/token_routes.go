package apiserver

import (
	"context"
	"math/big"
	"net/http"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/danielgtaylor/huma/v2"
)

const maxSearchBytes = 64

type TokenRoutes struct {
	Reader  token.Reader
	ChainID int64
}
type tokenListInput struct {
	Phase  string `query:"phase"`
	Query  string `query:"q" maxLength:"64"`
	Sort   string `query:"sort"`
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100"`
}
type tokenListOutput struct{ Body tokenListBody }
type tokenListBody struct {
	Items      []tokenDTO  `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
	Snapshot   snapshotDTO `json:"snapshot"`
}
type tokenDTO struct {
	Address      string `json:"address"`
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	Phase        string `json:"phase"`
	LaunchTime   string `json:"launch_time"`
	LaunchBlock  int64  `json:"launch_block"`
	TotalSupply  string `json:"total_supply"`
	MarketCapETH string `json:"market_cap_eth"`
	Volume24hETH string `json:"volume_24h_eth"`
	HolderCount  int64  `json:"holder_count"`
}

func (r TokenRoutes) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "listTokens", Method: http.MethodGet, Path: "/tokens", Tags: []string{"tokens"}}, r.list)
}
func (r TokenRoutes) list(ctx context.Context, in *tokenListInput) (*tokenListOutput, error) {
	if r.Reader == nil {
		return nil, huma.Error503ServiceUnavailable("token reader unavailable")
	}
	limit := in.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return nil, huma.Error400BadRequest("limit must be between 1 and 100")
	}
	if in.Phase == "" {
		in.Phase = "curve"
	}
	if in.Sort == "" {
		in.Sort = "newest"
	}
	if len(in.Query) > maxSearchBytes {
		return nil, apiProblem(http.StatusBadRequest, "invalid_query", "Search text must be at most 64 bytes")
	}
	if in.Phase != "curve" && in.Phase != "graduated" {
		return nil, apiProblem(http.StatusBadRequest, "invalid_phase", "Unsupported token phase")
	}
	if in.Sort != "newest" && in.Sort != "oldest" && in.Sort != "market_cap" && in.Sort != "volume_24h" {
		return nil, apiProblem(http.StatusBadRequest, "invalid_sort", "Unsupported token sort")
	}
	var cur *pagination.Cursor
	var err error
	if in.Cursor != "" {
		c, e := pagination.Decode(in.Cursor)
		if e != nil {
			return nil, apiProblem(http.StatusBadRequest, "invalid_cursor", "Cursor is invalid")
		}
		cur = &c
	}
	p, err := r.Reader.List(ctx, token.ListQuery{ChainID: r.ChainID, Phase: in.Phase, Search: in.Query, Sort: in.Sort, Cursor: cur, Limit: limit})
	if err != nil {
		return nil, mapReadError(ctx, err)
	}
	out := tokenListOutput{Body: tokenListBody{Items: make([]tokenDTO, 0, len(p.Items)), NextCursor: p.NextCursor, Snapshot: snapDTO(p.Snapshot, p.Finality)}}
	for _, v := range p.Items {
		out.Body.Items = append(out.Body.Items, tokenDTO{Address: v.Address.Hex(), Name: v.Name, Symbol: v.Symbol, Phase: v.Phase, LaunchTime: v.LaunchTime.UTC().Format("2006-01-02T15:04:05.000000Z"), LaunchBlock: v.LaunchBlock, TotalSupply: decimal(v.TotalSupply), MarketCapETH: decimal(v.MarketCapETH), Volume24hETH: decimal(v.Volume24hETH), HolderCount: v.HolderCount})
	}
	return &out, nil
}
func decimal(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}
