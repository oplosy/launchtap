package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/status"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// StatusReader reads the indexer's last committed state and heartbeat for GET /v1/status.
type StatusReader struct {
	Pool DBTX
}

func (r StatusReader) Indexer(ctx context.Context, chainID int64, deploymentID string) (status.Indexer, error) {
	row, err := sqlc.New(r.Pool).GetIndexerStatus(ctx, sqlc.GetIndexerStatusParams{ChainID: chainID, DeploymentID: deploymentID})
	if err != nil {
		return status.Indexer{}, fmt.Errorf("get indexer status: %w", err)
	}
	if !row.CheckedAt.Valid {
		return status.Indexer{}, errors.New("get indexer status: database returned no clock")
	}
	out := status.Indexer{
		ObservedBlock: optionalInt64(row.ObservedNumber), SafeBlock: optionalInt64(row.SafeNumber),
		FinalizedBlock: optionalInt64(row.FinalizedNumber), CheckedAt: row.CheckedAt.Time,
	}
	if row.BeatAt.Valid {
		at := row.BeatAt.Time
		out.HeartbeatAt = &at
	}
	return out, nil
}

func optionalInt64(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	out := value.Int64
	return &out
}
