package apiserver

import (
	"context"
	"net/http"

	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/profile"
	"github.com/danielgtaylor/huma/v2"
)

type ProfileRoutes struct {
	Reader   profile.Reader
	Verifier privyauth.Verifier
	ChainID  int64
}

type profileInput struct {
	Authorization string `header:"Authorization" required:"true"`
	IdentityToken string `header:"privy-id-token" required:"true"`
}

type profileActionDTO struct {
	Token       string `json:"token"`
	Curve       string `json:"curve"`
	Name        string `json:"name"`
	Symbol      string `json:"symbol"`
	Phase       string `json:"phase"`
	CreatorFees string `json:"creator_fees"`
	Refund      string `json:"refund"`
}

type profileBody struct {
	Snapshot snapshotDTO        `json:"snapshot"`
	Items    []profileActionDTO `json:"items"`
}

type profileOutput struct{ Body profileBody }

func (r ProfileRoutes) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "getProfile", Method: http.MethodGet, Path: "/profile", Tags: []string{"profile"}, Middlewares: huma.Middlewares{noStore}}, r.get)
}

func (r ProfileRoutes) get(ctx context.Context, in *profileInput) (*profileOutput, error) {
	if r.Reader == nil || r.Verifier == nil {
		return nil, huma.Error503ServiceUnavailable("profile reader unavailable")
	}
	principal, err := authenticatePrivy(ctx, r.Verifier, WatchlistAuthInput{Authorization: in.Authorization, IdentityToken: in.IdentityToken})
	if err != nil {
		return nil, err
	}
	page, err := r.Reader.List(ctx, r.ChainID, principal.Wallets)
	if err != nil {
		return nil, mapReadError(ctx, err)
	}
	body := profileBody{Snapshot: snapDTO(page.Snapshot, page.Finality), Items: make([]profileActionDTO, 0, len(page.Items))}
	for _, item := range page.Items {
		body.Items = append(body.Items, profileActionDTO{Token: item.Token.Hex(), Curve: item.Curve.Hex(), Name: item.Name, Symbol: item.Symbol, Phase: item.Phase, CreatorFees: decimal(item.CreatorFees), Refund: decimal(item.Refund)})
	}
	return &profileOutput{Body: body}, nil
}
