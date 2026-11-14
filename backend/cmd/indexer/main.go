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
	"github.com/Contictus/launchtap/backend/internal/metrics"
	"github.com/Contictus/launchtap/backend/internal/reconcile"
	"github.com/Contictus/launchtap/backend/internal/redact"
	"github.com/Contictus/launchtap/backend/internal/stats"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/swapbackfill"
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
	health := new(indexer.HealthTracker)
	metricsRegistry := metrics.NewRegistry(metrics.ComponentIndexer)
	indexerMetrics := metrics.NewIndexer(metricsRegistry, health.Snapshot)
	source, err := chain.Dial(ctx, c.RPCURL, chain.RPCConfig{Timeout: c.RPCTimeout, MaxRetries: c.RPCMaxRetries, RetryBackoff: c.RPCRetryBackoff, Observer: indexerMetrics})
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
	discovery, err := newDiscovery(source, decoder, deployment, c.IndexerLogAddressBatchSize)
	if err != nil {
		return err
	}
	if discovery, err = discovery.WithLogConcurrency(int(c.IndexerLogFetchConcurrency)); err != nil {
		return err
	}
	store := storepostgres.IndexerStore{Pool: pool, Beginner: owner.Beginner(), Owner: owner, ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID, Factory: deployment.Factory, OnNotify: indexerMetrics.Notified}
	router := indexer.LedgerRouter{ChainID: int64(c.ChainID)}
	health.Set(indexer.Health{ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID, OwnershipHeld: true, RPCHealthy: true})
	healthStore := storepostgres.NewAdapter(pool)
	if deployment.EngineVersion == 2 {
		// Holder views exclude the chain's PoolManager for engine-2 tokens; newDiscovery has
		// already required the manifest's V4 section.
		if err := healthStore.RegisterV4PoolManager(ctx, int64(c.ChainID), deployment.V4.PoolManager); err != nil {
			return err
		}
	}
	refreshOperationalHealth := func() {
		snapshot, err := healthStore.ReadOperationalHealth(ctx, int64(c.ChainID), c.DeploymentID)
		health.Database(err == nil)
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
		OnCommitted: health.Committed, OnFailure: health.Failed, OnHeads: health.ChainHead,
		Senders: source, OnSenderMisses: indexerMetrics.SenderLookupMissed,
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
	// One market_dirty NOTIFY must wake both aggregation workers, so the listener feeds a fan-out
	// instead of a single receiver channel.
	wake := make(chan struct{}, 1)
	positionWake := make(chan struct{}, 1)
	notified := make(chan struct{}, 1)
	go fanOutWake(ctx, notified, wake, positionWake)
	healthMux := http.NewServeMux()
	healthMux.Handle("/healthz", indexer.HealthHandler(health))
	healthMux.Handle("/metrics", metrics.Handler(metricsRegistry))
	healthServer := &http.Server{Addr: c.IndexerHealthAddr, Handler: healthMux}
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
	aggregation := stats.Worker{Source: storepostgres.AggregationSource{Adapter: storepostgres.NewAdapter(pool), ChainID: int64(c.ChainID)}, WorkerID: c.IndexerWorkerID, PollInterval: stats.DefaultDirtyPollInterval, BatchSize: 32, Wake: wake, OnComputed: indexerMetrics.Recomputed, OnError: func(claim stats.Claim, err error) {
		health.Failed(err)
		slog.Error("aggregation compute failed", "chain_id", claim.ChainID, "token", fmt.Sprintf("%x", claim.Token), "error", err)
	}}
	go func() {
		if err := storepostgres.ListenMarketDirty(ctx, pool, notified); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("market dirty listener stopped", "error", err)
		}
	}()
	sweeper := stats.WindowSweeper{Source: storepostgres.AggregationSource{Adapter: storepostgres.NewAdapter(pool), ChainID: int64(c.ChainID)}, ChainID: int64(c.ChainID), Interval: stats.DefaultSweepInterval, Window: stats.DefaultSweepWindow, Overlap: stats.DefaultSweepOverlap,
		Wake: func() {
			select {
			case wake <- struct{}{}:
			default:
			}
		},
		OnError: func(err error) { slog.Error("volume window sweep failed", "chain_id", c.ChainID, "error", err) }}
	go func() {
		if err := sweeper.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("volume window sweeper stopped", "error", err)
		}
	}()
	positions := stats.Worker{Source: storepostgres.PositionSource{Adapter: storepostgres.NewAdapter(pool), ChainID: int64(c.ChainID)}, WorkerID: c.IndexerWorkerID + "-positions", PollInterval: stats.DefaultDirtyPollInterval, BatchSize: 64, DrainFullBatches: true, Wake: positionWake, OnError: func(claim stats.Claim, err error) {
		indexerMetrics.PositionFailed()
		slog.Error("position compute failed", "chain_id", claim.ChainID, "error", err)
	}}
	// Retries sender lookups that missed during ingest, only up to the committed safe head. It
	// uses the shared pool (never the ownership connection) and never stops ingest.
	backfillStore := swapbackfill.PostgresStore{Pool: pool, ChainID: int64(c.ChainID), Factory: deployment.Factory, DeploymentID: c.DeploymentID}
	senderBackfill := swapbackfill.Scheduler{
		Deps: swapbackfill.Deps{
			Store:   backfillStore,
			Senders: source,
			Log:     slog.Default(),
			Sleep:   swapbackfill.RateSleeper(swapbackfill.DefaultRate),
		},
		Interval: c.IndexerSenderBackfillInterval, Batch: swapbackfill.DefaultBatch, MaxTransactions: int(c.IndexerSenderBackfillMaxTxs),
		SafeBlock: backfillStore.SafeBlock, Backlog: backfillStore.CountUnattributed, OnBacklog: indexerMetrics.SenderBackfillPending,
		OnFilled: indexerMetrics.SenderBackfillFilled, OnError: indexerMetrics.SenderBackfillFailed,
		Secrets: []string{c.RPCURL, c.DatabaseURL},
	}
	// Joined on return, before the deferred pool.Close, so no pass uses a closed pool.
	senderBackfillDone := make(chan struct{})
	go func() {
		defer close(senderBackfillDone)
		_ = senderBackfill.Run(ctx)
	}()
	// Compares sampled projections with chain reads at the observed block and reports drift
	// (docs/specs/2026-11-14-state-reconciliation-preflight.md). Read-only; never stops ingest.
	reconciler := reconcile.Worker{
		Store: storepostgres.ReconcileSource{Pool: pool, ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID},
		Chain: chain.StateReader{Client: source},
		OnResult: func(result reconcile.Result) {
			indexerMetrics.Reconciled(result)
			for _, mismatch := range result.Mismatches {
				slog.Error("indexed state differs from chain", "block", result.Block, "check", mismatch.Check, "token", mismatch.Token.Hex(), "subject", mismatch.Subject.Hex(), "indexed", mismatch.Indexed, "chain", mismatch.Chain)
			}
		},
		OnError: func(err error) {
			slog.Warn("state reconciliation pass failed; retrying next interval", "error", redact.Error(err, c.RPCURL, c.DatabaseURL))
		},
	}
	reconcilerDone := make(chan struct{})
	go func() {
		defer close(reconcilerDone)
		_ = reconciler.Run(ctx)
	}()
	defer func() {
		stop()
		<-senderBackfillDone
		<-reconcilerDone
	}()
	aggregationErrors := make(chan error, 2)
	for _, worker := range []stats.Worker{aggregation, positions} {
		go func() {
			if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				health.Failed(err)
				aggregationErrors <- err
				stop()
			}
		}()
	}
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

// fanOutWake forwards each wake signal from in to every out channel without blocking: a full
// channel already holds a pending wake.
func fanOutWake(ctx context.Context, in <-chan struct{}, outs ...chan<- struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-in:
			for _, out := range outs {
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}
}

// newDiscovery builds the deployment engine's discoverer. Engine 1 checks every launch pair
// against the Uniswap V2 factory; engine 2 checks every launch and graduated pool against the
// manifest's V4 section.
func newDiscovery(source *chain.Client, decoder *chain.Decoder, deployment deployments.Deployment, addressBatchSize uint64) (*chain.Discoverer, error) {
	if deployment.EngineVersion == 2 {
		if deployment.V4 == nil {
			return nil, errors.New("engine-2 deployment has no V4 section")
		}
		return chain.NewDiscovererV2(source, decoder, deployment.Factory, addressBatchSize, chain.V4Config{PoolManager: deployment.V4.PoolManager, Locker: deployment.V4.Locker, Hook: deployment.V4.Hook})
	}
	return chain.NewDiscovererWithPairVerification(source, decoder, deployment.Factory, addressBatchSize, func(ctx context.Context, launch chain.TokenLaunched) error {
		return chain.VerifyLaunchPair(ctx, source, deployment.UniV2Factory, deployment.WETH, deployment.PairInitCodeHash, launch)
	})
}
