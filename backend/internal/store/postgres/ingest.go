package postgres

import (
	"context"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// Event ingest: the ledger insert plus the projections and dirty marks each event drives.

// IngestTokenLaunch writes a canonical launch and its projection only once.
func (adapter *Adapter) IngestTokenLaunch(ctx context.Context, event ledger.TokenLaunch) (ledger.InsertResult, error) {
	// A launch adds the token, its curve and pair exclusions, and clears launch-mint marks; marks
	// written before it in this transaction resolved without the token, so they are forgotten too.
	adapter.forgetDirtyMarks()
	defer adapter.forgetDirtyMarks()
	result, err := adapter.InsertTokenLaunch(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	identity := projectionIdentity(event.EventCoordinates)
	if err := adapter.queries.ApplyTokenLaunchProjection(ctx, identity); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply token launch projection: %w", err)
	}
	if err := adapter.markTokenDirty(ctx, event.ChainID, event.Token); err != nil {
		return ledger.InsertResult{}, err
	}
	// The launch mint is ingested before this row exists, so it was marked as a position (R4).
	if err := adapter.queries.ClearLaunchPositionDirty(ctx, sqlc.ClearLaunchPositionDirtyParams{ChainID: event.ChainID, TokenAddress: event.Token[:], Curve: event.Curve[:], LpPair: event.LPPair[:]}); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("clear launch position dirty: %w", err)
	}
	return result, nil
}

// IngestTrade writes reserves and candles only for a newly inserted trade.
func (adapter *Adapter) IngestTrade(ctx context.Context, event ledger.Trade) (ledger.InsertResult, error) {
	result, err := adapter.InsertTrade(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	return result, adapter.applyTradeProjections(ctx, event)
}

// applyTradeProjections updates the curve reserves and candles from a stored curve trade and
// marks the token and the trader's position dirty. Engine 1 and 2 trades share it.
func (adapter *Adapter) applyTradeProjections(ctx context.Context, event ledger.Trade) error {
	identity := projectionIdentity(event.EventCoordinates)
	if err := adapter.queries.ApplyTradeReserveProjection(ctx, sqlc.ApplyTradeReserveProjectionParams(identity)); err != nil {
		return fmt.Errorf("apply trade reserve projection: %w", err)
	}
	if err := adapter.queries.ApplyMarketTradeCandles(ctx, sqlc.ApplyMarketTradeCandlesParams(identity)); err != nil {
		return fmt.Errorf("apply trade candles: %w", err)
	}
	if err := adapter.markTokenDirty(ctx, event.ChainID, event.Token); err != nil {
		return err
	}
	return adapter.markPositionDirty(ctx, event.ChainID, event.Trader, event.Token)
}

func (adapter *Adapter) IngestGraduation(ctx context.Context, event ledger.Graduation) (ledger.InsertResult, error) {
	// A graduation moves the token to its pair, which pair marks resolve against.
	defer adapter.forgetDirtyMarks()
	result, err := adapter.InsertGraduation(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	identity := projectionIdentity(event.EventCoordinates)
	if err := adapter.queries.ApplyGraduationProjection(ctx, sqlc.ApplyGraduationProjectionParams(identity)); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply graduation projection: %w", err)
	}
	if err := adapter.queries.ApplyGraduationPoolSyncReserveProjection(ctx, sqlc.ApplyGraduationPoolSyncReserveProjectionParams(identity)); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply graduation pool sync reserve projection: %w", err)
	}
	if err := adapter.markTokenDirty(ctx, event.ChainID, event.Token); err != nil {
		return ledger.InsertResult{}, err
	}
	return result, nil
}

func (adapter *Adapter) IngestTransfer(ctx context.Context, event ledger.Transfer) (ledger.InsertResult, error) {
	result, err := adapter.InsertTransfer(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	value, err := uint256("transfer value", event.Value)
	if err != nil {
		return ledger.InsertResult{}, err
	}
	applied, err := adapter.queries.ApplyTransferProjection(ctx, sqlc.ApplyTransferProjectionParams{ChainID: event.ChainID, TokenAddress: sqlc.Address(event.Token), Column3: event.From[:], Column4: pgtype.Numeric{Int: value.BigInt(), Valid: true}, Column5: event.To[:], FirstAcquiredBlockNumber: pgtype.Int8{Int64: event.BlockNumber, Valid: true}})
	if err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply transfer projection: %w", err)
	}
	if !applied.Valid || !applied.Bool {
		return ledger.InsertResult{}, &NegativeBalanceError{Token: event.Token.Hex(), Holder: event.From.Hex(), Key: eventKey(event.ChainID, sqlc.Hash(event.TxHash), event.LogIndex)}
	}
	if err := adapter.markTokenDirty(ctx, event.ChainID, event.Token); err != nil {
		return ledger.InsertResult{}, err
	}
	if err := adapter.markTransferPositionsDirty(ctx, event); err != nil {
		return ledger.InsertResult{}, err
	}
	return result, nil
}

func (adapter *Adapter) IngestPoolSync(ctx context.Context, event ledger.PoolSync) (ledger.InsertResult, error) {
	result, err := adapter.InsertPoolSync(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	identity := projectionIdentity(event.EventCoordinates)
	if err := adapter.queries.ApplyPoolSyncReserveProjection(ctx, sqlc.ApplyPoolSyncReserveProjectionParams(identity)); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply pool sync reserve projection: %w", err)
	}
	if err := adapter.markPairTokenDirty(ctx, event.ChainID, event.Pair); err != nil {
		return ledger.InsertResult{}, err
	}
	return result, nil
}

func (adapter *Adapter) IngestPoolSwap(ctx context.Context, event ledger.PoolSwap) (ledger.InsertResult, error) {
	result, err := adapter.InsertPoolSwap(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	identity := projectionIdentity(event.EventCoordinates)
	if err := adapter.queries.ApplyMarketTradeCandles(ctx, sqlc.ApplyMarketTradeCandlesParams(identity)); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply pool swap candles: %w", err)
	}
	if err := adapter.markPairTokenDirty(ctx, event.ChainID, event.Pair); err != nil {
		return ledger.InsertResult{}, err
	}
	// The query is a no-op for an unknown sender (NULL tx_from), so skip the round trip.
	if event.TxFrom == nil {
		return result, nil
	}
	sender := *event.TxFrom
	if err := adapter.markOnce(dirtyMark{kind: markPairSwapPosition, chainID: event.ChainID, subject: addressSubject(event.Pair), wallet: sender}, func() error {
		if err := adapter.queries.MarkPoolSwapPositionDirty(ctx, sqlc.MarkPoolSwapPositionDirtyParams{ChainID: event.ChainID, Pair: event.Pair[:], TxFrom: sender[:], Factory: adapter.factory[:]}); err != nil {
			return fmt.Errorf("mark pool swap position dirty: %w", err)
		}
		return nil
	}); err != nil {
		return ledger.InsertResult{}, err
	}
	return result, nil
}

// IngestCreatorFeeClaim debits an engine-2 creator fee balance; for an engine-1 token the delta
// is a no-op.
func (adapter *Adapter) IngestCreatorFeeClaim(ctx context.Context, event ledger.CreatorFeeClaim) (ledger.InsertResult, error) {
	result, err := adapter.InsertCreatorFeeClaim(ctx, event)
	return adapter.applyCreatorFeeDeltasIfInserted(ctx, result, err, event.EventCoordinates, event.Token)
}

func (adapter *Adapter) IngestProtocolFeeClaim(ctx context.Context, event ledger.ProtocolFeeClaim) (ledger.InsertResult, error) {
	result, err := adapter.InsertProtocolFeeClaim(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	return result, adapter.markTokenDirty(ctx, event.ChainID, event.Token)
}

func (adapter *Adapter) IngestRefundCredit(ctx context.Context, event ledger.RefundCredit) (ledger.InsertResult, error) {
	result, err := adapter.InsertRefundCredit(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	return result, adapter.markTokenDirty(ctx, event.ChainID, event.Token)
}

func (adapter *Adapter) IngestRefundClaim(ctx context.Context, event ledger.RefundClaim) (ledger.InsertResult, error) {
	result, err := adapter.InsertRefundClaim(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	return result, adapter.markTokenDirty(ctx, event.ChainID, event.Token)
}

func (adapter *Adapter) IngestPoolMint(ctx context.Context, event ledger.PoolMint) (ledger.InsertResult, error) {
	result, err := adapter.InsertPoolMint(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	return result, adapter.markPairTokenDirty(ctx, event.ChainID, event.Pair)
}

func (adapter *Adapter) IngestPoolBurn(ctx context.Context, event ledger.PoolBurn) (ledger.InsertResult, error) {
	result, err := adapter.InsertPoolBurn(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	return result, adapter.markPairTokenDirty(ctx, event.ChainID, event.Pair)
}

// The remaining canonical events have no token projection side effect; they
// still use the same idempotent insert contract so the router can handle all 18
// ABI event types uniformly.
func (adapter *Adapter) IngestLaunchFeeClaim(ctx context.Context, event ledger.LaunchFeeClaim) (ledger.InsertResult, error) {
	return adapter.InsertLaunchFeeClaim(ctx, event)
}

func (adapter *Adapter) IngestLaunchPauseEvent(ctx context.Context, event ledger.LaunchPauseEvent) (ledger.InsertResult, error) {
	return adapter.InsertLaunchPauseEvent(ctx, event)
}

func (adapter *Adapter) IngestTradingPauseEvent(ctx context.Context, event ledger.TradingPauseEvent) (ledger.InsertResult, error) {
	return adapter.InsertTradingPauseEvent(ctx, event)
}

func (adapter *Adapter) IngestEngineConfiguration(ctx context.Context, event ledger.EngineConfiguration) (ledger.InsertResult, error) {
	return adapter.InsertEngineConfiguration(ctx, event)
}

func (adapter *Adapter) IngestFutureDefaultsConfiguration(ctx context.Context, event ledger.FutureDefaultsConfiguration) (ledger.InsertResult, error) {
	return adapter.InsertFutureDefaultsConfiguration(ctx, event)
}

func (adapter *Adapter) IngestFutureTreasuryConfiguration(ctx context.Context, event ledger.FutureTreasuryConfiguration) (ledger.InsertResult, error) {
	return adapter.InsertFutureTreasuryConfiguration(ctx, event)
}

func projectionIdentity(coordinates ledger.EventCoordinates) sqlc.ApplyTokenLaunchProjectionParams {
	return sqlc.ApplyTokenLaunchProjectionParams{ChainID: coordinates.ChainID, TxHash: sqlc.Hash(coordinates.TxHash), LogIndex: coordinates.LogIndex}
}
