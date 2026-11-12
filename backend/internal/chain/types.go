// Package chain owns Ethereum RPC access and ABI decoding. It has no persistence
// or feature-package dependencies.
package chain

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type EmitterKind uint8

const (
	EmitterFactory EmitterKind = iota + 1
	EmitterCurve
	EmitterToken
	EmitterPair
	// EmitterLocker is an engine-2 deployment's LP fee locker.
	EmitterLocker
	// EmitterPoolManager is the singleton Uniswap V4 PoolManager, scoped to known pools.
	EmitterPoolManager
)

type LogCoordinates struct {
	BlockNumber      uint64
	BlockHash        common.Hash
	TransactionIndex uint
	TransactionHash  common.Hash
	LogIndex         uint
}

func (c LogCoordinates) String() string {
	return fmt.Sprintf("block=%d tx=%s tx_index=%d log_index=%d", c.BlockNumber, c.TransactionHash.Hex(), c.TransactionIndex, c.LogIndex)
}

type DecodedLog struct {
	Coordinates LogCoordinates
	Emitter     common.Address
	Kind        EmitterKind
	Name        string
	Value       any
}

type TokenLaunched struct {
	Token, Curve, Creator, LPPair, WETH, ProtocolTreasury        common.Address
	EngineVersion                                                uint16
	Name, Symbol                                                 string
	TotalSupply, VirtualETH, VirtualToken, CurveTokens, LPTokens *big.Int
	GraduationETH, LaunchFeePaid                                 *big.Int
	TradeFeeBPS, ProtocolShareBPS                                uint16
}

type Trade struct {
	Token, Trader                                             common.Address
	IsBuy                                                     bool
	ETHGross, ETHRefund, TokenAmount, ProtocolFee, CreatorFee *big.Int
	NewETHReserve, NewTokenReserve                            *big.Int
}

type Graduated struct {
	Token, LPPair                              common.Address
	ETHToPool, TokensToPool, LPLiquidityBurned *big.Int
}
type CreatorFeesClaimed struct {
	Token, Creator common.Address
	Amount         *big.Int
}
type ProtocolFeesClaimed struct {
	Token, Treasury common.Address
	Amount          *big.Int
}
type LaunchFeesClaimed struct {
	Treasury common.Address
	Amount   *big.Int
}
type RefundCredited struct {
	Token, Account common.Address
	Amount         *big.Int
}
type RefundClaimed struct {
	Token, Account common.Address
	Amount         *big.Int
}
type LaunchPauseSet struct{ Paused bool }
type TradingPauseSet struct{ Paused bool }
type EngineConfigured struct {
	EngineVersion  uint16
	Implementation common.Address
	Enabled        bool
}
type FutureDefaultsConfigured struct{ ConfigHash common.Hash }
type FutureTreasuryConfigured struct{ PreviousTreasury, NewTreasury common.Address }
type Transfer struct {
	Token, From, To common.Address
	Value           *big.Int
}
type PoolMint struct {
	Pair, Sender     common.Address
	Amount0, Amount1 *big.Int
}
type PoolBurn struct {
	Pair, Sender     common.Address
	Amount0, Amount1 *big.Int
	To               common.Address
}
type PoolSwap struct {
	Pair, Sender                                 common.Address
	Amount0In, Amount1In, Amount0Out, Amount1Out *big.Int
	To                                           common.Address
	// TxFrom is the transaction sender. It is not ABI-decoded; the indexer engine fills it
	// from eth_getTransactionByHash and leaves it nil when the lookup is unavailable.
	TxFrom *common.Address
}
type PoolSync struct {
	Pair               common.Address
	Reserve0, Reserve1 *big.Int
}

// AddressSet is copied on construction so concurrent discovery cannot observe
// caller-owned map mutation.
type AddressSet map[common.Address]struct{}

func NewAddressSet(addresses ...common.Address) AddressSet {
	result := make(AddressSet, len(addresses))
	for _, address := range addresses {
		result[address] = struct{}{}
	}
	return result
}

type Emitters struct {
	Factory common.Address
	Curves  AddressSet
	Tokens  AddressSet
	Pairs   AddressSet
	// Locker and PoolManager are set only for engine 2. PoolManager logs are accepted only for
	// a pool id in Pools (pool id -> token): the PoolManager emits for every pool on the chain.
	Locker      common.Address
	PoolManager common.Address
	Pools       map[common.Hash]common.Address
}

func (e Emitters) Kind(address common.Address) (EmitterKind, bool) {
	if address == e.Factory {
		return EmitterFactory, true
	}
	if _, ok := e.Curves[address]; ok {
		return EmitterCurve, true
	}
	if _, ok := e.Tokens[address]; ok {
		return EmitterToken, true
	}
	if _, ok := e.Pairs[address]; ok {
		return EmitterPair, true
	}
	if address == e.Locker && e.Locker != (common.Address{}) {
		return EmitterLocker, true
	}
	if address == e.PoolManager && e.PoolManager != (common.Address{}) {
		return EmitterPoolManager, true
	}
	return 0, false
}
