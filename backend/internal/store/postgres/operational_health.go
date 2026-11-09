package postgres

import (
	"context"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
)

// ReadOperationalHealth reads only committed operational counters. It is kept
// in the PostgreSQL boundary so sqlc types do not reach the indexer package.
func (adapter *Adapter) ReadOperationalHealth(
	ctx context.Context,
	chainID int64,
	deploymentID string,
) (indexer.OperationalHealth, error) {
	row, err := adapter.queries.ReadOperationalHealth(ctx, sqlc.ReadOperationalHealthParams{
		ChainID:      chainID,
		DeploymentID: deploymentID,
	})
	if err != nil {
		return indexer.OperationalHealth{}, fmt.Errorf("read operational health: %w", err)
	}
	rows, err := adapter.queries.ListTokenPhaseCounts(ctx, chainID)
	if err != nil {
		return indexer.OperationalHealth{}, fmt.Errorf("list token phase counts: %w", err)
	}
	result := indexer.OperationalHealth{
		DirtyWork:         row.DirtyWork,
		PositionDirtyWork: row.PositionDirtyWork,
		LastReorgID:       row.LastReorgID,
		LastReorgDepth:    row.LastReorgDepth,
		PhaseCounts:       make(map[string]int64, len(rows)),
	}
	if row.LastReorgAt.Valid {
		result.LastReorgAt = row.LastReorgAt.Time
	}
	for _, phase := range rows {
		result.PhaseCounts[phase.Phase] = phase.TokenCount
	}
	return result, nil
}
