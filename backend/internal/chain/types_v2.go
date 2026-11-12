package chain

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Engine v2 decoded events. Events whose ABI is unchanged from v1 (protocol and launch fee
// claims, refunds, pauses, configuration, Transfer) decode into the v1 types.

// TokenLaunchedV2 drops v1's lpPair and weth (the pool uses native ETH) and appends the
// graduation hook, the LP fee locker, the anti-snipe profile and the LP creator share.
type TokenLaunchedV2 struct {
	Token, Curve, Creator, ProtocolTreasury                      common.Address
	EngineVersion                                                uint16
	Name, Symbol                                                 string
	TotalSupply, VirtualETH, VirtualToken, CurveTokens, LPTokens *big.Int
	GraduationETH, LaunchFeePaid                                 *big.Int
	TradeFeeBPS, ProtocolShareBPS                                uint16
	Hook, Locker                                                 common.Address
	AntiSnipeStartBPS                                            uint16
	AntiSnipeDurationSeconds                                     uint32
	LPCreatorShareBPS                                            uint16
}

// TradeV2 is v1's Trade plus the anti-snipe tax; ETHGross includes SnipeTax.
type TradeV2 struct {
	Token, Trader                                             common.Address
	IsBuy                                                     bool
	ETHGross, ETHRefund, TokenAmount, ProtocolFee, CreatorFee *big.Int
	NewETHReserve, NewTokenReserve                            *big.Int
	SnipeTax                                                  *big.Int
}

// GraduatedV2 reports the guarded V4 pool and the locked full-range position.
type GraduatedV2 struct {
	Token                                    common.Address
	PoolID                                   common.Hash
	PositionTokenID, ETHToPool, TokensToPool *big.Int
	Liquidity, SqrtPriceX96                  *big.Int
	ETHRemainder, TokenRemainder             *big.Int
}

// CreatorFeesClaimedV2 names the claiming fee recipient, which may differ from the creator
// after a recipient change.
type CreatorFeesClaimedV2 struct {
	Token, Account common.Address
	Amount         *big.Int
}

type SnipeTaxClaimed struct {
	Token, Treasury common.Address
	Amount          *big.Int
}

type FeeRecipientProposed struct{ Token, Current, Next common.Address }

type FeeRecipientAccepted struct{ Token, Previous, Next common.Address }

type LpFeesCollected struct {
	Token                  common.Address
	ETHAmount, TokenAmount *big.Int
}

type LpFeesClaimed struct {
	Token, Recipient       common.Address
	ETHAmount, TokenAmount *big.Int
}

// V4Initialize is the singleton PoolManager's pool creation event.
type V4Initialize struct {
	PoolID               common.Hash
	Currency0, Currency1 common.Address
	Fee                  uint32
	TickSpacing          int32
	Hooks                common.Address
	SqrtPriceX96         *big.Int
	Tick                 int32
}

// V4ModifyLiquidity reports a signed liquidity change of one position in a pool.
type V4ModifyLiquidity struct {
	PoolID               common.Hash
	Sender               common.Address
	TickLower, TickUpper int32
	LiquidityDelta       *big.Int
	Salt                 common.Hash
}

// V4Swap carries the swapper's signed deltas: a negative amount is paid into the pool. Sender
// is the router, not the trader.
type V4Swap struct {
	PoolID                  common.Hash
	Sender                  common.Address
	Amount0, Amount1        *big.Int
	SqrtPriceX96, Liquidity *big.Int
	Tick                    int32
	Fee                     uint32
	// TxFrom is the transaction sender. It is not ABI-decoded; the indexer engine fills it
	// from eth_getTransactionByHash and leaves it nil when the lookup is unavailable.
	TxFrom *common.Address
}
