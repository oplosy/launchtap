package ledger

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Engine v2 ledger events. Events whose ABI is unchanged from v1 (Transfer, protocol and launch
// fee claims, refunds, pauses, configuration) reuse the v1 types.

// TokenLaunchV2 has no pair or WETH: the graduated pool is a native-ETH Uniswap V4 pool keyed by
// the hook.
type TokenLaunchV2 struct {
	EventCoordinates
	Token, Curve, Creator, ProtocolTreasury             common.Address
	EngineVersion                                       uint16
	Name, Symbol                                        string
	TotalSupply, VirtualETH, VirtualToken               *big.Int
	CurveTokens, LPTokens, GraduationETH, LaunchFeePaid *big.Int
	TradeFeeBPS, ProtocolShareBPS                       uint16
	Hook, Locker                                        common.Address
	AntiSnipeStartBPS                                   uint16
	AntiSnipeDurationSeconds                            uint32
	LPCreatorShareBPS                                   uint16
}

// TradeV2 is a v1 trade plus the anti-snipe tax, which ETHGross includes.
type TradeV2 struct {
	Trade
	SnipeTax *big.Int
}

// GraduationV2 records the V4 pool and the locked full-range position.
type GraduationV2 struct {
	EventCoordinates
	Token                                    common.Address
	PoolID                                   common.Hash
	PositionTokenID, ETHToPool, TokensToPool *big.Int
	Liquidity, SqrtPriceX96                  *big.Int
	ETHRemainder, TokenRemainder             *big.Int
}

type SnipeTaxClaim struct {
	EventCoordinates
	Token, Treasury common.Address
	Amount          *big.Int
}

type FeeRecipientProposal struct {
	EventCoordinates
	Token, Current, Next common.Address
}

type FeeRecipientAcceptance struct {
	EventCoordinates
	Token, Previous, Next common.Address
}

type LpFeeCollection struct {
	EventCoordinates
	Token                  common.Address
	ETHAmount, TokenAmount *big.Int
}

type LpFeeClaim struct {
	EventCoordinates
	Token, Recipient       common.Address
	ETHAmount, TokenAmount *big.Int
}

type V4PoolInitialization struct {
	EventCoordinates
	PoolID               common.Hash
	Currency0, Currency1 common.Address
	Fee                  uint32
	TickSpacing          int32
	Hooks                common.Address
	SqrtPriceX96         *big.Int
	Tick                 int32
}

// V4LiquidityChange carries a signed liquidity delta.
type V4LiquidityChange struct {
	EventCoordinates
	PoolID               common.Hash
	Sender               common.Address
	TickLower, TickUpper int32
	LiquidityDelta       *big.Int
	Salt                 common.Hash
}

// V4Swap carries the swapper's signed deltas. Sender is the router, never the trader; TxFrom is
// the transaction sender, nil while unknown.
type V4Swap struct {
	EventCoordinates
	PoolID                  common.Hash
	Sender                  common.Address
	Amount0, Amount1        *big.Int
	SqrtPriceX96, Liquidity *big.Int
	Tick                    int32
	Fee                     uint32
	TxFrom                  *common.Address
}
