package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// Client telemetry (docs/specs/2026-11-18-client-telemetry-preflight.md): the web app posts
// closed-set error and Web Vitals events, which become Prometheus series and nothing else.
const (
	clientEventsPath     = "/client-events"
	clientEventsPattern  = "POST /v1" + clientEventsPath
	maxClientEventBytes  = 8 << 10
	maxClientEvents      = 20
	clientEventsPerMin   = 30
	maxClientVitalMillis = 60_000
	maxClientCLS         = 10
)

// ClientTelemetry receives validated client events. Every string argument is from the closed
// sets below; vital values are milliseconds, except CLS, which is unitless.
type ClientTelemetry interface {
	ClientError(source, route, class string)
	ClientVital(name, route string, value float64)
}

var (
	clientErrorSources = closedSet("route_boundary", "global_boundary", "window_error", "unhandled_rejection")
	clientErrorClasses = closedSet("Error", "TypeError", "ReferenceError", "RangeError", "SyntaxError", "ChunkLoadError", "other")
	clientVitalNames   = closedSet("LCP", "INP", "CLS", "FCP", "TTFB")
	// The web app's top-level route groups (web/src/telemetry/route-group.ts).
	clientRouteGroups = closedSet("home", "explore", "graduated", "token", "create", "wallet", "profile", "forum",
		"analytics", "docs", "leaderboard", "notifications", "transactions", "other")
)

func closedSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[v] = struct{}{}
	}
	return out
}

type ClientEventRoutes struct {
	Telemetry ClientTelemetry
	// AllowedOrigins is the API's CORS allow-list; a batch must come from one of them.
	AllowedOrigins []string
	Limiter        *SubjectLimiter
}

type clientEventsInput struct {
	Origin  string `header:"Origin"`
	RawBody []byte `contentType:"text/plain"`
}

type clientEventsOutput struct {
	Status int `status:"204"`
}

type clientEvent struct {
	Type   string   `json:"type"`
	Source string   `json:"source,omitempty"`
	Route  string   `json:"route"`
	Class  string   `json:"class,omitempty"`
	Name   string   `json:"name,omitempty"`
	Value  *float64 `json:"value,omitempty"`
}

type clientEventBatch struct {
	Events []clientEvent `json:"events"`
}

func (r ClientEventRoutes) Register(api huma.API) {
	if r.Limiter == nil {
		r.Limiter = NewSubjectLimiter(clientEventsPerMin, time.Minute)
	}
	huma.Register(api, huma.Operation{
		OperationID: "postClientEvents", Method: http.MethodPost, Path: clientEventsPath, Tags: []string{"telemetry"},
		Summary:      "Report browser errors and Web Vitals",
		Description:  "Closed-set values only; the API keeps counters and histograms and stores nothing else. The body is JSON sent as text/plain so browsers need no CORS preflight.",
		MaxBodyBytes: maxClientEventBytes,
		RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{
			"text/plain": {Schema: &huma.Schema{Type: "string", Description: `JSON: {"events":[{"type":"error","source":"window_error","route":"token","class":"TypeError"},{"type":"vital","name":"LCP","route":"token","value":1234}]}`}},
		}},
	}, r.post)
}

func (r ClientEventRoutes) post(ctx context.Context, in *clientEventsInput) (*clientEventsOutput, error) {
	if !originAllowed(r.AllowedOrigins, in.Origin) {
		return nil, apiRequestProblem(ctx, http.StatusForbidden, "origin_not_allowed", "origin not allowed")
	}
	if !r.Limiter.Allow(ClientKey(ctx), time.Now()) {
		return nil, apiRequestProblem(ctx, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
	}
	events, err := parseClientEvents(in.RawBody)
	if err != nil {
		return nil, apiRequestProblem(ctx, http.StatusBadRequest, "invalid_client_events", err.Error())
	}
	if r.Telemetry != nil {
		for _, e := range events {
			if e.Type == "error" {
				r.Telemetry.ClientError(e.Source, e.Route, e.Class)
			} else {
				r.Telemetry.ClientVital(e.Name, e.Route, *e.Value)
			}
		}
	}
	return &clientEventsOutput{Status: http.StatusNoContent}, nil
}

// parseClientEvents decodes and validates a whole batch; any bad event rejects all of them.
func parseClientEvents(body []byte) ([]clientEvent, error) {
	if len(body) > maxClientEventBytes {
		return nil, errors.New("body is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var batch clientEventBatch
	if err := decoder.Decode(&batch); err != nil {
		return nil, errors.New("body must be a JSON object with an events array")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("body must hold exactly one JSON object")
	}
	if len(batch.Events) == 0 || len(batch.Events) > maxClientEvents {
		return nil, errors.New("events must hold 1 to 20 entries")
	}
	for _, e := range batch.Events {
		if err := validateClientEvent(e); err != nil {
			return nil, err
		}
	}
	return batch.Events, nil
}

func validateClientEvent(e clientEvent) error {
	if !inSet(clientRouteGroups, e.Route) {
		return errors.New("unknown route group")
	}
	switch e.Type {
	case "error":
		if !inSet(clientErrorSources, e.Source) || !inSet(clientErrorClasses, e.Class) || e.Name != "" || e.Value != nil {
			return errors.New("error events need a known source and class and no name or value")
		}
	case "vital":
		if !inSet(clientVitalNames, e.Name) || e.Source != "" || e.Class != "" || e.Value == nil {
			return errors.New("vital events need a known name and a value and no source or class")
		}
		limit := float64(maxClientVitalMillis)
		if e.Name == "CLS" {
			limit = maxClientCLS
		}
		if v := *e.Value; math.IsNaN(v) || v < 0 || v > limit {
			return errors.New("vital value is out of range")
		}
	default:
		return errors.New("unknown event type")
	}
	return nil
}

func inSet(values map[string]struct{}, v string) bool {
	_, ok := values[v]
	return ok
}
