package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Contictus/launchtap/backend/deployments"
	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/config"
	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/stats"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		slog.Error("indexer stopped", "error", err)
		os.Exit(indexer.ExitCode(err))
	}
}
func run() error {
	c, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if err := c.RequireIndexer(); err != nil {
		return err
	}
	registry, err := loadDeploymentRegistry()
	if err != nil {
		return err
	}
	deployment, err := registry.Resolve(c.ChainID, c.DeploymentID, c.IndexerConfirmations)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	source, err := chain.Dial(ctx, c.RPCURL, chain.RPCConfig{Timeout: c.RPCTimeout, MaxRetries: c.RPCMaxRetries, RetryBackoff: c.RPCRetryBackoff})
	if err != nil {
		return err
	}
	defer source.Close()
	pool, owner, err := initializeIndexerResources(ctx, source, c.ChainID, deployment,
		func() (*pgxpool.Pool, error) {
			return storepostgres.OpenPool(ctx, c.DatabaseURL, storepostgres.PoolOptions{MaxConns: int32(c.DatabaseMaxConns)})
		},
		func() (*storepostgres.Ownership, error) {
			return storepostgres.AcquireOwnership(ctx, c.DatabaseURL, int64(c.ChainID), c.DeploymentID)
		},
	)
	if err != nil {
		return err
	}
	defer pool.Close()
	defer func() { _ = owner.Close() }()
	decoder, err := chain.NewDecoder(deployment.EngineVersion)
	if err != nil {
		return err
	}
	discovery, err := chain.NewDiscovererWithPairVerification(source, decoder, deployment.Factory, c.IndexerLogAddressBatchSize, func(ctx context.Context, launch chain.TokenLaunched) error {
		return chain.VerifyLaunchPair(ctx, source, deployment.UniV2Factory, deployment.WETH, deployment.PairInitCodeHash, launch)
	})
	if err != nil {
		return err
	}
	store := storepostgres.IndexerStore{Pool: pool, Beginner: owner.Beginner(), Owner: owner, ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID}
	router := indexer.LedgerRouter{ChainID: int64(c.ChainID)}
	health := new(indexer.HealthTracker)
	health.Set(indexer.Health{ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID, OwnershipHeld: true, RPCHealthy: true})
	healthStore := storepostgres.NewAdapter(pool)
	refreshOperationalHealth := func() {
		snapshot, err := healthStore.ReadOperationalHealth(ctx, int64(c.ChainID), c.DeploymentID)
		if err != nil {
			health.Failed(err)
			slog.Warn("operational health refresh failed", "error", err)
			return
		}
		health.Operational(snapshot)
	}
	refreshOperationalHealth()
	engine, err := indexer.New(indexer.Settings{
		ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID, Factory: deployment.Factory,
		StartBlock: int64(deployment.StartBlock), ChunkSize: int64(c.IndexerChunkSize), PollInterval: c.IndexerPollInterval,
		ReorgSearchDepth: c.IndexerReorgSearchDepth, ReorgRecoveryMode: c.IndexerReorgRecoveryMode,
		OnCommitted: health.Committed, OnFailure: health.Failed,
	}, store, source, discovery, decoder, router)
	if err != nil {
		return err
	}
	watchErrors := make(chan error, 1)
	go func() {
		if err := owner.Watch(ctx); err != nil && !errors.Is(err, context.Canceled) {
			health.OwnershipLost(err)
			watchErrors <- err
			stop()
		}
	}()
	go func() {
		ticker := time.NewTicker(c.IndexerPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refreshOperationalHealth()
			}
		}
	}()
	wake := make(chan struct{}, 1)
	healthServer := &http.Server{Addr: c.IndexerHealthAddr, Handler: indexer.HealthHandler(health)}
	go func() {
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("health server stopped", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = healthServer.Shutdown(shutdownCtx)
	}()
	aggregation := stats.Worker{Source: storepostgres.AggregationSource{Adapter: storepostgres.NewAdapter(pool), ChainID: int64(c.ChainID)}, WorkerID: c.IndexerWorkerID, PollInterval: stats.DefaultDirtyPollInterval, BatchSize: 32, Wake: wake, OnError: func(claim stats.Claim, err error) {
		health.Failed(err)
		slog.Error("aggregation compute failed", "chain_id", claim.ChainID, "token", fmt.Sprintf("%x", claim.Token), "error", err)
	}}
	go func() {
		if err := storepostgres.ListenMarketDirty(ctx, pool, wake); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("market dirty listener stopped", "error", err)
		}
	}()
	aggregationErrors := make(chan error, 1)
	go func() {
		if err := aggregation.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			health.Failed(err)
			aggregationErrors <- err
			stop()
		}
	}()
	slog.Info("indexer started", "chain_id", c.ChainID, "deployment_id", c.DeploymentID, "start_block", deployment.StartBlock,
		"reorg_search_depth", c.IndexerReorgSearchDepth, "reorg_recovery_mode", c.IndexerReorgRecoveryMode, "started_at", time.Now().UTC())
	runErr := engine.Run(ctx)
	select {
	case err := <-watchErrors:
		return fmt.Errorf("ownership watchdog: %w", err)
	case err := <-aggregationErrors:
		return fmt.Errorf("aggregation worker: %w", err)
	default:
	}
	return runErr
}

func initializeIndexerResources(ctx context.Context, source chain.RuntimeVerifier, configuredChainID uint64, deployment deployments.Deployment, openPool func() (*pgxpool.Pool, error), acquireOwnership func() (*storepostgres.Ownership, error)) (*pgxpool.Pool, *storepostgres.Ownership, error) {
	if err := chain.VerifyRPCChainID(ctx, source, configuredChainID, deployment); err != nil {
		return nil, nil, fmt.Errorf("verify indexer RPC chain identity before database startup: %w", err)
	}
	if err := chain.VerifyDeploymentBytecode(ctx, source, deployment); err != nil {
		return nil, nil, fmt.Errorf("verify indexer deployment bytecode before database startup: %w", err)
	}
	pool, err := openPool()
	if err != nil {
		return nil, nil, fmt.Errorf("open indexer database pool: %w", err)
	}
	owner, err := acquireOwnership()
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("acquire indexer writer ownership: %w", err)
	}
	return pool, owner, nil
}

func loadDeploymentRegistry() (*deployments.Registry, error) {
	if manifest := os.Getenv("DEPLOYMENT_MANIFEST_PATH"); manifest != "" {
		return deployments.LoadEmbeddedWithManifest(manifest)
	}
	return deployments.LoadEmbedded()
}
