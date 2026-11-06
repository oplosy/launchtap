package feed

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Cursor is a chain position ordered by (Block, TxIndex, LogIndex). The zero value precedes every event.
type Cursor struct {
	Block    int64
	TxIndex  int32
	LogIndex int32
}

type Trade struct {
	Token        common.Address
	Name, Symbol string
	Source       string
	Trader       *common.Address
	Buy          bool
	ETHVolume    *big.Int
	TokenVolume  *big.Int
	Block        int64
	TxIndex      int32
	TxHash       common.Hash
	LogIndex     int32
	Time         time.Time
	Finality     string
}

type Graduation struct {
	Token, Pair  common.Address
	Name, Symbol string
	TxHash       common.Hash
	Block        int64
	TxIndex      int32
	LogIndex     int32
}

type Launch struct {
	Token, Creator common.Address
	Name, Symbol   string
	TxHash         common.Hash
	Block          int64
	TxIndex        int32
	LogIndex       int32
}

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
