package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/social"
	"github.com/danielgtaylor/huma/v2"
	"github.com/ethereum/go-ethereum/common"
)

const (
	commentListCacheControl = "public, max-age=5"
	commentTimeLayout       = "2006-01-02T15:04:05.000000Z"
)

// CommentRoutes serves token comments and reports.
type CommentRoutes struct {
	Verifier     privyauth.Verifier
	Store        social.CommentStore
	ChainID      int64
	DeploymentID string
	// Logger records unexpected store failures; Server.RegisterCommentRoutes defaults it.
	Logger *slog.Logger
}

type commentAuthorDTO struct {
	Username *string `json:"username" nullable:"true" doc:"Lowercase username, or null when the author has none or is not public."`
	Wallet   *string `json:"wallet" nullable:"true" doc:"Checksummed posting wallet, or null when the author shows a username only."`
	Role     string  `json:"role" enum:"creator,holder"`
}

type commentDTO struct {
	ID        string           `json:"id" doc:"Decimal string."`
	Token     string           `json:"token"`
	Body      string           `json:"body"`
	CreatedAt string           `json:"created_at"`
	Author    commentAuthorDTO `json:"author"`
}

type commentListInput struct {
	Address string `path:"address" pattern:"^0x[0-9a-fA-F]{40}$"`
	Cursor  string `query:"cursor" maxLength:"256"`
	Limit   int    `query:"limit" default:"20" doc:"Number of comments, 1-50; defaults to 20."`
}
type commentListBody struct {
	Items        []commentDTO `json:"items"`
	NextCursor   string       `json:"next_cursor,omitempty"`
	CommentCount int64        `json:"comment_count"`
}
type commentListOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         commentListBody
}

type commentTokenInput struct {
	WatchlistAuthInput
	Address string `path:"address" pattern:"^0x[0-9a-fA-F]{40}$"`
}
type eligibleWalletDTO struct {
	Address string `json:"address"`
	Role    string `json:"role" enum:"creator,holder"`
}
type eligibilityBody struct {
	EligibleWallets []eligibleWalletDTO `json:"eligible_wallets"`
	Banned          bool                `json:"banned"`
	Username        *string             `json:"username" nullable:"true"`
}
type eligibilityOutput struct{ Body eligibilityBody }

type postCommentInput struct {
	WatchlistAuthInput
	Address string `path:"address" pattern:"^0x[0-9a-fA-F]{40}$"`
	Body    struct {
		Body   string `json:"body" maxLength:"2000" doc:"Comment text, at most 500 code points after normalization."`
		Wallet string `json:"wallet" pattern:"^0x[0-9a-fA-F]{40}$" doc:"Linked wallet that posts; must be the token creator or a holder."`
	}
}
type postCommentOutput struct{ Body commentDTO }

type reportCommentInput struct {
	WatchlistAuthInput
	ID   int64 `path:"id"`
	Body struct {
		Reason string `json:"reason" maxLength:"16" doc:"One of spam, scam, abuse, other."`
	}
}
type reportCommentOutput struct{}

func (r CommentRoutes) Register(api huma.API) {
	private := huma.Middlewares{noStore}
	huma.Register(api, huma.Operation{OperationID: "listTokenComments", Method: http.MethodGet, Path: "/tokens/{address}/comments", Tags: []string{"comments"}}, r.list)
	huma.Register(api, huma.Operation{OperationID: "getCommentEligibility", Method: http.MethodGet, Path: "/tokens/{address}/comments/eligibility", Tags: []string{"comments"}, Middlewares: private}, r.eligibility)
	huma.Register(api, huma.Operation{OperationID: "postTokenComment", Method: http.MethodPost, Path: "/tokens/{address}/comments", Tags: []string{"comments"}, DefaultStatus: http.StatusCreated, Middlewares: private}, r.post)
	huma.Register(api, huma.Operation{OperationID: "reportComment", Method: http.MethodPut, Path: "/comments/{id}/report", Tags: []string{"comments"}, DefaultStatus: http.StatusNoContent, Middlewares: private}, r.report)
}

func (r CommentRoutes) scope() social.Scope {
	return social.Scope{ChainID: r.ChainID, DeploymentID: r.DeploymentID}
}

// actor authenticates the caller and returns it with its de-duplicated linked wallets.
func (r CommentRoutes) actor(ctx context.Context, in WatchlistAuthInput) (social.Actor, error) {
	if r.Store == nil || r.Verifier == nil {
		return social.Actor{}, huma.Error503ServiceUnavailable("comments unavailable")
	}
	principal, err := authenticatePrivy(ctx, r.Verifier, in)
	if err != nil {
		return social.Actor{}, err
	}
	seen := make(map[common.Address]struct{}, len(principal.Wallets))
	wallets := make([]common.Address, 0, len(principal.Wallets))
	for _, w := range principal.Wallets {
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		wallets = append(wallets, w)
	}
	return social.Actor{Scope: r.scope(), PrivyDID: principal.PrivyDID, Wallets: wallets}, nil
}

// fail maps a store or rule error to its API problem. Unexpected failures are logged with the
// request ID and the error type only: store errors may embed the DID or the comment body.
func (r CommentRoutes) fail(ctx context.Context, err error) error {
	var body *social.BodyError
	var limited *social.RateLimitedError
	switch {
	case errors.As(err, &body):
		return apiProblem(http.StatusUnprocessableEntity, "invalid_comment", "comment rule: "+body.Rule)
	case errors.As(err, &limited):
		seconds := int64((limited.RetryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		return huma.ErrorWithHeaders(
			apiProblem(http.StatusTooManyRequests, "rate_limited", "Too many requests"),
			http.Header{"Retry-After": []string{strconv.FormatInt(seconds, 10)}})
	case errors.Is(err, social.ErrUnknownToken):
		return apiProblem(http.StatusNotFound, "token_not_found", "Token not found")
	case errors.Is(err, social.ErrCommentNotFound):
		return apiProblem(http.StatusNotFound, "comment_not_found", "Comment not found")
	case errors.Is(err, social.ErrWalletNotLinked):
		return apiProblem(http.StatusForbidden, "wallet_not_linked", "Wallet is not linked to this account")
	case errors.Is(err, social.ErrNotHolder):
		return apiProblem(http.StatusForbidden, "not_holder", "Wallet is neither the creator nor a holder of this token")
	case errors.Is(err, social.ErrBanned):
		return apiProblem(http.StatusForbidden, "banned", "Account is banned")
	case errors.Is(err, social.ErrOwnComment):
		return apiProblem(http.StatusUnprocessableEntity, "own_comment", "You cannot report your own comment")
	case errors.Is(err, social.ErrInvalidReport):
		return apiProblem(http.StatusUnprocessableEntity, "invalid_report", "Unknown report reason")
	case errors.Is(err, social.ErrInvalidCursor):
		return apiProblem(http.StatusUnprocessableEntity, "invalid_cursor", "Cursor is invalid")
	case errors.Is(err, context.Canceled):
		return err
	default:
		if r.Logger != nil {
			r.Logger.ErrorContext(ctx, "comment request failed", "request_id", RequestID(ctx), "error_type", fmt.Sprintf("%T", err))
		}
		return apiRequestProblem(ctx, http.StatusInternalServerError, "internal_error", "Request failed")
	}
}

func toCommentDTO(c social.Comment) commentDTO {
	dto := commentDTO{
		ID: strconv.FormatInt(c.ID, 10), Token: c.Token.Hex(), Body: c.Body,
		CreatedAt: c.CreatedAt.UTC().Format(commentTimeLayout),
		Author:    commentAuthorDTO{Username: c.Author.Username, Role: string(c.Author.Role)},
	}
	if c.Author.Wallet != nil {
		w := c.Author.Wallet.Hex()
		dto.Author.Wallet = &w
	}
	return dto
}

func (r CommentRoutes) list(ctx context.Context, in *commentListInput) (*commentListOutput, error) {
	if r.Store == nil {
		return nil, huma.Error503ServiceUnavailable("comments unavailable")
	}
	if in.Limit < 1 || in.Limit > social.MaxPageSize {
		return nil, apiProblem(http.StatusUnprocessableEntity, "invalid_limit", "limit must be between 1 and 50")
	}
	token, err := address(in.Address)
	if err != nil {
		return nil, err
	}
	page, err := r.Store.List(ctx, r.scope(), token, in.Cursor, in.Limit)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	out := commentListOutput{CacheControl: commentListCacheControl, Body: commentListBody{
		Items: make([]commentDTO, 0, len(page.Items)), NextCursor: page.NextCursor, CommentCount: page.Count,
	}}
	for _, c := range page.Items {
		out.Body.Items = append(out.Body.Items, toCommentDTO(c))
	}
	return &out, nil
}

func (r CommentRoutes) eligibility(ctx context.Context, in *commentTokenInput) (*eligibilityOutput, error) {
	actor, err := r.actor(ctx, in.WatchlistAuthInput)
	if err != nil {
		return nil, err
	}
	token, err := address(in.Address)
	if err != nil {
		return nil, err
	}
	e, err := r.Store.Eligibility(ctx, actor, token)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	out := eligibilityOutput{Body: eligibilityBody{
		EligibleWallets: make([]eligibleWalletDTO, 0, len(e.Wallets)), Banned: e.Banned, Username: e.Username,
	}}
	for _, w := range e.Wallets {
		out.Body.EligibleWallets = append(out.Body.EligibleWallets, eligibleWalletDTO{Address: w.Address.Hex(), Role: string(w.Role)})
	}
	return &out, nil
}

func (r CommentRoutes) post(ctx context.Context, in *postCommentInput) (*postCommentOutput, error) {
	actor, err := r.actor(ctx, in.WatchlistAuthInput)
	if err != nil {
		return nil, err
	}
	token, err := address(in.Address)
	if err != nil {
		return nil, err
	}
	wallet, err := address(in.Body.Wallet)
	if err != nil {
		return nil, err
	}
	body, err := social.NormalizeBody(in.Body.Body)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	c, err := r.Store.Post(ctx, actor, token, wallet, body)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	return &postCommentOutput{Body: toCommentDTO(c)}, nil
}

func (r CommentRoutes) report(ctx context.Context, in *reportCommentInput) (*reportCommentOutput, error) {
	actor, err := r.actor(ctx, in.WatchlistAuthInput)
	if err != nil {
		return nil, err
	}
	reason, err := social.ParseReportReason(in.Body.Reason)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	if in.ID <= 0 {
		return nil, r.fail(ctx, social.ErrCommentNotFound)
	}
	if err := r.Store.Report(ctx, actor, in.ID, reason); err != nil {
		return nil, r.fail(ctx, err)
	}
	return &reportCommentOutput{}, nil
}
