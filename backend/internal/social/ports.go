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
	// Post normalizes the raw body with NormalizeBody after the ban check and stores it. It
	// returns ErrBanned, *BodyError, ErrUnknownToken, ErrWalletNotLinked, ErrNotHolder or
	// *RateLimitedError.
	Post(ctx context.Context, a Actor, token, wallet common.Address, body string) (Comment, error)
	// Report is idempotent per (comment, reporter). It returns ErrBanned, ErrCommentNotFound,
	// ErrOwnComment or *RateLimitedError.
	Report(ctx context.Context, a Actor, commentID int64, reason ReportReason) error
	// Vote sets (1, -1) or clears (0) the caller's vote; value must come from ParseVote. The
	// caller needs a linked wallet that is the creator or a holder of the comment's token. It
	// returns ErrBanned, ErrCommentNotFound, ErrOwnVote, ErrNotHolder or *RateLimitedError.
	Vote(ctx context.Context, a Actor, commentID int64, value int) (VoteResult, error)
}

// Moderator is the operator-side port. Every method is one transaction that also appends a
// moderation_actions row and sends one `comment` notification per affected token. A missing
// comment or user returns ErrNotFound and writes nothing. operator and reason must satisfy
// ValidateModerationText.
type Moderator interface {
	// OpenReports lists comments with open reports, newest open report first.
	OpenReports(ctx context.Context, s Scope, limit int) ([]ReportSummary, error)
	// Hide hides the comment and resolves its open reports as actioned.
	Hide(ctx context.Context, s Scope, commentID int64, operator, reason string) error
	Unhide(ctx context.Context, s Scope, commentID int64, operator, reason string) error
	// Ban bans the DID (creating its user row) and resolves open reports on all its comments as
	// actioned. It returns the tokens the DID has commented on.
	Ban(ctx context.Context, s Scope, did string, operator, reason string) ([]common.Address, error)
	BanCommentAuthor(ctx context.Context, s Scope, commentID int64, operator, reason string) ([]common.Address, error)
	Unban(ctx context.Context, s Scope, did string, operator, reason string) ([]common.Address, error)
	// Resolve closes the comment's open reports and returns how many it closed.
	Resolve(ctx context.Context, s Scope, commentID int64, r Resolution, operator, reason string) (int64, error)
}

// ProfileStore is the persistence port of usernames and published wallets.
type ProfileStore interface {
	// Get returns the caller's profile; a caller without a user row has an empty profile.
	Get(ctx context.Context, a Actor) (PublicProfile, error)
	// Put sets the username (nil keeps the current one) and replaces the published wallets. It
	// returns ErrBanned, ErrTooManyPublicWallets, ErrWalletNotLinked, *RateLimitedError or
	// ErrUsernameTaken. username must already be normalized.
	Put(ctx context.Context, a Actor, username *string, wallets []common.Address) (PublicProfile, error)
	// Delete releases the username and unpublishes every wallet. It is never rate-limited and is
	// allowed for banned users; it counts as a username change.
	Delete(ctx context.Context, a Actor) error
}
