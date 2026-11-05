// Package watchlist defines the per-user token watchlist port. A watchlist is owned by a
// verified Privy DID within one chain and deployment.
package watchlist

import (
	"context"
	"errors"
	"time"

	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/ethereum/go-ethereum/common"
)

// MaxEntries is the most tokens one owner may watch.
const MaxEntries = 200

var (
	ErrFull         = errors.New("watchlist full")
	ErrUnknownToken = errors.New("unknown token")
)

// Owner identifies one user's watchlist. PrivyDID must come from a verified token.
type Owner struct {
	ChainID      int64
	DeploymentID string
	PrivyDID     string
}

// Entry is one watched token. Summary is nil when the token row no longer exists.
type Entry struct {
	Token   common.Address
	AddedAt time.Time
	Summary *token.Summary
}

type Store interface {
	// Add is idempotent. It returns ErrUnknownToken or ErrFull when it cannot add.
	Add(ctx context.Context, o Owner, t common.Address) error
	// Remove is idempotent.
	Remove(ctx context.Context, o Owner, t common.Address) error
	// List returns entries newest first, at most MaxEntries.
	List(ctx context.Context, o Owner) ([]Entry, error)
}
