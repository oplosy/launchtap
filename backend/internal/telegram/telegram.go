// Package telegram delivers the account's notification inbox to a linked Telegram chat
// (docs/specs/2026-11-12-telegram-alerts-design.md). The API side issues one-time link codes and
// keeps settings; the cmd/telegram worker binds chats and sends safe-block inbox events in chain
// order behind a forward-only per-link cursor.
package telegram

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Contictus/launchtap/backend/internal/inbox"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
)

const (
	// CodeTTL is how long a link code stays valid.
	CodeTTL = 15 * time.Minute
	// MaxEventsPerPass caps the messages one link gets in one delivery pass.
	MaxEventsPerPass = 10
	codeBytes        = 32
	codeLength       = 43 // base64url without padding of codeBytes
	maxNameRunes     = 48
)

var (
	// MinTradeWei is the inbox floor (0.5 ETH) and the default threshold; MaxTradeWei is 10,000 ETH.
	MinTradeWei = big.NewInt(500_000_000_000_000_000)
	MaxTradeWei = new(big.Int).Mul(big.NewInt(10_000), big.NewInt(1_000_000_000_000_000_000))

	ErrInvalidCode      = errors.New("invalid telegram link code")
	ErrInvalidThreshold = errors.New("invalid large-trade threshold")
	ErrNotLinked        = errors.New("telegram not linked")
)

// NewCode returns a fresh link code (the Telegram start parameter) and the SHA-256 digest that is
// the only form stored.
func NewCode() (string, [32]byte, error) {
	raw := make([]byte, codeBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", [32]byte{}, err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	return code, sha256.Sum256([]byte(code)), nil
}

// HashCode validates a code's shape and returns its digest.
func HashCode(code string) ([32]byte, error) {
	if len(code) != codeLength {
		return [32]byte{}, ErrInvalidCode
	}
	for i := 0; i < len(code); i++ {
		c := code[i]
		if !isCodeChar(c) {
			return [32]byte{}, ErrInvalidCode
		}
	}
	return sha256.Sum256([]byte(code)), nil
}

// isCodeChar reports whether c is in the start-parameter alphabet [A-Za-z0-9_-].
func isCodeChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}

// Settings choose which inbox events a linked chat receives.
type Settings struct {
	Graduations bool
	LargeTrades bool
	// MinTradeWei is the smallest gross trade amount sent, within [MinTradeWei, MaxTradeWei].
	MinTradeWei *big.Int
}

func DefaultSettings() Settings {
	return Settings{Graduations: true, LargeTrades: true, MinTradeWei: new(big.Int).Set(MinTradeWei)}
}

func (s Settings) Validate() error {
	if s.MinTradeWei == nil || s.MinTradeWei.Cmp(MinTradeWei) < 0 || s.MinTradeWei.Cmp(MaxTradeWei) > 0 {
		return ErrInvalidThreshold
	}
	return nil
}

// Status is the account's view of its link.
type Status struct {
	Linked   bool
	LinkedAt time.Time
	Settings Settings
}

// AccountStore is the API's port: link codes, settings and unlinking for one account.
type AccountStore interface {
	// CreateLinkCode replaces the account's pending code and returns its expiry (database clock).
	CreateLinkCode(ctx context.Context, o watchlist.Owner, digest [32]byte) (time.Time, error)
	Status(ctx context.Context, o watchlist.Owner) (Status, error)
	// UpdateSettings returns ErrNotLinked when the account has no chat.
	UpdateSettings(ctx context.Context, o watchlist.Owner, s Settings) error
	// Unlink is idempotent.
	Unlink(ctx context.Context, o watchlist.Owner) error
}

// Link is one bound chat as the worker sees it.
type Link struct {
	Owner    watchlist.Owner
	ChatID   int64
	Settings Settings
	Cursor   inbox.Position
}

// DeliveryStore is the worker's port.
type DeliveryStore interface {
	// ClaimCode binds chatID to the code's account and consumes the code. It reports false for an
	// unknown, used or expired code. The new link's cursor is the current safe head.
	ClaimCode(ctx context.Context, chainID int64, deploymentID string, digest [32]byte, chatID int64) (bool, error)
	// UnlinkChat removes the chat's link and reports whether one existed.
	UnlinkChat(ctx context.Context, chainID int64, deploymentID string, chatID int64) (bool, error)
	Links(ctx context.Context, chainID int64, deploymentID string) ([]Link, error)
	// Pending returns up to limit events after l.Cursor in safe blocks that pass l.Settings, in
	// chain order.
	Pending(ctx context.Context, l Link, limit int) ([]inbox.Event, error)
	// Advance moves the cursor forward to p while the account is still bound to l.ChatID.
	Advance(ctx context.Context, l Link, p inbox.Position) error
}

// Command is a bot command a private chat sent.
type Command int

const (
	CommandHelp Command = iota
	CommandStart
	CommandStop
)

// ParseCommand reads "/start [arg]", "/stop" (optionally "@BotName"-suffixed); anything else is
// help.
func ParseCommand(text string) (Command, string) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return CommandHelp, ""
	}
	name, _, _ := strings.Cut(fields[0], "@")
	switch name {
	case "/start":
		if len(fields) > 1 {
			return CommandStart, fields[1]
		}
		return CommandStart, ""
	case "/stop":
		return CommandStop, ""
	}
	return CommandHelp, ""
}

// Message is the plain-text alert for an event. Name and symbol are user-controlled; the message is
// sent without a parse mode, so they cannot add markup.
func Message(e inbox.Event, webOrigin string) string {
	symbol := truncate(strings.TrimSpace(e.Symbol))
	name := truncate(strings.TrimSpace(e.Name))
	link := strings.TrimRight(webOrigin, "/") + "/token/" + strings.ToLower(e.Token.Hex())
	if e.Kind == inbox.KindGraduation {
		return "$" + symbol + " (" + name + ") graduated: " + formatETH(e.ETHWei) + " ETH moved to the pool\n" + link
	}
	side := "sell"
	if e.SideBuy != nil && *e.SideBuy {
		side = "buy"
	}
	venue := "the pool"
	if e.Source == "curve" {
		venue = "the curve"
	}
	line := formatETH(e.ETHWei) + " ETH on " + venue
	if e.Trader != nil {
		hex := strings.ToLower(e.Trader.Hex())
		line += " by " + hex[:6] + "…" + hex[len(hex)-4:]
	}
	return "Large " + side + " on $" + symbol + " (" + name + ")\n" + line + "\n" + link
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxNameRunes {
		return s
	}
	return string([]rune(s)[:maxNameRunes-1]) + "…"
}

// formatETH renders wei as ETH with at most four decimals, truncated.
func formatETH(wei *big.Int) string {
	if wei == nil {
		return "0"
	}
	unit := big.NewInt(1_000_000_000_000_000_000)
	whole, frac := new(big.Int).QuoRem(wei, unit, new(big.Int))
	digits := new(big.Int).Quo(frac, big.NewInt(100_000_000_000_000)).String()
	digits = strings.Repeat("0", 4-len(digits)) + digits
	digits = strings.TrimRight(digits, "0")
	if digits == "" {
		return whole.String()
	}
	return whole.String() + "." + digits
}
