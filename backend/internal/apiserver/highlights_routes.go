package apiserver

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/danielgtaylor/huma/v2"
)

const (
	highlightsWindow = 60 * time.Minute
	highlightsLimit  = 20
	highlightsTTL    = 15 * time.Second
)

// HighlightsRoutes serves GET /v1/tokens/highlights behind a single-entry TTL cache.
type HighlightsRoutes struct {
	Reader  token.HighlightsReader
	ChainID int64
	// Now supplies the request clock; nil means time.Now.
	Now func() time.Time
	// TTL bounds cache age; zero means 15s.
	TTL time.Duration
}

type trendingDTO struct {
	TokenDTO
	WindowVolumeETH string `json:"window_volume_eth"`
	DistinctBuyers  int64  `json:"distinct_buyers"`
}
type kingDTO struct {
	TokenDTO
	GraduationProgressBPS int32  `json:"graduation_progress_bps"`
	RemainingETH          string `json:"remaining_eth"`
}
type highlightsBody struct {
	Trending    []trendingDTO           `json:"trending"`
	King        nullableObject[kingDTO] `json:"king_of_the_hill"`
	WindowStart string                  `json:"window_start"`
	AsOf        string                  `json:"as_of"`
	Finality    string                  `json:"finality"`
	Snapshot    snapshotDTO             `json:"snapshot"`
}
type highlightsOutput struct{ Body highlightsBody }

type highlightsCache struct {
	mu        sync.Mutex
	body      *highlightsBody
	fetchedAt time.Time
}

func (r HighlightsRoutes) Register(api huma.API) {
	cache := &highlightsCache{}
	huma.Register(api, huma.Operation{OperationID: "getTokenHighlights", Method: http.MethodGet, Path: "/tokens/highlights", Tags: []string{"tokens"}},
		func(ctx context.Context, _ *struct{}) (*highlightsOutput, error) { return r.handle(ctx, cache) })
}

func (r HighlightsRoutes) handle(ctx context.Context, cache *highlightsCache) (*highlightsOutput, error) {
	if r.Reader == nil {
		return nil, huma.Error503ServiceUnavailable("highlights reader unavailable")
	}
	clock := time.Now
	if r.Now != nil {
		clock = r.Now
	}
	ttl := r.TTL
	if ttl <= 0 {
		ttl = highlightsTTL
	}
	// The lock is held across the read so concurrent misses share one query. A failed read
	// leaves the previous entry untouched and caches nothing.
	cache.mu.Lock()
	defer cache.mu.Unlock()
	now := clock().UTC()
	if cache.body != nil {
		if age := now.Sub(cache.fetchedAt); age >= 0 && age < ttl {
			return &highlightsOutput{Body: *cache.body}, nil
		}
	}
	h, err := r.Reader.Highlights(ctx, r.ChainID, now.Add(-highlightsWindow), highlightsLimit)
	if err != nil {
		return nil, mapReadError(ctx, err)
	}
	body := highlightsBody{
		Trending:    make([]trendingDTO, 0, len(h.Trending)),
		WindowStart: h.WindowStart.UTC().Format(time.RFC3339Nano),
		AsOf:        now.Format(time.RFC3339Nano),
		Finality:    h.Finality,
		Snapshot:    snapDTO(h.Snapshot, h.Finality),
	}
	for _, v := range h.Trending {
		body.Trending = append(body.Trending, trendingDTO{TokenDTO: toTokenDTO(v.Summary), WindowVolumeETH: decimal(v.WindowVolumeETH), DistinctBuyers: v.DistinctBuyers})
	}
	if h.King != nil {
		body.King = nullableObject[kingDTO]{V: &kingDTO{TokenDTO: toTokenDTO(h.King.Summary), GraduationProgressBPS: h.King.GraduationProgressBPS, RemainingETH: decimal(h.King.RemainingETH)}}
	}
	cache.body, cache.fetchedAt = &body, now
	return &highlightsOutput{Body: body}, nil
}
