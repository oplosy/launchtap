package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/danielgtaylor/huma/v2"
)

// maxPrivyDIDBytes bounds the verified DID before it becomes part of a database key. A real
// Privy DID is short, so an oversized one is treated as an invalid credential.
const maxPrivyDIDBytes = 255

type WatchlistRoutes struct {
	Verifier     privyauth.Verifier
	Store        watchlist.Store
	ChainID      int64
	DeploymentID string
	// Logger records unexpected store failures; Server.RegisterWatchlistRoutes defaults it.
	Logger *slog.Logger
}

// WatchlistAuthInput leaves the credential headers optional at the schema level so a missing
// header yields the same 401 authentication_failed as an invalid one.
type WatchlistAuthInput struct {
	Authorization string `header:"Authorization"`
	IdentityToken string `header:"privy-id-token"`
}
type watchlistEntryInput struct {
	WatchlistAuthInput
	Address string `path:"address"`
}
type watchlistEntryDTO struct {
	Address string                   `json:"address"`
	AddedAt string                   `json:"added_at"`
	Token   nullableObject[TokenDTO] `json:"token"`
}
type watchlistBody struct {
	Items []watchlistEntryDTO `json:"items"`
}
type watchlistOutput struct{ Body watchlistBody }
type watchlistEmptyOutput struct{}

func noStore(ctx huma.Context, next func(huma.Context)) {
	ctx.SetHeader("Cache-Control", "private, no-store")
	next(ctx)
}

func (r WatchlistRoutes) Register(api huma.API) {
	mw := huma.Middlewares{noStore}
	huma.Register(api, huma.Operation{OperationID: "listWatchlist", Method: http.MethodGet, Path: "/me/watchlist", Tags: []string{"watchlist"}, Middlewares: mw}, r.list)
	huma.Register(api, huma.Operation{OperationID: "addWatchlistEntry", Method: http.MethodPut, Path: "/me/watchlist/{address}", Tags: []string{"watchlist"}, DefaultStatus: http.StatusNoContent, Middlewares: mw}, r.add)
	huma.Register(api, huma.Operation{OperationID: "removeWatchlistEntry", Method: http.MethodDelete, Path: "/me/watchlist/{address}", Tags: []string{"watchlist"}, DefaultStatus: http.StatusNoContent, Middlewares: mw}, r.remove)
}

func (r WatchlistRoutes) owner(ctx context.Context, in WatchlistAuthInput) (watchlist.Owner, error) {
	if r.Store == nil || r.Verifier == nil {
		return watchlist.Owner{}, huma.Error503ServiceUnavailable("watchlist unavailable")
	}
	access, err := privyauth.ParseBearer(in.Authorization)
	if err != nil {
		return watchlist.Owner{}, apiProblem(http.StatusUnauthorized, "authentication_failed", "Authentication failed")
	}
	principal, err := r.Verifier.Verify(ctx, access, in.IdentityToken)
	if err != nil || principal.PrivyDID == "" || len(principal.PrivyDID) > maxPrivyDIDBytes {
		return watchlist.Owner{}, apiProblem(http.StatusUnauthorized, "authentication_failed", "Authentication failed")
	}
	return watchlist.Owner{ChainID: r.ChainID, DeploymentID: r.DeploymentID, PrivyDID: principal.PrivyDID}, nil
}

// fail maps a store error to its API problem. Unexpected failures are logged with the request
// ID and the error type only: store errors may embed the DID or query arguments.
func (r WatchlistRoutes) fail(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, watchlist.ErrUnknownToken):
		return apiProblem(http.StatusNotFound, "token_not_found", "Token not found")
	case errors.Is(err, watchlist.ErrFull):
		return apiProblem(http.StatusConflict, "watchlist_full", "Watchlist is full")
	case errors.Is(err, context.Canceled):
		return err
	default:
		if r.Logger != nil {
			r.Logger.ErrorContext(ctx, "watchlist request failed", "request_id", RequestID(ctx), "error_type", fmt.Sprintf("%T", err))
		}
		return apiRequestProblem(ctx, http.StatusInternalServerError, "internal_error", "Request failed")
	}
}

func (r WatchlistRoutes) list(ctx context.Context, in *WatchlistAuthInput) (*watchlistOutput, error) {
	owner, err := r.owner(ctx, *in)
	if err != nil {
		return nil, err
	}
	entries, err := r.Store.List(ctx, owner)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	out := watchlistOutput{Body: watchlistBody{Items: make([]watchlistEntryDTO, 0, len(entries))}}
	for _, e := range entries {
		item := watchlistEntryDTO{Address: e.Token.Hex(), AddedAt: e.AddedAt.UTC().Format("2006-01-02T15:04:05.000000Z")}
		if e.Summary != nil {
			dto := toTokenDTO(*e.Summary)
			item.Token = nullableObject[TokenDTO]{V: &dto}
		}
		out.Body.Items = append(out.Body.Items, item)
	}
	return &out, nil
}

func (r WatchlistRoutes) add(ctx context.Context, in *watchlistEntryInput) (*watchlistEmptyOutput, error) {
	owner, err := r.owner(ctx, in.WatchlistAuthInput)
	if err != nil {
		return nil, err
	}
	token, err := address(in.Address)
	if err != nil {
		return nil, err
	}
	if err := r.Store.Add(ctx, owner, token); err != nil {
		return nil, r.fail(ctx, err)
	}
	return &watchlistEmptyOutput{}, nil
}

func (r WatchlistRoutes) remove(ctx context.Context, in *watchlistEntryInput) (*watchlistEmptyOutput, error) {
	owner, err := r.owner(ctx, in.WatchlistAuthInput)
	if err != nil {
		return nil, err
	}
	token, err := address(in.Address)
	if err != nil {
		return nil, err
	}
	if err := r.Store.Remove(ctx, owner, token); err != nil {
		return nil, r.fail(ctx, err)
	}
	return &watchlistEmptyOutput{}, nil
}
