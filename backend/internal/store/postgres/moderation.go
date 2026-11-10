package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/social"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ModerationStore is the operator-side store of comment moderation. Each mutation is one
// transaction: target lookup, change, audit row, report resolution and one `comment`
// notification per affected token.
type ModerationStore struct {
	Pool *pgxpool.Pool
}

var _ social.Moderator = ModerationStore{}

// Audit action names; they mirror the moderation_actions_action_valid CHECK.
const (
	actionHide    = "hide_comment"
	actionUnhide  = "unhide_comment"
	actionBan     = "ban_user"
	actionUnban   = "unban_user"
	actionResolve = "resolve_reports"
)

func (s ModerationStore) OpenReports(ctx context.Context, sc social.Scope, limit int) ([]social.ReportSummary, error) {
	if limit < 1 {
		return nil, errors.New("report limit must be positive")
	}
	rows, err := sqlc.New(s.Pool).ListOpenReportedComments(ctx, sqlc.ListOpenReportedCommentsParams{
		ChainID: sc.ChainID, DeploymentID: sc.DeploymentID, RowLimit: int32(min(limit, 1<<30)),
	})
	if err != nil {
		return nil, fmt.Errorf("list open reports: %w", err)
	}
	out := make([]social.ReportSummary, 0, len(rows))
	for _, row := range rows {
		if !row.CreatedAt.Valid {
			return nil, errors.New("reported comment has invalid created_at")
		}
		reasons := make([]social.ReportReason, 0, len(row.Reasons))
		for _, r := range row.Reasons {
			reasons = append(reasons, social.ReportReason(r))
		}
		out = append(out, social.ReportSummary{
			CommentID: row.ID, Token: common.Address(row.TokenAddress), CreatedAt: row.CreatedAt.Time, Body: row.Body,
			AuthorDID: row.AuthorDid, Wallet: common.Address(row.Wallet), Reports: row.Reports, Reasons: reasons,
		})
	}
	return out, nil
}

func (s ModerationStore) Hide(ctx context.Context, sc social.Scope, commentID int64, operator, reason string) error {
	if err := social.ValidateModerationText(operator, reason); err != nil {
		return err
	}
	return WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *Adapter) error {
		q := adapter.queries
		target, err := commentTarget(ctx, q, sc, commentID)
		if err != nil {
			return err
		}
		if err := q.HideComment(ctx, sqlc.HideCommentParams{Reason: reason, CommentID: commentID, ChainID: sc.ChainID, DeploymentID: sc.DeploymentID}); err != nil {
			return fmt.Errorf("hide comment: %w", err)
		}
		if _, err := q.ResolveCommentReports(ctx, sqlc.ResolveCommentReportsParams{Resolution: string(social.ResolutionActioned), CommentID: commentID, ChainID: sc.ChainID, DeploymentID: sc.DeploymentID}); err != nil {
			return fmt.Errorf("resolve reports: %w", err)
		}
		if err := audit(ctx, q, sc, actionHide, pgtype.Int8{Int64: commentID, Valid: true}, pgtype.Text{}, operator, reason); err != nil {
			return err
		}
		return notifyComment(ctx, q, sc, common.Address(target.TokenAddress))
	})
}

func (s ModerationStore) Unhide(ctx context.Context, sc social.Scope, commentID int64, operator, reason string) error {
	if err := social.ValidateModerationText(operator, reason); err != nil {
		return err
	}
	return WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *Adapter) error {
		q := adapter.queries
		target, err := commentTarget(ctx, q, sc, commentID)
		if err != nil {
			return err
		}
		if err := q.UnhideComment(ctx, sqlc.UnhideCommentParams{CommentID: commentID, ChainID: sc.ChainID, DeploymentID: sc.DeploymentID}); err != nil {
			return fmt.Errorf("unhide comment: %w", err)
		}
		if err := audit(ctx, q, sc, actionUnhide, pgtype.Int8{Int64: commentID, Valid: true}, pgtype.Text{}, operator, reason); err != nil {
			return err
		}
		return notifyComment(ctx, q, sc, common.Address(target.TokenAddress))
	})
}

func (s ModerationStore) Ban(ctx context.Context, sc social.Scope, did string, operator, reason string) ([]common.Address, error) {
	if err := social.ValidateModerationText(operator, reason); err != nil {
		return nil, err
	}
	var tokens []common.Address
	err := WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *Adapter) error {
		var err error
		tokens, err = ban(ctx, adapter.queries, sc, did, pgtype.Int8{}, operator, reason)
		return err
	})
	return tokens, err
}

func (s ModerationStore) BanCommentAuthor(ctx context.Context, sc social.Scope, commentID int64, operator, reason string) ([]common.Address, error) {
	if err := social.ValidateModerationText(operator, reason); err != nil {
		return nil, err
	}
	var tokens []common.Address
	err := WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *Adapter) error {
		target, err := commentTarget(ctx, adapter.queries, sc, commentID)
		if err != nil {
			return err
		}
		tokens, err = ban(ctx, adapter.queries, sc, target.AuthorDid, pgtype.Int8{Int64: commentID, Valid: true}, operator, reason)
		return err
	})
	return tokens, err
}

func (s ModerationStore) Unban(ctx context.Context, sc social.Scope, did string, operator, reason string) ([]common.Address, error) {
	if err := social.ValidateModerationText(operator, reason); err != nil {
		return nil, err
	}
	var tokens []common.Address
	err := WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *Adapter) error {
		q := adapter.queries
		if err := lockSubject(ctx, q, sc, did); err != nil {
			return err
		}
		rows, err := q.UnbanSocialUser(ctx, sqlc.UnbanSocialUserParams{ChainID: sc.ChainID, DeploymentID: sc.DeploymentID, PrivyDid: did})
		if err != nil {
			return fmt.Errorf("unban user: %w", err)
		}
		if rows == 0 {
			return social.ErrNotFound
		}
		tokens, err = authorTokens(ctx, q, sc, did)
		if err != nil {
			return err
		}
		if err := audit(ctx, q, sc, actionUnban, pgtype.Int8{}, pgtype.Text{String: did, Valid: true}, operator, reason); err != nil {
			return err
		}
		return notifyTokens(ctx, q, sc, tokens)
	})
	return tokens, err
}

// Resolve closes the comment's open reports without touching the comment. The audit reason is
// prefixed with the resolution because moderation_actions has no column for it.
func (s ModerationStore) Resolve(ctx context.Context, sc social.Scope, commentID int64, r social.Resolution, operator, reason string) (int64, error) {
	if _, err := social.ParseResolution(string(r)); err != nil {
		return 0, err
	}
	if err := social.ValidateModerationText(operator, reason); err != nil {
		return 0, err
	}
	var closed int64
	err := WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *Adapter) error {
		q := adapter.queries
		if _, err := commentTarget(ctx, q, sc, commentID); err != nil {
			return err
		}
		var err error
		closed, err = q.ResolveCommentReports(ctx, sqlc.ResolveCommentReportsParams{Resolution: string(r), CommentID: commentID, ChainID: sc.ChainID, DeploymentID: sc.DeploymentID})
		if err != nil {
			return fmt.Errorf("resolve reports: %w", err)
		}
		return audit(ctx, q, sc, actionResolve, pgtype.Int8{Int64: commentID, Valid: true}, pgtype.Text{}, operator, string(r)+": "+reason)
	})
	return closed, err
}

// ban runs inside the caller's transaction. It serializes with the DID's own posts and reports
// through the same advisory lock they take.
func ban(ctx context.Context, q *sqlc.Queries, sc social.Scope, did string, commentID pgtype.Int8, operator, reason string) ([]common.Address, error) {
	if err := lockSubject(ctx, q, sc, did); err != nil {
		return nil, err
	}
	known, err := q.SocialDIDKnown(ctx, sqlc.SocialDIDKnownParams{ChainID: sc.ChainID, DeploymentID: sc.DeploymentID, PrivyDid: did})
	if err != nil {
		return nil, fmt.Errorf("check user: %w", err)
	}
	if !known {
		return nil, social.ErrNotFound
	}
	if err := q.BanSocialUser(ctx, sqlc.BanSocialUserParams{ChainID: sc.ChainID, DeploymentID: sc.DeploymentID, PrivyDid: did, Reason: reason}); err != nil {
		return nil, fmt.Errorf("ban user: %w", err)
	}
	if _, err := q.ResolveAuthorReports(ctx, sqlc.ResolveAuthorReportsParams{Resolution: string(social.ResolutionActioned), ChainID: sc.ChainID, DeploymentID: sc.DeploymentID, PrivyDid: did}); err != nil {
		return nil, fmt.Errorf("resolve author reports: %w", err)
	}
	tokens, err := authorTokens(ctx, q, sc, did)
	if err != nil {
		return nil, err
	}
	if err := audit(ctx, q, sc, actionBan, commentID, pgtype.Text{String: did, Valid: true}, operator, reason); err != nil {
		return nil, err
	}
	if err := notifyTokens(ctx, q, sc, tokens); err != nil {
		return nil, err
	}
	return tokens, nil
}

func lockSubject(ctx context.Context, q *sqlc.Queries, sc social.Scope, did string) error {
	if err := q.LockSocialSubject(ctx, sqlc.LockSocialSubjectParams{ChainID: sc.ChainID, DeploymentID: sc.DeploymentID, SubjectKey: did}); err != nil {
		return fmt.Errorf("lock social subject: %w", err)
	}
	return nil
}

// commentTarget locks and returns the comment, or social.ErrNotFound.
func commentTarget(ctx context.Context, q *sqlc.Queries, sc social.Scope, commentID int64) (sqlc.GetCommentForModerationRow, error) {
	row, err := q.GetCommentForModeration(ctx, sqlc.GetCommentForModerationParams{CommentID: commentID, ChainID: sc.ChainID, DeploymentID: sc.DeploymentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.GetCommentForModerationRow{}, social.ErrNotFound
	}
	if err != nil {
		return sqlc.GetCommentForModerationRow{}, fmt.Errorf("get comment: %w", err)
	}
	return row, nil
}

func authorTokens(ctx context.Context, q *sqlc.Queries, sc social.Scope, did string) ([]common.Address, error) {
	rows, err := q.ListAuthorCommentTokens(ctx, sqlc.ListAuthorCommentTokensParams{ChainID: sc.ChainID, DeploymentID: sc.DeploymentID, PrivyDid: did})
	if err != nil {
		return nil, fmt.Errorf("list author tokens: %w", err)
	}
	tokens := make([]common.Address, 0, len(rows))
	for _, a := range rows {
		tokens = append(tokens, common.Address(a))
	}
	return tokens, nil
}

func audit(ctx context.Context, q *sqlc.Queries, sc social.Scope, action string, commentID pgtype.Int8, targetDID pgtype.Text, operator, reason string) error {
	err := q.InsertModerationAction(ctx, sqlc.InsertModerationActionParams{
		ChainID: sc.ChainID, DeploymentID: sc.DeploymentID, Action: action,
		CommentID: commentID, TargetDid: targetDID, Operator: operator, Reason: reason,
	})
	if err != nil {
		return fmt.Errorf("record moderation action: %w", err)
	}
	return nil
}

func notifyTokens(ctx context.Context, q *sqlc.Queries, sc social.Scope, tokens []common.Address) error {
	for _, token := range tokens {
		if err := notifyComment(ctx, q, sc, token); err != nil {
			return err
		}
	}
	return nil
}

// notifyComment sends the same `comment` hint a post sends; NOTIFY is delivered on commit only.
func notifyComment(ctx context.Context, q *sqlc.Queries, sc social.Scope, token common.Address) error {
	payload, err := json.Marshal(map[string]any{"type": "comment", "chain_id": sc.ChainID, "deployment_id": sc.DeploymentID, "token": token.Hex()})
	if err != nil {
		return fmt.Errorf("encode comment notification: %w", err)
	}
	if err := q.NotifyAPIRefresh(ctx, string(payload)); err != nil {
		return fmt.Errorf("notify comment: %w", err)
	}
	return nil
}
