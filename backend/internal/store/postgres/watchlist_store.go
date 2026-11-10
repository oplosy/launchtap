package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WatchlistStore keeps per-user watchlists in watchlist_entries.
type WatchlistStore struct {
	Pool *pgxpool.Pool
}

var (
	_ watchlist.Store          = WatchlistStore{}
	_ watchlist.WatcherCounter = WatchlistStore{}
)

// CountWatchers counts the watchlist entries for token t in one chain and deployment.
func (s WatchlistStore) CountWatchers(ctx context.Context, chainID int64, deploymentID string, t common.Address) (int64, error) {
	n, err := sqlc.New(s.Pool).CountTokenWatchers(ctx, sqlc.CountTokenWatchersParams{ChainID: chainID, DeploymentID: deploymentID, TokenAddress: sqlc.Address(t)})
	if err != nil {
		return 0, fmt.Errorf("count token watchers: %w", err)
	}
	return n, nil
}

// ownerLockKey is the advisory-lock key for one owner. The 0x1f separator cannot appear in a
// numeric chain id, so distinct owners never share a key by concatenation.
func ownerLockKey(o watchlist.Owner) string {
	return strconv.FormatInt(o.ChainID, 10) + "\x1f" + o.DeploymentID + "\x1f" + o.PrivyDID
}

// Add inserts the entry under a per-owner advisory lock so the cap check and the insert are
// serialized. Re-adding an existing entry succeeds without counting against the cap.
func (s WatchlistStore) Add(ctx context.Context, o watchlist.Owner, t common.Address) error {
	return WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *Adapter) error {
		q := adapter.queries
		if err := q.LockWatchlistOwner(ctx, ownerLockKey(o)); err != nil {
			return fmt.Errorf("lock watchlist owner: %w", err)
		}
		exists, err := q.TokenExists(ctx, sqlc.TokenExistsParams{ChainID: o.ChainID, TokenAddress: sqlc.Address(t)})
		if err != nil {
			return fmt.Errorf("check watchlist token: %w", err)
		}
		if !exists {
			return watchlist.ErrUnknownToken
		}
		rows, err := q.InsertWatchlistEntry(ctx, sqlc.InsertWatchlistEntryParams{ChainID: o.ChainID, DeploymentID: o.DeploymentID, PrivyDid: o.PrivyDID, TokenAddress: sqlc.Address(t)})
		if err != nil {
			return fmt.Errorf("insert watchlist entry: %w", err)
		}
		if rows == 0 {
			return nil
		}
		count, err := q.CountWatchlist(ctx, sqlc.CountWatchlistParams{ChainID: o.ChainID, DeploymentID: o.DeploymentID, PrivyDid: o.PrivyDID})
		if err != nil {
			return fmt.Errorf("count watchlist: %w", err)
		}
		if count > watchlist.MaxEntries {
			return watchlist.ErrFull
		}
		return nil
	})
}

func (s WatchlistStore) Remove(ctx context.Context, o watchlist.Owner, t common.Address) error {
	_, err := sqlc.New(s.Pool).DeleteWatchlistEntry(ctx, sqlc.DeleteWatchlistEntryParams{ChainID: o.ChainID, DeploymentID: o.DeploymentID, PrivyDid: o.PrivyDID, TokenAddress: sqlc.Address(t)})
	if err != nil {
		return fmt.Errorf("delete watchlist entry: %w", err)
	}
	return nil
}

func (s WatchlistStore) List(ctx context.Context, o watchlist.Owner) ([]watchlist.Entry, error) {
	rows, err := sqlc.New(s.Pool).ListWatchlist(ctx, sqlc.ListWatchlistParams{ChainID: o.ChainID, DeploymentID: o.DeploymentID, PrivyDid: o.PrivyDID, RowLimit: watchlist.MaxEntries})
	if err != nil {
		return nil, fmt.Errorf("list watchlist: %w", err)
	}
	out := make([]watchlist.Entry, 0, len(rows))
	for _, row := range rows {
		if !row.CreatedAt.Valid {
			return nil, errors.New("watchlist entry has invalid created_at")
		}
		entry := watchlist.Entry{Token: common.Address(row.WatchedTokenAddress), AddedAt: row.CreatedAt.Time}
		if row.TokenAvailable {
			if !row.Name.Valid || !row.Symbol.Valid || !row.Phase.Valid || !row.LaunchBlockNumber.Valid || !row.LaunchBlockTime.Valid || !row.TokenTotalSupply.Valid {
				return nil, errors.New("watchlist token row is incomplete")
			}
			supply, err := numericBigExact(row.TokenTotalSupply)
			if err != nil {
				return nil, fmt.Errorf("watchlist token supply: %w", err)
			}
			entry.Summary = &token.Summary{
				Address: entry.Token, Name: row.Name.String, Symbol: row.Symbol.String, Phase: row.Phase.String,
				LaunchTime: row.LaunchBlockTime.Time, LaunchBlock: row.LaunchBlockNumber.Int64,
				TotalSupply: supply, MarketCapETH: row.MarketCapEthWad.BigInt(),
				Volume24hETH: row.Volume24hEthWad.BigInt(), HolderCount: row.HolderCount,
			}
		}
		out = append(out, entry)
	}
	return out, nil
}
