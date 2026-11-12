package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/Contictus/launchtap/backend/internal/inbox"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/Contictus/launchtap/backend/internal/telegram"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TelegramStore keeps Telegram links, link codes and delivery cursors
// (docs/specs/2026-11-12-telegram-alerts-design.md).
type TelegramStore struct {
	Pool *pgxpool.Pool
}

var (
	_ telegram.AccountStore  = TelegramStore{}
	_ telegram.DeliveryStore = TelegramStore{}
)

func (s TelegramStore) CreateLinkCode(ctx context.Context, o watchlist.Owner, digest [32]byte) (time.Time, error) {
	expires, err := sqlc.New(s.Pool).UpsertTelegramLinkCode(ctx, sqlc.UpsertTelegramLinkCodeParams{
		ChainID: o.ChainID, DeploymentID: o.DeploymentID, PrivyDid: o.PrivyDID, CodeSha256: sqlc.Hash(digest),
		TtlSeconds: int32(telegram.CodeTTL / time.Second),
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("store telegram link code: %w", err)
	}
	return expires.Time.UTC(), nil
}

func (s TelegramStore) Status(ctx context.Context, o watchlist.Owner) (telegram.Status, error) {
	row, err := sqlc.New(s.Pool).GetTelegramLink(ctx, sqlc.GetTelegramLinkParams{ChainID: o.ChainID, DeploymentID: o.DeploymentID, PrivyDid: o.PrivyDID})
	if errors.Is(err, pgx.ErrNoRows) {
		return telegram.Status{Settings: telegram.DefaultSettings()}, nil
	}
	if err != nil {
		return telegram.Status{}, fmt.Errorf("read telegram link: %w", err)
	}
	settings, err := telegramSettings(row.Graduations, row.LargeTrades, row.MinTradeWeiText)
	if err != nil {
		return telegram.Status{}, err
	}
	return telegram.Status{Linked: true, LinkedAt: row.LinkedAt.Time.UTC(), Settings: settings}, nil
}

func (s TelegramStore) UpdateSettings(ctx context.Context, o watchlist.Owner, settings telegram.Settings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	n, err := sqlc.New(s.Pool).UpdateTelegramSettings(ctx, sqlc.UpdateTelegramSettingsParams{
		Graduations: settings.Graduations, LargeTrades: settings.LargeTrades, MinTradeWeiText: settings.MinTradeWei.String(),
		ChainID: o.ChainID, DeploymentID: o.DeploymentID, PrivyDid: o.PrivyDID,
	})
	if err != nil {
		return fmt.Errorf("update telegram settings: %w", err)
	}
	if n == 0 {
		return telegram.ErrNotLinked
	}
	return nil
}

// Unlink removes the link and any pending code in one transaction.
func (s TelegramStore) Unlink(ctx context.Context, o watchlist.Owner) error {
	return pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.DeleteTelegramLink(ctx, sqlc.DeleteTelegramLinkParams{ChainID: o.ChainID, DeploymentID: o.DeploymentID, PrivyDid: o.PrivyDID}); err != nil {
			return fmt.Errorf("delete telegram link: %w", err)
		}
		if err := q.DeleteTelegramLinkCode(ctx, sqlc.DeleteTelegramLinkCodeParams{ChainID: o.ChainID, DeploymentID: o.DeploymentID, PrivyDid: o.PrivyDID}); err != nil {
			return fmt.Errorf("delete telegram link code: %w", err)
		}
		return nil
	})
}

// ClaimCode consumes the code and binds the chat in one transaction: a chat bound to another
// account moves, and a re-linked account keeps its settings with a fresh cursor.
func (s TelegramStore) ClaimCode(ctx context.Context, chainID int64, deploymentID string, digest [32]byte, chatID int64) (bool, error) {
	if chatID <= 0 {
		return false, nil
	}
	claimed := false
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		code, err := q.ConsumeTelegramLinkCode(ctx, sqlc.ConsumeTelegramLinkCodeParams{ChainID: chainID, DeploymentID: deploymentID, CodeSha256: sqlc.Hash(digest)})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("consume telegram link code: %w", err)
		}
		if !code.Live {
			return nil
		}
		if err := q.DeleteTelegramChatOfOtherAccount(ctx, sqlc.DeleteTelegramChatOfOtherAccountParams{
			ChainID: chainID, DeploymentID: deploymentID, ChatID: chatID, PrivyDid: code.PrivyDid,
		}); err != nil {
			return fmt.Errorf("free telegram chat: %w", err)
		}
		if err := q.UpsertTelegramLink(ctx, sqlc.UpsertTelegramLinkParams{
			ChainID: chainID, DeploymentID: deploymentID, PrivyDid: code.PrivyDid, ChatID: chatID,
		}); err != nil {
			return fmt.Errorf("store telegram link: %w", err)
		}
		claimed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return claimed, nil
}

func (s TelegramStore) UnlinkChat(ctx context.Context, chainID int64, deploymentID string, chatID int64) (bool, error) {
	n, err := sqlc.New(s.Pool).DeleteTelegramLinkByChat(ctx, sqlc.DeleteTelegramLinkByChatParams{ChainID: chainID, DeploymentID: deploymentID, ChatID: chatID})
	if err != nil {
		return false, fmt.Errorf("delete telegram chat link: %w", err)
	}
	return n > 0, nil
}

func (s TelegramStore) Links(ctx context.Context, chainID int64, deploymentID string) ([]telegram.Link, error) {
	rows, err := sqlc.New(s.Pool).ListTelegramLinks(ctx, sqlc.ListTelegramLinksParams{ChainID: chainID, DeploymentID: deploymentID})
	if err != nil {
		return nil, fmt.Errorf("list telegram links: %w", err)
	}
	links := make([]telegram.Link, 0, len(rows))
	for _, row := range rows {
		settings, err := telegramSettings(row.Graduations, row.LargeTrades, row.MinTradeWeiText)
		if err != nil {
			return nil, err
		}
		links = append(links, telegram.Link{
			Owner:    watchlist.Owner{ChainID: chainID, DeploymentID: deploymentID, PrivyDID: row.PrivyDid},
			ChatID:   row.ChatID,
			Settings: settings,
			Cursor:   inbox.Position{Block: row.DeliveredBlock, TxIndex: row.DeliveredTxIndex, LogIndex: row.DeliveredLogIndex},
		})
	}
	return links, nil
}

func (s TelegramStore) Pending(ctx context.Context, l telegram.Link, limit int) ([]inbox.Event, error) {
	if limit < 1 || limit > telegram.MaxEventsPerPass {
		return nil, fmt.Errorf("telegram page size %d out of range", limit)
	}
	if err := l.Settings.Validate(); err != nil {
		return nil, err
	}
	rows, err := sqlc.New(s.Pool).ListTelegramPendingEvents(ctx, sqlc.ListTelegramPendingEventsParams{
		ChainID: l.Owner.ChainID, DeploymentID: l.Owner.DeploymentID, PrivyDid: l.Owner.PrivyDID,
		AfterBlock: l.Cursor.Block, AfterTxIndex: l.Cursor.TxIndex, AfterLogIndex: l.Cursor.LogIndex,
		Graduations: l.Settings.Graduations, LargeTrades: l.Settings.LargeTrades,
		MinTradeWeiText: l.Settings.MinTradeWei.String(), PageSize: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list pending telegram events: %w", err)
	}
	events := make([]inbox.Event, 0, len(rows))
	for _, row := range rows {
		event, err := inboxEvent(sqlc.ListInboxEventsRow(row))
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (s TelegramStore) Advance(ctx context.Context, l telegram.Link, p inbox.Position) error {
	if err := sqlc.New(s.Pool).AdvanceTelegramCursor(ctx, sqlc.AdvanceTelegramCursorParams{
		Block: p.Block, TxIndex: p.TxIndex, LogIndex: p.LogIndex,
		ChainID: l.Owner.ChainID, DeploymentID: l.Owner.DeploymentID, PrivyDid: l.Owner.PrivyDID, ChatID: l.ChatID,
	}); err != nil {
		return fmt.Errorf("advance telegram cursor: %w", err)
	}
	return nil
}

func telegramSettings(graduations, largeTrades bool, minTradeWei string) (telegram.Settings, error) {
	threshold, ok := new(big.Int).SetString(minTradeWei, 10)
	if !ok {
		return telegram.Settings{}, fmt.Errorf("telegram threshold %q is not an integer", minTradeWei)
	}
	return telegram.Settings{Graduations: graduations, LargeTrades: largeTrades, MinTradeWei: threshold}, nil
}
