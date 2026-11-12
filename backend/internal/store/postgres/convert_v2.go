package postgres

import (
	"fmt"
	"math/big"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// uint256s converts named unsigned amounts in order, failing on the first invalid one.
func uint256s(entity string, names []string, values ...*big.Int) ([]sqlc.Uint256, error) {
	converted := make([]sqlc.Uint256, len(values))
	for index, value := range values {
		var err error
		if converted[index], err = uint256(entity+" "+names[index], value); err != nil {
			return nil, err
		}
	}
	return converted, nil
}

func uint256Pointer(name string, value *big.Int) (*sqlc.Uint256, error) {
	converted, err := uint256(name, value)
	if err != nil {
		return nil, err
	}
	return &converted, nil
}

// V4 signed deltas are int128/int256 on chain; NUMERIC(78,0) holds |value| < 10^78.
var maxV4SignedNumeric = new(big.Int).Exp(big.NewInt(10), big.NewInt(78), nil)

func v4SignedNumeric(name string, value *big.Int) (pgtype.Numeric, error) {
	if value == nil {
		return pgtype.Numeric{}, fmt.Errorf("%s: value is nil", name)
	}
	if new(big.Int).Abs(value).Cmp(maxV4SignedNumeric) >= 0 {
		return pgtype.Numeric{}, fmt.Errorf("%s: value exceeds 78 decimal digits", name)
	}
	return pgtype.Numeric{Int: new(big.Int).Set(value), Valid: true}, nil
}

func tokenLaunchV2Params(event ledger.TokenLaunchV2) (sqlc.InsertTokenLaunchV2Params, error) {
	b := eventParams(event.EventCoordinates)
	amounts, err := uint256s("token launch", []string{"total supply", "virtual ETH", "virtual token", "curve tokens", "LP tokens", "graduation ETH", "launch fee"},
		event.TotalSupply, event.VirtualETH, event.VirtualToken, event.CurveTokens, event.LPTokens, event.GraduationETH, event.LaunchFeePaid)
	if err != nil {
		return sqlc.InsertTokenLaunchV2Params{}, err
	}
	return sqlc.InsertTokenLaunchV2Params{
		ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex,
		TokenAddress: sqlc.Address(event.Token), CurveAddress: sqlc.Address(event.Curve), Creator: sqlc.Address(event.Creator), ProtocolTreasury: sqlc.Address(event.ProtocolTreasury),
		EngineVersion: int32(event.EngineVersion), Name: event.Name, Symbol: event.Symbol,
		TotalSupply: amounts[0], VirtualEth: amounts[1], VirtualToken: amounts[2], CurveTokens: amounts[3], LpTokens: amounts[4], GraduationEth: amounts[5], LaunchFeePaid: amounts[6],
		TradeFeeBps: int32(event.TradeFeeBPS), ProtocolShareBps: int32(event.ProtocolShareBPS),
		Hook: addressPointer(event.Hook), Locker: addressPointer(event.Locker),
		AntiSnipeStartBps:        pgtype.Int4{Int32: int32(event.AntiSnipeStartBPS), Valid: true},
		AntiSnipeDurationSeconds: pgtype.Int8{Int64: int64(event.AntiSnipeDurationSeconds), Valid: true},
		LpCreatorShareBps:        pgtype.Int4{Int32: int32(event.LPCreatorShareBPS), Valid: true},
	}, nil
}

func tradeV2Params(event ledger.TradeV2) (sqlc.InsertTradeV2Params, error) {
	v1, err := tradeParams(event.Trade)
	if err != nil {
		return sqlc.InsertTradeV2Params{}, err
	}
	snipeTax, err := uint256("trade snipe tax", event.SnipeTax)
	if err != nil {
		return sqlc.InsertTradeV2Params{}, err
	}
	return sqlc.InsertTradeV2Params{
		ChainID: v1.ChainID, BlockNumber: v1.BlockNumber, BlockHash: v1.BlockHash, BlockTime: v1.BlockTime, TransactionIndex: v1.TransactionIndex, TxHash: v1.TxHash, LogIndex: v1.LogIndex,
		TokenAddress: v1.TokenAddress, Trader: v1.Trader, IsBuy: v1.IsBuy, EthGross: v1.EthGross, EthRefund: v1.EthRefund, TokenAmount: v1.TokenAmount,
		ProtocolFee: v1.ProtocolFee, CreatorFee: v1.CreatorFee, NewEthReserve: v1.NewEthReserve, NewTokenReserve: v1.NewTokenReserve, SnipeTax: snipeTax,
	}, nil
}

func graduationV2Params(event ledger.GraduationV2) (sqlc.InsertGraduationV2Params, error) {
	b := eventParams(event.EventCoordinates)
	amounts, err := uint256s("graduation", []string{"ETH to pool", "tokens to pool"}, event.ETHToPool, event.TokensToPool)
	if err != nil {
		return sqlc.InsertGraduationV2Params{}, err
	}
	pointers := make([]*sqlc.Uint256, 0, 5)
	for _, field := range []struct {
		name  string
		value *big.Int
	}{{"position token id", event.PositionTokenID}, {"liquidity", event.Liquidity}, {"sqrt price", event.SqrtPriceX96}, {"ETH remainder", event.ETHRemainder}, {"token remainder", event.TokenRemainder}} {
		pointer, err := uint256Pointer("graduation "+field.name, field.value)
		if err != nil {
			return sqlc.InsertGraduationV2Params{}, err
		}
		pointers = append(pointers, pointer)
	}
	poolID := sqlc.Hash(event.PoolID)
	return sqlc.InsertGraduationV2Params{
		ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex,
		TokenAddress: sqlc.Address(event.Token), EthToPool: amounts[0], TokensToPool: amounts[1], PoolID: &poolID,
		PositionTokenID: pointers[0], Liquidity: pointers[1], SqrtPriceX96: pointers[2], EthRemainder: pointers[3], TokenRemainder: pointers[4],
	}, nil
}

func snipeTaxClaimParams(event ledger.SnipeTaxClaim) (sqlc.InsertSnipeTaxClaimParams, error) {
	b := eventParams(event.EventCoordinates)
	amount, err := uint256("snipe tax claim amount", event.Amount)
	if err != nil {
		return sqlc.InsertSnipeTaxClaimParams{}, err
	}
	return sqlc.InsertSnipeTaxClaimParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, TokenAddress: sqlc.Address(event.Token), Treasury: sqlc.Address(event.Treasury), Amount: amount}, nil
}

func feeRecipientProposalParams(event ledger.FeeRecipientProposal) sqlc.InsertFeeRecipientProposalParams {
	b := eventParams(event.EventCoordinates)
	return sqlc.InsertFeeRecipientProposalParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, TokenAddress: sqlc.Address(event.Token), CurrentRecipient: sqlc.Address(event.Current), NextRecipient: sqlc.Address(event.Next)}
}

func feeRecipientAcceptanceParams(event ledger.FeeRecipientAcceptance) sqlc.InsertFeeRecipientAcceptanceParams {
	b := eventParams(event.EventCoordinates)
	return sqlc.InsertFeeRecipientAcceptanceParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, TokenAddress: sqlc.Address(event.Token), PreviousRecipient: sqlc.Address(event.Previous), NextRecipient: sqlc.Address(event.Next)}
}

func lpFeeCollectionParams(event ledger.LpFeeCollection) (sqlc.InsertLpFeeCollectionParams, error) {
	b := eventParams(event.EventCoordinates)
	amounts, err := uint256s("LP fee collection", []string{"ETH amount", "token amount"}, event.ETHAmount, event.TokenAmount)
	if err != nil {
		return sqlc.InsertLpFeeCollectionParams{}, err
	}
	return sqlc.InsertLpFeeCollectionParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, TokenAddress: sqlc.Address(event.Token), EthAmount: amounts[0], TokenAmount: amounts[1]}, nil
}

func lpFeeClaimParams(event ledger.LpFeeClaim) (sqlc.InsertLpFeeClaimParams, error) {
	b := eventParams(event.EventCoordinates)
	amounts, err := uint256s("LP fee claim", []string{"ETH amount", "token amount"}, event.ETHAmount, event.TokenAmount)
	if err != nil {
		return sqlc.InsertLpFeeClaimParams{}, err
	}
	return sqlc.InsertLpFeeClaimParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, TokenAddress: sqlc.Address(event.Token), Recipient: sqlc.Address(event.Recipient), EthAmount: amounts[0], TokenAmount: amounts[1]}, nil
}

func v4PoolInitializationParams(event ledger.V4PoolInitialization) (sqlc.InsertV4PoolInitializationParams, error) {
	b := eventParams(event.EventCoordinates)
	sqrtPrice, err := uint256("V4 initialize sqrt price", event.SqrtPriceX96)
	if err != nil {
		return sqlc.InsertV4PoolInitializationParams{}, err
	}
	return sqlc.InsertV4PoolInitializationParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, PoolID: sqlc.Hash(event.PoolID), Currency0: sqlc.Address(event.Currency0), Currency1: sqlc.Address(event.Currency1), Fee: int32(event.Fee), TickSpacing: event.TickSpacing, Hooks: sqlc.Address(event.Hooks), SqrtPriceX96: sqrtPrice, Tick: event.Tick}, nil
}

func v4LiquidityChangeParams(event ledger.V4LiquidityChange) (sqlc.InsertV4LiquidityChangeParams, error) {
	b := eventParams(event.EventCoordinates)
	delta, err := v4SignedNumeric("V4 liquidity delta", event.LiquidityDelta)
	if err != nil {
		return sqlc.InsertV4LiquidityChangeParams{}, err
	}
	return sqlc.InsertV4LiquidityChangeParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, PoolID: sqlc.Hash(event.PoolID), Sender: sqlc.Address(event.Sender), TickLower: event.TickLower, TickUpper: event.TickUpper, LiquidityDelta: delta, Salt: sqlc.Hash(event.Salt)}, nil
}

func v4SwapParams(event ledger.V4Swap) (sqlc.InsertV4SwapParams, error) {
	b := eventParams(event.EventCoordinates)
	amount0, err := v4SignedNumeric("V4 swap amount0", event.Amount0)
	if err != nil {
		return sqlc.InsertV4SwapParams{}, err
	}
	amount1, err := v4SignedNumeric("V4 swap amount1", event.Amount1)
	if err != nil {
		return sqlc.InsertV4SwapParams{}, err
	}
	amounts, err := uint256s("V4 swap", []string{"sqrt price", "liquidity"}, event.SqrtPriceX96, event.Liquidity)
	if err != nil {
		return sqlc.InsertV4SwapParams{}, err
	}
	var txFrom *sqlc.Address
	if event.TxFrom != nil {
		txFrom = addressPointer(*event.TxFrom)
	}
	return sqlc.InsertV4SwapParams{ChainID: b.ChainID, BlockNumber: b.BlockNumber, BlockHash: b.BlockHash, BlockTime: b.BlockTime, TransactionIndex: b.TransactionIndex, TxHash: b.TxHash, LogIndex: b.LogIndex, PoolID: sqlc.Hash(event.PoolID), Sender: sqlc.Address(event.Sender), Amount0Delta: amount0, Amount1Delta: amount1, SqrtPriceX96: amounts[0], Liquidity: amounts[1], Tick: event.Tick, Fee: int32(event.Fee), TxFrom: txFrom}, nil
}
