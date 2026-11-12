package stats

import (
	"context"
	"math/big"
	"time"

	"github.com/Contictus/launchtap/backend/internal/pagination"
)

type Protocol struct {
	Volume24hETH, VolumeAllTimeETH                                                             *big.Int
	Launches24h, LaunchesAllTime, Trades24h, TradesAllTime, Graduations24h, GraduationsAllTime int64
	UpdatedAt                                                                                  time.Time
	Snapshot                                                                                   pagination.Snapshot
	Finality                                                                                   string
}

// Daily is one canonical protocol aggregate for a UTC calendar day. All
// amounts remain integer base units so callers can safely parse them into
// arbitrary-precision values.
type Daily struct {
	Day         time.Time
	VolumeETH   *big.Int
	Launches    int64
	Trades      int64
	Graduations int64
}

type DailyQuery struct {
	From  time.Time
	To    time.Time
	Limit int
}

type DailyPage struct {
	Items    []Daily
	Snapshot pagination.Snapshot
	Finality string
}

type Reader interface {
	ReadProtocol(context.Context, int64) (Protocol, error)
}

type DailyReader interface {
	ReadProtocolDaily(context.Context, int64, DailyQuery) (DailyPage, error)
}

// RevenueSource is the ETH (wei) one revenue source credited to the protocol, per engine.
type RevenueSource struct {
	Engine1, Engine2, Total *big.Int
}

// Revenue is the protocol's earned ETH by source (docs/specs/2026-11-12-protocol-revenue-design.md).
type Revenue struct {
	LaunchFees, TradeFees, SnipeTax, GraduationRemainder, LPFees RevenueSource
	Total                                                        *big.Int
	Snapshot                                                     pagination.Snapshot
	Finality                                                     string
}

type RevenueReader interface {
	ReadProtocolRevenue(context.Context, int64) (Revenue, error)
}
