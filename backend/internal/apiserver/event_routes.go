package apiserver

import (
	"context"
	"net/http"
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
	Streams  *StreamLimiter
	shutdown <-chan struct{}
}

type eventInput struct {
	LastEventID string `header:"Last-Event-ID"`
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

func (r EventRoutes) stream(ctx context.Context, _ *eventInput, send sse.Sender) {
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
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
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
		case event, ok := <-subscription.C:
			if !ok {
				return
			}
			if event.ChainID != r.ChainID || (event.DeploymentID != "" && event.DeploymentID != r.DeploymentID) {
				continue
			}
			var data any
			switch event.Type {
			case "launch":
				data = launchEvent{ChainID: event.ChainID, DeploymentID: deployment(event.DeploymentID, r.DeploymentID), Token: event.Token, AsOfBlock: event.AsOfBlock, AsOfBlockHash: event.AsOfBlockHash}
			case "token":
				data = tokenEvent{ChainID: event.ChainID, DeploymentID: deployment(event.DeploymentID, r.DeploymentID), Token: event.Token, AsOfBlock: event.AsOfBlock, AsOfBlockHash: event.AsOfBlockHash}
			case "reorg":
				data = reorgEvent{ChainID: event.ChainID, DeploymentID: deployment(event.DeploymentID, r.DeploymentID), AsOfBlock: event.AsOfBlock, AsOfBlockHash: event.AsOfBlockHash, CommonAncestor: event.CommonAncestor}
			default:
				continue
			}
			if err := send(sse.Message{Data: data}); err != nil {
				return
			}
		}
	}
}

func deployment(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
