package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Contictus/launchtap/backend/internal/social"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProfileStore keeps usernames and published wallets. Writes run under the same per-DID advisory
// lock as comments and reports; intervals and holds use the database clock.
type ProfileStore struct {
	Pool *pgxpool.Pool
}

var _ social.ProfileStore = ProfileStore{}

const (
	usernameIntervalSeconds = float64(social.UsernameChangeInterval / time.Second)
	usernameHoldSeconds     = float64(social.UsernameHold / time.Second)
)

func (s ProfileStore) Get(ctx context.Context, a social.Actor) (social.PublicProfile, error) {
	return readProfile(ctx, sqlc.New(s.Pool), a)
}

// readProfile loads the caller's profile. A caller without a user row has an empty profile.
func readProfile(ctx context.Context, q *sqlc.Queries, a social.Actor) (social.PublicProfile, error) {
	profile := social.PublicProfile{PublicWallets: []common.Address{}}
	state, err := q.GetProfileState(ctx, sqlc.GetProfileStateParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID})
	if errors.Is(err, pgx.ErrNoRows) {
		return profile, nil
	}
	if err != nil {
		return social.PublicProfile{}, fmt.Errorf("get profile: %w", err)
	}
	if state.Username.Valid {
		name := state.Username.String
		profile.Username = &name
	}
	if state.UsernameChangedAt.Valid {
		changed := state.UsernameChangedAt.Time.UTC()
		profile.UsernameChangedAt = &changed
		if next := changed.Add(social.UsernameChangeInterval); state.DbNow.Valid && next.After(state.DbNow.Time) {
			profile.NextChangeAt = &next
		}
	}
	profile.Banned = state.BannedAt.Valid
	rows, err := q.ListOwnPublicWallets(ctx, sqlc.ListOwnPublicWalletsParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID})
	if err != nil {
		return social.PublicProfile{}, fmt.Errorf("list public wallets: %w", err)
	}
	for _, w := range rows {
		profile.PublicWallets = append(profile.PublicWallets, common.Address(w))
	}
	return profile, nil
}

func (s ProfileStore) Put(ctx context.Context, a social.Actor, username *string, wallets []common.Address) (social.PublicProfile, error) {
	requested, requestedRaw := dedupeWallets(wallets)
	linked, _ := dedupeWallets(a.Wallets)
	var out social.PublicProfile
	err := WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *Adapter) error {
		q := adapter.queries
		user, err := lockUser(ctx, q, a)
		if err != nil {
			return err
		}
		if user.BannedAt.Valid {
			return social.ErrBanned
		}
		if len(requested) > social.MaxPublicWallets {
			return social.ErrTooManyPublicWallets
		}
		for _, w := range requested {
			if !containsWallet(linked, w) {
				return social.ErrWalletNotLinked
			}
		}
		if username != nil && (!user.Username.Valid || user.Username.String != *username) {
			if err := changeUsername(ctx, q, a, user, *username); err != nil {
				return err
			}
		}
		// R10: keep exactly the requested wallets (all currently linked); move each to the caller.
		if err := q.PrunePublicWallets(ctx, sqlc.PrunePublicWalletsParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID, KeepWallets: requestedRaw}); err != nil {
			return fmt.Errorf("prune public wallets: %w", err)
		}
		for _, w := range requested {
			if err := q.ClaimPublicWallet(ctx, sqlc.ClaimPublicWalletParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, Wallet: sqlc.Address(w), PrivyDid: a.PrivyDID}); err != nil {
				return fmt.Errorf("claim public wallet: %w", err)
			}
		}
		out, err = readProfile(ctx, q, a)
		return err
	})
	if err != nil {
		return social.PublicProfile{}, err
	}
	return out, nil
}

// changeUsername applies the 7-day interval (the first set is free), the 30-day hold and the
// unique name, then releases the previous name.
func changeUsername(ctx context.Context, q *sqlc.Queries, a social.Actor, user sqlc.GetSocialUserRow, name string) error {
	if user.UsernameChangedAt.Valid {
		state, err := q.GetProfileState(ctx, sqlc.GetProfileStateParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID})
		if err != nil {
			return fmt.Errorf("get profile: %w", err)
		}
		if next := user.UsernameChangedAt.Time.Add(social.UsernameChangeInterval); next.After(state.DbNow.Time) {
			return &social.RateLimitedError{RetryAfter: retryAfter(next, state.DbNow.Time)}
		}
	}
	unavailable, err := q.UsernameUnavailable(ctx, sqlc.UsernameUnavailableParams{
		ChainID: a.ChainID, DeploymentID: a.DeploymentID, Username: name, PrivyDid: a.PrivyDID, HoldSeconds: usernameHoldSeconds,
	})
	if err != nil {
		return fmt.Errorf("check username: %w", err)
	}
	if unavailable.Bool {
		return social.ErrUsernameTaken
	}
	if err := q.SetUsername(ctx, sqlc.SetUsernameParams{Username: pgtype.Text{String: name, Valid: true}, ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return social.ErrUsernameTaken
		}
		return fmt.Errorf("set username: %w", err)
	}
	if user.Username.Valid {
		if err := q.ReleaseUsername(ctx, sqlc.ReleaseUsernameParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, Username: user.Username.String, PrivyDid: a.PrivyDID}); err != nil {
			return fmt.Errorf("release username: %w", err)
		}
	}
	return nil
}

func (s ProfileStore) Delete(ctx context.Context, a social.Actor) error {
	return WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *Adapter) error {
		q := adapter.queries
		user, err := lockUser(ctx, q, a)
		if err != nil {
			return err
		}
		if user.Username.Valid {
			if err := q.ReleaseUsername(ctx, sqlc.ReleaseUsernameParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, Username: user.Username.String, PrivyDid: a.PrivyDID}); err != nil {
				return fmt.Errorf("release username: %w", err)
			}
			if err := q.SetUsername(ctx, sqlc.SetUsernameParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID}); err != nil {
				return fmt.Errorf("clear username: %w", err)
			}
		}
		if err := q.PrunePublicWallets(ctx, sqlc.PrunePublicWalletsParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID, KeepWallets: [][]byte{}}); err != nil {
			return fmt.Errorf("delete public wallets: %w", err)
		}
		return nil
	})
}
