package apiserver

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/Contictus/launchtap/backend/internal/curve"
	"github.com/Contictus/launchtap/backend/internal/quote"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/danielgtaylor/huma/v2"
	"github.com/ethereum/go-ethereum/common"
)

type QuoteRoutes struct{ Provider quote.Provider }
type quoteInput struct {
	Token string `path:"token"`
	Body  struct {
		Side   string `json:"side" enum:"buy,sell"`
		Amount string `json:"amount"`
	}
}
type quoteOutput struct{ Body quoteBody }
type quoteBody struct {
	Input              string `json:"input"`
	Output             string `json:"output"`
	ProtocolFee        string `json:"protocol_fee"`
	CreatorFee         string `json:"creator_fee"`
	Refund             string `json:"refund"`
	Graduates          bool   `json:"graduates"`
	NextVirtualETH     string `json:"next_virtual_eth"`
	NextVirtualToken   string `json:"next_virtual_token"`
	AsOfBlock          int64  `json:"as_of_block"`
	Finality           string `json:"finality"`
	ReserveSourceBlock int64  `json:"reserve_source_block"`
	ReserveSourceHash  string `json:"reserve_source_hash"`
	SnipeTax           string `json:"snipe_tax" doc:"Engine-2 anti-snipe tax taken from a buy at the indexed head's block time; 0 for engine 1, sells and after the window."`
	TaxBPS             uint16 `json:"tax_bps" doc:"Engine-2 anti-snipe rate in force at the indexed head's block time; 0 for engine 1 and after the window."`
	TaxEndsAt          string `json:"tax_ends_at" doc:"RFC 3339 time at which the anti-snipe tax reaches zero; empty when no tax is in force."`
	Informational      bool   `json:"informational"`
}

func (r QuoteRoutes) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "quoteCurve", Method: http.MethodPost, Path: "/tokens/{token}/quote", Tags: []string{"quotes"}}, r.handle)
}
func (r QuoteRoutes) handle(ctx context.Context, in *quoteInput) (*quoteOutput, error) {
	if r.Provider == nil {
		return nil, huma.Error503ServiceUnavailable("quote provider unavailable")
	}
	if !common.IsHexAddress(in.Token) {
		return nil, huma.Error400BadRequest("invalid token address")
	}
	amount, err := parseCanonicalUint256(in.Body.Amount)
	if err != nil {
		return nil, huma.Error400BadRequest("amount must be a canonical uint256")
	}
	side := in.Body.Side
	if side != "buy" && side != "sell" {
		return nil, huma.Error400BadRequest("side must be buy or sell")
	}
	q, err := r.Provider.Quote(ctx, common.HexToAddress(in.Token), side == "buy", amount)
	if err != nil {
		var phase curve.ErrWrongPhase
		var oversell curve.ErrOversell
		switch {
		case errors.Is(err, token.ErrNotFound):
			return nil, apiProblem(http.StatusNotFound, "token_not_found", "Token not found")
		case errors.As(err, &phase):
			return nil, apiProblem(http.StatusConflict, "wrong_phase", "Token is not in curve phase")
		case errors.As(err, &oversell):
			return nil, apiProblem(http.StatusUnprocessableEntity, "oversell", "Sell amount exceeds sold supply")
		case errors.Is(err, curve.ErrZeroInput):
			return nil, apiProblem(http.StatusBadRequest, "zero_input", "Amount must be greater than zero")
		case errors.Is(err, curve.ErrZeroOutput):
			return nil, apiProblem(http.StatusUnprocessableEntity, "zero_output", "Quote output is zero")
		default:
			return nil, apiRequestProblem(ctx, http.StatusInternalServerError, "quote_failed", "Quote could not be computed")
		}
	}
	return &quoteOutput{Body: quoteBody{Input: decimal(q.Input), Output: decimal(q.Output), ProtocolFee: decimal(q.ProtocolFee), CreatorFee: decimal(q.CreatorFee), Refund: decimal(q.Refund), Graduates: q.Graduates, NextVirtualETH: decimal(q.NextVirtualETH), NextVirtualToken: decimal(q.NextVirtualToken), AsOfBlock: q.AsOfBlock, Finality: q.Finality, ReserveSourceBlock: q.ReserveSourceBlock, ReserveSourceHash: q.ReserveSourceHash.Hex(), SnipeTax: decimal(q.SnipeTax), TaxBPS: q.TaxBPS, TaxEndsAt: rfc3339OrEmpty(q.TaxEndsAt), Informational: true}}, nil
}
func parseCanonicalUint256(s string) (*big.Int, error) {
	if s == "" || len(s) > 78 || (len(s) > 1 && s[0] == '0') || s[0] == '-' || strings.ContainsAny(s, ".+eE") {
		return nil, curve.ErrInvalidAmount{Field: "amount"}
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() < 0 || n.BitLen() > 256 {
		return nil, curve.ErrInvalidAmount{Field: "amount"}
	}
	return n, nil
}

func rfc3339OrEmpty(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}
