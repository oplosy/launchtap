package apiserver

import (
	"context"
	"net/http"
	"time"

	"github.com/Contictus/launchtap/backend/internal/status"
	"github.com/danielgtaylor/huma/v2"
)

// StatusRoutes serves GET /v1/status: whether the indexed data is current. Unlike /readyz it
// answers 200 while the indexer is behind, so the web can keep serving and show a notice.
type StatusRoutes struct {
	Reader       status.Reader
	ChainID      int64
	DeploymentID string
}

type indexerStatusDTO struct {
	Stale               bool    `json:"stale" doc:"True when the indexer has not committed a step within stale_after_seconds, or never has."`
	StaleAfterSeconds   int64   `json:"stale_after_seconds"`
	HeartbeatAt         *string `json:"heartbeat_at" nullable:"true" doc:"Database time of the last committed indexer step (RFC 3339), or null before the first."`
	HeartbeatAgeSeconds *int64  `json:"heartbeat_age_seconds" nullable:"true" doc:"Whole seconds since heartbeat_at, or null before the first step."`
	ObservedBlock       *int64  `json:"observed_block" nullable:"true"`
	SafeBlock           *int64  `json:"safe_block" nullable:"true"`
	FinalizedBlock      *int64  `json:"finalized_block" nullable:"true"`
}

type statusBody struct {
	ChainID      int64            `json:"chain_id"`
	DeploymentID string           `json:"deployment_id"`
	Indexer      indexerStatusDTO `json:"indexer"`
}

type statusOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         statusBody
}

func (r StatusRoutes) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "getStatus", Method: http.MethodGet, Path: "/status", Tags: []string{"system"}}, r.get)
}

func (r StatusRoutes) get(ctx context.Context, _ *struct{}) (*statusOutput, error) {
	if r.Reader == nil {
		return nil, huma.Error503ServiceUnavailable("status unavailable")
	}
	indexer, err := r.Reader.Indexer(ctx, r.ChainID, r.DeploymentID)
	if err != nil {
		return nil, internalProblem(ctx, err, "internal_error", "Request failed")
	}
	dto := indexerStatusDTO{
		Stale: indexer.Stale(), StaleAfterSeconds: int64(status.IndexerStaleAfter / time.Second),
		ObservedBlock: indexer.ObservedBlock, SafeBlock: indexer.SafeBlock, FinalizedBlock: indexer.FinalizedBlock,
	}
	if age, ok := indexer.HeartbeatAge(); ok {
		at := indexer.HeartbeatAt.UTC().Format(time.RFC3339)
		seconds := int64(age / time.Second)
		dto.HeartbeatAt, dto.HeartbeatAgeSeconds = &at, &seconds
	}
	return &statusOutput{CacheControl: "no-store", Body: statusBody{ChainID: r.ChainID, DeploymentID: r.DeploymentID, Indexer: dto}}, nil
}
