package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"

	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/telegram"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/danielgtaylor/huma/v2"
)

// TelegramRoutes link a Telegram chat and keep its alert settings
// (docs/specs/2026-11-12-telegram-alerts-design.md).
type TelegramRoutes struct {
	Verifier     privyauth.Verifier
	Store        telegram.AccountStore
	ChainID      int64
	DeploymentID string
	// BotUsername is the bot's Telegram username without "@"; empty turns the routes off (503).
	BotUsername string
	// NewCode defaults to telegram.NewCode; tests replace it.
	NewCode func() (string, [32]byte, error)
	Logger  *slog.Logger
}

type telegramSettingsDTO struct {
	Graduations bool   `json:"graduations" doc:"Send graduations of watched tokens."`
	LargeTrades bool   `json:"large_trades" doc:"Send trades of at least min_trade_wei on watched tokens."`
	MinTradeWei string `json:"min_trade_wei" maxLength:"80" doc:"Decimal wei, 0.5 ETH to 10,000 ETH."`
}

type telegramStatusBody struct {
	Linked      bool    `json:"linked"`
	LinkedAt    *string `json:"linked_at" nullable:"true"`
	Graduations bool    `json:"graduations"`
	LargeTrades bool    `json:"large_trades"`
	MinTradeWei string  `json:"min_trade_wei" doc:"Decimal wei; the default 0.5 ETH when not linked."`
}
type telegramStatusOutput struct{ Body telegramStatusBody }

type telegramSettingsInput struct {
	WatchlistAuthInput
	Body telegramSettingsDTO
}

type telegramLinkBody struct {
	URL       string `json:"url" doc:"Telegram deep link; open it and press Start to connect the chat."`
	ExpiresAt string `json:"expires_at"`
}
type telegramLinkOutput struct{ Body telegramLinkBody }

func (r TelegramRoutes) Register(api huma.API) {
	mw := huma.Middlewares{noStore}
	tags := []string{"telegram"}
	huma.Register(api, huma.Operation{OperationID: "getTelegram", Method: http.MethodGet, Path: "/me/telegram", Tags: tags, Middlewares: mw}, r.status)
	huma.Register(api, huma.Operation{OperationID: "updateTelegram", Method: http.MethodPut, Path: "/me/telegram", Tags: tags, Middlewares: mw}, r.update)
	huma.Register(api, huma.Operation{OperationID: "unlinkTelegram", Method: http.MethodDelete, Path: "/me/telegram", Tags: tags, DefaultStatus: http.StatusNoContent, Middlewares: mw}, r.unlink)
	huma.Register(api, huma.Operation{OperationID: "createTelegramLink", Method: http.MethodPost, Path: "/me/telegram/link", Tags: tags, Middlewares: mw}, r.link)
}

func (r TelegramRoutes) owner(ctx context.Context, in WatchlistAuthInput) (watchlist.Owner, error) {
	if r.Store == nil || r.Verifier == nil || r.BotUsername == "" {
		return watchlist.Owner{}, apiProblem(http.StatusServiceUnavailable, "telegram_unavailable", "Telegram alerts are not available")
	}
	principal, err := authenticatePrivy(ctx, r.Verifier, in)
	if err != nil {
		return watchlist.Owner{}, err
	}
	return watchlist.Owner{ChainID: r.ChainID, DeploymentID: r.DeploymentID, PrivyDID: principal.PrivyDID}, nil
}

// fail logs only the request ID and the error type: store errors may embed the DID.
func (r TelegramRoutes) fail(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	if r.Logger != nil {
		r.Logger.ErrorContext(ctx, "telegram request failed", "request_id", RequestID(ctx), "error_type", fmt.Sprintf("%T", err))
	}
	return apiRequestProblem(ctx, http.StatusInternalServerError, "internal_error", "Request failed")
}

func telegramStatusDTO(s telegram.Status) telegramStatusBody {
	body := telegramStatusBody{
		Linked: s.Linked, Graduations: s.Settings.Graduations, LargeTrades: s.Settings.LargeTrades, MinTradeWei: decimal(s.Settings.MinTradeWei),
	}
	if s.Linked {
		at := s.LinkedAt.UTC().Format("2006-01-02T15:04:05.000000Z")
		body.LinkedAt = &at
	}
	return body
}

func (r TelegramRoutes) status(ctx context.Context, in *WatchlistAuthInput) (*telegramStatusOutput, error) {
	owner, err := r.owner(ctx, *in)
	if err != nil {
		return nil, err
	}
	status, err := r.Store.Status(ctx, owner)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	return &telegramStatusOutput{Body: telegramStatusDTO(status)}, nil
}

func (r TelegramRoutes) update(ctx context.Context, in *telegramSettingsInput) (*telegramStatusOutput, error) {
	owner, err := r.owner(ctx, in.WatchlistAuthInput)
	if err != nil {
		return nil, err
	}
	threshold, ok := parseCanonicalDecimal(in.Body.MinTradeWei)
	settings := telegram.Settings{Graduations: in.Body.Graduations, LargeTrades: in.Body.LargeTrades, MinTradeWei: threshold}
	if !ok || settings.Validate() != nil {
		return nil, apiProblem(http.StatusUnprocessableEntity, "invalid_threshold", "min_trade_wei must be a whole number of wei from 0.5 ETH to 10,000 ETH")
	}
	err = r.Store.UpdateSettings(ctx, owner, settings)
	if errors.Is(err, telegram.ErrNotLinked) {
		return nil, apiProblem(http.StatusNotFound, "telegram_not_linked", "No Telegram chat is connected")
	}
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	status, err := r.Store.Status(ctx, owner)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	return &telegramStatusOutput{Body: telegramStatusDTO(status)}, nil
}

func (r TelegramRoutes) unlink(ctx context.Context, in *WatchlistAuthInput) (*watchlistEmptyOutput, error) {
	owner, err := r.owner(ctx, *in)
	if err != nil {
		return nil, err
	}
	if err := r.Store.Unlink(ctx, owner); err != nil {
		return nil, r.fail(ctx, err)
	}
	return &watchlistEmptyOutput{}, nil
}

func (r TelegramRoutes) link(ctx context.Context, in *WatchlistAuthInput) (*telegramLinkOutput, error) {
	owner, err := r.owner(ctx, *in)
	if err != nil {
		return nil, err
	}
	newCode := r.NewCode
	if newCode == nil {
		newCode = telegram.NewCode
	}
	code, digest, err := newCode()
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	expires, err := r.Store.CreateLinkCode(ctx, owner, digest)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	return &telegramLinkOutput{Body: telegramLinkBody{
		URL:       "https://t.me/" + r.BotUsername + "?start=" + code,
		ExpiresAt: expires.UTC().Format("2006-01-02T15:04:05.000000Z"),
	}}, nil
}

// parseCanonicalDecimal accepts digits only, without leading zeros, up to 78 digits.
func parseCanonicalDecimal(s string) (*big.Int, bool) {
	if s == "" || len(s) > 78 || (len(s) > 1 && s[0] == '0') {
		return nil, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return nil, false
		}
	}
	return new(big.Int).SetString(s, 10)
}
