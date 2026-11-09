// Package social holds the pure rules of the social layer: token comments, reports and
// usernames. It imports only the standard library and go-ethereum/common, holds no SQL and no
// HTTP types, and is the single home of the policy constants that the stores pass to SQL.
package social

import "time"

const (
	// MaxBodyRunes is the longest comment body, counted in code points.
	MaxBodyRunes = 500
	// MaxNewlineRun is the longest run of consecutive line feeds a body may contain.
	MaxNewlineRun = 3
	// CommentMinInterval is the minimum gap between two comments of one user.
	CommentMinInterval = 15 * time.Second
	// CommentsPerHour and ReportsPerHour cap one user within RateWindow.
	CommentsPerHour = 30
	ReportsPerHour  = 20
	// RateWindow is the rolling window of the hourly caps, measured on the database clock.
	RateWindow = time.Hour
	// UsernameChangeInterval is the wait between two username changes.
	UsernameChangeInterval = 7 * 24 * time.Hour
	// UsernameHold is how long a released username stays unavailable to other users.
	UsernameHold = 30 * 24 * time.Hour
	// MaxPublicWallets caps the wallets one user may publish next to a username.
	MaxPublicWallets = 5
	// DefaultPageSize and MaxPageSize bound comment pages.
	DefaultPageSize = 20
	MaxPageSize     = 50
)
