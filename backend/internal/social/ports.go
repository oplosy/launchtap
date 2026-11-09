package social

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

// CommentStore is the persistence port of token comments and reports.
type CommentStore interface {
	// List returns visible comments newest first. An empty cursor starts at the newest comment.
	// It returns ErrUnknownToken or ErrInvalidCursor.
	List(ctx context.Context, s Scope, token common.Address, cursor string, limit int) (CommentPage, error)
	// Eligibility lists the caller's linked wallets that may comment on the token.
	Eligibility(ctx context.Context, a Actor, token common.Address) (Eligibility, error)
	// Post stores a normalized body. It returns ErrBanned, ErrUnknownToken, ErrWalletNotLinked,
	// ErrNotHolder or *RateLimitedError.
	Post(ctx context.Context, a Actor, token, wallet common.Address, body string) (Comment, error)
	// Report is idempotent per (comment, reporter). It returns ErrBanned, ErrCommentNotFound,
	// ErrOwnComment or *RateLimitedError.
	Report(ctx context.Context, a Actor, commentID int64, reason ReportReason) error
}
