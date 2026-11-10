package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Contictus/launchtap/backend/internal/inbox"
	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/danielgtaylor/huma/v2"
)

// InboxRoutes serves the signed-in account's notification inbox (spec P0-C4).
type InboxRoutes struct {
	Verifier     privyauth.Verifier
	Store        inbox.Store
	ChainID      int64
	DeploymentID string
	// Logger records unexpected store failures; Server.RegisterInboxRoutes defaults it.
	Logger *slog.Logger
}

type inboxListInput struct {
	WatchlistAuthInput
	Cursor string `query:"cursor" maxLength:"64" doc:"next_cursor of the previous page."`
	Limit  int    `query:"limit" default:"20" doc:"Number of notifications, 1-50; defaults to 20."`
}

type inboxItemDTO struct {
	ID        string  `json:"id" doc:"<block>-<tx_index>-<log_index>; also usable as a cursor and a read position."`
	Kind      string  `json:"kind" enum:"graduation,large_trade"`
	Token     string  `json:"token"`
	Name      string  `json:"name"`
	Symbol    string  `json:"symbol"`
	BlockTime string  `json:"block_time"`
	TxHash    string  `json:"tx_hash"`
	ETHWei    string  `json:"eth_wei" doc:"Gross trade amount, or the ETH moved to the pool at graduation."`
	SideBuy   *bool   `json:"side_buy" nullable:"true" doc:"large_trade only."`
	Trader    *string `json:"trader" nullable:"true" doc:"large_trade only; null when the pool-swap sender is unknown."`
	Source    *string `json:"source" nullable:"true" enum:"curve,dex" doc:"large_trade only."`
}

type inboxBody struct {
	Items       []inboxItemDTO `json:"items"`
	NextCursor  string         `json:"next_cursor,omitempty"`
	UnreadCount int            `json:"unread_count" doc:"Unread notifications, at most 100."`
}
type inboxOutput struct{ Body inboxBody }

type inboxReadInput struct {
	WatchlistAuthInput
	Body struct {
		Through string `json:"through" maxLength:"64" doc:"Mark every notification up to and including this id as read."`
	}
}

func (r InboxRoutes) Register(api huma.API) {
	mw := huma.Middlewares{noStore}
	huma.Register(api, huma.Operation{OperationID: "listNotifications", Method: http.MethodGet, Path: "/me/notifications", Tags: []string{"notifications"}, Middlewares: mw}, r.list)
	huma.Register(api, huma.Operation{OperationID: "markNotificationsRead", Method: http.MethodPut, Path: "/me/notifications/read", Tags: []string{"notifications"}, DefaultStatus: http.StatusNoContent, Middlewares: mw}, r.markRead)
}

func (r InboxRoutes) owner(ctx context.Context, in WatchlistAuthInput) (watchlist.Owner, error) {
	if r.Store == nil || r.Verifier == nil {
		return watchlist.Owner{}, huma.Error503ServiceUnavailable("notifications unavailable")
	}
	principal, err := authenticatePrivy(ctx, r.Verifier, in)
	if err != nil {
		return watchlist.Owner{}, err
	}
	return watchlist.Owner{ChainID: r.ChainID, DeploymentID: r.DeploymentID, PrivyDID: principal.PrivyDID}, nil
}

// fail logs only the request ID and the error type: store errors may embed the DID.
func (r InboxRoutes) fail(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	if r.Logger != nil {
		r.Logger.ErrorContext(ctx, "notifications request failed", "request_id", RequestID(ctx), "error_type", fmt.Sprintf("%T", err))
	}
	return apiRequestProblem(ctx, http.StatusInternalServerError, "internal_error", "Request failed")
}

func (r InboxRoutes) list(ctx context.Context, in *inboxListInput) (*inboxOutput, error) {
	owner, err := r.owner(ctx, in.WatchlistAuthInput)
	if err != nil {
		return nil, err
	}
	if in.Limit < 1 || in.Limit > inbox.MaxPageSize {
		return nil, apiProblem(http.StatusUnprocessableEntity, "invalid_limit", "Limit must be between 1 and 50")
	}
	var before *inbox.Position
	if in.Cursor != "" {
		p, err := inbox.ParsePosition(in.Cursor)
		if err != nil {
			return nil, apiProblem(http.StatusUnprocessableEntity, "invalid_cursor", "Invalid cursor")
		}
		before = &p
	}
	page, err := r.Store.List(ctx, owner, before, in.Limit)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	out := inboxOutput{Body: inboxBody{Items: make([]inboxItemDTO, 0, len(page.Items)), UnreadCount: page.Unread}}
	if page.Next != nil {
		out.Body.NextCursor = page.Next.String()
	}
	for _, e := range page.Items {
		item := inboxItemDTO{
			ID: e.Position.String(), Kind: e.Kind, Token: e.Token.Hex(), Name: e.Name, Symbol: e.Symbol,
			BlockTime: e.BlockTime.UTC().Format("2006-01-02T15:04:05.000000Z"), TxHash: e.TxHash.Hex(),
			ETHWei: decimal(e.ETHWei), SideBuy: e.SideBuy,
		}
		if e.Trader != nil {
			trader := e.Trader.Hex()
			item.Trader = &trader
		}
		if e.Source != "" {
			source := e.Source
			item.Source = &source
		}
		out.Body.Items = append(out.Body.Items, item)
	}
	return &out, nil
}

func (r InboxRoutes) markRead(ctx context.Context, in *inboxReadInput) (*watchlistEmptyOutput, error) {
	owner, err := r.owner(ctx, in.WatchlistAuthInput)
	if err != nil {
		return nil, err
	}
	through, err := inbox.ParsePosition(in.Body.Through)
	if err != nil {
		return nil, apiProblem(http.StatusUnprocessableEntity, "invalid_notification_id", "Invalid notification id")
	}
	if err := r.Store.MarkRead(ctx, owner, through); err != nil {
		return nil, r.fail(ctx, err)
	}
	return &watchlistEmptyOutput{}, nil
}
