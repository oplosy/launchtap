package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// InvariantConflictError reports that an immutable database identity was
// replayed with a different payload.
type InvariantConflictError struct {
	Entity string
	Key    string
	Cause  error
}

type refreshDeploymentContextKey struct{}

// NegativeBalanceError identifies a canonical transfer that cannot be folded.
type NegativeBalanceError struct {
	Token, Holder string
	Key           string
}

func (err *NegativeBalanceError) Error() string {
	return fmt.Sprintf("holder balance would become negative for token=%s holder=%s at %s", err.Token, err.Holder, err.Key)
}

func (err *InvariantConflictError) Error() string {
	return fmt.Sprintf("%s invariant conflict at %s", err.Entity, err.Key)
}

func (err *InvariantConflictError) Unwrap() error {
	return err.Cause
}

// Adapter adds persistence invariants around generated sqlc queries. Most
// methods use the supplied database handle as-is; protocol aggregate refreshes
// open a transaction for a pool-backed adapter and reuse an existing tx.
type Adapter struct {
	queries  *sqlc.Queries
	beginner TransactionBeginner
	// factory is excluded from wallet position tracking (R4). The zero value is already excluded.
	factory common.Address
}

func NewAdapter(db DBTX) *Adapter {
	beginner, _ := db.(TransactionBeginner)
	return &Adapter{queries: sqlc.New(db), beginner: beginner}
}

// WithFactory returns a copy of the adapter that excludes the configured factory from wallet
// position dirty marking.
func (adapter *Adapter) WithFactory(factory common.Address) *Adapter {
	clone := *adapter
	clone.factory = factory
	return &clone
}

func (adapter *Adapter) markPositionDirty(ctx context.Context, chainID int64, wallet, token common.Address) error {
	if err := adapter.queries.MarkPositionDirty(ctx, sqlc.MarkPositionDirtyParams{ChainID: chainID, Wallet: wallet[:], TokenAddress: token[:], Factory: adapter.factory[:]}); err != nil {
		return fmt.Errorf("mark position dirty: %w", err)
	}
	return nil
}

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

// IngestTokenLaunch writes a canonical launch and its projection only once.
func (adapter *Adapter) IngestTokenLaunch(ctx context.Context, event ledger.TokenLaunch) (ledger.InsertResult, error) {
	result, err := adapter.InsertTokenLaunch(ctx, event)
	if err != nil || !result.Inserted {
		return result, err
	}
	identity := projectionIdentity(event.EventCoordinates)
	if err := adapter.queries.ApplyTokenLaunchProjection(ctx, identity); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("apply token launch projection: %w", err)
	}
	if err := adapter.queries.MarkTokenDirty(ctx, sqlc.MarkTokenDirtyParams{ChainID: event.ChainID, TokenAddress: sqlc.Address(event.Token)}); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("mark token dirty: %w", err)
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
	if err := adapter.queries.MarkTokenDirty(ctx, sqlc.MarkTokenDirtyParams{ChainID: event.ChainID, TokenAddress: sqlc.Address(event.Token)}); err != nil {
		return fmt.Errorf("mark token dirty: %w", err)
	}
	return adapter.markPositionDirty(ctx, event.ChainID, event.Trader, event.Token)
}

func (adapter *Adapter) IngestGraduation(ctx context.Context, event ledger.Graduation) (ledger.InsertResult, error) {
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
	if err := adapter.queries.MarkTokenDirty(ctx, sqlc.MarkTokenDirtyParams{ChainID: event.ChainID, TokenAddress: sqlc.Address(event.Token)}); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("mark token dirty: %w", err)
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
	if err := adapter.queries.MarkTokenDirty(ctx, sqlc.MarkTokenDirtyParams{ChainID: event.ChainID, TokenAddress: sqlc.Address(event.Token)}); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("mark token dirty: %w", err)
	}
	if err := adapter.queries.MarkTransferPositionsDirty(ctx, sqlc.MarkTransferPositionsDirtyParams{ChainID: event.ChainID, TokenAddress: event.Token[:], FromAddress: event.From[:], ToAddress: event.To[:], Factory: adapter.factory[:]}); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("mark transfer positions dirty: %w", err)
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
	if err := adapter.queries.MarkPairTokenDirty(ctx, sqlc.MarkPairTokenDirtyParams{ChainID: event.ChainID, LpPair: addressPointer(event.Pair)}); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("mark pair token dirty: %w", err)
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
	if err := adapter.queries.MarkPairTokenDirty(ctx, sqlc.MarkPairTokenDirtyParams{ChainID: event.ChainID, LpPair: addressPointer(event.Pair)}); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("mark pair token dirty: %w", err)
	}
	var txFrom []byte
	if event.TxFrom != nil {
		txFrom = event.TxFrom[:]
	}
	if err := adapter.queries.MarkPoolSwapPositionDirty(ctx, sqlc.MarkPoolSwapPositionDirtyParams{ChainID: event.ChainID, Pair: event.Pair[:], TxFrom: txFrom, Factory: adapter.factory[:]}); err != nil {
		return ledger.InsertResult{}, fmt.Errorf("mark pool swap position dirty: %w", err)
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

func (adapter *Adapter) markTokenDirty(ctx context.Context, chainID int64, token common.Address) error {
	if err := adapter.queries.MarkTokenDirty(ctx, sqlc.MarkTokenDirtyParams{ChainID: chainID, TokenAddress: sqlc.Address(token)}); err != nil {
		return fmt.Errorf("mark token dirty: %w", err)
	}
	return nil
}

func (adapter *Adapter) markPairTokenDirty(ctx context.Context, chainID int64, pair common.Address) error {
	if err := adapter.queries.MarkPairTokenDirty(ctx, sqlc.MarkPairTokenDirtyParams{ChainID: chainID, LpPair: addressPointer(pair)}); err != nil {
		return fmt.Errorf("mark pair token dirty: %w", err)
	}
	return nil
}

func projectionIdentity(coordinates ledger.EventCoordinates) sqlc.ApplyTokenLaunchProjectionParams {
	return sqlc.ApplyTokenLaunchProjectionParams{ChainID: coordinates.ChainID, TxHash: sqlc.Hash(coordinates.TxHash), LogIndex: coordinates.LogIndex}
}

func (adapter *Adapter) UpsertIndexedBlock(ctx context.Context, block ledger.IndexedBlock) (ledger.UpsertResult, error) {
	arg := indexedBlockParams(block)
	rows, err := adapter.queries.UpsertIndexedBlock(ctx, arg)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return ledger.UpsertResult{}, invariantConflict(
				"indexed_block",
				fmt.Sprintf("chain_id=%d block_number=%d", arg.ChainID, arg.BlockNumber),
				err,
			)
		}
		return ledger.UpsertResult{}, fmt.Errorf("upsert indexed block: %w", err)
	}
	if rows == 1 {
		return ledger.UpsertResult{Changed: true}, nil
	}
	stored, err := adapter.queries.GetIndexedBlockByNumber(ctx, sqlc.GetIndexedBlockByNumberParams{
		ChainID: arg.ChainID, BlockNumber: arg.BlockNumber,
	})
	if err != nil {
		return ledger.UpsertResult{}, fmt.Errorf("read conflicting indexed block: %w", err)
	}
	if equalIndexedBlockIdentity(stored, block) {
		return ledger.UpsertResult{}, fmt.Errorf("upsert indexed block affected no rows despite identical identity")
	}
	return ledger.UpsertResult{}, invariantConflict(
		"indexed_block",
		fmt.Sprintf("chain_id=%d block_number=%d", arg.ChainID, arg.BlockNumber),
		nil,
	)
}

// UpsertIndexedBlocks writes blocks of one chain, each block number once, with one multi-row
// upsert under UpsertIndexedBlock's conflict rule. When fewer rows are affected than given, the
// blocks are replayed through UpsertIndexedBlock (on the same connection, so inside the caller's
// transaction) to return the exact conflicting block's error. A unique violation aborts the
// statement, so it is reported for the whole block range.
func (adapter *Adapter) UpsertIndexedBlocks(ctx context.Context, blocks []ledger.IndexedBlock) (ledger.UpsertResult, error) {
	if len(blocks) == 0 {
		return ledger.UpsertResult{Changed: true}, nil
	}
	arg := sqlc.UpsertIndexedBlocksParams{
		ChainID:          blocks[0].ChainID,
		BlockNumbers:     make([]int64, len(blocks)),
		BlockHashes:      make([][]byte, len(blocks)),
		ParentHashes:     make([][]byte, len(blocks)),
		BlockTimes:       make([]pgtype.Timestamptz, len(blocks)),
		FinalityStatuses: make([]string, len(blocks)),
	}
	seen := make(map[int64]struct{}, len(blocks))
	low, high := blocks[0].BlockNumber, blocks[0].BlockNumber
	for index := range blocks {
		block := &blocks[index]
		if block.ChainID != arg.ChainID {
			return ledger.UpsertResult{}, fmt.Errorf("upsert indexed blocks: mixed chains %d and %d", arg.ChainID, block.ChainID)
		}
		if _, duplicate := seen[block.BlockNumber]; duplicate {
			return ledger.UpsertResult{}, fmt.Errorf("upsert indexed blocks: block %d given twice", block.BlockNumber)
		}
		seen[block.BlockNumber] = struct{}{}
		low, high = min(low, block.BlockNumber), max(high, block.BlockNumber)
		arg.BlockNumbers[index] = block.BlockNumber
		arg.BlockHashes[index] = block.BlockHash.Bytes()
		arg.ParentHashes[index] = block.ParentHash.Bytes()
		arg.BlockTimes[index] = timestamptz(block.BlockTime)
		arg.FinalityStatuses[index] = block.FinalityStatus
	}
	rows, err := adapter.queries.UpsertIndexedBlocks(ctx, arg)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return ledger.UpsertResult{}, invariantConflict(
				"indexed_block",
				fmt.Sprintf("chain_id=%d block_number=%d..%d", arg.ChainID, low, high),
				err,
			)
		}
		return ledger.UpsertResult{}, fmt.Errorf("upsert indexed blocks: %w", err)
	}
	if rows == int64(len(blocks)) {
		return ledger.UpsertResult{Changed: true}, nil
	}
	for _, block := range blocks {
		if _, err := adapter.UpsertIndexedBlock(ctx, block); err != nil {
			return ledger.UpsertResult{}, err
		}
	}
	return ledger.UpsertResult{}, fmt.Errorf("upsert indexed blocks affected %d of %d rows but no block conflicts", rows, len(blocks))
}

func (adapter *Adapter) GetIndexedBlockByNumber(
	ctx context.Context,
	chainID int64,
	blockNumber int64,
) (IndexedBlock, error) {
	block, err := adapter.queries.GetIndexedBlockByNumber(ctx, sqlc.GetIndexedBlockByNumberParams{
		ChainID: chainID, BlockNumber: blockNumber,
	})
	if err != nil {
		return IndexedBlock{}, fmt.Errorf("get indexed block by number: %w", err)
	}
	return ledger.IndexedBlock{
		ChainID: block.ChainID, BlockNumber: block.BlockNumber, BlockHash: common.Hash(block.BlockHash),
		ParentHash: common.Hash(block.ParentHash), BlockTime: block.BlockTime.Time, FinalityStatus: block.FinalityStatus,
	}, nil
}

func (adapter *Adapter) GetIndexedBlockByHash(
	ctx context.Context,
	chainID int64,
	blockHash common.Hash,
) (IndexedBlock, error) {
	block, err := adapter.queries.GetIndexedBlockByHash(ctx, sqlc.GetIndexedBlockByHashParams{
		ChainID: chainID, BlockHash: sqlc.Hash(blockHash),
	})
	if err != nil {
		return IndexedBlock{}, fmt.Errorf("get indexed block by hash: %w", err)
	}
	return ledger.IndexedBlock{
		ChainID: block.ChainID, BlockNumber: block.BlockNumber, BlockHash: common.Hash(block.BlockHash),
		ParentHash: common.Hash(block.ParentHash), BlockTime: block.BlockTime.Time, FinalityStatus: block.FinalityStatus,
	}, nil
}

func (adapter *Adapter) UpsertSyncState(ctx context.Context, state SyncState) (SyncState, error) {
	stored, err := adapter.queries.UpsertSyncState(ctx, sqlc.UpsertSyncStateParams(state))
	if err != nil {
		return SyncState{}, fmt.Errorf("upsert sync state: %w", err)
	}
	return SyncState(stored), nil
}

func (adapter *Adapter) GetSyncState(ctx context.Context, chainID int64, deploymentID string) (SyncState, error) {
	stored, err := adapter.queries.GetSyncState(ctx, sqlc.GetSyncStateParams{
		ChainID: chainID, DeploymentID: deploymentID,
	})
	if err != nil {
		return SyncState{}, fmt.Errorf("get sync state: %w", err)
	}
	return SyncState(stored), nil
}

func (adapter *Adapter) RebuildTokenProjections(ctx context.Context, chainID int64, token common.Address) error {
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

func (adapter *Adapter) ClaimAggregationDirty(
	ctx context.Context,
	workerID string,
	batchSize int32,
) ([]DirtyClaim, error) {
	rows, err := adapter.queries.ClaimAggregationDirty(ctx, sqlc.ClaimAggregationDirtyParams{
		WorkerID: pgtype.Text{String: workerID, Valid: true}, BatchSize: batchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("claim dirty aggregation: %w", err)
	}
	claims := make([]DirtyClaim, 0, len(rows))
	for _, row := range rows {
		if !row.ClaimedGeneration.Valid {
			return nil, fmt.Errorf("claim dirty aggregation: returned NULL claimed generation")
		}
		claims = append(claims, DirtyClaim{
			ChainID: row.ChainID, TokenAddress: row.TokenAddress,
			ClaimedGeneration: row.ClaimedGeneration.Int64,
		})
	}
	return claims, nil
}

func (adapter *Adapter) CompleteAggregationDirty(
	ctx context.Context,
	claim DirtyClaim,
	workerID string,
) (bool, error) {
	rows, err := adapter.queries.CompleteAggregationDirty(ctx, sqlc.CompleteAggregationDirtyParams{
		ChainID: claim.ChainID, TokenAddress: claim.TokenAddress,
		ClaimedGeneration: claim.ClaimedGeneration,
		WorkerID:          pgtype.Text{String: workerID, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("complete dirty aggregation: %w", err)
	}
	return rows == 1, nil
}

func (adapter *Adapter) RecomputeTokenStats(ctx context.Context, chainID int64, token common.Address) error {
	if err := adapter.queries.RecomputeTokenStats(ctx, sqlc.RecomputeTokenStatsParams{ChainID: chainID, TokenAddress: sqlc.Address(token)}); err != nil {
		return fmt.Errorf("recompute token stats: %w", err)
	}
	return nil
}
func (adapter *Adapter) DeleteTokenStats(ctx context.Context, chainID int64, token common.Address) error {
	if _, err := adapter.queries.DeleteTokenStats(ctx, sqlc.DeleteTokenStatsParams{ChainID: chainID, TokenAddress: sqlc.Address(token)}); err != nil {
		return fmt.Errorf("delete token stats: %w", err)
	}
	return nil
}

// RecomputeProtocolAggregates rebuilds protocol_daily and protocol_stats from the whole history
// and resets the daily watermark. Reorg recovery uses it after deleting canonical rows.
func (adapter *Adapter) RecomputeProtocolAggregates(ctx context.Context, chainID int64) error {
	return adapter.withinProtocolTx(ctx, func(ctx context.Context, transaction *Adapter) error {
		return transaction.refreshProtocolAggregates(ctx, chainID, true)
	})
}

// RefreshProtocolAggregates brings the protocol aggregates up to the indexed head, rebuilding
// only the UTC days of blocks indexed since the last refresh. It falls back to a full rebuild
// when no watermark exists yet or the watermark is above the head.
func (adapter *Adapter) RefreshProtocolAggregates(ctx context.Context, chainID int64) error {
	return adapter.withinProtocolTx(ctx, func(ctx context.Context, transaction *Adapter) error {
		return transaction.refreshProtocolAggregates(ctx, chainID, false)
	})
}

func (adapter *Adapter) withinProtocolTx(ctx context.Context, fn func(context.Context, *Adapter) error) error {
	if adapter.beginner != nil {
		return WithinTx(ctx, adapter.beginner, fn)
	}
	return fn(ctx, adapter)
}

// refreshProtocolAggregates runs within the caller's transaction. The advisory lock is taken
// before the watermark read so a concurrent refresh, including a reorg's full rebuild, is fully
// committed before this one decides which days to rebuild.
func (adapter *Adapter) refreshProtocolAggregates(ctx context.Context, chainID int64, full bool) error {
	if err := adapter.queries.LockProtocolAggregates(ctx, chainID); err != nil {
		return fmt.Errorf("lock protocol aggregates: %w", err)
	}
	bounds, err := adapter.queries.GetProtocolAggregateBounds(ctx, chainID)
	if err != nil {
		return fmt.Errorf("read protocol aggregate bounds: %w", err)
	}
	// -1 means "none" for both values.
	if bounds.DailyThroughBlock < 0 || bounds.DailyThroughBlock > bounds.HeadBlock {
		full = true
	}
	fromDay := pgtype.Date{InfinityModifier: pgtype.NegativeInfinity, Valid: true}
	rebuild := full
	if !full && bounds.HeadBlock > bounds.DailyThroughBlock {
		fromDay, err = adapter.queries.GetProtocolDailyRefreshDay(ctx, sqlc.GetProtocolDailyRefreshDayParams{
			ChainID: chainID, AfterBlock: bounds.DailyThroughBlock, ThroughBlock: bounds.HeadBlock,
		})
		if err != nil {
			return fmt.Errorf("read protocol daily refresh day: %w", err)
		}
		if !fromDay.Valid {
			return fmt.Errorf("no indexed block between %d and head %d", bounds.DailyThroughBlock, bounds.HeadBlock)
		}
		rebuild = true
	}
	if rebuild {
		if err := adapter.queries.ClearProtocolDaily(ctx, sqlc.ClearProtocolDailyParams{ChainID: chainID, FromDay: fromDay}); err != nil {
			return fmt.Errorf("clear protocol daily: %w", err)
		}
		if err := adapter.queries.RecomputeProtocolDaily(ctx, sqlc.RecomputeProtocolDailyParams{ChainID: chainID, FromDay: fromDay}); err != nil {
			return fmt.Errorf("recompute protocol daily: %w", err)
		}
	}
	through := pgtype.Int8{Int64: bounds.HeadBlock, Valid: bounds.HeadBlock >= 0}
	if err := adapter.queries.RecomputeProtocolStats(ctx, sqlc.RecomputeProtocolStatsParams{ChainID: chainID, DailyThroughBlock: through}); err != nil {
		return fmt.Errorf("recompute protocol aggregates: %w", err)
	}
	return nil
}

func invariantConflict(entity, key string, cause error) error {
	return &InvariantConflictError{Entity: entity, Key: key, Cause: cause}
}

func eventKey(chainID int64, txHash sqlc.Hash, logIndex int32) string {
	return fmt.Sprintf("chain_id=%d tx_hash=%x log_index=%d", chainID, txHash, logIndex)
}

func equalIndexedBlockIdentity(stored sqlc.IndexedBlock, attempted ledger.IndexedBlock) bool {
	return stored.ChainID == attempted.ChainID &&
		stored.BlockNumber == attempted.BlockNumber &&
		stored.BlockHash == sqlc.Hash(attempted.BlockHash) &&
		stored.ParentHash == sqlc.Hash(attempted.ParentHash) &&
		equalTimestamptz(stored.BlockTime, timestamptz(attempted.BlockTime))
}
