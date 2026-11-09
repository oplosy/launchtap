package social

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum/common"
)

// Scope is the chain and deployment every social row belongs to.
type Scope struct {
	ChainID      int64
	DeploymentID string
}

// Actor is a verified Privy principal within a scope. Wallets are its currently linked wallets.
type Actor struct {
	Scope
	PrivyDID string
	Wallets  []common.Address
}

// Role is the posting wallet's relation to the token.
type Role string

const (
	RoleCreator Role = "creator"
	RoleHolder  Role = "holder"
)

// ReportReason is why a comment was reported.
type ReportReason string

const (
	ReasonSpam  ReportReason = "spam"
	ReasonScam  ReportReason = "scam"
	ReasonAbuse ReportReason = "abuse"
	ReasonOther ReportReason = "other"
)

// Author is the public identity shown next to a comment. At least one of Username and Wallet is set.
type Author struct {
	Username *string
	Wallet   *common.Address
	Role     Role
}

type Comment struct {
	ID        int64
	Token     common.Address
	Body      string
	CreatedAt time.Time
	Author    Author
}

type CommentPage struct {
	Items      []Comment
	NextCursor string
	Count      int64
}

type EligibleWallet struct {
	Address common.Address
	Role    Role
}

type Eligibility struct {
	Wallets  []EligibleWallet
	Banned   bool
	Username *string
}

var (
	ErrUnknownToken    = errors.New("unknown token")
	ErrWalletNotLinked = errors.New("wallet not linked")
	ErrNotHolder       = errors.New("wallet is neither creator nor holder")
	ErrBanned          = errors.New("user banned")
	ErrCommentNotFound = errors.New("comment not found")
	ErrOwnComment      = errors.New("cannot report own comment")
	ErrInvalidCursor   = errors.New("invalid cursor")
	ErrInvalidReport   = errors.New("invalid report reason")
)

// RateLimitedError reports a rate limit; RetryAfter is at least one second.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("rate limited, retry after %s", e.RetryAfter)
}

// BodyError names the body rule that failed: empty, too_long, control, bidi, utf8 or newlines.
type BodyError struct{ Rule string }

func (e *BodyError) Error() string { return "invalid comment body: " + e.Rule }

// NormalizeBody applies the comment body rules of the spec section "Comment rules" in order:
// invalid UTF-8 is rejected; CRLF then a lone CR become LF; any control rune except LF is
// rejected (checked before the trim, so a lone tab is control rather than empty); surrounding
// whitespace is trimmed; an empty result is rejected; bidirectional override
// and isolate runes (U+202A-U+202E, U+2066-U+2069) are rejected; more than MaxNewlineRun
// consecutive LF are rejected; more than MaxBodyRunes code points are rejected.
func NormalizeBody(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", &BodyError{Rule: "utf8"}
	}
	body := strings.ReplaceAll(raw, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	for _, r := range body {
		if isControlRune(r) {
			return "", &BodyError{Rule: "control"}
		}
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "", &BodyError{Rule: "empty"}
	}
	run, runes := 0, 0
	longRun := false
	for _, r := range body {
		runes++
		if isControlRune(r) {
			return "", &BodyError{Rule: "control"}
		}
		if (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) {
			return "", &BodyError{Rule: "bidi"}
		}
		if r == '\n' {
			run++
			if run > MaxNewlineRun {
				longRun = true
			}
		} else {
			run = 0
		}
	}
	if longRun {
		return "", &BodyError{Rule: "newlines"}
	}
	if runes > MaxBodyRunes {
		return "", &BodyError{Rule: "too_long"}
	}
	return body, nil
}

// isControlRune reports runes rejected with the control rule: every control rune except LF, the
// Unicode line and paragraph separators, the invisible directional marks (LRM, RLM, ALM) and the
// zero-width space and byte order mark. ZWNJ and ZWJ stay allowed (emoji and some scripts need them).
func isControlRune(r rune) bool {
	switch r {
	case 0x0A, 0x200C, 0x200D: // LF, ZWNJ, ZWJ
		return false
	case 0x2028, 0x2029, 0x200E, 0x200F, 0x061C, 0x200B, 0xFEFF: // LS, PS, LRM, RLM, ALM, ZWSP, BOM
		return true
	}
	return unicode.IsControl(r)
}

// ParseReportReason accepts exactly the lowercase reasons.
func ParseReportReason(s string) (ReportReason, error) {
	switch r := ReportReason(s); r {
	case ReasonSpam, ReasonScam, ReasonAbuse, ReasonOther:
		return r, nil
	}
	return "", ErrInvalidReport
}

const cursorPrefix = "c1:"

// EncodeCursor returns the opaque cursor that resumes below comment id.
func EncodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorPrefix + strconv.FormatInt(id, 10)))
}

// DecodeCursor parses a cursor made by EncodeCursor. An empty string means "first page" to the
// caller, which tests for it before decoding; it is rejected here.
func DecodeCursor(s string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || !strings.HasPrefix(string(raw), cursorPrefix) {
		return 0, ErrInvalidCursor
	}
	digits := strings.TrimPrefix(string(raw), cursorPrefix)
	if digits == "" || digits[0] < '0' || digits[0] > '9' {
		return 0, ErrInvalidCursor
	}
	id, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalidCursor
	}
	return id, nil
}

// DisplayAuthor is the read-time author rule (R2) that the ListTokenComments query implements in
// SQL: the username unless the author is banned; the posting wallet when there is no visible
// username or the wallet is published. At least one of the two is always present.
func DisplayAuthor(username *string, banned bool, wallet common.Address, published bool, role Role) Author {
	a := Author{Role: role}
	if username != nil && !banned {
		name := *username
		a.Username = &name
	}
	if a.Username == nil || published {
		w := wallet
		a.Wallet = &w
	}
	return a
}
