package apiserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
)

func GenerateOpenAPI() ([]byte, error) {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.RegisterTokenRoutes(TokenRoutes{})
	s.RegisterCandleRoutes(CandleRoutes{})
	s.RegisterPublicRoutes(PublicRoutes{})
	s.RegisterQuoteRoutes(QuoteRoutes{})
	s.RegisterMetadataRoutes(MetadataRoutes{})
	s.RegisterEventRoutes(EventRoutes{})
	s.RegisterObservationRoutes(ObservationRoutes{})
	s.RegisterProfileRoutes(ProfileRoutes{})
	s.RegisterReferenceRoutes(ReferenceRoutes{})
	s.RegisterHighlightsRoutes(HighlightsRoutes{})
	s.RegisterFeedRoutes(FeedRoutes{})
	s.RegisterWalletRoutes(WalletRoutes{})
	s.RegisterWatchlistRoutes(WatchlistRoutes{})
	s.RegisterCommentRoutes(CommentRoutes{})
	s.RegisterWatchersRoutes(WatchersRoutes{})
	s.RegisterInboxRoutes(InboxRoutes{})
	s.RegisterPublicProfileRoutes(PublicProfileRoutes{})
	b, err := json.MarshalIndent(s.API.OpenAPI(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
