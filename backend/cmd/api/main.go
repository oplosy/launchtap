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
	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/quote"
	"github.com/Contictus/launchtap/backend/internal/realtime"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
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
	server := apiserver.New(apiConfig, ready, slog.Default())
	server.RegisterTokenRoutes(apiserver.TokenRoutes{Reader: storepostgres.TokenReader{Pool: pool, DeploymentID: c.DeploymentID}, ChainID: int64(c.ChainID)})
	server.RegisterCandleRoutes(apiserver.CandleRoutes{Reader: storepostgres.CandleReader{Pool: pool, DeploymentID: c.DeploymentID}, ChainID: int64(c.ChainID)})
	tokens := storepostgres.TokenReader{Pool: pool, DeploymentID: c.DeploymentID}
	market := storepostgres.MarketReader{Pool: pool, DeploymentID: c.DeploymentID}
	protocol := storepostgres.ProtocolReader{Pool: pool, DeploymentID: c.DeploymentID}
	server.RegisterPublicRoutes(apiserver.PublicRoutes{Tokens: tokens, Market: market, Protocol: protocol, ProtocolDaily: protocol, ChainID: int64(c.ChainID)})
	server.RegisterQuoteRoutes(apiserver.QuoteRoutes{Provider: quote.Service{Reader: tokens, ChainID: int64(c.ChainID)}})
	hub := realtime.NewHub(1000, 16)
	server.RegisterMetadataRoutes(apiserver.MetadataRoutes{Store: storepostgres.MetadataStore{Pool: pool, DeploymentID: c.DeploymentID}, Verifier: verifier, ChainID: int64(c.ChainID)})
	server.RegisterProfileRoutes(apiserver.ProfileRoutes{Reader: storepostgres.ProfileReader{Pool: pool, DeploymentID: c.DeploymentID}, Verifier: verifier, ChainID: int64(c.ChainID)})
	server.RegisterEventRoutes(apiserver.EventRoutes{Hub: hub, ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID, Streams: apiserver.NewStreamLimiter(int(c.APISSEMaxPerClient))})
	server.RegisterObservationRoutes(apiserver.ObservationRoutes{Reader: storepostgres.ObservationReader{Pool: pool}, ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID})
	go listenRefreshHints(ctx, pool, hub, int64(c.ChainID), c.DeploymentID)
	server.HTTP.Addr = c.APIAddr
	errCh := make(chan error, 1)
	go func() { errCh <- server.HTTP.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), apiserver.DefaultConfig().ShutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func loadDeploymentRegistry() (*deployments.Registry, error) {
	if manifest := os.Getenv("DEPLOYMENT_MANIFEST_PATH"); manifest != "" {
		return deployments.LoadEmbeddedWithManifest(manifest)
	}
	return deployments.LoadEmbedded()
}

func listenRefreshHints(ctx context.Context, pool *pgxpool.Pool, hub *realtime.Hub, chainID int64, deploymentID string) {
	for ctx.Err() == nil {
		err := storepostgres.ListenAPIRefresh(ctx, pool, func(payload []byte) {
			event, err := realtime.Decode(payload)
			if err != nil || event.ChainID != chainID || (event.DeploymentID != "" && event.DeploymentID != deploymentID) {
				return
			}
			hub.Publish(event)
		})
		if err == nil || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return
		}
		slog.Warn("API refresh listener disconnected", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
