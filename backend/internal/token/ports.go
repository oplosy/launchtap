// Package token defines the application-owned token read contract.
package token

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/ethereum/go-ethereum/common"
)

var ErrNotFound = errors.New("token not found")

type ListQuery struct {
	ChainID int64
	Phase   string
	Search  string
	Sort    string
	Cursor  *pagination.Cursor
	Limit   int
	// Age is "all" (or empty), "24h" or "7d". Now is the request time used to derive the
	// first-page cutoff; later pages reuse the cursor's pinned cutoff.
	Age string
	Now time.Time
}

type Summary struct {
	Address      common.Address
	Name         string
	Symbol       string
	Phase        string
	LaunchTime   time.Time
	LaunchBlock  int64
	TotalSupply  *big.Int
	MarketCapETH *big.Int
	Volume24hETH *big.Int
	HolderCount  int64
}

type Page struct {
	Items      []Summary
	Snapshot   pagination.Snapshot
	NextCursor string
	Finality   string
}

// Reader is implemented by the PostgreSQL adapter without exposing sqlc types.
type Reader interface {
	List(ctx context.Context, query ListQuery) (Page, error)
	Get(ctx context.Context, chainID int64, address common.Address) (Detail, error)
}

type Detail struct {
	Summary
	Curve, Pair, WETH, Creator, ProtocolTreasury                                 common.Address
	EngineVersion                                                                uint16
	InitialVirtualETH, InitialVirtualToken, CurveTokens, LPTokens, GraduationETH *big.Int
	TradeFeeBPS, ProtocolShareBPS                                                uint16
	ReserveSource                                                                string
	ETHReserve, TokenReserve                                                     *big.Int
	RealCurveETH                                                                 *big.Int
	GraduationProgressBPS                                                        int32
	ReserveBlock                                                                 int64
	ReserveHash                                                                  common.Hash
	Description, ImageURL, XURL, TelegramURL                                     string
	SpotPriceETH, FDVETH, LiquidityETH, ATHPriceETH                              *big.Int
	ATHAt                                                                        time.Time
	PriceChange24hBPS                                                            int64
	Snapshot                                                                     pagination.Snapshot
	Finality                                                                     string
}

type QuoteState struct {
	Detail                    Detail
	ProtocolFees, CreatorFees *big.Int
}

type QuoteReader interface {
	ReadQuoteState(context.Context, int64, common.Address) (QuoteState, error)
}
