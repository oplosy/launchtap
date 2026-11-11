package indexer

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/ethereum/go-ethereum/common"
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

	// Engine v2. Like the v1 ingests, each is idempotent: replaying an identical event inserts
	// nothing, and a different event at the same coordinates is an invariant conflict.
	IngestTokenLaunchV2(context.Context, ledger.TokenLaunchV2) (ledger.InsertResult, error)
	IngestTradeV2(context.Context, ledger.TradeV2) (ledger.InsertResult, error)
	IngestGraduationV2(context.Context, ledger.GraduationV2) (ledger.InsertResult, error)
	IngestSnipeTaxClaim(context.Context, ledger.SnipeTaxClaim) (ledger.InsertResult, error)
	IngestFeeRecipientProposal(context.Context, ledger.FeeRecipientProposal) (ledger.InsertResult, error)
	IngestFeeRecipientAcceptance(context.Context, ledger.FeeRecipientAcceptance) (ledger.InsertResult, error)
	IngestLpFeeCollection(context.Context, ledger.LpFeeCollection) (ledger.InsertResult, error)
	IngestLpFeeClaim(context.Context, ledger.LpFeeClaim) (ledger.InsertResult, error)
	IngestV4PoolInitialization(context.Context, ledger.V4PoolInitialization) (ledger.InsertResult, error)
	IngestV4LiquidityChange(context.Context, ledger.V4LiquidityChange) (ledger.InsertResult, error)
	IngestV4Swap(context.Context, ledger.V4Swap) (ledger.InsertResult, error)
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
	if err := checkOrdering(logs, identities); err != nil {
		return err
	}
	for _, log := range logs {
		if _, err := r.apply(ctx, log, blocks); err != nil {
			return err
		}
	}
	return nil
}

// logPosition orders logs canonically: block, transaction index, log index.
type logPosition struct{ block, tx, log uint64 }

func positionOf(c chain.LogCoordinates) logPosition {
	return logPosition{block: c.BlockNumber, tx: uint64(c.TransactionIndex), log: uint64(c.LogIndex)}
}

func (p logPosition) before(other logPosition) bool {
	if p.block != other.block {
		return p.block < other.block
	}
	if p.tx != other.tx {
		return p.tx < other.tx
	}
	return p.log < other.log
}

// checkOrdering rejects a chunk that contradicts the launch lifecycle before anything is
// written: a curve trade before its launch block or after its graduation block, and a V4 swap
// on a pool that has not graduated by that log. A V4 pool's Initialize and opening
// ModifyLiquidity precede Graduated inside the graduation transaction, so only swaps are held
// to the graduation position.
func checkOrdering(logs []chain.DecodedLog, identities []TokenIdentity) error {
	launchBlocks := make(map[[20]byte]int64)
	graduationBlocks := make(map[[20]byte]int64)
	persistedPools := make(map[[32]byte]struct{})
	for _, identity := range identities {
		launchBlocks[[20]byte(identity.Token)] = identity.Launch.BlockNumber
		if identity.GraduationBlock != nil {
			graduationBlocks[[20]byte(identity.Token)] = *identity.GraduationBlock
		}
		if identity.PoolID != nil {
			persistedPools[*identity.PoolID] = struct{}{}
		}
	}
	graduatedPools := make(map[[32]byte]logPosition)
	for _, log := range logs {
		switch value := log.Value.(type) {
		case chain.TokenLaunched:
			launchBlocks[value.Token] = int64(log.Coordinates.BlockNumber)
		case chain.TokenLaunchedV2:
			launchBlocks[value.Token] = int64(log.Coordinates.BlockNumber)
		case chain.Graduated:
			graduationBlocks[value.Token] = int64(log.Coordinates.BlockNumber)
		case chain.GraduatedV2:
			graduationBlocks[value.Token] = int64(log.Coordinates.BlockNumber)
			graduatedPools[value.PoolID] = positionOf(log.Coordinates)
		}
	}
	for _, log := range logs {
		var token [20]byte
		switch value := log.Value.(type) {
		case chain.Trade:
			token = value.Token
		case chain.TradeV2:
			token = value.Token
		case chain.V4Swap:
			if _, persisted := persistedPools[value.PoolID]; persisted {
				continue
			}
			if graduation, ok := graduatedPools[value.PoolID]; !ok || !graduation.before(positionOf(log.Coordinates)) {
				return fmt.Errorf("V4 swap before graduation for pool %s at %s", value.PoolID.Hex(), log.Coordinates)
			}
			continue
		default:
			continue
		}
		if graduation, exists := graduationBlocks[token]; exists && int64(log.Coordinates.BlockNumber) > graduation {
			return fmt.Errorf("trade after graduation for token %s", common.Address(token).Hex())
		}
		if launch, exists := launchBlocks[token]; exists && int64(log.Coordinates.BlockNumber) < launch {
			return fmt.Errorf("pre-launch trade for token %s", common.Address(token).Hex())
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
	case chain.TokenLaunchedV2:
		return r.Sink.IngestTokenLaunchV2(ctx, ledger.TokenLaunchV2{EventCoordinates: c, Token: v.Token, Curve: v.Curve, Creator: v.Creator, ProtocolTreasury: v.ProtocolTreasury, EngineVersion: v.EngineVersion, Name: v.Name, Symbol: v.Symbol, TotalSupply: cp(v.TotalSupply), VirtualETH: cp(v.VirtualETH), VirtualToken: cp(v.VirtualToken), CurveTokens: cp(v.CurveTokens), LPTokens: cp(v.LPTokens), GraduationETH: cp(v.GraduationETH), LaunchFeePaid: cp(v.LaunchFeePaid), TradeFeeBPS: v.TradeFeeBPS, ProtocolShareBPS: v.ProtocolShareBPS, Hook: v.Hook, Locker: v.Locker, AntiSnipeStartBPS: v.AntiSnipeStartBPS, AntiSnipeDurationSeconds: v.AntiSnipeDurationSeconds, LPCreatorShareBPS: v.LPCreatorShareBPS})
	case chain.TradeV2:
		return r.Sink.IngestTradeV2(ctx, ledger.TradeV2{Trade: ledger.Trade{EventCoordinates: c, Token: v.Token, Trader: v.Trader, IsBuy: v.IsBuy, ETHGross: cp(v.ETHGross), ETHRefund: cp(v.ETHRefund), TokenAmount: cp(v.TokenAmount), ProtocolFee: cp(v.ProtocolFee), CreatorFee: cp(v.CreatorFee), NewETHReserve: cp(v.NewETHReserve), NewTokenReserve: cp(v.NewTokenReserve)}, SnipeTax: cp(v.SnipeTax)})
	case chain.GraduatedV2:
		return r.Sink.IngestGraduationV2(ctx, ledger.GraduationV2{EventCoordinates: c, Token: v.Token, PoolID: v.PoolID, PositionTokenID: cp(v.PositionTokenID), ETHToPool: cp(v.ETHToPool), TokensToPool: cp(v.TokensToPool), Liquidity: cp(v.Liquidity), SqrtPriceX96: cp(v.SqrtPriceX96), ETHRemainder: cp(v.ETHRemainder), TokenRemainder: cp(v.TokenRemainder)})
	case chain.CreatorFeesClaimedV2:
		// The v2 claimant is the current fee recipient; it is stored in the creator column.
		return r.Sink.IngestCreatorFeeClaim(ctx, ledger.CreatorFeeClaim{EventCoordinates: c, Token: v.Token, Creator: v.Account, Amount: cp(v.Amount)})
	case chain.SnipeTaxClaimed:
		return r.Sink.IngestSnipeTaxClaim(ctx, ledger.SnipeTaxClaim{EventCoordinates: c, Token: v.Token, Treasury: v.Treasury, Amount: cp(v.Amount)})
	case chain.FeeRecipientProposed:
		return r.Sink.IngestFeeRecipientProposal(ctx, ledger.FeeRecipientProposal{EventCoordinates: c, Token: v.Token, Current: v.Current, Next: v.Next})
	case chain.FeeRecipientAccepted:
		return r.Sink.IngestFeeRecipientAcceptance(ctx, ledger.FeeRecipientAcceptance{EventCoordinates: c, Token: v.Token, Previous: v.Previous, Next: v.Next})
	case chain.LpFeesCollected:
		return r.Sink.IngestLpFeeCollection(ctx, ledger.LpFeeCollection{EventCoordinates: c, Token: v.Token, ETHAmount: cp(v.ETHAmount), TokenAmount: cp(v.TokenAmount)})
	case chain.LpFeesClaimed:
		return r.Sink.IngestLpFeeClaim(ctx, ledger.LpFeeClaim{EventCoordinates: c, Token: v.Token, Recipient: v.Recipient, ETHAmount: cp(v.ETHAmount), TokenAmount: cp(v.TokenAmount)})
	case chain.V4Initialize:
		return r.Sink.IngestV4PoolInitialization(ctx, ledger.V4PoolInitialization{EventCoordinates: c, PoolID: v.PoolID, Currency0: v.Currency0, Currency1: v.Currency1, Fee: v.Fee, TickSpacing: v.TickSpacing, Hooks: v.Hooks, SqrtPriceX96: cp(v.SqrtPriceX96), Tick: v.Tick})
	case chain.V4ModifyLiquidity:
		return r.Sink.IngestV4LiquidityChange(ctx, ledger.V4LiquidityChange{EventCoordinates: c, PoolID: v.PoolID, Sender: v.Sender, TickLower: v.TickLower, TickUpper: v.TickUpper, LiquidityDelta: cp(v.LiquidityDelta), Salt: v.Salt})
	case chain.V4Swap:
		var txFrom *common.Address
		if v.TxFrom != nil {
			sender := *v.TxFrom
			txFrom = &sender
		}
		return r.Sink.IngestV4Swap(ctx, ledger.V4Swap{EventCoordinates: c, PoolID: v.PoolID, Sender: v.Sender, Amount0: cp(v.Amount0), Amount1: cp(v.Amount1), SqrtPriceX96: cp(v.SqrtPriceX96), Liquidity: cp(v.Liquidity), Tick: v.Tick, Fee: v.Fee, TxFrom: txFrom})
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
