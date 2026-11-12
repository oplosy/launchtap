package postgres

import (
	"fmt"
	"math/big"
	"time"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgtype"
)

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

// addressPointer binds a nullable address column (engine 2 has no pair or WETH).
func addressPointer(address common.Address) *sqlc.Address {
	value := sqlc.Address(address)
	return &value
}

// addressOrZero reads a nullable address column. Every engine-1 row carries the address; an
// engine-2 row has none and reads as the zero address until the engine-aware read paths
// (Plan v2-B Tasks 8 and 10) expose the V4 pool instead.
func addressOrZero(value *sqlc.Address) common.Address {
	if value == nil {
		return common.Address{}
	}
	return common.Address(*value)
}

func eventParams(coordinates ledger.EventCoordinates) eventParameters {
	return eventParameters{
		ChainID: coordinates.ChainID, BlockNumber: coordinates.BlockNumber,
		BlockHash: sqlc.Hash(coordinates.BlockHash), BlockTime: timestamptz(coordinates.BlockTime),
		TransactionIndex: coordinates.TransactionIndex, TxHash: sqlc.Hash(coordinates.TxHash), LogIndex: coordinates.LogIndex,
	}
}

type eventParameters struct {
	ChainID          int64
	BlockNumber      int64
	BlockHash        sqlc.Hash
	BlockTime        pgtype.Timestamptz
	TransactionIndex int32
	TxHash           sqlc.Hash
	LogIndex         int32
}

func tradeParams(event ledger.Trade) (sqlc.InsertTradeParams, error) {
	base := eventParams(event.EventCoordinates)
	ethGross, err := uint256("trade eth gross", event.ETHGross)
	if err != nil {
		return sqlc.InsertTradeParams{}, err
	}
	ethRefund, err := uint256("trade eth refund", event.ETHRefund)
	if err != nil {
		return sqlc.InsertTradeParams{}, err
	}
	tokenAmount, err := uint256("trade token amount", event.TokenAmount)
	if err != nil {
		return sqlc.InsertTradeParams{}, err
	}
	protocolFee, err := uint256("trade protocol fee", event.ProtocolFee)
	if err != nil {
		return sqlc.InsertTradeParams{}, err
	}
	creatorFee, err := uint256("trade creator fee", event.CreatorFee)
	if err != nil {
		return sqlc.InsertTradeParams{}, err
	}
	newETHReserve, err := uint256("trade new ETH reserve", event.NewETHReserve)
	if err != nil {
		return sqlc.InsertTradeParams{}, err
	}
	newTokenReserve, err := uint256("trade new token reserve", event.NewTokenReserve)
	if err != nil {
		return sqlc.InsertTradeParams{}, err
	}
	return sqlc.InsertTradeParams{
		ChainID: base.ChainID, BlockNumber: base.BlockNumber, BlockHash: base.BlockHash, BlockTime: base.BlockTime,
		TransactionIndex: base.TransactionIndex, TxHash: base.TxHash, LogIndex: base.LogIndex,
		TokenAddress: sqlc.Address(event.Token), Trader: sqlc.Address(event.Trader), IsBuy: event.IsBuy,
		EthGross: ethGross, EthRefund: ethRefund, TokenAmount: tokenAmount, ProtocolFee: protocolFee,
		CreatorFee: creatorFee, NewEthReserve: newETHReserve, NewTokenReserve: newTokenReserve,
	}, nil
}

func launchPauseParams(event ledger.LaunchPauseEvent) sqlc.InsertLaunchPauseEventParams {
	base := eventParams(event.EventCoordinates)
	return sqlc.InsertLaunchPauseEventParams{ChainID: base.ChainID, BlockNumber: base.BlockNumber, BlockHash: base.BlockHash, BlockTime: base.BlockTime, TransactionIndex: base.TransactionIndex, TxHash: base.TxHash, LogIndex: base.LogIndex, Paused: event.Paused}
}

func tokenLaunchParams(event ledger.TokenLaunch) (sqlc.InsertTokenLaunchParams, error) {
	b := eventParams(event.EventCoordinates)
	values := []*big.Int{event.TotalSupply, event.VirtualETH, event.VirtualToken, event.CurveTokens, event.LPTokens, event.GraduationETH, event.LaunchFeePaid}
	converted := make([]sqlc.Uint256, len(values))
	for index, value := range values {
		var err error
		converted[index], err = uint256("token launch amount", value)
		if err != nil {
			return sqlc.InsertTokenLaunchParams{}, err
		}
	}
	return sqlc.InsertTokenLaunchParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, TokenAddress: sqlc.Address(event.Token), CurveAddress: sqlc.Address(event.Curve), Creator: sqlc.Address(event.Creator), LpPair: addressPointer(event.LPPair), Weth: addressPointer(event.WETH), ProtocolTreasury: sqlc.Address(event.ProtocolTreasury), EngineVersion: int32(event.EngineVersion), Name: event.Name, Symbol: event.Symbol, TotalSupply: converted[0], VirtualEth: converted[1], VirtualToken: converted[2], CurveTokens: converted[3], LpTokens: converted[4], GraduationEth: converted[5], LaunchFeePaid: converted[6], TradeFeeBps: int32(event.TradeFeeBPS), ProtocolShareBps: int32(event.ProtocolShareBPS)}, nil
}

func graduationParams(event ledger.Graduation) (sqlc.InsertGraduationParams, error) {
	b := eventParams(event.EventCoordinates)
	eth, err := uint256("graduation ETH to pool", event.ETHToPool)
	if err != nil {
		return sqlc.InsertGraduationParams{}, err
	}
	tokens, err := uint256("graduation tokens to pool", event.TokensToPool)
	if err != nil {
		return sqlc.InsertGraduationParams{}, err
	}
	lp, err := uint256("graduation LP burned", event.LPLiquidityBurned)
	if err != nil {
		return sqlc.InsertGraduationParams{}, err
	}
	return sqlc.InsertGraduationParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, TokenAddress: sqlc.Address(event.Token), LpPair: addressPointer(event.LPPair), EthToPool: eth, TokensToPool: tokens, LpLiquidityBurned: &lp}, nil
}

func creatorFeeClaimParams(event ledger.CreatorFeeClaim) (sqlc.InsertCreatorFeeClaimParams, error) {
	b := eventParams(event.EventCoordinates)
	amount, err := uint256("creator fee claim amount", event.Amount)
	if err != nil {
		return sqlc.InsertCreatorFeeClaimParams{}, err
	}
	return sqlc.InsertCreatorFeeClaimParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, TokenAddress: sqlc.Address(event.Token), Creator: sqlc.Address(event.Creator), Amount: amount}, nil
}
func protocolFeeClaimParams(event ledger.ProtocolFeeClaim) (sqlc.InsertProtocolFeeClaimParams, error) {
	b := eventParams(event.EventCoordinates)
	amount, err := uint256("protocol fee claim amount", event.Amount)
	if err != nil {
		return sqlc.InsertProtocolFeeClaimParams{}, err
	}
	return sqlc.InsertProtocolFeeClaimParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, TokenAddress: sqlc.Address(event.Token), Treasury: sqlc.Address(event.Treasury), Amount: amount}, nil
}
func launchFeeClaimParams(event ledger.LaunchFeeClaim) (sqlc.InsertLaunchFeeClaimParams, error) {
	b := eventParams(event.EventCoordinates)
	amount, err := uint256("launch fee claim amount", event.Amount)
	if err != nil {
		return sqlc.InsertLaunchFeeClaimParams{}, err
	}
	return sqlc.InsertLaunchFeeClaimParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, Treasury: sqlc.Address(event.Treasury), Amount: amount}, nil
}
func refundCreditParams(event ledger.RefundCredit) (sqlc.InsertRefundCreditParams, error) {
	b := eventParams(event.EventCoordinates)
	amount, err := uint256("refund credit amount", event.Amount)
	if err != nil {
		return sqlc.InsertRefundCreditParams{}, err
	}
	return sqlc.InsertRefundCreditParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, TokenAddress: sqlc.Address(event.Token), Account: sqlc.Address(event.Account), Amount: amount}, nil
}
func refundClaimParams(event ledger.RefundClaim) (sqlc.InsertRefundClaimParams, error) {
	b := eventParams(event.EventCoordinates)
	amount, err := uint256("refund claim amount", event.Amount)
	if err != nil {
		return sqlc.InsertRefundClaimParams{}, err
	}
	return sqlc.InsertRefundClaimParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, TokenAddress: sqlc.Address(event.Token), Account: sqlc.Address(event.Account), Amount: amount}, nil
}
func transferParams(event ledger.Transfer) (sqlc.InsertTransferParams, error) {
	b := eventParams(event.EventCoordinates)
	value, err := uint256("transfer value", event.Value)
	if err != nil {
		return sqlc.InsertTransferParams{}, err
	}
	return sqlc.InsertTransferParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, TokenAddress: sqlc.Address(event.Token), FromAddress: sqlc.Address(event.From), ToAddress: sqlc.Address(event.To), Value: value}, nil
}

func poolMintParams(event ledger.PoolMint) (sqlc.InsertPoolMintParams, error) {
	b := eventParams(event.EventCoordinates)
	a0, err := uint256("pool mint amount0", event.Amount0)
	if err != nil {
		return sqlc.InsertPoolMintParams{}, err
	}
	a1, err := uint256("pool mint amount1", event.Amount1)
	if err != nil {
		return sqlc.InsertPoolMintParams{}, err
	}
	return sqlc.InsertPoolMintParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, PairAddress: sqlc.Address(event.Pair), Sender: sqlc.Address(event.Sender), Amount0: a0, Amount1: a1}, nil
}
func poolBurnParams(event ledger.PoolBurn) (sqlc.InsertPoolBurnParams, error) {
	b := eventParams(event.EventCoordinates)
	a0, err := uint256("pool burn amount0", event.Amount0)
	if err != nil {
		return sqlc.InsertPoolBurnParams{}, err
	}
	a1, err := uint256("pool burn amount1", event.Amount1)
	if err != nil {
		return sqlc.InsertPoolBurnParams{}, err
	}
	return sqlc.InsertPoolBurnParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, PairAddress: sqlc.Address(event.Pair), Sender: sqlc.Address(event.Sender), Amount0: a0, Amount1: a1, ToAddress: sqlc.Address(event.To)}, nil
}
func poolSwapParams(event ledger.PoolSwap) (sqlc.InsertPoolSwapParams, error) {
	b := eventParams(event.EventCoordinates)
	amounts := []*big.Int{event.Amount0In, event.Amount1In, event.Amount0Out, event.Amount1Out}
	converted := make([]sqlc.Uint256, len(amounts))
	for i, v := range amounts {
		var err error
		converted[i], err = uint256("pool swap amount", v)
		if err != nil {
			return sqlc.InsertPoolSwapParams{}, err
		}
	}
	return sqlc.InsertPoolSwapParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, PairAddress: sqlc.Address(event.Pair), Sender: sqlc.Address(event.Sender), Amount0In: converted[0], Amount1In: converted[1], Amount0Out: converted[2], Amount1Out: converted[3], ToAddress: sqlc.Address(event.To), TxFrom: txFromBytes(event.TxFrom)}, nil
}

// txFromBytes maps an unknown (nil) sender to SQL NULL.
func txFromBytes(sender *common.Address) *sqlc.Address {
	if sender == nil {
		return nil
	}
	converted := sqlc.Address(*sender)
	return &converted
}

// poolSwapMatchParams drops tx_from: the sender is enrichment, not part of event identity.
func poolSwapMatchParams(arg sqlc.InsertPoolSwapParams) sqlc.PoolSwapMatchesEventParams {
	return sqlc.PoolSwapMatchesEventParams{
		ChainID: arg.ChainID, BlockNumber: arg.BlockNumber, BlockHash: arg.BlockHash, BlockTime: arg.BlockTime,
		TransactionIndex: arg.TransactionIndex, TxHash: arg.TxHash, LogIndex: arg.LogIndex,
		PairAddress: arg.PairAddress, Sender: arg.Sender, Amount0In: arg.Amount0In, Amount1In: arg.Amount1In,
		Amount0Out: arg.Amount0Out, Amount1Out: arg.Amount1Out, ToAddress: arg.ToAddress,
	}
}
func poolSyncParams(event ledger.PoolSync) (sqlc.InsertPoolSyncParams, error) {
	b := eventParams(event.EventCoordinates)
	r0, err := uint256("pool sync reserve0", event.Reserve0)
	if err != nil {
		return sqlc.InsertPoolSyncParams{}, err
	}
	r1, err := uint256("pool sync reserve1", event.Reserve1)
	if err != nil {
		return sqlc.InsertPoolSyncParams{}, err
	}
	return sqlc.InsertPoolSyncParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, PairAddress: sqlc.Address(event.Pair), Reserve0: r0, Reserve1: r1}, nil
}
func tradingPauseParams(event ledger.TradingPauseEvent) sqlc.InsertTradingPauseEventParams {
	b := eventParams(event.EventCoordinates)
	return sqlc.InsertTradingPauseEventParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, Paused: event.Paused}
}
func engineConfigurationParams(event ledger.EngineConfiguration) sqlc.InsertEngineConfigurationParams {
	b := eventParams(event.EventCoordinates)
	return sqlc.InsertEngineConfigurationParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, EngineVersion: int32(event.EngineVersion), Implementation: sqlc.Address(event.Implementation), Enabled: event.Enabled}
}
func futureDefaultsConfigurationParams(event ledger.FutureDefaultsConfiguration) sqlc.InsertFutureDefaultsConfigurationParams {
	b := eventParams(event.EventCoordinates)
	return sqlc.InsertFutureDefaultsConfigurationParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, ConfigHash: sqlc.Hash(event.ConfigHash)}
}
func futureTreasuryConfigurationParams(event ledger.FutureTreasuryConfiguration) sqlc.InsertFutureTreasuryConfigurationParams {
	b := eventParams(event.EventCoordinates)
	return sqlc.InsertFutureTreasuryConfigurationParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, PreviousTreasury: sqlc.Address(event.PreviousTreasury), NewTreasury: sqlc.Address(event.NewTreasury)}
}

func indexedBlockParams(block ledger.IndexedBlock) sqlc.UpsertIndexedBlockParams {
	return sqlc.UpsertIndexedBlockParams{ChainID: block.ChainID, BlockNumber: block.BlockNumber, BlockHash: sqlc.Hash(block.BlockHash), ParentHash: sqlc.Hash(block.ParentHash), BlockTime: timestamptz(block.BlockTime), FinalityStatus: block.FinalityStatus}
}

func uint256(name string, value *big.Int) (sqlc.Uint256, error) {
	result, err := sqlc.NewUint256(value)
	if err != nil {
		return sqlc.Uint256{}, fmt.Errorf("%s: %w", name, err)
	}
	return result, nil
}

// maxSignedNumeric bounds the signed NUMERIC(79,0) columns (wallet realized PnL): |value| < 10^79.
var maxSignedNumeric = new(big.Int).Exp(big.NewInt(10), big.NewInt(79), nil)

// signedNumeric converts a signed integer for a NUMERIC(79,0) column. The Uint256 codec rejects
// negatives, so signed columns stay pgtype.Numeric.
func signedNumeric(name string, value *big.Int) (pgtype.Numeric, error) {
	if value == nil {
		return pgtype.Numeric{}, fmt.Errorf("%s: value is nil", name)
	}
	if new(big.Int).Abs(value).Cmp(maxSignedNumeric) >= 0 {
		return pgtype.Numeric{}, fmt.Errorf("%s: value exceeds 79 decimal digits", name)
	}
	return pgtype.Numeric{Int: new(big.Int).Set(value), Valid: true}, nil
}

// signedBigInt converts a finite, integral, non-NULL numeric into a signed integer of at most 79
// decimal digits.
func signedBigInt(name string, numeric pgtype.Numeric) (*big.Int, error) {
	if !numeric.Valid {
		return nil, fmt.Errorf("%s: value is NULL", name)
	}
	if numeric.NaN || numeric.InfinityModifier != pgtype.Finite || numeric.Int == nil {
		return nil, fmt.Errorf("%s: value is not finite", name)
	}
	value := new(big.Int).Set(numeric.Int)
	if value.Sign() != 0 && numeric.Exp != 0 {
		if numeric.Exp > 78 || numeric.Exp < -79 {
			return nil, fmt.Errorf("%s: value out of range", name)
		}
		exponent := int64(numeric.Exp)
		if exponent < 0 {
			exponent = -exponent
		}
		power := new(big.Int).Exp(big.NewInt(10), big.NewInt(exponent), nil)
		if numeric.Exp > 0 {
			value.Mul(value, power)
		} else {
			remainder := new(big.Int)
			value.QuoRem(value, power, remainder)
			if remainder.Sign() != 0 {
				return nil, fmt.Errorf("%s: value is fractional", name)
			}
		}
	}
	if new(big.Int).Abs(value).Cmp(maxSignedNumeric) >= 0 {
		return nil, fmt.Errorf("%s: value exceeds 79 decimal digits", name)
	}
	return value, nil
}
