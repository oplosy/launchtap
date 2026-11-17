package postgres

import (
	"context"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// Canonical ledger inserts: each event row is written once and replays are checked for drift.

func (adapter *Adapter) InsertTrade(ctx context.Context, trade ledger.Trade) (ledger.InsertResult, error) {
	arg, err := tradeParams(trade)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	rows, err := adapter.queries.InsertTrade(ctx, arg)
	if err != nil {
		return ledger.InsertResult{}, fmt.Errorf("insert trade: %w", err)
	}
	if rows == 1 {
		return ledger.InsertResult{Inserted: true}, nil
	}
	stored, err := adapter.queries.TradeMatchesEvent(ctx, sqlc.TradeMatchesEventParams(arg))
	if err != nil {
		return ledger.InsertResult{}, fmt.Errorf("read conflicting trade: %w", err)
	}
	if stored.Valid && stored.Bool {
		return ledger.InsertResult{}, nil
	}
	return ledger.InsertResult{}, invariantConflict("trade", eventKey(arg.ChainID, arg.TxHash, arg.LogIndex), nil)
}

func (adapter *Adapter) InsertLaunchPauseEvent(
	ctx context.Context,
	event ledger.LaunchPauseEvent,
) (ledger.InsertResult, error) {
	arg := launchPauseParams(event)
	rows, err := adapter.queries.InsertLaunchPauseEvent(ctx, arg)
	if err != nil {
		return ledger.InsertResult{}, fmt.Errorf("insert launch pause event: %w", err)
	}
	if rows == 1 {
		return ledger.InsertResult{Inserted: true}, nil
	}
	stored, err := adapter.queries.LaunchPauseEventMatchesEvent(ctx, sqlc.LaunchPauseEventMatchesEventParams(arg))
	if err != nil {
		return ledger.InsertResult{}, fmt.Errorf("read conflicting launch pause event: %w", err)
	}
	if stored.Valid && stored.Bool {
		return ledger.InsertResult{}, nil
	}
	return ledger.InsertResult{}, invariantConflict("launch_pause_event", eventKey(arg.ChainID, arg.TxHash, arg.LogIndex), nil)
}

func (adapter *Adapter) InsertTokenLaunch(ctx context.Context, event ledger.TokenLaunch) (ledger.InsertResult, error) {
	arg, err := tokenLaunchParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	result, err := adapter.insertEvent(ctx, "token_launch", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertTokenLaunch(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.TokenLaunchMatchesEvent(ctx, sqlc.TokenLaunchMatchesEventParams(arg))
	})
	if err == nil && result.Inserted {
		if deploymentID, ok := ctx.Value(refreshDeploymentContextKey{}).(string); ok && deploymentID != "" {
			adapter.notifyAPIRefresh(ctx, map[string]any{"type": "launch", "chain_id": event.ChainID, "deployment_id": deploymentID, "token": event.Token.Hex(), "as_of_block": event.BlockNumber, "as_of_block_hash": event.BlockHash.Hex()})
		}
	}
	return result, err
}

func (adapter *Adapter) InsertGraduation(ctx context.Context, event ledger.Graduation) (ledger.InsertResult, error) {
	arg, err := graduationParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "graduation", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertGraduation(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.GraduationMatchesEvent(ctx, sqlc.GraduationMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertCreatorFeeClaim(ctx context.Context, event ledger.CreatorFeeClaim) (ledger.InsertResult, error) {
	arg, err := creatorFeeClaimParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "creator_fee_claim", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertCreatorFeeClaim(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.CreatorFeeClaimMatchesEvent(ctx, sqlc.CreatorFeeClaimMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertProtocolFeeClaim(ctx context.Context, event ledger.ProtocolFeeClaim) (ledger.InsertResult, error) {
	arg, err := protocolFeeClaimParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "protocol_fee_claim", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertProtocolFeeClaim(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.ProtocolFeeClaimMatchesEvent(ctx, sqlc.ProtocolFeeClaimMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertLaunchFeeClaim(ctx context.Context, event ledger.LaunchFeeClaim) (ledger.InsertResult, error) {
	arg, err := launchFeeClaimParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "launch_fee_claim", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertLaunchFeeClaim(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.LaunchFeeClaimMatchesEvent(ctx, sqlc.LaunchFeeClaimMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertRefundCredit(ctx context.Context, event ledger.RefundCredit) (ledger.InsertResult, error) {
	arg, err := refundCreditParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "refund_credit", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertRefundCredit(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.RefundCreditMatchesEvent(ctx, sqlc.RefundCreditMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertRefundClaim(ctx context.Context, event ledger.RefundClaim) (ledger.InsertResult, error) {
	arg, err := refundClaimParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "refund_claim", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertRefundClaim(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.RefundClaimMatchesEvent(ctx, sqlc.RefundClaimMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertTransfer(ctx context.Context, event ledger.Transfer) (ledger.InsertResult, error) {
	arg, err := transferParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "transfer", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertTransfer(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.TransferMatchesEvent(ctx, sqlc.TransferMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertPoolMint(ctx context.Context, event ledger.PoolMint) (ledger.InsertResult, error) {
	arg, err := poolMintParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "pool_mint", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertPoolMint(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.PoolMintMatchesEvent(ctx, sqlc.PoolMintMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertPoolBurn(ctx context.Context, event ledger.PoolBurn) (ledger.InsertResult, error) {
	arg, err := poolBurnParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "pool_burn", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertPoolBurn(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.PoolBurnMatchesEvent(ctx, sqlc.PoolBurnMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertPoolSwap(ctx context.Context, event ledger.PoolSwap) (ledger.InsertResult, error) {
	arg, err := poolSwapParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "pool_swap", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertPoolSwap(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.PoolSwapMatchesEvent(ctx, poolSwapMatchParams(arg))
	})
}

func (adapter *Adapter) InsertPoolSync(ctx context.Context, event ledger.PoolSync) (ledger.InsertResult, error) {
	arg, err := poolSyncParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "pool_sync", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertPoolSync(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.PoolSyncMatchesEvent(ctx, sqlc.PoolSyncMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertTradingPauseEvent(ctx context.Context, event ledger.TradingPauseEvent) (ledger.InsertResult, error) {
	arg := tradingPauseParams(event)
	return adapter.insertEvent(ctx, "trading_pause_event", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertTradingPauseEvent(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.TradingPauseEventMatchesEvent(ctx, sqlc.TradingPauseEventMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertEngineConfiguration(ctx context.Context, event ledger.EngineConfiguration) (ledger.InsertResult, error) {
	arg := engineConfigurationParams(event)
	return adapter.insertEvent(ctx, "engine_configuration", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertEngineConfiguration(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.EngineConfigurationMatchesEvent(ctx, sqlc.EngineConfigurationMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertFutureDefaultsConfiguration(ctx context.Context, event ledger.FutureDefaultsConfiguration) (ledger.InsertResult, error) {
	arg := futureDefaultsConfigurationParams(event)
	return adapter.insertEvent(ctx, "future_defaults_configuration", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertFutureDefaultsConfiguration(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.FutureDefaultsConfigurationMatchesEvent(ctx, sqlc.FutureDefaultsConfigurationMatchesEventParams(arg))
	})
}

func (adapter *Adapter) InsertFutureTreasuryConfiguration(ctx context.Context, event ledger.FutureTreasuryConfiguration) (ledger.InsertResult, error) {
	arg := futureTreasuryConfigurationParams(event)
	return adapter.insertEvent(ctx, "future_treasury_configuration", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertFutureTreasuryConfiguration(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.FutureTreasuryConfigurationMatchesEvent(ctx, sqlc.FutureTreasuryConfigurationMatchesEventParams(arg))
	})
}

func (adapter *Adapter) insertEvent(ctx context.Context, entity string, coordinates eventParameters, insert func() (int64, error), matches func() (pgtype.Bool, error)) (ledger.InsertResult, error) {
	rows, err := insert()
	if err != nil {
		return ledger.InsertResult{}, fmt.Errorf("insert %s: %w", entity, err)
	}
	if rows == 1 {
		return ledger.InsertResult{Inserted: true}, nil
	}
	matching, err := matches()
	if err != nil {
		return ledger.InsertResult{}, fmt.Errorf("read conflicting %s: %w", entity, err)
	}
	if matching.Valid && matching.Bool {
		return ledger.InsertResult{}, nil
	}
	return ledger.InsertResult{}, invariantConflict(entity, eventKey(coordinates.ChainID, coordinates.TxHash, coordinates.LogIndex), nil)
}
