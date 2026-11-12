package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Contictus/launchtap/backend/internal/social"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SocialStore keeps comments and reports. Per-user limits are derived from the rows the actions
// write, checked under a per-DID advisory lock on the database clock.
type SocialStore struct {
	Pool *pgxpool.Pool
}

var _ social.CommentStore = SocialStore{}

const rateWindowSeconds = float64(social.RateWindow / time.Second)

// dedupeWallets drops repeated addresses, keeping first-seen order; QualifyWallets would return
// one row per repeat. The result is never nil (a nil array would bind as NULL).
func dedupeWallets(in []common.Address) (out []common.Address, raw [][]byte) {
	seen := make(map[common.Address]struct{}, len(in))
	out = make([]common.Address, 0, len(in))
	raw = make([][]byte, 0, len(in))
	for _, w := range in {
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
		b := make([]byte, len(w))
		copy(b, w[:])
		raw = append(raw, b)
	}
	return out, raw
}

// retryAfter is the time from now until `until`, rounded up to whole seconds, at least one second.
func retryAfter(until, now time.Time) time.Duration {
	d := until.Sub(now)
	secs := (d + time.Second - 1) / time.Second
	if secs < 1 {
		secs = 1
	}
	return secs * time.Second
}

func tokenExists(ctx context.Context, q *sqlc.Queries, chainID int64, token common.Address) error {
	ok, err := q.TokenExists(ctx, sqlc.TokenExistsParams{ChainID: chainID, TokenAddress: sqlc.Address(token)})
	if err != nil {
		return fmt.Errorf("check comment token: %w", err)
	}
	if !ok {
		return social.ErrUnknownToken
	}
	return nil
}

// lockUser serializes the caller's social writes, ensures its row exists and returns it.
func lockUser(ctx context.Context, q *sqlc.Queries, a social.Actor) (sqlc.GetSocialUserRow, error) {
	if err := q.LockSocialSubject(ctx, sqlc.LockSocialSubjectParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, SubjectKey: a.PrivyDID}); err != nil {
		return sqlc.GetSocialUserRow{}, fmt.Errorf("lock social subject: %w", err)
	}
	if err := q.EnsureSocialUser(ctx, sqlc.EnsureSocialUserParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID}); err != nil {
		return sqlc.GetSocialUserRow{}, fmt.Errorf("ensure social user: %w", err)
	}
	user, err := q.GetSocialUser(ctx, sqlc.GetSocialUserParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID})
	if err != nil {
		return sqlc.GetSocialUserRow{}, fmt.Errorf("get social user: %w", err)
	}
	return user, nil
}

func (s SocialStore) List(ctx context.Context, sc social.Scope, token common.Address, cursor string, limit int) (social.CommentPage, error) {
	if limit <= 0 {
		limit = social.DefaultPageSize
	}
	if limit > social.MaxPageSize {
		limit = social.MaxPageSize
	}
	var before pgtype.Int8
	if cursor != "" {
		id, err := social.DecodeCursor(cursor)
		if err != nil {
			return social.CommentPage{}, err
		}
		before = pgtype.Int8{Int64: id, Valid: true}
	}
	q := sqlc.New(s.Pool)
	if err := tokenExists(ctx, q, sc.ChainID, token); err != nil {
		return social.CommentPage{}, err
	}
	rows, err := q.ListTokenComments(ctx, sqlc.ListTokenCommentsParams{
		ChainID: sc.ChainID, DeploymentID: sc.DeploymentID, TokenAddress: sqlc.Address(token),
		BeforeID: before, RowLimit: int32(limit + 1),
	})
	if err != nil {
		return social.CommentPage{}, fmt.Errorf("list comments: %w", err)
	}
	count, err := q.CountVisibleComments(ctx, sqlc.CountVisibleCommentsParams{ChainID: sc.ChainID, DeploymentID: sc.DeploymentID, TokenAddress: sqlc.Address(token)})
	if err != nil {
		return social.CommentPage{}, fmt.Errorf("count comments: %w", err)
	}
	page := social.CommentPage{Items: make([]social.Comment, 0, min(len(rows), limit)), Count: count}
	for i, row := range rows {
		if i == limit {
			page.NextCursor = social.EncodeCursor(page.Items[limit-1].ID)
			break
		}
		c, err := commentFromRow(row, token)
		if err != nil {
			return social.CommentPage{}, err
		}
		page.Items = append(page.Items, c)
	}
	return page, nil
}

func commentFromRow(row sqlc.ListTokenCommentsRow, token common.Address) (social.Comment, error) {
	return commentFromParts(row.ID, token, row.Body, row.CreatedAt, row.Role, row.DisplayUsername, row.DisplayWallet, row.Score)
}

// commentFromParts maps the read-time author display columns shared by the token comment list
// and the forum feed.
func commentFromParts(id int64, token common.Address, body string, createdAt pgtype.Timestamptz, role string, username pgtype.Text, wallet []byte, score int64) (social.Comment, error) {
	if !createdAt.Valid {
		return social.Comment{}, errors.New("comment has invalid created_at")
	}
	author := social.Author{Role: social.Role(role)}
	if username.Valid {
		name := username.String
		author.Username = &name
	}
	if wallet != nil {
		if len(wallet) != common.AddressLength {
			return social.Comment{}, errors.New("comment wallet has invalid length")
		}
		w := common.BytesToAddress(wallet)
		author.Wallet = &w
	}
	return social.Comment{ID: id, Token: token, Body: body, CreatedAt: createdAt.Time, Author: author, Score: score}, nil
}

func (s SocialStore) Eligibility(ctx context.Context, a social.Actor, token common.Address) (social.Eligibility, error) {
	q := sqlc.New(s.Pool)
	if err := tokenExists(ctx, q, a.ChainID, token); err != nil {
		return social.Eligibility{}, err
	}
	out := social.Eligibility{Wallets: []social.EligibleWallet{}}
	user, err := q.GetSocialUser(ctx, sqlc.GetSocialUserParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID})
	switch {
	case err == nil:
		out.Banned = user.BannedAt.Valid
		if user.Username.Valid && !out.Banned {
			name := user.Username.String
			out.Username = &name
		}
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return social.Eligibility{}, fmt.Errorf("get social user: %w", err)
	}
	_, raw := dedupeWallets(a.Wallets)
	rows, err := q.QualifyWallets(ctx, sqlc.QualifyWalletsParams{Wallets: raw, ChainID: a.ChainID, TokenAddress: sqlc.Address(token), DeploymentID: a.DeploymentID})
	if err != nil {
		return social.Eligibility{}, fmt.Errorf("qualify wallets: %w", err)
	}
	for _, row := range rows {
		out.Wallets = append(out.Wallets, social.EligibleWallet{Address: common.BytesToAddress(row.Wallet), Role: social.Role(row.Role)})
	}
	return out, nil
}

func (s SocialStore) Post(ctx context.Context, a social.Actor, token, wallet common.Address, raw string) (social.Comment, error) {
	var posted social.Comment
	err := WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *Adapter) error {
		q := adapter.queries
		user, err := lockUser(ctx, q, a)
		if err != nil {
			return err
		}
		if user.BannedAt.Valid {
			return social.ErrBanned
		}
		body, err := social.NormalizeBody(raw)
		if err != nil {
			return err
		}
		if err := tokenExists(ctx, q, a.ChainID, token); err != nil {
			return err
		}
		linked, raw := dedupeWallets(a.Wallets)
		if !containsWallet(linked, wallet) {
			return social.ErrWalletNotLinked
		}
		if err := q.PrunePublicWallets(ctx, sqlc.PrunePublicWalletsParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID, KeepWallets: raw}); err != nil {
			return fmt.Errorf("prune public wallets: %w", err)
		}
		role, err := walletRole(ctx, q, a, token, wallet, raw)
		if err != nil {
			return err
		}
		state, err := q.CommentRateState(ctx, sqlc.CommentRateStateParams{WindowSeconds: rateWindowSeconds, ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID, HourlyLimit: social.CommentsPerHour})
		if err != nil {
			return fmt.Errorf("comment rate state: %w", err)
		}
		if wait := commentWait(state); wait > 0 {
			return &social.RateLimitedError{RetryAfter: wait}
		}
		row, err := q.InsertComment(ctx, sqlc.InsertCommentParams{
			ChainID: a.ChainID, DeploymentID: a.DeploymentID, TokenAddress: sqlc.Address(token),
			PrivyDid: a.PrivyDID, Wallet: sqlc.Address(wallet), AuthorRole: string(role), Body: body,
		})
		if err != nil {
			return fmt.Errorf("insert comment: %w", err)
		}
		if !row.CreatedAt.Valid {
			return errors.New("inserted comment has invalid created_at")
		}
		published, err := q.IsPublicWallet(ctx, sqlc.IsPublicWalletParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, PrivyDid: a.PrivyDID, Wallet: sqlc.Address(wallet)})
		if err != nil {
			return fmt.Errorf("check public wallet: %w", err)
		}
		var username *string
		if user.Username.Valid {
			name := user.Username.String
			username = &name
		}
		payload, err := json.Marshal(map[string]any{"type": "comment", "chain_id": a.ChainID, "deployment_id": a.DeploymentID, "token": token.Hex()})
		if err != nil {
			return fmt.Errorf("encode comment notification: %w", err)
		}
		if err := q.NotifyAPIRefresh(ctx, string(payload)); err != nil {
			return fmt.Errorf("notify comment: %w", err)
		}
		posted = social.Comment{
			ID: row.ID, Token: token, Body: row.Body, CreatedAt: row.CreatedAt.Time,
			Author: social.DisplayAuthor(username, false, wallet, published, role),
		}
		return nil
	})
	if err != nil {
		return social.Comment{}, err
	}
	return posted, nil
}

func containsWallet(list []common.Address, w common.Address) bool {
	for _, x := range list {
		if x == w {
			return true
		}
	}
	return false
}

func walletRole(ctx context.Context, q *sqlc.Queries, a social.Actor, token, wallet common.Address, raw [][]byte) (social.Role, error) {
	rows, err := q.QualifyWallets(ctx, sqlc.QualifyWalletsParams{Wallets: raw, ChainID: a.ChainID, TokenAddress: sqlc.Address(token), DeploymentID: a.DeploymentID})
	if err != nil {
		return "", fmt.Errorf("qualify wallets: %w", err)
	}
	for _, row := range rows {
		if common.BytesToAddress(row.Wallet) == wallet {
			return social.Role(row.Role), nil
		}
	}
	return "", social.ErrNotHolder
}

// commentWait is how long the caller must wait before posting, zero when it may post now.
// All times are database-clock values.
func commentWait(st sqlc.CommentRateStateRow) time.Duration {
	if !st.DbNow.Valid {
		return 0
	}
	now := st.DbNow.Time
	var until time.Time
	if st.LastCreatedAt.Valid {
		if next := st.LastCreatedAt.Time.Add(social.CommentMinInterval); next.After(now) {
			until = next
		}
	}
	// ReleaseAt is the CommentsPerHour-th newest comment in the window: once it leaves, fewer
	// than CommentsPerHour remain, even when the window holds more rows than the cap.
	if st.CountInWindow >= social.CommentsPerHour && st.ReleaseAt.Valid {
		if free := st.ReleaseAt.Time.Add(social.RateWindow); free.After(until) {
			until = free
		}
	}
	if until.IsZero() {
		return 0
	}
	return retryAfter(until, now)
}

func (s SocialStore) Report(ctx context.Context, a social.Actor, commentID int64, reason social.ReportReason) error {
	return WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *Adapter) error {
		q := adapter.queries
		user, err := lockUser(ctx, q, a)
		if err != nil {
			return err
		}
		if user.BannedAt.Valid {
			return social.ErrBanned
		}
		target, err := q.GetReportableComment(ctx, sqlc.GetReportableCommentParams{CommentID: commentID, ChainID: a.ChainID, DeploymentID: a.DeploymentID})
		if errors.Is(err, pgx.ErrNoRows) {
			return social.ErrCommentNotFound
		}
		if err != nil {
			return fmt.Errorf("get reportable comment: %w", err)
		}
		if err := tokenExists(ctx, q, a.ChainID, common.Address(target.TokenAddress)); err != nil {
			if errors.Is(err, social.ErrUnknownToken) {
				return social.ErrCommentNotFound
			}
			return err
		}
		if target.AuthorDid == a.PrivyDID {
			return social.ErrOwnComment
		}
		// A repeat report is answered before the rate check and costs nothing: the insert is
		// a no-op for it. A new report is counted after insertion, so one over the cap rolls the
		// whole transaction back.
		rows, err := q.InsertReport(ctx, sqlc.InsertReportParams{CommentID: commentID, ChainID: a.ChainID, DeploymentID: a.DeploymentID, ReporterDid: a.PrivyDID, Reason: string(reason)})
		if err != nil {
			return fmt.Errorf("insert report: %w", err)
		}
		if rows == 0 {
			return nil
		}
		state, err := q.ReportRateState(ctx, sqlc.ReportRateStateParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, ReporterDid: a.PrivyDID, WindowSeconds: rateWindowSeconds, HourlyLimit: social.ReportsPerHour + 1})
		if err != nil {
			return fmt.Errorf("report rate state: %w", err)
		}
		// The count includes the report just inserted (the newest row), so the (cap+1)-th newest
		// is the cap-th newest of the earlier reports: once it leaves the window a retry fits.
		if state.CountInWindow > social.ReportsPerHour && state.ReleaseAt.Valid && state.DbNow.Valid {
			return &social.RateLimitedError{RetryAfter: retryAfter(state.ReleaseAt.Time.Add(social.RateWindow), state.DbNow.Time)}
		}
		return nil
	})
}

// Vote sets or clears the caller's vote on a visible comment
// (docs/specs/2026-11-12-comment-votes-design.md). Setting a vote needs a linked wallet that is
// the creator or a holder of the comment's token; clearing does not. A new or changed vote is
// counted after it is written, so one over the hourly cap rolls the whole transaction back.
func (s SocialStore) Vote(ctx context.Context, a social.Actor, commentID int64, value int) (social.VoteResult, error) {
	var out social.VoteResult
	err := WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *Adapter) error {
		q := adapter.queries
		user, err := lockUser(ctx, q, a)
		if err != nil {
			return err
		}
		if user.BannedAt.Valid {
			return social.ErrBanned
		}
		target, err := q.GetReportableComment(ctx, sqlc.GetReportableCommentParams{CommentID: commentID, ChainID: a.ChainID, DeploymentID: a.DeploymentID})
		if errors.Is(err, pgx.ErrNoRows) {
			return social.ErrCommentNotFound
		}
		if err != nil {
			return fmt.Errorf("get votable comment: %w", err)
		}
		token := common.Address(target.TokenAddress)
		if err := tokenExists(ctx, q, a.ChainID, token); err != nil {
			if errors.Is(err, social.ErrUnknownToken) {
				return social.ErrCommentNotFound
			}
			return err
		}
		if target.AuthorDid == a.PrivyDID {
			return social.ErrOwnVote
		}
		if value == 0 {
			if err := q.DeleteCommentVote(ctx, sqlc.DeleteCommentVoteParams{CommentID: commentID, VoterDid: a.PrivyDID}); err != nil {
				return fmt.Errorf("delete vote: %w", err)
			}
		} else {
			_, raw := dedupeWallets(a.Wallets)
			qualified, err := q.QualifyWallets(ctx, sqlc.QualifyWalletsParams{Wallets: raw, ChainID: a.ChainID, TokenAddress: sqlc.Address(token), DeploymentID: a.DeploymentID})
			if err != nil {
				return fmt.Errorf("qualify wallets: %w", err)
			}
			if len(qualified) == 0 {
				return social.ErrNotHolder
			}
			changed, err := q.UpsertCommentVote(ctx, sqlc.UpsertCommentVoteParams{CommentID: commentID, ChainID: a.ChainID, DeploymentID: a.DeploymentID, VoterDid: a.PrivyDID, VoteValue: int16(value)})
			if err != nil {
				return fmt.Errorf("upsert vote: %w", err)
			}
			if changed > 0 {
				state, err := q.VoteRateState(ctx, sqlc.VoteRateStateParams{ChainID: a.ChainID, DeploymentID: a.DeploymentID, VoterDid: a.PrivyDID, WindowSeconds: rateWindowSeconds, HourlyLimit: social.VotesPerHour + 1})
				if err != nil {
					return fmt.Errorf("vote rate state: %w", err)
				}
				if state.CountInWindow > social.VotesPerHour && state.ReleaseAt.Valid && state.DbNow.Valid {
					return &social.RateLimitedError{RetryAfter: retryAfter(state.ReleaseAt.Time.Add(social.RateWindow), state.DbNow.Time)}
				}
			}
		}
		score, err := q.CommentScore(ctx, commentID)
		if err != nil {
			return fmt.Errorf("comment score: %w", err)
		}
		out = social.VoteResult{Score: score, Vote: value}
		return nil
	})
	if err != nil {
		return social.VoteResult{}, err
	}
	return out, nil
}
