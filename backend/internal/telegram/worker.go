package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	// DefaultInterval is the delivery pass period.
	DefaultInterval = 5 * time.Second
	// DefaultPace spaces messages to stay near 25 per second overall.
	DefaultPace = 40 * time.Millisecond
	pollTimeout = 25 * time.Second
	pollBackoff = 5 * time.Second
)

// Sender sends one plain-text message.
type Sender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

// Poller long-polls bot updates.
type Poller interface {
	GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error)
}

// Worker binds chats from bot commands and delivers pending events. Run it in one process per
// deployment; cmd/telegram enforces that with an advisory lock. Logs never carry chat ids or DIDs.
type Worker struct {
	Store        DeliveryStore
	Bot          Sender
	ChainID      int64
	DeploymentID string
	// WebOrigin prefixes the token links in alerts and the help text.
	WebOrigin string
	Logger    *slog.Logger
	Interval  time.Duration
	Pace      time.Duration
	// Sleep waits d or until ctx ends; tests replace it.
	Sleep func(ctx context.Context, d time.Duration) error
}

func (w *Worker) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

func (w *Worker) sleep(ctx context.Context, d time.Duration) error {
	if w.Sleep != nil {
		return w.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// DeliverOnce runs one delivery pass. A 429 ends the pass after waiting retry_after; a 403 (bot
// blocked or chat gone) unlinks the chat; any other send failure leaves that link's cursor for
// the next pass. Store failures end the pass with an error.
func (w *Worker) DeliverOnce(ctx context.Context) error {
	links, err := w.Store.Links(ctx, w.ChainID, w.DeploymentID)
	if err != nil {
		return fmt.Errorf("list telegram links: %w", err)
	}
	pace := w.Pace
	if pace == 0 {
		pace = DefaultPace
	}
	for _, link := range links {
		events, err := w.Store.Pending(ctx, link, MaxEventsPerPass)
		if err != nil {
			return fmt.Errorf("list pending telegram events: %w", err)
		}
		for _, e := range events {
			err := w.Bot.SendMessage(ctx, link.ChatID, Message(e, w.WebOrigin))
			var apiErr *APIError
			switch {
			case err == nil:
			case errors.As(err, &apiErr) && apiErr.Code == 403:
				if _, err := w.Store.UnlinkChat(ctx, w.ChainID, w.DeploymentID, link.ChatID); err != nil {
					return fmt.Errorf("unlink blocked telegram chat: %w", err)
				}
				w.logger().InfoContext(ctx, "telegram chat unlinked after a refusal", "description", apiErr.Description)
			case errors.As(err, &apiErr) && apiErr.Code == 429:
				w.logger().WarnContext(ctx, "telegram rate limited", "retry_after", apiErr.RetryAfter)
				return w.sleep(ctx, max(apiErr.RetryAfter, time.Second))
			default:
				if ctx.Err() != nil {
					return ctx.Err()
				}
				w.logger().WarnContext(ctx, "telegram send failed", "error", err)
			}
			if err != nil {
				break
			}
			if err := w.Store.Advance(ctx, link, e.Position); err != nil {
				return fmt.Errorf("advance telegram cursor: %w", err)
			}
			if err := w.sleep(ctx, pace); err != nil {
				return err
			}
		}
	}
	return nil
}

// HandleUpdate answers one bot update. Only private chats are served; replies that fail are
// logged, not retried.
func (w *Worker) HandleUpdate(ctx context.Context, u Update) {
	m := u.Message
	if m == nil || m.ChatType != "private" {
		return
	}
	notifications := strings.TrimRight(w.WebOrigin, "/") + "/notifications"
	var reply string
	command, arg := ParseCommand(m.Text)
	switch {
	case command == CommandStart && arg != "":
		reply = "This link is invalid or expired. Open a new one from " + notifications + "."
		if digest, err := HashCode(arg); err == nil {
			ok, err := w.Store.ClaimCode(ctx, w.ChainID, w.DeploymentID, digest, m.ChatID)
			if err != nil {
				w.logger().ErrorContext(ctx, "telegram link failed", "error", err)
				reply = "Something went wrong. Try again in a minute."
			} else if ok {
				reply = "Connected. You will get alerts for the tokens you watch. Change them at " + notifications + ", or send /stop to disconnect."
			}
		}
	case command == CommandStop:
		ok, err := w.Store.UnlinkChat(ctx, w.ChainID, w.DeploymentID, m.ChatID)
		switch {
		case err != nil:
			w.logger().ErrorContext(ctx, "telegram unlink failed", "error", err)
			reply = "Something went wrong. Try again in a minute."
		case ok:
			reply = "Disconnected. You will get no more alerts here."
		default:
			reply = "This chat is not connected."
		}
	default:
		reply = "Connect this chat from " + notifications + " to get alerts for the tokens you watch. Send /stop to disconnect."
	}
	if err := w.Bot.SendMessage(ctx, m.ChatID, reply); err != nil && ctx.Err() == nil {
		w.logger().WarnContext(ctx, "telegram reply failed", "error", err)
	}
}

// Run polls updates and runs delivery passes until ctx ends or a store failure persists.
func (w *Worker) Run(ctx context.Context, poller Poller) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, 2)
	go func() { errs <- w.poll(ctx, poller) }()
	go func() { errs <- w.deliverLoop(ctx) }()
	err := <-errs
	cancel()
	<-errs
	return err
}

func (w *Worker) poll(ctx context.Context, poller Poller) error {
	var offset int64
	for {
		updates, err := poller.GetUpdates(ctx, offset, pollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var apiErr *APIError
			if errors.As(err, &apiErr) && apiErr.Code == 401 {
				return fmt.Errorf("telegram bot token rejected: %w", err)
			}
			w.logger().WarnContext(ctx, "telegram getUpdates failed", "error", err)
			if err := w.sleep(ctx, pollBackoff); err != nil {
				return err
			}
			continue
		}
		for _, u := range updates {
			w.HandleUpdate(ctx, u)
			offset = u.UpdateID + 1
		}
	}
}

func (w *Worker) deliverLoop(ctx context.Context) error {
	interval := w.Interval
	if interval == 0 {
		interval = DefaultInterval
	}
	for {
		if err := w.DeliverOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.logger().ErrorContext(ctx, "telegram delivery pass failed", "error", err)
		}
		if err := w.sleep(ctx, interval); err != nil {
			return err
		}
	}
}
