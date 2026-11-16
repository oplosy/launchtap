package apiserver

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/Contictus/launchtap/backend/internal/observation"
	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/danielgtaylor/huma/v2"
	"github.com/ethereum/go-ethereum/common"
)

type ObservationRoutes struct {
	Reader       observation.Reader
	ChainID      int64
	DeploymentID string
}

type observationInput struct {
	TxHash string `path:"tx_hash"`
}
type canonicalEventDTO struct {
	Kind             string  `json:"kind"`
	TxHash           string  `json:"tx_hash"`
	Token            *string `json:"token,omitempty"`
	Pair             *string `json:"pair,omitempty"`
	BlockNumber      int64   `json:"block_number"`
	BlockHash        string  `json:"block_hash"`
	BlockTime        string  `json:"block_time"`
	TransactionIndex int32   `json:"transaction_index"`
	LogIndex         int32   `json:"log_index"`
	Finality         string  `json:"finality"`
}
type canonicalObservationBody struct {
	ChainID      int64               `json:"chain_id"`
	DeploymentID string              `json:"deployment_id"`
	TxHash       string              `json:"tx_hash"`
	Snapshot     snapshotDTO         `json:"snapshot"`
	Finality     string              `json:"finality"`
	Events       []canonicalEventDTO `json:"events"`
}
type canonicalObservationOutput struct{ Body canonicalObservationBody }

func (r ObservationRoutes) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "getCanonicalTransaction", Method: http.MethodGet, Path: "/transactions/{tx_hash}", Tags: []string{"transactions"}}, r.get)
}

func (r ObservationRoutes) get(ctx context.Context, in *observationInput) (*canonicalObservationOutput, error) {
	if r.Reader == nil {
		return nil, huma.Error503ServiceUnavailable("canonical observation unavailable")
	}
	if !validTxHash(in.TxHash) {
		return nil, apiProblem(http.StatusBadRequest, "invalid_transaction_hash", "Transaction hash must be a 32-byte hex value")
	}
	hash := common.HexToHash(in.TxHash)
	v, err := r.Reader.Get(ctx, r.ChainID, r.DeploymentID, hash)
	if err != nil {
		if errors.Is(err, pagination.ErrSnapshotUnavailable) {
			return nil, apiProblem(http.StatusServiceUnavailable, "snapshot_unavailable", "Canonical read snapshot is not available yet")
		}
		if errors.Is(err, observation.ErrNotFound) {
			return nil, apiProblem(http.StatusNotFound, "transaction_not_indexed", "Transaction has no canonical indexed event at the current snapshot")
		}
		return nil, internalProblem(ctx, err, "internal_error", "Request failed")
	}
	body := canonicalObservationBody{ChainID: v.ChainID, DeploymentID: v.DeploymentID, TxHash: hash.Hex(), Snapshot: snapDTO(v.Snapshot, v.Finality), Finality: v.Finality, Events: make([]canonicalEventDTO, 0, len(v.Events))}
	for _, event := range v.Events {
		item := canonicalEventDTO{Kind: event.Kind, TxHash: event.TxHash.Hex(), BlockNumber: event.BlockNumber, BlockHash: event.BlockHash.Hex(), BlockTime: event.BlockTime, TransactionIndex: event.TransactionIndex, LogIndex: event.LogIndex, Finality: event.Finality}
		if event.Token != nil {
			value := event.Token.Hex()
			item.Token = &value
		}
		if event.Pair != nil {
			value := event.Pair.Hex()
			item.Pair = &value
		}
		body.Events = append(body.Events, item)
	}
	return &canonicalObservationOutput{Body: body}, nil
}

func validTxHash(value string) bool {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}
