package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/forum"
	"github.com/Contictus/launchtap/backend/internal/social"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ForumStore implements forum.Store over token_comments (spec P0-C5).
type ForumStore struct {
	Pool *pgxpool.Pool
}

var _ forum.Store = ForumStore{}

func clampForumLimit(limit int) int {
	if limit <= 0 {
		return forum.DefaultPageSize
	}
	return min(limit, forum.MaxPageSize)
}

func (s ForumStore) Posts(ctx context.Context, sc social.Scope, cursor string, limit int) (forum.PostPage, error) {
	limit = clampForumLimit(limit)
	var before pgtype.Int8
	if cursor != "" {
		id, err := social.DecodeCursor(cursor)
		if err != nil {
			return forum.PostPage{}, err
		}
		before = pgtype.Int8{Int64: id, Valid: true}
	}
	rows, err := sqlc.New(s.Pool).ListForumPosts(ctx, sqlc.ListForumPostsParams{
		ChainID: sc.ChainID, DeploymentID: sc.DeploymentID, BeforeID: before, RowLimit: int32(limit + 1),
	})
	if err != nil {
		return forum.PostPage{}, fmt.Errorf("list forum posts: %w", err)
	}
	page := forum.PostPage{Items: make([]forum.Post, 0, min(len(rows), limit))}
	for i, row := range rows {
		if i == limit {
			page.NextCursor = social.EncodeCursor(page.Items[limit-1].ID)
			break
		}
		token, err := forumToken(row.TokenAddress)
		if err != nil {
			return forum.PostPage{}, err
		}
		c, err := commentFromParts(row.ID, token, row.Body, row.CreatedAt, row.Role, row.DisplayUsername, row.DisplayWallet)
		if err != nil {
			return forum.PostPage{}, err
		}
		page.Items = append(page.Items, forum.Post{Comment: c, Name: row.Name, Symbol: row.Symbol})
	}
	return page, nil
}

func (s ForumStore) Communities(ctx context.Context, sc social.Scope, sort forum.Sort, limit int) ([]forum.Community, error) {
	var window pgtype.Int4
	switch sort {
	case forum.SortHot:
		window = pgtype.Int4{Int32: forum.HotWindowHours, Valid: true}
	case forum.SortTop:
	default:
		return nil, fmt.Errorf("forum communities: unknown sort %q", sort)
	}
	rows, err := sqlc.New(s.Pool).ListForumCommunities(ctx, sqlc.ListForumCommunitiesParams{
		ChainID: sc.ChainID, DeploymentID: sc.DeploymentID, WindowHours: window, RowLimit: int32(clampForumLimit(limit)),
	})
	if err != nil {
		return nil, fmt.Errorf("list forum communities: %w", err)
	}
	out := make([]forum.Community, 0, len(rows))
	for _, row := range rows {
		token, err := forumToken(row.TokenAddress)
		if err != nil {
			return nil, err
		}
		if !row.LastCommentAt.Valid {
			return nil, errors.New("forum community has no last comment time")
		}
		out = append(out, forum.Community{Token: token, Name: row.Name, Symbol: row.Symbol, CommentCount: row.CommentCount, LastCommentAt: row.LastCommentAt.Time})
	}
	return out, nil
}

func forumToken(raw []byte) (common.Address, error) {
	if len(raw) != common.AddressLength {
		return common.Address{}, errors.New("forum token address has invalid length")
	}
	return common.BytesToAddress(raw), nil
}
