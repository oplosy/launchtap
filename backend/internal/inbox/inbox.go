// Package inbox defines the per-account notification inbox port (spec P0-C4). The inbox is
// derived at read time from the canonical ledger and the account's watchlist; only a forward-only
// read position is stored.
package inbox

import (
	"context"
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/ethereum/go-ethereum/common"
)

const (
	// MaxPageSize and DefaultPageSize bound GET /me/notifications.
	MaxPageSize     = 50
	DefaultPageSize = 20
	// UnreadCap is the most unread events counted; the API reports at most this many.
	UnreadCap = 100
)

// Kind values.
const (
	KindGraduation = "graduation"
	KindLargeTrade = "large_trade"
)

// ErrInvalidID means a notification id or cursor is not "<block>-<tx_index>-<log_index>".
var ErrInvalidID = errors.New("invalid notification id")

// Position orders events on chain; it is also the event id and the pagination cursor.
type Position struct {
	Block    int64
	TxIndex  int32
	LogIndex int32
}

func (p Position) String() string {
	return strconv.FormatInt(p.Block, 10) + "-" + strconv.FormatInt(int64(p.TxIndex), 10) + "-" + strconv.FormatInt(int64(p.LogIndex), 10)
}

// ParsePosition parses an id produced by Position.String. It accepts only canonical decimal
// numbers (no sign, no leading zeros except "0") within range, so each position has one id.
func ParsePosition(raw string) (Position, error) {
	parts := strings.Split(raw, "-")
	if len(parts) != 3 {
		return Position{}, ErrInvalidID
	}
	var values [3]int64
	for i, part := range parts {
		if part == "" || len(part) > 19 || (len(part) > 1 && part[0] == '0') {
			return Position{}, ErrInvalidID
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return Position{}, ErrInvalidID
			}
		}
		v, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return Position{}, ErrInvalidID
		}
		values[i] = v
	}
	if values[1] > math.MaxInt32 || values[2] > math.MaxInt32 {
		return Position{}, ErrInvalidID
	}
	return Position{Block: values[0], TxIndex: int32(values[1]), LogIndex: int32(values[2])}, nil
}

// Event is one inbox entry. Name and Symbol are user-controlled text.
type Event struct {
	Position  Position
	Kind      string
	Token     common.Address
	Name      string
	Symbol    string
	BlockTime time.Time
	TxHash    common.Hash
	// ETHWei is the gross trade amount (large_trade) or the ETH moved to the pool (graduation).
	ETHWei *big.Int
	// SideBuy, Trader and Source are set for large_trade only; Trader is nil for a pool swap
	// whose sender is not yet known.
	SideBuy *bool
	Trader  *common.Address
	Source  string
}

// Page is a newest-first slice of the inbox. Next is set when older events may exist.
type Page struct {
	Items  []Event
	Next   *Position
	Unread int
}

type Store interface {
	// List returns up to limit events strictly older than before (nil: newest), and the unread
	// count (at most UnreadCap).
	List(ctx context.Context, o watchlist.Owner, before *Position, limit int) (Page, error)
	// MarkRead moves the read position to through when it is newer than the stored one.
	MarkRead(ctx context.Context, o watchlist.Owner, through Position) error
}
