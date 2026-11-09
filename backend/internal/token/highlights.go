package token

import (
	"context"
	"math/big"
	"time"

	"github.com/Contictus/launchtap/backend/internal/pagination"
)

// TrendingToken is a token ranked by recent trading activity.
type TrendingToken struct {
	Summary
	WindowVolumeETH *big.Int
	DistinctBuyers  int64
}

// KingOfTheHill is the bonding-curve token closest to graduation.
type KingOfTheHill struct {
	Summary
	GraduationProgressBPS int32
	RemainingETH          *big.Int
}

// Highlights is the discovery payload read from one consistent snapshot.
type Highlights struct {
	Trending    []TrendingToken
	King        *KingOfTheHill
	WindowStart time.Time
	Snapshot    pagination.Snapshot
	Finality    string
}

// HighlightsReader is implemented by the PostgreSQL adapter without exposing sqlc types.
type HighlightsReader interface {
	Highlights(ctx context.Context, chainID int64, windowStart time.Time, limit int) (Highlights, error)
}
