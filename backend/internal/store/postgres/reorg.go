package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgtype"
)

// Reorg handling: ancestor search, rollback of canonical rows above it, and projection rebuild.

func (adapter *Adapter) RebuildTokenProjections(ctx context.Context, chainID int64, token common.Address) error {
	defer adapter.forgetDirtyMarks()
	exists, err := adapter.queries.HasCanonicalLaunch(ctx, sqlc.HasCanonicalLaunchParams{ChainID: chainID, TokenAddress: sqlc.Address(token)})
	if err != nil {
		return fmt.Errorf("check canonical launch: %w", err)
	}
	if !exists {
		return adapter.queries.ClearOrphanProjections(ctx, sqlc.ClearOrphanProjectionsParams{ChainID: chainID, TokenAddress: sqlc.Address(token)})
	}
	if err := adapter.queries.RebuildTokenProjections(ctx, sqlc.RebuildTokenProjectionsParams{
		RebuildChainID: chainID, RebuildTokenAddress: token[:],
	}); err != nil {
		return fmt.Errorf("rebuild token projections: %w", err)
	}
	return nil
}

// FindCommonAncestor returns the highest locally indexed block among candidate
// hashes supplied by the reorg detector. It does not perform RPC work.
func (adapter *Adapter) FindCommonAncestor(ctx context.Context, chainID int64, candidates []common.Hash) (ledger.IndexedBlock, error) {
	hashes := make([][]byte, len(candidates))
	for index := range candidates {
		hashes[index] = candidates[index][:]
	}
	row, err := adapter.queries.FindCommonAncestor(ctx, sqlc.FindCommonAncestorParams{ChainID: chainID, Column2: hashes})
	if err != nil {
		return ledger.IndexedBlock{}, fmt.Errorf("find common ancestor: %w", err)
	}
	return ledger.IndexedBlock{ChainID: chainID, BlockNumber: row.BlockNumber, BlockHash: common.Hash(row.BlockHash)}, nil
}

// AffectedTokensAbove identifies projection owners before canonical events are deleted.
func (adapter *Adapter) AffectedTokensAbove(ctx context.Context, chainID, ancestor int64) ([]common.Address, error) {
	rows, err := adapter.queries.AffectedTokensAbove(ctx, sqlc.AffectedTokensAboveParams{ChainID: chainID, BlockNumber: ancestor})
	if err != nil {
		return nil, fmt.Errorf("find affected tokens: %w", err)
	}
	result := make([]common.Address, len(rows))
	for index := range rows {
		result[index] = common.Address(rows[index])
	}
	return result, nil
}

// AffectedPositionPairsAbove lists (wallet, token) pairs touched by canonical events above the
// ancestor, with excluded wallets (R4) already dropped. It must run before DeleteCanonicalAbove,
// because a token launched above the ancestor loses its tokens row in the same recovery.
func (adapter *Adapter) AffectedPositionPairsAbove(ctx context.Context, chainID, ancestor int64) ([]indexer.PositionPair, error) {
	rows, err := adapter.queries.ListPositionPairsAbove(ctx, sqlc.ListPositionPairsAboveParams{ChainID: chainID, BlockNumber: ancestor, Factory: adapter.factory[:]})
	if err != nil {
		return nil, fmt.Errorf("find affected position pairs: %w", err)
	}
	result := make([]indexer.PositionPair, len(rows))
	for index := range rows {
		result[index] = indexer.PositionPair{Wallet: common.Address(rows[index].Wallet), Token: common.Address(rows[index].TokenAddress)}
	}
	return result, nil
}

// MarkPositionsDirty enqueues pairs collected by AffectedPositionPairsAbove for recompute.
func (adapter *Adapter) MarkPositionsDirty(ctx context.Context, chainID int64, pairs []indexer.PositionPair) error {
	if len(pairs) == 0 {
		return nil
	}
	wallets := make([][]byte, len(pairs))
	tokens := make([][]byte, len(pairs))
	for index := range pairs {
		wallets[index] = pairs[index].Wallet[:]
		tokens[index] = pairs[index].Token[:]
	}
	if err := adapter.queries.MarkPositionPairsDirty(ctx, sqlc.MarkPositionPairsDirtyParams{ChainID: chainID, Wallets: wallets, TokenAddresses: tokens}); err != nil {
		return fmt.Errorf("mark position pairs dirty: %w", err)
	}
	return nil
}

// DeleteCanonicalAbove deletes every canonical event before its block-ledger rows.
// The caller must run it inside the transaction that will rebuild affected projections.
func (adapter *Adapter) DeleteCanonicalAbove(ctx context.Context, chainID, ancestor int64) error {
	arg := sqlc.DeleteTradesAboveParams{ChainID: chainID, BlockNumber: ancestor}
	deletes := []func(context.Context) error{
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteCreatorFeeClaimsAbove(ctx, sqlc.DeleteCreatorFeeClaimsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteProtocolFeeClaimsAbove(ctx, sqlc.DeleteProtocolFeeClaimsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteLaunchFeeClaimsAbove(ctx, sqlc.DeleteLaunchFeeClaimsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteRefundCreditsAbove(ctx, sqlc.DeleteRefundCreditsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteRefundClaimsAbove(ctx, sqlc.DeleteRefundClaimsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteTransfersAbove(ctx, sqlc.DeleteTransfersAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeletePoolMintsAbove(ctx, sqlc.DeletePoolMintsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeletePoolBurnsAbove(ctx, sqlc.DeletePoolBurnsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeletePoolSwapsAbove(ctx, sqlc.DeletePoolSwapsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeletePoolSyncsAbove(ctx, sqlc.DeletePoolSyncsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteLaunchPauseEventsAbove(ctx, sqlc.DeleteLaunchPauseEventsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteTradingPauseEventsAbove(ctx, sqlc.DeleteTradingPauseEventsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteEngineConfigurationsAbove(ctx, sqlc.DeleteEngineConfigurationsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteFutureDefaultsConfigurationsAbove(ctx, sqlc.DeleteFutureDefaultsConfigurationsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteFutureTreasuryConfigurationsAbove(ctx, sqlc.DeleteFutureTreasuryConfigurationsAboveParams(arg))
			return err
		},
		// Engine-2 ledger. V4 pool rows reference their graduation, so they go before it.
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteSnipeTaxClaimsAbove(ctx, sqlc.DeleteSnipeTaxClaimsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteFeeRecipientProposalsAbove(ctx, sqlc.DeleteFeeRecipientProposalsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteFeeRecipientAcceptancesAbove(ctx, sqlc.DeleteFeeRecipientAcceptancesAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteLpFeeCollectionsAbove(ctx, sqlc.DeleteLpFeeCollectionsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteLpFeeClaimsAbove(ctx, sqlc.DeleteLpFeeClaimsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteV4SwapsAbove(ctx, sqlc.DeleteV4SwapsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteV4LiquidityChangesAbove(ctx, sqlc.DeleteV4LiquidityChangesAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteV4PoolInitializationsAbove(ctx, sqlc.DeleteV4PoolInitializationsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteGraduationsAbove(ctx, sqlc.DeleteGraduationsAboveParams(arg))
			return err
		},
		func(ctx context.Context) error { _, err := adapter.queries.DeleteTradesAbove(ctx, arg); return err },
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteTokenLaunchesAbove(ctx, sqlc.DeleteTokenLaunchesAboveParams(arg))
			return err
		},
		func(ctx context.Context) error {
			_, err := adapter.queries.DeleteIndexedBlocksAbove(ctx, sqlc.DeleteIndexedBlocksAboveParams(arg))
			return err
		},
	}
	for _, deleteRows := range deletes {
		if err := deleteRows(ctx); err != nil {
			return fmt.Errorf("delete canonical rows above %d: %w", ancestor, err)
		}
	}
	return nil
}

func (adapter *Adapter) RecordReorg(ctx context.Context, record indexer.ReorgRecord) (int64, error) {
	row, err := adapter.queries.RecordIndexerReorg(ctx, sqlc.RecordIndexerReorgParams{
		ChainID: record.ChainID, DeploymentID: record.DeploymentID,
		DetectedTipNumber: record.DetectedTipNumber, DetectedTipHash: sqlc.Hash(record.DetectedTipHash),
		CommonAncestorNumber: record.CommonAncestorNumber, CommonAncestorHash: sqlc.Hash(record.CommonAncestorHash),
		Depth: record.Depth, DetectedAt: pgtype.Timestamptz{Time: record.DetectedAt, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("record reorg: %w", err)
	}
	return row, nil
}

func (adapter *Adapter) notifyAPIRefresh(ctx context.Context, event map[string]any) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	if err := adapter.queries.NotifyAPIRefresh(ctx, string(payload)); err != nil {
		slog.Warn("API refresh notification failed", "error", err)
	}
}

func (adapter *Adapter) CompleteReorg(ctx context.Context, id int64) error {
	row, err := adapter.queries.CompleteIndexerReorg(ctx, id)
	if err != nil {
		return fmt.Errorf("complete reorg %d: %w", id, err)
	}
	adapter.notifyAPIRefresh(ctx, map[string]any{"type": "reorg", "chain_id": row.ChainID, "deployment_id": row.DeploymentID, "as_of_block": row.CommonAncestorNumber, "as_of_block_hash": common.Hash(row.CommonAncestorHash).Hex(), "common_ancestor": row.CommonAncestorNumber})
	return nil
}
