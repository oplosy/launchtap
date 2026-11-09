// Command backfill-swap-sender fills pool_swaps.tx_from for swaps whose sender lookup failed (or
// that predate sender attribution) and marks the affected wallet positions for recompute.
//
// It is one-off and resumable: tx_from IS NULL is the cursor, so a rerun only touches what is
// still missing. Transactions the RPC cannot resolve are skipped for the run and the command
// exits non-zero.
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
	"time"

	"github.com/Contictus/launchtap/backend/deployments"
	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/config"
	"github.com/Contictus/launchtap/backend/internal/redact"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/swapbackfill"
)

const (
	defaultRate  = 5
	defaultBatch = 50
	maxBatch     = 100
	minRate      = 0.001
)

type flags struct {
	Rate   float64
	Batch  int
	DryRun bool
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
		Sleep:   rateSleeper(f.Rate),
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
	set.BoolVar(&f.DryRun, "dry-run", false, "fetch and log senders, write nothing")
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

func rateSleeper(rate float64) func(context.Context) error {
	interval := time.Duration(float64(time.Second) / rate)
	return func(ctx context.Context) error {
		timer := time.NewTimer(interval)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
}

func loadDeploymentRegistry() (*deployments.Registry, error) {
	if manifest := os.Getenv("DEPLOYMENT_MANIFEST_PATH"); manifest != "" {
		return deployments.LoadEmbeddedWithManifest(manifest)
	}
	return deployments.LoadEmbedded()
}
