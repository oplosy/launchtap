package apiserver

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Contictus/launchtap/backend/internal/realtime"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"
)

type EventRoutes struct {
	Hub          *realtime.Hub
	ChainID      int64
	DeploymentID string
	Heartbeat    time.Duration
	// Streams caps concurrent streams per client; nil disables the per-client cap.
	Streams *StreamLimiter
	// Coalesce batches launch/token hints per stream; repeated hints for the same token within
	// one window are delivered once. Zero uses the default window.
	Coalesce time.Duration
	shutdown <-chan struct{}
}

const defaultEventCoalesce = 500 * time.Millisecond

type eventInput struct {
	LastEventID string `header:"Last-Event-ID"`
	Token       string `query:"token" pattern:"^0x[0-9a-fA-F]{40}$" doc:"Forward only token hints for this address, plus reorgs."`
}

type launchEvent struct {
	ChainID       int64  `json:"chain_id"`
	DeploymentID  string `json:"deployment_id"`
	Token         string `json:"token"`
	AsOfBlock     int64  `json:"as_of_block,omitempty"`
	AsOfBlockHash string `json:"as_of_block_hash,omitempty"`
}

type tokenEvent struct {
	ChainID       int64  `json:"chain_id"`
	DeploymentID  string `json:"deployment_id"`
	Token         string `json:"token,omitempty"`
	AsOfBlock     int64  `json:"as_of_block,omitempty"`
	AsOfBlockHash string `json:"as_of_block_hash,omitempty"`
}

type reorgEvent struct {
	ChainID        int64  `json:"chain_id"`
	DeploymentID   string `json:"deployment_id"`
	AsOfBlock      int64  `json:"as_of_block,omitempty"`
	AsOfBlockHash  string `json:"as_of_block_hash,omitempty"`
	CommonAncestor int64  `json:"common_ancestor"`
}

func (r EventRoutes) Register(api huma.API) {
	sse.Register(api, huma.Operation{OperationID: "events", Method: http.MethodGet, Path: "/events", Tags: []string{"events"}}, map[string]any{
		"launch": launchEvent{}, "token": tokenEvent{}, "reorg": reorgEvent{},
	}, r.stream)
}

func (r EventRoutes) stream(ctx context.Context, input *eventInput, send sse.Sender) {
	if r.Hub == nil {
		return
	}
	release, ok := r.Streams.Acquire(ClientKey(ctx))
	if !ok {
		_ = send(sse.Message{Comment: "per-client stream limit reached", Retry: 10000})
		return
	}
	defer release()
	subscription, err := r.Hub.Subscribe()
	if err != nil {
		_ = send(sse.Message{Comment: "subscriber capacity reached", Retry: 3000})
		return
	}
	defer subscription.Close()
	if err := send(sse.Message{Comment: "refresh-only stream; fetch REST after reconnect", Retry: 3000}); err != nil {
		return
	}
	heartbeat := r.Heartbeat
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	window := r.Coalesce
	if window <= 0 {
		window = defaultEventCoalesce
	}
	tokenFilter := ""
	if input != nil {
		tokenFilter = strings.ToLower(input.Token)
	}
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	pending := newEventBatch()
	var flushTimer *time.Timer
	var flush <-chan time.Time
	defer func() {
		if flushTimer != nil {
			flushTimer.Stop()
		}
	}()
	sendPending := func() bool {
		for _, event := range pending.drain() {
			if err := send(sse.Message{Data: r.payload(event)}); err != nil {
				return false
			}
		}
		flushTimer, flush = nil, nil
		return true
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.shutdown:
			return
		case <-ticker.C:
			if err := send(sse.Message{Comment: "heartbeat"}); err != nil {
				return
			}
		case <-flush:
			if !sendPending() {
				return
			}
		case event, ok := <-subscription.C:
			if !ok {
				return
			}
			if event.ChainID != r.ChainID || (event.DeploymentID != "" && event.DeploymentID != r.DeploymentID) {
				continue
			}
			switch event.Type {
			case "reorg":
				// A reorg invalidates every view; deliver queued hints and the reorg immediately.
				if flushTimer != nil {
					flushTimer.Stop()
				}
				if !sendPending() {
					return
				}
				if err := send(sse.Message{Data: r.payload(event)}); err != nil {
					return
				}
			case "launch", "token":
				if tokenFilter != "" && (event.Type == "launch" || strings.ToLower(event.Token) != tokenFilter) {
					continue
				}
				pending.add(event)
				if flushTimer == nil {
					flushTimer = time.NewTimer(window)
					flush = flushTimer.C
				}
			}
		}
	}
}

func (r EventRoutes) payload(event realtime.Event) any {
	scope := deployment(event.DeploymentID, r.DeploymentID)
	switch event.Type {
	case "launch":
		return launchEvent{ChainID: event.ChainID, DeploymentID: scope, Token: event.Token, AsOfBlock: event.AsOfBlock, AsOfBlockHash: event.AsOfBlockHash}
	case "token":
		return tokenEvent{ChainID: event.ChainID, DeploymentID: scope, Token: event.Token, AsOfBlock: event.AsOfBlock, AsOfBlockHash: event.AsOfBlockHash}
	default:
		return reorgEvent{ChainID: event.ChainID, DeploymentID: scope, AsOfBlock: event.AsOfBlock, AsOfBlockHash: event.AsOfBlockHash, CommonAncestor: event.CommonAncestor}
	}
}

// eventBatch keeps the latest hint per (type, token) in first-seen order.
type eventBatch struct {
	order  []string
	latest map[string]realtime.Event
}

func newEventBatch() *eventBatch { return &eventBatch{latest: make(map[string]realtime.Event)} }

func (b *eventBatch) add(event realtime.Event) {
	key := event.Type + "|" + strings.ToLower(event.Token)
	if _, exists := b.latest[key]; !exists {
		b.order = append(b.order, key)
	}
	b.latest[key] = event
}

func (b *eventBatch) drain() []realtime.Event {
	events := make([]realtime.Event, 0, len(b.order))
	for _, key := range b.order {
		events = append(events, b.latest[key])
	}
	b.order = b.order[:0]
	clear(b.latest)
	return events
}

func deployment(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
