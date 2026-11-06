package feed

import (
	"context"

	"github.com/Contictus/launchtap/backend/internal/realtime"
)

// Cursor is a chain position ordered by (Block, TxIndex, LogIndex). The zero value precedes every event.
type Cursor struct {
	Block    int64
	TxIndex  int32
	LogIndex int32
}

// Trade, Graduation and Launch live in realtime (Event embeds them, and realtime cannot import feed).
type (
	Trade      = realtime.Trade
	Graduation = realtime.Graduation
	Launch     = realtime.Launch
)

// Watermarks holds the safe and finalized block numbers; 0 when unknown.
type Watermarks struct{ Safe, Finalized int64 }

type Reader interface {
	TradesAfter(ctx context.Context, chainID int64, after Cursor, limit int) ([]Trade, error) // ascending
	RecentTrades(ctx context.Context, chainID int64, limit int) ([]Trade, error)              // descending
	GraduationsAfter(ctx context.Context, chainID int64, after Cursor, limit int) ([]Graduation, error)
	LaunchesAfter(ctx context.Context, chainID int64, after Cursor, limit int) ([]Launch, error)
	Newest(ctx context.Context, chainID int64) (trade, graduation, launch Cursor, err error)
	Watermarks(ctx context.Context, chainID int64, deploymentID string) (Watermarks, error)
}
