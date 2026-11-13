package postgres

import (
	"bytes"
	"context"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgtype"
)

// Engine v2 ingests. Each insert is idempotent through insertEvent; projection side effects run
// only for a newly inserted row, as in the v1 ingests.

func (adapter *Adapter) InsertTokenLaunchV2(ctx context.Context, event ledger.TokenLaunchV2) (ledger.InsertResult, error) {
	arg, err := tokenLaunchV2Params(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	result, err := adapter.insertEvent(ctx, "token_launch", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertTokenLaunchV2(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.TokenLaunchV2MatchesEvent(ctx, sqlc.TokenLaunchV2MatchesEventParams(arg))
	})
	if err == nil && result.Inserted {
		if deploymentID, ok := ctx.Value(refreshDeploymentContextKey{}).(string); ok && deploymentID != "" {
			adapter.notifyAPIRefresh(ctx, map[string]any{"type": "launch", "chain_id": event.ChainID, "deployment_id": deploymentID, "token": event.Token.Hex(), "as_of_block": event.BlockNumber, "as_of_block_hash": event.BlockHash.Hex()})
		}
	}
	return result, err
}

// IngestTokenLaunchV2 writes the launch and its tokens row, which carries the v2 columns.
func (adapter *Adapter) IngestTokenLaunchV2(ctx context.Context, event ledger.TokenLaunchV2) (ledger.InsertResult, error) {
	// A launch adds the token and its curve and locker exclusions, and clears launch-mint marks;
	// marks written before it in this transaction resolved without the token, so they are forgotten too.
	adapter.forgetDirtyMarks()
	defer adapter.forgetDirtyMarks()
	result, err := adapter.InsertTokenLaunchV2(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	if err := adapter.queries.ApplyTokenLaunchProjection(ctx, projectionIdentity(event.EventCoordinates)); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply token launch projection: %w", err)
	}
	if err := adapter.markTokenDirty(ctx, event.ChainID, event.Token); err != nil {
		return ledger.InsertResult{}, err
	}
	// Engine 2 has no pair; only the curve's launch-mint position mark is dropped (R4).
	if err := adapter.queries.ClearLaunchPositionDirty(ctx, sqlc.ClearLaunchPositionDirtyParams{ChainID: event.ChainID, TokenAddress: event.Token[:], Curve: event.Curve[:]}); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("clear launch position dirty: %w", err)
	}
	return result, nil
}

func (adapter *Adapter) InsertTradeV2(ctx context.Context, event ledger.TradeV2) (ledger.InsertResult, error) {
	arg, err := tradeV2Params(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "trade", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertTradeV2(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.TradeV2MatchesEvent(ctx, sqlc.TradeV2MatchesEventParams(arg))
	})
}

// IngestTradeV2 has the v1 trade's side effects (the curve reserves, candles, and the token and
// trader position marks) and credits the trade's creator fee to the recipient in force.
func (adapter *Adapter) IngestTradeV2(ctx context.Context, event ledger.TradeV2) (ledger.InsertResult, error) {
	result, err := adapter.InsertTradeV2(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	if err := adapter.applyTradeProjections(ctx, event.Trade); err != nil {
		return ledger.InsertResult{}, err
	}
	return result, adapter.applyCreatorFeeDeltas(ctx, event.EventCoordinates)
}

func (adapter *Adapter) InsertGraduationV2(ctx context.Context, event ledger.GraduationV2) (ledger.InsertResult, error) {
	arg, err := graduationV2Params(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "graduation", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertGraduationV2(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.GraduationV2MatchesEvent(ctx, sqlc.GraduationV2MatchesEventParams(arg))
	})
}

// IngestGraduationV2 moves the token to the graduated phase, registers its V4 pool, and prices
// the token from the pool's opening sqrtPriceX96.
func (adapter *Adapter) IngestGraduationV2(ctx context.Context, event ledger.GraduationV2) (ledger.InsertResult, error) {
	// A graduation registers the V4 pool that pool marks resolve against.
	defer adapter.forgetDirtyMarks()
	result, err := adapter.InsertGraduationV2(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	identity := projectionIdentity(event.EventCoordinates)
	if err := adapter.queries.ApplyGraduationProjection(ctx, sqlc.ApplyGraduationProjectionParams(identity)); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply graduation projection: %w", err)
	}
	if err := adapter.queries.ApplyV4PoolProjection(ctx, sqlc.ApplyV4PoolProjectionParams(identity)); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply V4 pool projection: %w", err)
	}
	if err := adapter.queries.ApplyV4ReserveProjection(ctx, sqlc.ApplyV4ReserveProjectionParams(identity)); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply V4 reserve projection: %w", err)
	}
	return result, adapter.markTokenDirty(ctx, event.ChainID, event.Token)
}

func (adapter *Adapter) IngestSnipeTaxClaim(ctx context.Context, event ledger.SnipeTaxClaim) (ledger.InsertResult, error) {
	arg, err := snipeTaxClaimParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	result, err := adapter.insertEvent(ctx, "snipe_tax_claim", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertSnipeTaxClaim(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.SnipeTaxClaimMatchesEvent(ctx, sqlc.SnipeTaxClaimMatchesEventParams(arg))
	})
	return adapter.markTokenDirtyIfInserted(ctx, result, err, event.ChainID, event.Token)
}

func (adapter *Adapter) IngestFeeRecipientProposal(ctx context.Context, event ledger.FeeRecipientProposal) (ledger.InsertResult, error) {
	arg := feeRecipientProposalParams(event)
	result, err := adapter.insertEvent(ctx, "fee_recipient_proposal", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertFeeRecipientProposal(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.FeeRecipientProposalMatchesEvent(ctx, sqlc.FeeRecipientProposalMatchesEventParams(arg))
	})
	return adapter.markTokenDirtyIfInserted(ctx, result, err, event.ChainID, event.Token)
}

func (adapter *Adapter) IngestFeeRecipientAcceptance(ctx context.Context, event ledger.FeeRecipientAcceptance) (ledger.InsertResult, error) {
	arg := feeRecipientAcceptanceParams(event)
	result, err := adapter.insertEvent(ctx, "fee_recipient_acceptance", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertFeeRecipientAcceptance(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.FeeRecipientAcceptanceMatchesEvent(ctx, sqlc.FeeRecipientAcceptanceMatchesEventParams(arg))
	})
	return adapter.markTokenDirtyIfInserted(ctx, result, err, event.ChainID, event.Token)
}

func (adapter *Adapter) IngestLpFeeCollection(ctx context.Context, event ledger.LpFeeCollection) (ledger.InsertResult, error) {
	arg, err := lpFeeCollectionParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	result, err := adapter.insertEvent(ctx, "lp_fee_collection", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertLpFeeCollection(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.LpFeeCollectionMatchesEvent(ctx, sqlc.LpFeeCollectionMatchesEventParams(arg))
	})
	return adapter.applyCreatorFeeDeltasIfInserted(ctx, result, err, event.EventCoordinates, event.Token)
}

func (adapter *Adapter) IngestLpFeeClaim(ctx context.Context, event ledger.LpFeeClaim) (ledger.InsertResult, error) {
	arg, err := lpFeeClaimParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	result, err := adapter.insertEvent(ctx, "lp_fee_claim", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertLpFeeClaim(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.LpFeeClaimMatchesEvent(ctx, sqlc.LpFeeClaimMatchesEventParams(arg))
	})
	return adapter.applyCreatorFeeDeltasIfInserted(ctx, result, err, event.EventCoordinates, event.Token)
}

// Pool initializations and liquidity changes are ledger-only: the graduation already prices the
// pool, and the locker position is read from the graduation. V4 swap positions are not projected
// yet (backlog).

func (adapter *Adapter) IngestV4PoolInitialization(ctx context.Context, event ledger.V4PoolInitialization) (ledger.InsertResult, error) {
	arg, err := v4PoolInitializationParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "v4_pool_initialization", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertV4PoolInitialization(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.V4PoolInitializationMatchesEvent(ctx, sqlc.V4PoolInitializationMatchesEventParams(arg))
	})
}

func (adapter *Adapter) IngestV4LiquidityChange(ctx context.Context, event ledger.V4LiquidityChange) (ledger.InsertResult, error) {
	arg, err := v4LiquidityChangeParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "v4_liquidity_change", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertV4LiquidityChange(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.V4LiquidityChangeMatchesEvent(ctx, sqlc.V4LiquidityChangeMatchesEventParams(arg))
	})
}

// IngestV4Swap stores the swap, moves the token's pool price, adds the swap to the candles, and
// marks the token aggregate and the (tx_from, token) position dirty.
// The identity check ignores tx_from: the sender is enrichment that a replay may know when the
// first ingest did not.
func (adapter *Adapter) IngestV4Swap(ctx context.Context, event ledger.V4Swap) (ledger.InsertResult, error) {
	result, err := adapter.insertV4Swap(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	identity := projectionIdentity(event.EventCoordinates)
	if err := adapter.queries.ApplyV4ReserveProjection(ctx, sqlc.ApplyV4ReserveProjectionParams(identity)); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply V4 reserve projection: %w", err)
	}
	if err := adapter.queries.ApplyMarketTradeCandles(ctx, sqlc.ApplyMarketTradeCandlesParams(identity)); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply V4 swap candles: %w", err)
	}
	poolID := sqlc.Hash(event.PoolID)
	if err := adapter.markOnce(dirtyMark{kind: markV4PoolToken, chainID: event.ChainID, subject: event.PoolID}, func() error {
		if err := adapter.queries.MarkV4PoolTokenDirty(ctx, sqlc.MarkV4PoolTokenDirtyParams{ChainID: event.ChainID, PoolID: &poolID}); err != nil {
			return fmt.Errorf("mark V4 pool token dirty: %w", err)
		}
		return nil
	}); err != nil {
		return ledger.InsertResult{}, err
	}
	if event.TxFrom != nil {
		sender := *event.TxFrom
		if err := adapter.markOnce(dirtyMark{kind: markV4SwapPosition, chainID: event.ChainID, subject: event.PoolID, wallet: sender}, func() error {
			if err := adapter.queries.MarkV4SwapPositionDirty(ctx, sqlc.MarkV4SwapPositionDirtyParams{ChainID: event.ChainID, PoolID: poolID[:], TxFrom: sender[:], Factory: adapter.factory[:]}); err != nil {
				return fmt.Errorf("mark V4 swap position dirty: %w", err)
			}
			return nil
		}); err != nil {
			return ledger.InsertResult{}, err
		}
	}
	return result, nil
}

func (adapter *Adapter) insertV4Swap(ctx context.Context, event ledger.V4Swap) (ledger.InsertResult, error) {
	arg, err := v4SwapParams(event)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	return adapter.insertEvent(ctx, "v4_swap", eventParams(event.EventCoordinates), func() (int64, error) { return adapter.queries.InsertV4Swap(ctx, arg) }, func() (pgtype.Bool, error) {
		return adapter.queries.V4SwapMatchesEvent(ctx, sqlc.V4SwapMatchesEventParams{
			ChainID: arg.ChainID, BlockNumber: arg.BlockNumber, BlockHash: arg.BlockHash, BlockTime: arg.BlockTime, TransactionIndex: arg.TransactionIndex, TxHash: arg.TxHash, LogIndex: arg.LogIndex,
			PoolID: arg.PoolID, Sender: arg.Sender, Amount0Delta: arg.Amount0Delta, Amount1Delta: arg.Amount1Delta, SqrtPriceX96: arg.SqrtPriceX96, Liquidity: arg.Liquidity, Tick: arg.Tick, Fee: arg.Fee,
		})
	})
}

// applyCreatorFeeDeltasIfInserted moves the owed balances of a newly inserted fee event and
// marks its token dirty.
func (adapter *Adapter) applyCreatorFeeDeltasIfInserted(ctx context.Context, result ledger.InsertResult, err error, coordinates ledger.EventCoordinates, token common.Address) (ledger.InsertResult, error) {
	if err != nil || !result.Inserted {
		return result, err
	}
	if err := adapter.applyCreatorFeeDeltas(ctx, coordinates); err != nil {
		return ledger.InsertResult{}, err
	}
	return result, adapter.markTokenDirty(ctx, coordinates.ChainID, token)
}

// applyCreatorFeeDeltas applies one event's rows of the creator_fee_deltas view. It is a no-op
// for an engine-1 token, whose fee events the view does not list.
func (adapter *Adapter) applyCreatorFeeDeltas(ctx context.Context, coordinates ledger.EventCoordinates) error {
	identity := projectionIdentity(coordinates)
	if err := adapter.queries.EnsureCreatorFeeAccounts(ctx, sqlc.EnsureCreatorFeeAccountsParams(identity)); err != nil {
		return fmt.Errorf("open creator fee accounts: %w", err)
	}
	if err := adapter.queries.ApplyCreatorFeeDeltas(ctx, sqlc.ApplyCreatorFeeDeltasParams(identity)); err != nil {
		return fmt.Errorf("apply creator fee deltas: %w", err)
	}
	return nil
}

// RegisterV4PoolManager records the chain's Uniswap V4 PoolManager, which holder views exclude
// for engine-2 tokens. A chain has one deployment (pre-flight B2), so registering a different
// PoolManager for a chain that already has one is an error.
func (adapter *Adapter) RegisterV4PoolManager(ctx context.Context, chainID int64, poolManager common.Address) error {
	stored, err := adapter.queries.RegisterV4PoolManager(ctx, sqlc.RegisterV4PoolManagerParams{ChainID: chainID, PoolManager: sqlc.Address(poolManager)})
	if err != nil {
		return fmt.Errorf("register V4 pool manager: %w", err)
	}
	if !bytes.Equal(stored, poolManager[:]) {
		return fmt.Errorf("chain %d already has V4 pool manager %#x, not %s", chainID, stored, poolManager)
	}
	return nil
}

func (adapter *Adapter) markTokenDirtyIfInserted(ctx context.Context, result ledger.InsertResult, err error, chainID int64, token [20]byte) (ledger.InsertResult, error) {
	if err != nil || !result.Inserted {
		return result, err
	}
	return result, adapter.markTokenDirty(ctx, chainID, token)
}
