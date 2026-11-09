// Command backfill-swap-sender fills pool_swaps.tx_from for swaps whose sender lookup failed (or
// that predate sender attribution) and marks the affected wallet positions for recompute.
//
// It is one-off and resumable: tx_from IS NULL is the cursor, so a rerun only touches what is
// still missing. Transactions the RPC cannot resolve are skipped for the run and the command
// exits non-zero.
//
// With -seed-positions it instead queues every (wallet, token) pair of the existing ledger
// history in position_dirty, so wallet positions cover activity from before the position queue
// existed. Run it after the sender backfill; it needs no RPC and is safe to rerun.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"syscall"

	"github.com/Contictus/launchtap/backend/deployments"
	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/config"
	"github.com/Contictus/launchtap/backend/internal/redact"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/swapbackfill"
	"github.com/ethereum/go-ethereum/common"
)

const (
	defaultRate  = swapbackfill.DefaultRate
	defaultBatch = swapbackfill.DefaultBatch
	maxBatch     = 100
	minRate      = 0.001
)

type flags struct {
	Rate          float64
	Batch         int
	DryRun        bool
	SeedPositions bool
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := execute(ctx, os.Args[1:], os.Getenv)
	stop()
	if err != nil {
		slog.Error("backfill-swap-sender failed", "error", redact.Error(err, os.Getenv("RPC_URL"), os.Getenv("DATABASE_URL")))
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string, getenv func(string) string) error {
	f, err := parseFlags(args)
	if err != nil {
		return err
	}
	c, err := config.Load(getenv)
	if err != nil {
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
	if f.SeedPositions {
		return seedPositions(ctx, c.DatabaseURL, int64(c.ChainID), deployment.Factory, f.DryRun)
	}
	source, err := chain.Dial(ctx, c.RPCURL, chain.RPCConfig{Timeout: c.RPCTimeout, MaxRetries: c.RPCMaxRetries, RetryBackoff: c.RPCRetryBackoff})
	if err != nil {
		return err
	}
	defer source.Close()
	// Never write senders read from an RPC that serves a different chain.
	if err := chain.VerifyRPCChainID(ctx, source, c.ChainID, deployment); err != nil {
		return fmt.Errorf("verify RPC chain identity: %w", err)
	}
	pool, err := storepostgres.OpenPool(ctx, c.DatabaseURL, storepostgres.PoolOptions{MaxConns: 2})
	if err != nil {
		return fmt.Errorf("open database pool: %w", err)
	}
	defer pool.Close()

	log := slog.Default()
	d := swapbackfill.Deps{
		Store:   swapbackfill.PostgresStore{Pool: pool, ChainID: int64(c.ChainID), Factory: deployment.Factory},
		Senders: source,
		Log:     log,
		Sleep:   swapbackfill.RateSleeper(f.Rate),
	}
	result, err := swapbackfill.Run(ctx, d, swapbackfill.Options{Batch: f.Batch, DryRun: f.DryRun})
	log.Info("backfill finished", "dry_run", f.DryRun, "transactions_listed", result.Listed,
		"swaps_updated", result.Updated, "transactions_resolved_dry_run", result.WouldUpdate, "transactions_skipped", result.Skipped)
	return err
}

func parseFlags(args []string) (flags, error) {
	var f flags
	set := flag.NewFlagSet("backfill-swap-sender", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.Float64Var(&f.Rate, "rate", defaultRate, "RPC requests per second")
	set.IntVar(&f.Batch, "batch", defaultBatch, "transaction hashes per RPC call (1-100)")
	set.BoolVar(&f.DryRun, "dry-run", false, "fetch and log senders (or count seed pairs), write nothing")
	set.BoolVar(&f.SeedPositions, "seed-positions", false, "queue every (wallet, token) pair of the existing ledger for position recompute instead of filling senders")
	if err := set.Parse(args); err != nil {
		return flags{}, fmt.Errorf("parse flags: %w", err)
	}
	if set.NArg() != 0 {
		return flags{}, fmt.Errorf("unexpected arguments: %v", set.Args())
	}
	if math.IsNaN(f.Rate) || math.IsInf(f.Rate, 0) || f.Rate < minRate {
		return flags{}, fmt.Errorf("-rate must be a finite number of at least %g", minRate)
	}
	if f.Batch < 1 || f.Batch > maxBatch {
		return flags{}, fmt.Errorf("-batch must be between 1 and %d", maxBatch)
	}
	return f, nil
}

// seedPositions queues every existing ledger (wallet, token) pair in one set-based statement
// (excluding the zero address, the factory and every curve and pair). It is idempotent.
func seedPositions(ctx context.Context, databaseURL string, chainID int64, factory common.Address, dryRun bool) error {
	pool, err := storepostgres.OpenPool(ctx, databaseURL, storepostgres.PoolOptions{MaxConns: 2})
	if err != nil {
		return fmt.Errorf("open database pool: %w", err)
	}
	defer pool.Close()
	seed, err := storepostgres.NewAdapter(pool).WithFactory(factory).SeedPositionDirty(ctx, chainID, dryRun)
	if err != nil {
		return err
	}
	slog.Info("position seed finished", "dry_run", dryRun, "pairs", seed.Pairs, "marked", seed.Marked)
	return nil
}

func loadDeploymentRegistry() (*deployments.Registry, error) {
	if manifest := os.Getenv("DEPLOYMENT_MANIFEST_PATH"); manifest != "" {
		return deployments.LoadEmbeddedWithManifest(manifest)
	}
	return deployments.LoadEmbedded()
}
