package social

import (
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// ReportSummary is one reported comment with its open reports grouped.
type ReportSummary struct {
	CommentID int64
	Token     common.Address
	CreatedAt time.Time
	Body      string
	AuthorDID string
	Wallet    common.Address
	Reports   int64
	Reasons   []ReportReason
}

// Resolution closes a comment's open reports.
type Resolution string

const (
	ResolutionDismissed Resolution = "dismissed"
	ResolutionActioned  Resolution = "actioned"
)

// ParseResolution accepts exactly the lowercase resolutions.
func ParseResolution(s string) (Resolution, error) {
	switch r := Resolution(s); r {
	case ResolutionDismissed, ResolutionActioned:
		return r, nil
	}
	return "", fmt.Errorf("resolution must be %q or %q", ResolutionDismissed, ResolutionActioned)
}

// ErrNotFound is returned by a moderation action whose comment or user does not exist in scope.
var ErrNotFound = errors.New("moderation target not found")

// ValidateModerationText checks the operator name and reason that every moderation action records.
func ValidateModerationText(operator, reason string) error {
	if n := len(operator); n < 1 || n > MaxOperatorBytes {
		return fmt.Errorf("operator must be 1-%d bytes", MaxOperatorBytes)
	}
	if n := len(reason); n < 1 || n > MaxModerationReasonBytes {
		return fmt.Errorf("reason must be 1-%d bytes", MaxModerationReasonBytes)
	}
	return nil
}
