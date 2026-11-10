// Package wallet holds the read-side types of the public wallet and leaderboard endpoints.
package wallet

import (
	"context"
	"errors"
	"math/big"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/Contictus/launchtap/backend/internal/trading"
	"github.com/ethereum/go-ethereum/common"
)

// ErrCursorMode reports a positions cursor issued for the other mode (open versus closed).
var ErrCursorMode = errors.New("cursor was issued for a different positions mode")

// Stamp is the read snapshot every response carries.
type Stamp struct {
	Snapshot pagination.Snapshot
	Finality string
}

// Overview is the wallet card. All PnL and value amounts are wei and signed where PnL.
type Overview struct {
	Stamp
	Address             common.Address
	RealizedPnL         *big.Int
	UnrealizedPnL       *big.Int
	PortfolioValue      *big.Int
	HeldCount           int64
	CreatedCount        int64
	TokensTraded        int64
	FirstBlock          *int64
	LastBlock           *int64
	AttributionComplete bool
	// Username is shown only for a published wallet of an unbanned user with a username.
	Username *string
}

type Position struct {
	Token         common.Address
	Name, Symbol  string
	Phase         string
	Balance       *big.Int
	RemainingCost *big.Int
	RealizedPnL   *big.Int
	CurrentValue  *big.Int
	TransferredIn *big.Int
	TradeCount    int64
	LastBlock     int64
}

type PositionsQuery struct {
	ChainID int64
	Wallet  common.Address
	Closed  bool
	Limit   int
	Cursor  *pagination.Cursor
}

// MaxOpenPositions caps the single-page open-positions list.
const MaxOpenPositions = 500

type PositionsPage struct {
	Stamp
	Items []Position
	// Truncated is set in open mode when the wallet holds more than MaxOpenPositions positions.
	Truncated  bool
	NextCursor string
}

// Trade is a market trade of the wallet together with the token it traded.
type Trade struct {
	Token        common.Address
	Name, Symbol string
	trading.Trade
}

type TradesQuery struct {
	ChainID int64
	Wallet  common.Address
	Limit   int
	Cursor  *pagination.Cursor
}

type TradesPage struct {
	Stamp
	Items      []Trade
	NextCursor string
}

type Created struct {
	Token        common.Address
	Name, Symbol string
	Phase        string
	MarketCap    *big.Int
	LaunchBlock  int64
}

type CreatedQuery struct {
	ChainID int64
	Wallet  common.Address
	Limit   int
	Cursor  *pagination.Cursor
}

type CreatedPage struct {
	Stamp
	Items      []Created
	NextCursor string
}

type Ranked struct {
	Rank         int
	Address      common.Address
	RealizedPnL  *big.Int
	TokensTraded int64
	// Username follows the same rule as Overview.Username.
	Username *string
}

// LeaderboardWindow selects the realized-PnL period a leaderboard ranks over.
type LeaderboardWindow int

const (
	// WindowAll ranks by all-time realized PnL (wallet_totals).
	WindowAll LeaderboardWindow = 0
	// Window7d and Window30d rank by realized PnL booked in legs whose block time is within the
	// last 7 or 30 days of the database clock.
	Window7d  LeaderboardWindow = 7
	Window30d LeaderboardWindow = 30
)

type LeaderboardPage struct {
	Stamp
	Items []Ranked
}

type Reader interface {
	GetWallet(ctx context.Context, chainID int64, wallet common.Address) (Overview, error)
	ListPositions(ctx context.Context, q PositionsQuery) (PositionsPage, error)
	ListTrades(ctx context.Context, q TradesQuery) (TradesPage, error)
	ListCreated(ctx context.Context, q CreatedQuery) (CreatedPage, error)
	Leaderboard(ctx context.Context, chainID int64, window LeaderboardWindow, limit int) (LeaderboardPage, error)
}
