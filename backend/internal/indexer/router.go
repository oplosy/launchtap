package indexer

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/ledger"
)

type EventSink interface {
	IngestTokenLaunch(context.Context, ledger.TokenLaunch) (ledger.InsertResult, error)
	IngestTrade(context.Context, ledger.Trade) (ledger.InsertResult, error)
	IngestGraduation(context.Context, ledger.Graduation) (ledger.InsertResult, error)
	IngestCreatorFeeClaim(context.Context, ledger.CreatorFeeClaim) (ledger.InsertResult, error)
	IngestProtocolFeeClaim(context.Context, ledger.ProtocolFeeClaim) (ledger.InsertResult, error)
	IngestLaunchFeeClaim(context.Context, ledger.LaunchFeeClaim) (ledger.InsertResult, error)
	IngestRefundCredit(context.Context, ledger.RefundCredit) (ledger.InsertResult, error)
	IngestRefundClaim(context.Context, ledger.RefundClaim) (ledger.InsertResult, error)
	IngestTransfer(context.Context, ledger.Transfer) (ledger.InsertResult, error)
	IngestPoolMint(context.Context, ledger.PoolMint) (ledger.InsertResult, error)
	IngestPoolBurn(context.Context, ledger.PoolBurn) (ledger.InsertResult, error)
	IngestPoolSwap(context.Context, ledger.PoolSwap) (ledger.InsertResult, error)
	IngestPoolSync(context.Context, ledger.PoolSync) (ledger.InsertResult, error)
	IngestLaunchPauseEvent(context.Context, ledger.LaunchPauseEvent) (ledger.InsertResult, error)
	IngestTradingPauseEvent(context.Context, ledger.TradingPauseEvent) (ledger.InsertResult, error)
	IngestEngineConfiguration(context.Context, ledger.EngineConfiguration) (ledger.InsertResult, error)
	IngestFutureDefaultsConfiguration(context.Context, ledger.FutureDefaultsConfiguration) (ledger.InsertResult, error)
	IngestFutureTreasuryConfiguration(context.Context, ledger.FutureTreasuryConfiguration) (ledger.InsertResult, error)
}

type LedgerRouter struct {
	Sink    EventSink
	ChainID int64
}

func (r LedgerRouter) Apply(ctx context.Context, u UnitOfWork, logs []chain.DecodedLog, blocks map[int64]ledger.IndexedBlock, identities []TokenIdentity) error {
	if r.Sink == nil {
		sink, ok := u.(EventSink)
		if !ok {
			return fmt.Errorf("transaction unit does not provide event sink")
		}
		r.Sink = sink
	}
	launchBlocks := make(map[[20]byte]int64)
	graduationBlocks := make(map[[20]byte]int64)
	for _, identity := range identities {
		launchBlocks[[20]byte(identity.Token)] = identity.Launch.BlockNumber
		if identity.GraduationBlock != nil {
			graduationBlocks[[20]byte(identity.Token)] = *identity.GraduationBlock
		}
	}
	for _, log := range logs {
		switch value := log.Value.(type) {
		case chain.TokenLaunched:
			launchBlocks[value.Token] = int64(log.Coordinates.BlockNumber)
		case chain.Graduated:
			graduationBlocks[value.Token] = int64(log.Coordinates.BlockNumber)
		}
	}
	for _, log := range logs {
		if trade, ok := log.Value.(chain.Trade); ok {
			if graduation, exists := graduationBlocks[trade.Token]; exists && int64(log.Coordinates.BlockNumber) > graduation {
				return fmt.Errorf("trade after graduation for token %s", trade.Token.Hex())
			}
			if launch, exists := launchBlocks[trade.Token]; exists && int64(log.Coordinates.BlockNumber) < launch {
				return fmt.Errorf("pre-launch trade for token %s", trade.Token.Hex())
			}
		}
	}
	for _, log := range logs {
		if _, err := r.apply(ctx, log, blocks); err != nil {
			return err
		}
	}
	return nil
}
func (r LedgerRouter) apply(ctx context.Context, log chain.DecodedLog, blocks map[int64]ledger.IndexedBlock) (ledger.InsertResult, error) {
	block := blocks[int64(log.Coordinates.BlockNumber)]
	c := ledger.EventCoordinates{ChainID: r.ChainID, BlockNumber: int64(log.Coordinates.BlockNumber), BlockHash: log.Coordinates.BlockHash, BlockTime: block.BlockTime, TransactionIndex: int32(log.Coordinates.TransactionIndex), TxHash: log.Coordinates.TransactionHash, LogIndex: int32(log.Coordinates.LogIndex)}
	// The chain ID is supplied by the transaction scope; block time is filled by
	// the caller's canonical block map before routing.
	switch v := log.Value.(type) {
	case chain.TokenLaunched:
		return r.Sink.IngestTokenLaunch(ctx, ledger.TokenLaunch{EventCoordinates: c, Token: v.Token, Curve: v.Curve, Creator: v.Creator, LPPair: v.LPPair, WETH: v.WETH, ProtocolTreasury: v.ProtocolTreasury, EngineVersion: v.EngineVersion, Name: v.Name, Symbol: v.Symbol, TotalSupply: cp(v.TotalSupply), VirtualETH: cp(v.VirtualETH), VirtualToken: cp(v.VirtualToken), CurveTokens: cp(v.CurveTokens), LPTokens: cp(v.LPTokens), GraduationETH: cp(v.GraduationETH), LaunchFeePaid: cp(v.LaunchFeePaid), TradeFeeBPS: v.TradeFeeBPS, ProtocolShareBPS: v.ProtocolShareBPS})
	case chain.Trade:
		return r.Sink.IngestTrade(ctx, ledger.Trade{EventCoordinates: c, Token: v.Token, Trader: v.Trader, IsBuy: v.IsBuy, ETHGross: cp(v.ETHGross), ETHRefund: cp(v.ETHRefund), TokenAmount: cp(v.TokenAmount), ProtocolFee: cp(v.ProtocolFee), CreatorFee: cp(v.CreatorFee), NewETHReserve: cp(v.NewETHReserve), NewTokenReserve: cp(v.NewTokenReserve)})
	case chain.Graduated:
		return r.Sink.IngestGraduation(ctx, ledger.Graduation{EventCoordinates: c, Token: v.Token, LPPair: v.LPPair, ETHToPool: cp(v.ETHToPool), TokensToPool: cp(v.TokensToPool), LPLiquidityBurned: cp(v.LPLiquidityBurned)})
	case chain.Transfer:
		return r.Sink.IngestTransfer(ctx, ledger.Transfer{EventCoordinates: c, Token: v.Token, From: v.From, To: v.To, Value: cp(v.Value)})
	case chain.PoolSync:
		return r.Sink.IngestPoolSync(ctx, ledger.PoolSync{EventCoordinates: c, Pair: v.Pair, Reserve0: cp(v.Reserve0), Reserve1: cp(v.Reserve1)})
	case chain.PoolSwap:
		return r.Sink.IngestPoolSwap(ctx, ledger.PoolSwap{EventCoordinates: c, Pair: v.Pair, Sender: v.Sender, Amount0In: cp(v.Amount0In), Amount1In: cp(v.Amount1In), Amount0Out: cp(v.Amount0Out), Amount1Out: cp(v.Amount1Out), To: v.To, TxFrom: v.TxFrom})
	case chain.PoolMint:
		return r.Sink.IngestPoolMint(ctx, ledger.PoolMint{EventCoordinates: c, Pair: v.Pair, Sender: v.Sender, Amount0: cp(v.Amount0), Amount1: cp(v.Amount1)})
	case chain.PoolBurn:
		return r.Sink.IngestPoolBurn(ctx, ledger.PoolBurn{EventCoordinates: c, Pair: v.Pair, Sender: v.Sender, Amount0: cp(v.Amount0), Amount1: cp(v.Amount1), To: v.To})
	case chain.CreatorFeesClaimed:
		return r.Sink.IngestCreatorFeeClaim(ctx, ledger.CreatorFeeClaim{EventCoordinates: c, Token: v.Token, Creator: v.Creator, Amount: cp(v.Amount)})
	case chain.ProtocolFeesClaimed:
		return r.Sink.IngestProtocolFeeClaim(ctx, ledger.ProtocolFeeClaim{EventCoordinates: c, Token: v.Token, Treasury: v.Treasury, Amount: cp(v.Amount)})
	case chain.RefundCredited:
		return r.Sink.IngestRefundCredit(ctx, ledger.RefundCredit{EventCoordinates: c, Token: v.Token, Account: v.Account, Amount: cp(v.Amount)})
	case chain.RefundClaimed:
		return r.Sink.IngestRefundClaim(ctx, ledger.RefundClaim{EventCoordinates: c, Token: v.Token, Account: v.Account, Amount: cp(v.Amount)})
	case chain.LaunchFeesClaimed:
		return r.Sink.IngestLaunchFeeClaim(ctx, ledger.LaunchFeeClaim{EventCoordinates: c, Treasury: v.Treasury, Amount: cp(v.Amount)})
	case chain.LaunchPauseSet:
		return r.Sink.IngestLaunchPauseEvent(ctx, ledger.LaunchPauseEvent{EventCoordinates: c, Paused: v.Paused})
	case chain.TradingPauseSet:
		return r.Sink.IngestTradingPauseEvent(ctx, ledger.TradingPauseEvent{EventCoordinates: c, Paused: v.Paused})
	case chain.EngineConfigured:
		return r.Sink.IngestEngineConfiguration(ctx, ledger.EngineConfiguration{EventCoordinates: c, EngineVersion: v.EngineVersion, Implementation: v.Implementation, Enabled: v.Enabled})
	case chain.FutureDefaultsConfigured:
		return r.Sink.IngestFutureDefaultsConfiguration(ctx, ledger.FutureDefaultsConfiguration{EventCoordinates: c, ConfigHash: v.ConfigHash})
	case chain.FutureTreasuryConfigured:
		return r.Sink.IngestFutureTreasuryConfiguration(ctx, ledger.FutureTreasuryConfiguration{EventCoordinates: c, PreviousTreasury: v.PreviousTreasury, NewTreasury: v.NewTreasury})
	default:
		return ledger.InsertResult{}, fmt.Errorf("unsupported decoded event %T", log.Value)
	}
}
func cp(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}
