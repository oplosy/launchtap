package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/social"
	"github.com/danielgtaylor/huma/v2"
	"github.com/ethereum/go-ethereum/common"
)

// PublicProfileRoutes serves the caller's username and published wallets.
type PublicProfileRoutes struct {
	Verifier     privyauth.Verifier
	Store        social.ProfileStore
	ChainID      int64
	DeploymentID string
	// Logger records unexpected store failures; Server.RegisterPublicProfileRoutes defaults it.
	Logger *slog.Logger
}

var addressPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

type publicProfileDTO struct {
	Username             *string  `json:"username" nullable:"true"`
	UsernameChangedAt    *string  `json:"username_changed_at" nullable:"true"`
	NextUsernameChangeAt *string  `json:"next_username_change_at" nullable:"true" doc:"Null when a username change is allowed now."`
	PublicWallets        []string `json:"public_wallets" doc:"Checksummed wallets the username is published on."`
	Banned               bool     `json:"banned"`
}

type publicProfileOutput struct{ Body publicProfileDTO }

type putPublicProfileInput struct {
	WatchlistAuthInput
	Body struct {
		Username      *string  `json:"username,omitempty" maxLength:"64" doc:"New username; omit to keep the current one."`
		PublicWallets []string `json:"public_wallets" maxItems:"16" doc:"Linked wallets to publish the username on, at most 5."`
	}
}

type deletePublicProfileOutput struct{}

func (r PublicProfileRoutes) Register(api huma.API) {
	private := huma.Middlewares{noStore}
	huma.Register(api, huma.Operation{OperationID: "getPublicProfile", Method: http.MethodGet, Path: "/me/public-profile", Tags: []string{"profile"}, Middlewares: private}, r.get)
	huma.Register(api, huma.Operation{OperationID: "putPublicProfile", Method: http.MethodPut, Path: "/me/public-profile", Tags: []string{"profile"}, Middlewares: private}, r.put)
	huma.Register(api, huma.Operation{OperationID: "deletePublicProfile", Method: http.MethodDelete, Path: "/me/public-profile", Tags: []string{"profile"}, DefaultStatus: http.StatusNoContent, Middlewares: private}, r.delete)
}

func (r PublicProfileRoutes) actor(ctx context.Context, in WatchlistAuthInput) (social.Actor, error) {
	if r.Store == nil || r.Verifier == nil {
		return social.Actor{}, huma.Error503ServiceUnavailable("profiles unavailable")
	}
	principal, err := authenticatePrivy(ctx, r.Verifier, in)
	if err != nil {
		return social.Actor{}, err
	}
	return social.Actor{
		Scope:    social.Scope{ChainID: r.ChainID, DeploymentID: r.DeploymentID},
		PrivyDID: principal.PrivyDID, Wallets: principal.Wallets,
	}, nil
}

// fail maps a store or rule error to its API problem. Unexpected failures log only the request ID
// and the error type: store errors may embed the DID or the username.
func (r PublicProfileRoutes) fail(ctx context.Context, err error) error {
	var limited *social.RateLimitedError
	switch {
	case errors.As(err, &limited):
		seconds := int64((limited.RetryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		return huma.ErrorWithHeaders(
			apiProblem(http.StatusTooManyRequests, "rate_limited", "Username was changed recently"),
			http.Header{"Retry-After": []string{strconv.FormatInt(seconds, 10)}})
	case errors.Is(err, social.ErrInvalidUsername):
		return apiProblem(http.StatusUnprocessableEntity, "invalid_username", "Username must be 3-20 characters of a-z, 0-9 and _ and must not start with 0x")
	case errors.Is(err, social.ErrUsernameReserved):
		return apiProblem(http.StatusUnprocessableEntity, "username_reserved", "Username is reserved")
	case errors.Is(err, social.ErrTooManyPublicWallets):
		return apiProblem(http.StatusUnprocessableEntity, "too_many_public_wallets", fmt.Sprintf("At most %d public wallets", social.MaxPublicWallets))
	case errors.Is(err, social.ErrUsernameTaken):
		return apiProblem(http.StatusConflict, "username_taken", "Username is taken")
	case errors.Is(err, social.ErrWalletNotLinked):
		return apiProblem(http.StatusForbidden, "wallet_not_linked", "Wallet is not linked to this account")
	case errors.Is(err, social.ErrBanned):
		return apiProblem(http.StatusForbidden, "banned", "Account is banned")
	case errors.Is(err, context.Canceled):
		return err
	default:
		if r.Logger != nil {
			r.Logger.ErrorContext(ctx, "profile request failed", "request_id", RequestID(ctx), "error_type", fmt.Sprintf("%T", err))
		}
		return apiRequestProblem(ctx, http.StatusInternalServerError, "internal_error", "Request failed")
	}
}

func toPublicProfileDTO(p social.PublicProfile) publicProfileDTO {
	dto := publicProfileDTO{Username: p.Username, Banned: p.Banned, PublicWallets: make([]string, 0, len(p.PublicWallets))}
	if p.UsernameChangedAt != nil {
		v := p.UsernameChangedAt.UTC().Format(commentTimeLayout)
		dto.UsernameChangedAt = &v
	}
	if p.NextChangeAt != nil {
		v := p.NextChangeAt.UTC().Format(commentTimeLayout)
		dto.NextUsernameChangeAt = &v
	}
	for _, w := range p.PublicWallets {
		dto.PublicWallets = append(dto.PublicWallets, w.Hex())
	}
	return dto
}

func (r PublicProfileRoutes) get(ctx context.Context, in *WatchlistAuthInput) (*publicProfileOutput, error) {
	a, err := r.actor(ctx, *in)
	if err != nil {
		return nil, err
	}
	p, err := r.Store.Get(ctx, a)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	return &publicProfileOutput{Body: toPublicProfileDTO(p)}, nil
}

func (r PublicProfileRoutes) put(ctx context.Context, in *putPublicProfileInput) (*publicProfileOutput, error) {
	a, err := r.actor(ctx, in.WatchlistAuthInput)
	if err != nil {
		return nil, err
	}
	var username *string
	if in.Body.Username != nil {
		name, err := social.NormalizeUsername(*in.Body.Username)
		if err != nil {
			return nil, r.fail(ctx, err)
		}
		username = &name
	}
	wallets := make([]common.Address, 0, len(in.Body.PublicWallets))
	for _, raw := range in.Body.PublicWallets {
		if !addressPattern.MatchString(raw) {
			return nil, apiProblem(http.StatusUnprocessableEntity, "invalid_wallet", "public_wallets entries must be 0x-prefixed 20-byte hex addresses")
		}
		wallets = append(wallets, common.HexToAddress(raw))
	}
	p, err := r.Store.Put(ctx, a, username, wallets)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	return &publicProfileOutput{Body: toPublicProfileDTO(p)}, nil
}

func (r PublicProfileRoutes) delete(ctx context.Context, in *WatchlistAuthInput) (*deletePublicProfileOutput, error) {
	a, err := r.actor(ctx, *in)
	if err != nil {
		return nil, err
	}
	if err := r.Store.Delete(ctx, a); err != nil {
		return nil, r.fail(ctx, err)
	}
	return &deletePublicProfileOutput{}, nil
}
