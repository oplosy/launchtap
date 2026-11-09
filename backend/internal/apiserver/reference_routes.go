package apiserver

import (
	"context"
	"net/http"
	"time"

	"github.com/Contictus/launchtap/backend/internal/pricefeed"
	"github.com/danielgtaylor/huma/v2"
)

const referenceCacheControl = "public, max-age=15"

// PriceSource supplies the latest ETH/USD reference sample.
type PriceSource interface{ Snapshot() pricefeed.Snapshot }

// ReferenceRoutes serves external reference data. A nil Prices means the
// source is disabled and every response field is null.
type ReferenceRoutes struct{ Prices PriceSource }

type ethUsdBody struct {
	Price             *string    `json:"price" nullable:"true" doc:"ETH/USD decimal string; null when disabled, missing or stale."`
	ProviderUpdatedAt *time.Time `json:"provider_updated_at" nullable:"true"`
	FetchedAt         *time.Time `json:"fetched_at" nullable:"true"`
	Source            *string    `json:"source" nullable:"true"`
	AttributionURL    *string    `json:"attribution_url" nullable:"true"`
}

type ethUsdOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         ethUsdBody
}

func (r ReferenceRoutes) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "getEthUsdReference", Method: http.MethodGet, Path: "/reference/eth-usd", Tags: []string{"reference"}}, r.ethUsd)
}

// ethUsd never fails because of the provider: unavailable data is null.
func (r ReferenceRoutes) ethUsd(context.Context, *struct{}) (*ethUsdOutput, error) {
	out := &ethUsdOutput{CacheControl: referenceCacheControl}
	if r.Prices == nil {
		return out, nil
	}
	snap := r.Prices.Snapshot()
	source, attribution := pricefeed.Source, pricefeed.AttributionURL
	out.Body = ethUsdBody{Price: snap.Price, ProviderUpdatedAt: snap.ProviderUpdatedAt, FetchedAt: snap.FetchedAt, Source: &source, AttributionURL: &attribution}
	return out, nil
}
