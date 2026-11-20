package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

// EngineDeployment is one launch engine an indexer ingests on its chain.
type EngineDeployment struct {
	DeploymentID  string
	EngineVersion uint16
	Factory       common.Address
	StartBlock    int64
}

// EngineAddedLateError stops an indexer whose cursor already passed the start block of an engine
// it has never ingested: starting would skip that engine's launches below the cursor
// (docs/specs/2026-11-20-dual-engine-preflight.md, decision 5).
type EngineAddedLateError struct {
	EngineDeploymentID string
	StartBlock         int64
	Observed           int64
}

func (e *EngineAddedLateError) Error() string {
	return fmt.Sprintf("engine deployment %q starts at block %d but the indexer already observed block %d; re-index from that block to add it", e.EngineDeploymentID, e.StartBlock, e.Observed)
}

// RecordEngineDeployments checks the configured engines against the ones this indexer recorded
// before and records the new ones, in one transaction under the writer ownership. A recorded
// engine whose version, factory or start block changed is refused.
func (o *Ownership) RecordEngineDeployments(ctx context.Context, chainID int64, deploymentID string, engines []EngineDeployment) error {
	return o.WithinTx(ctx, func(ctx context.Context, adapter *Adapter) error {
		recorded, err := adapter.queries.ListIndexerEngineDeployments(ctx, sqlc.ListIndexerEngineDeploymentsParams{ChainID: chainID, DeploymentID: deploymentID})
		if err != nil {
			return fmt.Errorf("list recorded engine deployments: %w", err)
		}
		var observed *int64
		state, err := adapter.queries.GetSyncState(ctx, sqlc.GetSyncStateParams{ChainID: chainID, DeploymentID: deploymentID})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return fmt.Errorf("read sync state: %w", err)
		case state.ObservedNumber.Valid:
			observed = &state.ObservedNumber.Int64
		}
		fresh, err := newEngineDeployments(recorded, deploymentID, engines, observed)
		if err != nil {
			return err
		}
		for _, engine := range fresh {
			if err := adapter.queries.InsertIndexerEngineDeployment(ctx, sqlc.InsertIndexerEngineDeploymentParams{
				ChainID: chainID, DeploymentID: deploymentID, EngineDeploymentID: engine.DeploymentID,
				EngineVersion: int32(engine.EngineVersion), Factory: engine.Factory.Bytes(), StartBlock: engine.StartBlock,
			}); err != nil {
				return fmt.Errorf("record engine deployment %q: %w", engine.DeploymentID, err)
			}
		}
		return nil
	})
}

// newEngineDeployments returns the configured engines not recorded yet. observed is the
// indexer's cursor, nil before its first committed step. The site deployment is the one the
// cursor has always ingested, so it is never added late: an indexer that ran before this table
// existed records it on its first start.
func newEngineDeployments(recorded []sqlc.ListIndexerEngineDeploymentsRow, siteDeploymentID string, engines []EngineDeployment, observed *int64) ([]EngineDeployment, error) {
	known := make(map[string]sqlc.ListIndexerEngineDeploymentsRow, len(recorded))
	for _, row := range recorded {
		known[row.EngineDeploymentID] = row
	}
	fresh := make([]EngineDeployment, 0, len(engines))
	for _, engine := range engines {
		row, ok := known[engine.DeploymentID]
		if ok {
			if row.EngineVersion != int32(engine.EngineVersion) || common.BytesToAddress(row.Factory) != engine.Factory || len(row.Factory) != common.AddressLength || row.StartBlock != engine.StartBlock {
				return nil, fmt.Errorf("engine deployment %q changed since it was first ingested (engine %d, factory %x, start block %d)", engine.DeploymentID, row.EngineVersion, row.Factory, row.StartBlock)
			}
			continue
		}
		if engine.DeploymentID != siteDeploymentID && observed != nil && *observed >= engine.StartBlock {
			return nil, &EngineAddedLateError{EngineDeploymentID: engine.DeploymentID, StartBlock: engine.StartBlock, Observed: *observed}
		}
		fresh = append(fresh, engine)
	}
	return fresh, nil
}
