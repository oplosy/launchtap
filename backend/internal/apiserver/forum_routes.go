package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Contictus/launchtap/backend/internal/forum"
	"github.com/Contictus/launchtap/backend/internal/social"
	"github.com/danielgtaylor/huma/v2"
)

const (
	forumPostsCacheControl       = "public, max-age=5"
	forumCommunitiesCacheControl = "public, max-age=15"
)

// ForumRoutes serves the cross-token forum reads (spec P0-C5).
type ForumRoutes struct {
	Store        forum.Store
	ChainID      int64
	DeploymentID string
	// Logger records unexpected store failures; Server.RegisterForumRoutes defaults it.
	Logger *slog.Logger
}

type forumPostsInput struct {
	Sort   string `query:"sort" default:"new" doc:"new (default): newest first, paginated; best: posts from the last 7 days by score, one page. Any other value is a 400 invalid_sort."`
	Cursor string `query:"cursor" maxLength:"256" doc:"Only for sort=new."`
	Limit  int    `query:"limit" default:"20" doc:"Number of posts, 1-50; defaults to 20."`
}

// forumPostDTO is a commentDTO plus the token's launch name and symbol, written out field by
// field because the schema generator skips unexported embedded structs.
type forumPostDTO struct {
	ID        string           `json:"id" doc:"Decimal string."`
	Token     string           `json:"token"`
	Body      string           `json:"body"`
	CreatedAt string           `json:"created_at"`
	Author    commentAuthorDTO `json:"author"`
	Score     int64            `json:"score" doc:"Sum of votes from voters who are not banned."`
	Name      string           `json:"name" doc:"Token launch name; user-controlled text."`
	Symbol    string           `json:"symbol" doc:"Token launch symbol; user-controlled text."`
}
type forumPostsBody struct {
	Items      []forumPostDTO `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}
type forumPostsOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         forumPostsBody
}

type forumCommunitiesInput struct {
	Sort  string `query:"sort" default:"hot" doc:"hot (default): visible comments in the last 24 hours; top: all time. Any other value is a 400 invalid_sort."`
	Limit int    `query:"limit" default:"20" doc:"Number of communities, 1-50; defaults to 20."`
}
type forumCommunityDTO struct {
	Token         string `json:"token"`
	Name          string `json:"name"`
	Symbol        string `json:"symbol"`
	CommentCount  int64  `json:"comment_count" doc:"Visible comments in the sort's window."`
	LastCommentAt string `json:"last_comment_at"`
}
type forumCommunitiesBody struct {
	Items []forumCommunityDTO `json:"items"`
}
type forumCommunitiesOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         forumCommunitiesBody
}

func (r ForumRoutes) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "listForumPosts", Method: http.MethodGet, Path: "/forum/posts", Tags: []string{"forum"}}, r.posts)
	huma.Register(api, huma.Operation{OperationID: "listForumCommunities", Method: http.MethodGet, Path: "/forum/communities", Tags: []string{"forum"}}, r.communities)
}

func (r ForumRoutes) scope() social.Scope {
	return social.Scope{ChainID: r.ChainID, DeploymentID: r.DeploymentID}
}

func forumLimit(limit int) error {
	if limit < 1 || limit > forum.MaxPageSize {
		return apiProblem(http.StatusUnprocessableEntity, "invalid_limit", "limit must be between 1 and 50")
	}
	return nil
}

func (r ForumRoutes) fail(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, social.ErrInvalidCursor):
		return apiProblem(http.StatusUnprocessableEntity, "invalid_cursor", "Cursor is invalid")
	case errors.Is(err, context.Canceled):
		return err
	default:
		if r.Logger != nil {
			r.Logger.ErrorContext(ctx, "forum request failed", "request_id", RequestID(ctx), "error_type", fmt.Sprintf("%T", err))
		}
		return apiRequestProblem(ctx, http.StatusInternalServerError, "internal_error", "Request failed")
	}
}

func (r ForumRoutes) posts(ctx context.Context, in *forumPostsInput) (*forumPostsOutput, error) {
	if r.Store == nil {
		return nil, huma.Error503ServiceUnavailable("forum unavailable")
	}
	sort, ok := forum.ParsePostSort(in.Sort)
	if !ok {
		return nil, apiProblem(http.StatusBadRequest, "invalid_sort", "sort must be new or best")
	}
	if err := forumLimit(in.Limit); err != nil {
		return nil, err
	}
	var page forum.PostPage
	if sort == forum.PostSortBest {
		items, err := r.Store.BestPosts(ctx, r.scope(), in.Limit)
		if err != nil {
			return nil, r.fail(ctx, err)
		}
		page.Items = items
	} else {
		var err error
		if page, err = r.Store.Posts(ctx, r.scope(), in.Cursor, in.Limit); err != nil {
			return nil, r.fail(ctx, err)
		}
	}
	out := forumPostsOutput{CacheControl: forumPostsCacheControl, Body: forumPostsBody{
		Items: make([]forumPostDTO, 0, len(page.Items)), NextCursor: page.NextCursor,
	}}
	for _, p := range page.Items {
		c := toCommentDTO(p.Comment)
		out.Body.Items = append(out.Body.Items, forumPostDTO{
			ID: c.ID, Token: c.Token, Body: c.Body, CreatedAt: c.CreatedAt, Author: c.Author, Score: c.Score, Name: p.Name, Symbol: p.Symbol,
		})
	}
	return &out, nil
}

func (r ForumRoutes) communities(ctx context.Context, in *forumCommunitiesInput) (*forumCommunitiesOutput, error) {
	if r.Store == nil {
		return nil, huma.Error503ServiceUnavailable("forum unavailable")
	}
	sort, ok := forum.ParseSort(in.Sort)
	if !ok {
		return nil, apiProblem(http.StatusBadRequest, "invalid_sort", "sort must be hot or top")
	}
	if err := forumLimit(in.Limit); err != nil {
		return nil, err
	}
	items, err := r.Store.Communities(ctx, r.scope(), sort, in.Limit)
	if err != nil {
		return nil, r.fail(ctx, err)
	}
	out := forumCommunitiesOutput{CacheControl: forumCommunitiesCacheControl, Body: forumCommunitiesBody{Items: make([]forumCommunityDTO, 0, len(items))}}
	for _, c := range items {
		out.Body.Items = append(out.Body.Items, forumCommunityDTO{
			Token: c.Token.Hex(), Name: c.Name, Symbol: c.Symbol, CommentCount: c.CommentCount,
			LastCommentAt: c.LastCommentAt.UTC().Format(commentTimeLayout),
		})
	}
	return &out, nil
}
