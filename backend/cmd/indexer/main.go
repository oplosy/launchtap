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
	"github.com/Contictus/launchtap/backend/internal/secretenv"
	"github.com/Contictus/launchtap/backend/internal/stats"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/swapbackfill"
	"github.com/Contictus/launchtap/backend/internal/wallet"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		slog.Error("indexer stopped", "error", redact.Error(err, os.Getenv("RPC_URL"), os.Getenv("RPC_FALLBACK_URL"), os.Getenv("DATABASE_URL")))
		os.Exit(indexer.ExitCode(err))
	}
}
func run() error {
	env, err := secretenv.Lookup(os.Getenv, "DATABASE_PASSWORD")
	if err != nil {
		return err
	}
	c, err := config.Load(env)
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
	engines, err := resolveEngineDeployments(registry, c.ChainID, c.EngineDeploymentIDs, c.IndexerConfirmations)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fallback, err := config.LoadRPCFallback(env, c.RPCURL)
	if err != nil {
		return err
	}
	health := &indexer.HealthTracker{Render: func(err error) string { return redact.Error(err, c.RPCURL, fallback.URL, c.DatabaseURL) }}
	metricsRegistry := metrics.NewRegistry(metrics.ComponentIndexer)
	indexerMetrics := metrics.NewIndexer(metricsRegistry, health.Snapshot)
	rpcConfig := chain.RPCConfig{Timeout: c.RPCTimeout, MaxRetries: c.RPCMaxRetries, RetryBackoff: c.RPCRetryBackoff, Observer: indexerMetrics}
	source, err := dialIndexerRPC(ctx, c.RPCURL, fallback, rpcConfig, c.ChainID, deployment, func(ctx context.Context, client *chain.Client) error {
		if err := verifyFallbackRPC(ctx, client, c.ChainID, deployment); err != nil {
			return err
		}
		return verifyOtherEngines(ctx, client, c.DeploymentID, engines)
	})
	if err != nil {
		return err
	}
	defer source.Close()
	// The site deployment is checked by dialIndexerRPC or initializeIndexerResources; every other
	// engine's bytecode and governance are checked here, still before the database opens.
	if err := verifyOtherEngines(ctx, source, c.DeploymentID, engines); err != nil {
		return err
	}
	// With a fallback, dialIndexerRPC already ran the identity checks on the provider started on.
	pool, owner, err := initializeIndexerResources(ctx, source, !fallback.Enabled(), c.ChainID, deployment,
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
	records := make([]storepostgres.EngineDeployment, 0, len(engines))
	for _, engine := range engines {
		records = append(records, storepostgres.EngineDeployment{DeploymentID: engine.DeploymentID, EngineVersion: engine.EngineVersion, Factory: engine.Factory, StartBlock: int64(engine.StartBlock)})
	}
	if err := owner.RecordEngineDeployments(ctx, int64(c.ChainID), c.DeploymentID, records); err != nil {
		return err
	}
	engineSources := make([]indexer.EngineSource, 0, len(engines))
	factories := make([]common.Address, 0, len(engines))
	startBlock := engines[0].StartBlock
	for _, engine := range engines {
		decoder, err := chain.NewDecoder(engine.EngineVersion)
		if err != nil {
			return err
		}
		discovery, err := newDiscovery(source, decoder, engine, c.IndexerLogAddressBatchSize)
		if err != nil {
			return err
		}
		if discovery, err = discovery.WithLogConcurrency(int(c.IndexerLogFetchConcurrency)); err != nil {
			return err
		}
		engineSources = append(engineSources, indexer.EngineSource{Discovery: discovery, Decoder: decoder, Factory: engine.Factory})
		factories = append(factories, engine.Factory)
		startBlock = min(startBlock, engine.StartBlock)
	}
	store := storepostgres.IndexerStore{Pool: pool, Beginner: owner.Beginner(), Owner: owner, ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID, Factories: factories, OnNotify: indexerMetrics.Notified}
	router := indexer.LedgerRouter{ChainID: int64(c.ChainID)}
	health.Set(indexer.Health{ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID, OwnershipHeld: true, RPCHealthy: true})
	healthStore := storepostgres.NewAdapter(pool)
	for _, engine := range engines {
		if engine.EngineVersion == 2 {
			// Holder views exclude the chain's PoolManager for engine-2 tokens; newDiscovery has
			// already required the manifest's V4 section.
			if err := healthStore.RegisterV4PoolManager(ctx, int64(c.ChainID), engine.V4.PoolManager); err != nil {
				return err
			}
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
	engine, err := indexer.NewWithEngines(indexer.Settings{
		ChainID: int64(c.ChainID), DeploymentID: c.DeploymentID,
		StartBlock: int64(startBlock), ChunkSize: int64(c.IndexerChunkSize), PollInterval: c.IndexerPollInterval,
		ReorgSearchDepth: c.IndexerReorgSearchDepth, ReorgRecoveryMode: c.IndexerReorgRecoveryMode,
		OnCommitted: health.Committed, OnFailure: health.Failed, OnHeads: health.ChainHead,
		Senders: source, OnSenderMisses: indexerMetrics.SenderLookupMissed,
	}, store, source, engineSources, router)
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
	// Re-ranks the 7d and 30d leaderboards so reads do not aggregate every realized leg
	// (docs/specs/2026-11-14-windowed-leaderboard-preflight.md). Never stops ingest.
	leaderboards := stats.LeaderboardRefresher{Source: storepostgres.LeaderboardWindowSource{Adapter: storepostgres.NewAdapter(pool), ChainID: int64(c.ChainID)},
		Windows: []int{int(wallet.Window7d), int(wallet.Window30d)}, Interval: stats.DefaultLeaderboardRefreshInterval,
		OnError: func(windowDays int, err error) {
			slog.Error("leaderboard window refresh failed", "chain_id", c.ChainID, "window_days", windowDays, "error", err)
		}}
	go func() {
		if err := leaderboards.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("leaderboard refresher stopped", "error", err)
		}
	}()
	positions := stats.Worker{Source: storepostgres.PositionSource{Adapter: storepostgres.NewAdapter(pool), ChainID: int64(c.ChainID)}, WorkerID: c.IndexerWorkerID + "-positions", PollInterval: stats.DefaultDirtyPollInterval, BatchSize: 64, DrainFullBatches: true, Wake: positionWake, OnError: func(claim stats.Claim, err error) {
		indexerMetrics.PositionFailed()
		slog.Error("position compute failed", "chain_id", claim.ChainID, "error", err)
	}}
	// Retries sender lookups that missed during ingest, only up to the committed safe head. It
	// uses the shared pool (never the ownership connection) and never stops ingest.
	backfillStore := swapbackfill.PostgresStore{Pool: pool, ChainID: int64(c.ChainID), Factories: factories, DeploymentID: c.DeploymentID}
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
		Secrets: []string{c.RPCURL, fallback.URL, c.DatabaseURL},
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
				attrs := []any{"block", result.Block, "check", mismatch.Check, "token", mismatch.Token.Hex(), "subject", mismatch.Subject.Hex(), "indexed", mismatch.Indexed, "chain", mismatch.Chain}
				if mismatch.Pool != (common.Hash{}) {
					attrs = append(attrs, "pool", mismatch.Pool.Hex())
				}
				slog.Error("indexed state differs from chain", attrs...)
			}
		},
		OnError: func(err error) {
			slog.Warn("state reconciliation pass failed; retrying next interval", "error", redact.Error(err, c.RPCURL, fallback.URL, c.DatabaseURL))
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
	slog.Info("indexer started", "chain_id", c.ChainID, "deployment_id", c.DeploymentID, "engine_deployment_ids", c.EngineDeploymentIDs, "start_block", startBlock,
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

// initializeIndexerResources verifies the RPC identity (unless verifyRPC is false because the
// caller already did) and then opens the database pool and writer ownership.
func initializeIndexerResources(ctx context.Context, source chain.RuntimeVerifier, verifyRPC bool, configuredChainID uint64, deployment deployments.Deployment, openPool func() (*pgxpool.Pool, error), acquireOwnership func() (*storepostgres.Ownership, error)) (*pgxpool.Pool, *storepostgres.Ownership, error) {
	if verifyRPC {
		if err := chain.VerifyRPCChainID(ctx, source, configuredChainID, deployment); err != nil {
			return nil, nil, fmt.Errorf("verify indexer RPC chain identity before database startup: %w", err)
		}
		if err := chain.VerifyDeploymentBytecode(ctx, source, deployment); err != nil {
			return nil, nil, fmt.Errorf("verify indexer deployment bytecode before database startup: %w", err)
		}
		if err := chain.VerifyGovernanceTimelock(ctx, source, deployment); err != nil {
			return nil, nil, fmt.Errorf("verify governance timelock before database startup: %w", err)
		}
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

// dialIndexerRPC dials the primary provider and, when configured, a fallback that must pass the
// same chain-id, bytecode, governance and finality-tag checks before the indexer starts
// (docs/specs/2026-11-17-rpc-failover-preflight.md).
func dialIndexerRPC(ctx context.Context, primaryURL string, fallback config.RPCFallbackConfig, rpcConfig chain.RPCConfig, chainID uint64, deployment deployments.Deployment, verifyFallback func(context.Context, *chain.Client) error) (*chain.Client, error) {
	primary, err := chain.Dial(ctx, primaryURL, rpcConfig)
	if err != nil || !fallback.Enabled() {
		return primary, err
	}
	secondary, err := chain.Dial(ctx, fallback.URL, rpcConfig)
	if err != nil {
		primary.Close()
		return nil, fmt.Errorf("dial fallback RPC: %w", err)
	}
	if err := verifyFallback(ctx, secondary); err != nil {
		primary.Close()
		secondary.Close()
		return nil, fmt.Errorf("verify fallback RPC before database startup: %w", err)
	}
	// An unreachable primary starts the indexer on the fallback; a primary that answers with the
	// wrong chain or bytecode is a misconfiguration and still refuses to start.
	startOnFallback := false
	if err := verifyPrimaryRPC(ctx, primary, chainID, deployment); err != nil {
		if !chain.IsProviderUnavailable(err) {
			primary.Close()
			secondary.Close()
			return nil, fmt.Errorf("verify indexer RPC before database startup: %w", err)
		}
		startOnFallback = true
		slog.Warn("primary RPC unreachable at startup; starting on the fallback", "error", redact.Error(err, primaryURL, fallback.URL))
	}
	combined, err := chain.WithFallback(primary, secondary, chain.FailoverConfig{ChainID: chainID, AfterFailures: fallback.AfterFailures, ProbeInterval: fallback.ProbeInterval, StartOnFallback: startOnFallback})
	if err != nil {
		primary.Close()
		secondary.Close()
		return nil, err
	}
	slog.Info("RPC failover enabled", "after_failures", fallback.AfterFailures, "probe_interval", fallback.ProbeInterval)
	return combined, nil
}

// verifyPrimaryRPC runs the identity checks on the primary and decides which provider the
// indexer starts on; initializeIndexerResources does not repeat them when a fallback is set.
func verifyPrimaryRPC(ctx context.Context, source chain.RuntimeVerifier, chainID uint64, deployment deployments.Deployment) error {
	if err := chain.VerifyRPCChainID(ctx, source, chainID, deployment); err != nil {
		return err
	}
	if err := chain.VerifyDeploymentBytecode(ctx, source, deployment); err != nil {
		return err
	}
	return chain.VerifyGovernanceTimelock(ctx, source, deployment)
}

type fallbackVerifier interface {
	chain.RuntimeVerifier
	Heads(context.Context) (chain.Heads, error)
}

func verifyFallbackRPC(ctx context.Context, source fallbackVerifier, chainID uint64, deployment deployments.Deployment) error {
	if err := chain.VerifyRPCChainID(ctx, source, chainID, deployment); err != nil {
		return err
	}
	if err := chain.VerifyDeploymentBytecode(ctx, source, deployment); err != nil {
		return err
	}
	if err := chain.VerifyGovernanceTimelock(ctx, source, deployment); err != nil {
		return err
	}
	if _, err := source.Heads(ctx); err != nil {
		return fmt.Errorf("read fallback finality heads: %w", err)
	}
	return nil
}

// resolveEngineDeployments resolves every engine manifest the indexer ingests on chainID. At most
// one manifest may serve each engine version (docs/specs/2026-11-20-dual-engine-preflight.md).
func resolveEngineDeployments(registry *deployments.Registry, chainID uint64, ids []string, confirmations *uint64) ([]deployments.Deployment, error) {
	if len(ids) == 0 {
		return nil, errors.New("no engine deployment configured")
	}
	resolved := make([]deployments.Deployment, 0, len(ids))
	versions := make(map[uint16]string, len(ids))
	for _, id := range ids {
		engine, err := registry.Resolve(chainID, id, confirmations)
		if err != nil {
			return nil, fmt.Errorf("resolve engine deployment %q: %w", id, err)
		}
		if other, taken := versions[engine.EngineVersion]; taken {
			return nil, fmt.Errorf("engine deployments %q and %q both serve engine %d", other, id, engine.EngineVersion)
		}
		versions[engine.EngineVersion] = id
		resolved = append(resolved, engine)
	}
	return resolved, nil
}

// verifyOtherEngines checks the bytecode and governance of every engine deployment other than
// the site deployment, which the RPC identity checks already cover.
func verifyOtherEngines(ctx context.Context, source chain.RuntimeVerifier, siteDeploymentID string, engines []deployments.Deployment) error {
	for _, engine := range engines {
		if engine.DeploymentID == siteDeploymentID {
			continue
		}
		if err := chain.VerifyDeploymentBytecode(ctx, source, engine); err != nil {
			return fmt.Errorf("verify engine deployment %q bytecode: %w", engine.DeploymentID, err)
		}
		if err := chain.VerifyGovernanceTimelock(ctx, source, engine); err != nil {
			return fmt.Errorf("verify engine deployment %q governance timelock: %w", engine.DeploymentID, err)
		}
	}
	return nil
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
