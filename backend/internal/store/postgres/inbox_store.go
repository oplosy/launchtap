package postgres

import (
	"context"
	"fmt"
	"math"

	"github.com/Contictus/launchtap/backend/internal/inbox"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InboxStore reads the derived notification inbox and keeps each account's read position.
type InboxStore struct {
	Pool *pgxpool.Pool
}

var _ inbox.Store = InboxStore{}

// List reads the page and the unread count in one repeatable-read transaction, so both see the
// same ledger and watchlist.
func (s InboxStore) List(ctx context.Context, o watchlist.Owner, before *inbox.Position, limit int) (inbox.Page, error) {
	if limit < 1 || limit > inbox.MaxPageSize {
		return inbox.Page{}, fmt.Errorf("inbox page size %d out of range", limit)
	}
	cursor := inbox.Position{Block: math.MaxInt64, TxIndex: math.MaxInt32, LogIndex: math.MaxInt32}
	if before != nil {
		cursor = *before
	}
	var page inbox.Page
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		// One extra row tells whether an older page exists.
		rows, err := q.ListInboxEvents(ctx, sqlc.ListInboxEventsParams{
			BeforeBlock: cursor.Block, BeforeTxIndex: cursor.TxIndex, BeforeLogIndex: cursor.LogIndex,
			PageSize: int32(limit + 1), ChainID: o.ChainID, DeploymentID: o.DeploymentID, PrivyDid: o.PrivyDID,
		})
		if err != nil {
			return fmt.Errorf("list inbox events: %w", err)
		}
		if len(rows) > limit {
			rows = rows[:limit]
			last := rows[limit-1]
			page.Next = &inbox.Position{Block: last.BlockNumber, TxIndex: last.TransactionIndex, LogIndex: last.LogIndex}
		}
		page.Items = make([]inbox.Event, 0, len(rows))
		for _, row := range rows {
			event, err := inboxEvent(row)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, event)
		}
		unread, err := q.CountUnreadInboxEvents(ctx, sqlc.CountUnreadInboxEventsParams{ChainID: o.ChainID, DeploymentID: o.DeploymentID, PrivyDid: o.PrivyDID})
		if err != nil {
			return fmt.Errorf("count unread inbox events: %w", err)
		}
		page.Unread = int(min(unread, int64(inbox.UnreadCap)))
		return nil
	})
	return page, err
}

// MarkRead stores through as the read position unless a newer one is already stored.
func (s InboxStore) MarkRead(ctx context.Context, o watchlist.Owner, through inbox.Position) error {
	if err := sqlc.New(s.Pool).MarkInboxRead(ctx, sqlc.MarkInboxReadParams{
		ChainID: o.ChainID, DeploymentID: o.DeploymentID, PrivyDid: o.PrivyDID,
		ReadBlock: through.Block, ReadTxIndex: through.TxIndex, ReadLogIndex: through.LogIndex,
	}); err != nil {
		return fmt.Errorf("mark inbox read: %w", err)
	}
	return nil
}

func inboxEvent(row sqlc.ListInboxEventsRow) (inbox.Event, error) {
	if len(row.TokenAddress) != common.AddressLength || len(row.TxHash) != common.HashLength || !row.BlockTime.Valid {
		return inbox.Event{}, fmt.Errorf("inbox event at block %d is malformed", row.BlockNumber)
	}
	amount, err := signedBigInt("inbox event ETH", row.EthWei)
	if err != nil {
		return inbox.Event{}, err
	}
	event := inbox.Event{
		Position: inbox.Position{Block: row.BlockNumber, TxIndex: row.TransactionIndex, LogIndex: row.LogIndex},
		Kind:     row.Kind, Token: common.BytesToAddress(row.TokenAddress), Name: row.Name, Symbol: row.Symbol,
		BlockTime: row.BlockTime.Time.UTC(), TxHash: common.BytesToHash(row.TxHash), ETHWei: amount, Source: row.Source.String,
	}
	if row.SideBuy.Valid {
		side := row.SideBuy.Bool
		event.SideBuy = &side
	}
	if len(row.TraderRaw) == common.AddressLength {
		trader := common.BytesToAddress(row.TraderRaw)
		event.Trader = &trader
	}
	return event, nil
}
