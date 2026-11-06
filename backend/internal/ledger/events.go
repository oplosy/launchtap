package ledger

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// EventCoordinates identify one immutable EVM log in the canonical ledger.
type EventCoordinates struct {
	ChainID          int64
	BlockNumber      int64
	BlockHash        common.Hash
	BlockTime        time.Time
	TransactionIndex int32
	TxHash           common.Hash
	LogIndex         int32
}

// IndexedBlock is a canonical block-ledger row, distinct from an event.
type IndexedBlock struct {
	ChainID        int64
	BlockNumber    int64
	BlockHash      common.Hash
	ParentHash     common.Hash
	BlockTime      time.Time
	FinalityStatus string
}

type InsertResult struct{ Inserted bool }
type UpsertResult struct{ Changed bool }

type TokenLaunch struct {
	EventCoordinates
	Token, Curve, Creator, LPPair, WETH, ProtocolTreasury common.Address
	EngineVersion                                         uint16
	Name, Symbol                                          string
	TotalSupply, VirtualETH, VirtualToken                 *big.Int
	CurveTokens, LPTokens, GraduationETH, LaunchFeePaid   *big.Int
	TradeFeeBPS, ProtocolShareBPS                         uint16
}

type Trade struct {
	EventCoordinates
	Token, Trader                                             common.Address
	IsBuy                                                     bool
	ETHGross, ETHRefund, TokenAmount, ProtocolFee, CreatorFee *big.Int
	NewETHReserve, NewTokenReserve                            *big.Int
}

type Graduation struct {
	EventCoordinates
	Token, LPPair                              common.Address
	ETHToPool, TokensToPool, LPLiquidityBurned *big.Int
}

type CreatorFeeClaim struct {
	EventCoordinates
	Token, Creator common.Address
	Amount         *big.Int
}
type ProtocolFeeClaim struct {
	EventCoordinates
	Token, Treasury common.Address
	Amount          *big.Int
}
type LaunchFeeClaim struct {
	EventCoordinates
	Treasury common.Address
	Amount   *big.Int
}
type RefundCredit struct {
	EventCoordinates
	Token, Account common.Address
	Amount         *big.Int
}
type RefundClaim struct {
	EventCoordinates
	Token, Account common.Address
	Amount         *big.Int
}
type Transfer struct {
	EventCoordinates
	Token, From, To common.Address
	Value           *big.Int
}
type PoolMint struct {
	EventCoordinates
	Pair, Sender     common.Address
	Amount0, Amount1 *big.Int
}
type PoolBurn struct {
	EventCoordinates
	Pair, Sender     common.Address
	Amount0, Amount1 *big.Int
	To               common.Address
}
type PoolSwap struct {
	EventCoordinates
	Pair, Sender                                 common.Address
	Amount0In, Amount1In, Amount0Out, Amount1Out *big.Int
	To                                           common.Address
	// TxFrom is the transaction sender, the wallet that traded. Nil means not yet known.
	TxFrom *common.Address
}
type PoolSync struct {
	EventCoordinates
	Pair               common.Address
	Reserve0, Reserve1 *big.Int
}
type LaunchPauseEvent struct {
	EventCoordinates
	Paused bool
}
type TradingPauseEvent struct {
	EventCoordinates
	Paused bool
}
type EngineConfiguration struct {
	EventCoordinates
	EngineVersion  uint16
	Implementation common.Address
	Enabled        bool
}
type FutureDefaultsConfiguration struct {
	EventCoordinates
	ConfigHash common.Hash
}
type FutureTreasuryConfiguration struct {
	EventCoordinates
	PreviousTreasury, NewTreasury common.Address
}
