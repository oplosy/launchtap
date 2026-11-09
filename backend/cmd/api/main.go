package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Contictus/launchtap/backend/deployments"
	"github.com/Contictus/launchtap/backend/internal/apiserver"
	"github.com/Contictus/launchtap/backend/internal/config"
	"github.com/Contictus/launchtap/backend/internal/feed"
	"github.com/Contictus/launchtap/backend/internal/metrics"
	"github.com/Contictus/launchtap/backend/internal/pricefeed"
	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/quote"
	"github.com/Contictus/launchtap/backend/internal/realtime"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	c, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if err := c.RequireAPI(); err != nil {
		return err
	}
	verifier, err := privyauth.NewES256Verifier(c.PrivyAppID, c.PrivyVerificationKey)
	if err != nil {
		return err
	}
	registry, err := loadDeploymentRegistry()
	if err != nil {
		return err
	}
	if _, err := registry.Resolve(c.ChainID, c.DeploymentID, c.IndexerConfirmations); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := storepostgres.OpenPool(ctx, c.DatabaseURL, storepostgres.PoolOptions{MaxConns: int32(c.DatabaseMaxConns)})
	if err != nil {
		return err
	}
	defer pool.Close()
	readyStore := storepostgres.NewAdapter(pool)
	ready := apiserver.ReadyFunc(func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return err
		}
		state, err := readyStore.GetSyncState(ctx, int64(c.ChainID), c.DeploymentID)
		if err != nil {
			return err
		}
		if !state.ObservedNumber.Valid || state.ObservedHash == nil || !state.ObservedAt.Valid {
			return errors.New("indexed watermark is incomplete")
		}
		return nil
	})
	apiConfig := apiserver.DefaultConfig()
	apiConfig.AllowedOrigins = c.APIAllowedOrigins
	apiConfig.TrustedProxies = c.APITrustedProxyCIDRs
	apiConfig.RateLimitPerMinute, apiConfig.RateLimitBurst = c.APIRateLimitPerMinute, c.APIRateLimitBurst
	metricsRegistry := metrics.NewRegistry(metrics.ComponentAPI)
	apiMetrics := metrics.NewAPI(metricsRegistry)
	apiConfig.Metrics = apiMetrics
	metrics.RegisterPool(metricsRegistry, "launchpad_api", pool.Stat)
	server := apiserver.New(apiConfig, ready, slog.Default())
	server.RegisterTokenRoutes(apiserver.TokenRoutes{Reader: storepostgres.TokenReader{Pool: pool, DeploymentID: c.DeploymentID}, ChainID: int64(c.ChainID)})
	server.RegisterHighlightsRoutes(apiserver.HighlightsRoutes{Reader: storepostgres.HighlightsReader{Pool: pool, DeploymentID: c.DeploymentID}, ChainID: int64(c.ChainID)})
	server.RegisterCandleRoutes(apiserver.CandleRoutes{Reader: storepostgres.CandleReader{Pool: pool, DeploymentID: c.DeploymentID}, ChainID: int64(c.ChainID)})
	tokens := storepostgres.TokenReader{Pool: pool, DeploymentID: c.DeploymentID}
	market := storepostgres.MarketReader{Pool: pool, DeploymentID: c.DeploymentID}
	protocol := storepostgres.ProtocolReader{Pool: pool, DeploymentID: c.DeploymentID}
	server.RegisterPublicRoutes(apiserver.PublicRoutes{Tokens: tokens, Market: market, Protocol: protocol, ProtocolDaily: protocol, ChainID: int64(c.ChainID)})
	server.RegisterQuoteRoutes(apiserver.QuoteRoutes{Provider: quote.Service{Reader: tokens, ChainID: int64(c.ChainID)}})
	hub := realtime.NewHub(1000, 64)
	server.RegisterMetadataRoutes(apiserver.MetadataRoutes{Store: storepostgres.MetadataStore{Pool: pool, DeploymentID: c.DeploymentID}, Verifier: verifier, ChainID: int64(c.ChainID)})
	server.RegisterProfileRoutes(apiserver.ProfileRoutes{Reader: storepostgres.ProfileReader{Pool: pool, DeploymentID: c.DeploymentID}, Verifier: verifier, ChainID: int64(c.ChainID)})
	server.RegisterWatchlistRoutes(apiserver.WatchlistRoutes{Verifier: verifier, Store: storepostgres.WatchlistStore{Pool: pool}, ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID})
	server.RegisterCommentRoutes(apiserver.CommentRoutes{Verifier: verifier, Store: storepostgres.SocialStore{Pool: pool}, ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID})
	server.RegisterEventRoutes(apiserver.EventRoutes{Hub: hub, ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID, Streams: apiserver.NewStreamLimiter(int(c.APISSEMaxPerClient))})
	feedReader := storepostgres.FeedReader{Pool: pool, DeploymentID: c.DeploymentID}
	server.RegisterFeedRoutes(apiserver.FeedRoutes{Reader: feedReader, ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID})
	server.RegisterWalletRoutes(apiserver.WalletRoutes{Reader: storepostgres.WalletReader{Pool: pool, DeploymentID: c.DeploymentID}, ChainID: int64(c.ChainID)})
	server.RegisterObservationRoutes(apiserver.ObservationRoutes{Reader: storepostgres.ObservationReader{Pool: pool}, ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID})
	references := apiserver.ReferenceRoutes{}
	if c.ETHUSDSource == config.ETHUSDSourceCoinGecko {
		apiMetrics.RegisterPriceFeed(metricsRegistry)
		prices := pricefeed.New(pricefeed.Options{APIKey: c.CoinGeckoAPIKey, Logger: slog.Default(), Observe: apiMetrics.ObservePriceFeed})
		go func() { _ = prices.Run(ctx) }() // stops with the signal context on shutdown
		references.Prices = prices
	}
	server.RegisterReferenceRoutes(references)
	tail := &feed.Tail{Reader: feedReader, Hub: hub, ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID, Observer: apiMetrics, Logger: slog.Default()}
	go func() { _ = tail.Start(ctx) }() // returns only when the signal context ends
	go listenRefreshHints(ctx, pool, hub, tail, int64(c.ChainID), c.DeploymentID)
	server.HTTP.Addr = c.APIAddr
	errCh := make(chan error, 2)
	go func() { errCh <- server.HTTP.ListenAndServe() }()
	var metricsServer *http.Server
	if c.APIMetricsAddr != "" {
		// Separate listener: metrics are never served on the public API_ADDR.
		metricsServer = &http.Server{Addr: c.APIMetricsAddr, Handler: metricsMux(metricsRegistry), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}()
	}
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), apiserver.DefaultConfig().ShutdownTimeout)
		defer cancel()
		if metricsServer != nil {
			_ = metricsServer.Shutdown(shutdownCtx)
		}
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// metricsMux serves only /metrics.
func metricsMux(g prometheus.Gatherer) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler(g))
	return mux
}

func loadDeploymentRegistry() (*deployments.Registry, error) {
	if manifest := os.Getenv("DEPLOYMENT_MANIFEST_PATH"); manifest != "" {
		return deployments.LoadEmbeddedWithManifest(manifest)
	}
	return deployments.LoadEmbedded()
}

func listenRefreshHints(ctx context.Context, pool *pgxpool.Pool, hub *realtime.Hub, tail *feed.Tail, chainID int64, deploymentID string) {
	receive := refreshReceiver(hub, tail, chainID, deploymentID)
	listen := func(ctx context.Context, ready chan<- struct{}) error {
		return storepostgres.ListenAPIRefreshReady(ctx, pool, ready, receive)
	}
	// Hints sent while the listener was down are lost, a reorg hint among them; the tail rewinds to the
	// safe block once LISTEN is active again.
	listenLoop(ctx, listen, tail.Reconnected, time.Second)
}

// listenLoop runs listen until ctx ends, restarting it backoff after each failure. listen closes ready
// once it is listening. onReconnect runs each time a restarted listener becomes ready, never for the
// first attempt.
func listenLoop(ctx context.Context, listen func(context.Context, chan<- struct{}) error, onReconnect func(), backoff time.Duration) {
	for attempt := 0; ctx.Err() == nil; attempt++ {
		ready := make(chan struct{})
		attemptCtx, cancel := context.WithCancel(ctx)
		watched := make(chan struct{})
		go func(restart bool) {
			defer close(watched)
			select {
			case <-ready:
				if restart {
					onReconnect()
				}
			case <-attemptCtx.Done():
			}
		}(attempt > 0)
		err := listen(attemptCtx, ready)
		cancel()
		<-watched
		if err == nil || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return
		}
		slog.Warn("API refresh listener disconnected", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// tailHinter is the part of feed.Tail that refreshReceiver drives.
type tailHinter interface{ Hint(realtime.Event) }

// refreshReceiver decodes an api_refresh payload, drops other scopes, publishes it to the hub and wakes
// the trade tail for chain events. A comment is not chain data, so it never wakes the tail.
func refreshReceiver(hub *realtime.Hub, tail tailHinter, chainID int64, deploymentID string) func([]byte) {
	return func(payload []byte) {
		event, err := realtime.Decode(payload)
		if err != nil || event.ChainID != chainID || (event.DeploymentID != "" && event.DeploymentID != deploymentID) {
			return
		}
		hub.Publish(event)
		// Tail.Hint does no scope check, so it is called only after the filter above.
		if event.Type != "comment" {
			tail.Hint(event)
		}
	}
}
