package apiserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/danielgtaylor/huma/v2"
	"github.com/ethereum/go-ethereum/common"
)

const watchersCacheControl = "public, max-age=15"

// WatchersRoutes serves the public per-token watch count (spec P0-C3).
type WatchersRoutes struct {
	Counter      watchlist.WatcherCounter
	ChainID      int64
	DeploymentID string
	// Logger records unexpected store failures; Server.RegisterWatchersRoutes defaults it.
	Logger *slog.Logger
}

type watchersInput struct {
	Address string `path:"address" pattern:"^0x[0-9a-fA-F]{40}$"`
}
type watchersBody struct {
	WatchCount int64 `json:"watch_count" doc:"Number of signed-in users watching this token in this deployment."`
}
type watchersOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         watchersBody
}

func (r WatchersRoutes) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "getTokenWatchers", Method: http.MethodGet, Path: "/tokens/{address}/watchers", Tags: []string{"watchlist"}}, r.get)
}

// get answers for any well-formed address: the count is about watchlist rows, not the token row,
// so an unknown or reorged token reports its count (normally 0) instead of 404.
func (r WatchersRoutes) get(ctx context.Context, in *watchersInput) (*watchersOutput, error) {
	n, err := r.Counter.CountWatchers(ctx, r.ChainID, r.DeploymentID, common.HexToAddress(in.Address))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if r.Logger != nil {
			r.Logger.ErrorContext(ctx, "watchers request failed", "request_id", RequestID(ctx), "error_type", fmt.Sprintf("%T", err))
		}
		return nil, apiRequestProblem(ctx, http.StatusInternalServerError, "internal_error", "Request failed")
	}
	return &watchersOutput{CacheControl: watchersCacheControl, Body: watchersBody{WatchCount: n}}, nil
}
