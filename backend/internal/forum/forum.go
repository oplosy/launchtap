// Package forum is the cross-token read model of token comments (spec P0-C5): the newest posts
// across all tokens and communities ranked by comment activity. Posting and moderation stay in
// package social; the forum only reads.
package forum

import (
	"context"
	"time"

	"github.com/Contictus/launchtap/backend/internal/social"
	"github.com/ethereum/go-ethereum/common"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 50
	// HotWindowHours is the community "hot" window, measured by the database clock.
	HotWindowHours = 24
)

// Sort ranks communities.
type Sort string

const (
	SortHot Sort = "hot"
	SortTop Sort = "top"
)

// ParseSort accepts exactly "hot" or "top".
func ParseSort(s string) (Sort, bool) {
	switch Sort(s) {
	case SortHot, SortTop:
		return Sort(s), true
	}
	return "", false
}

// Post is a visible comment with its token's launch name and symbol (user-controlled text).
type Post struct {
	social.Comment
	Name   string
	Symbol string
}

type PostPage struct {
	Items      []Post
	NextCursor string
}

// Community is a token ranked by visible comments in the sort's window.
type Community struct {
	Token         common.Address
	Name          string
	Symbol        string
	CommentCount  int64
	LastCommentAt time.Time
}

// Store reads the forum. Posts returns social.ErrInvalidCursor for a malformed cursor.
type Store interface {
	Posts(ctx context.Context, s social.Scope, cursor string, limit int) (PostPage, error)
	Communities(ctx context.Context, s social.Scope, sort Sort, limit int) ([]Community, error)
}
