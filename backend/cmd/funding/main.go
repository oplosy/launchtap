// Command funding resolves who sent each early wallet its first ETH
// (docs/specs/2026-11-12-funding-source-clusters-design.md). It is optional: without it the
// early-buyer clusters show Level 1 links only. One process per chain and deployment, held by a
// PostgreSQL advisory lock. Configuration: CHAIN_ID, DEPLOYMENT_ID, DATABASE_URL, LOG_LEVEL,
// FUNDING_RPC_URL (secret; an archive node with debug_traceBlockByNumber).
//
// `funding requeue-unknown` deletes the chain's unknown rows once and exits; the running worker
// then resolves those wallets again with the current rule
// (docs/specs/2026-11-16-owner-decisions-preflight.md).
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Contictus/launchtap/backend/internal/config"
	"github.com/Contictus/launchtap/backend/internal/funding"
	"github.com/Contictus/launchtap/backend/internal/redact"
	"github.com/Contictus/launchtap/backend/internal/secretenv"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

const (
	passInterval = 30 * time.Second
	passSize     = 20
	// walletTimeout bounds one wallet's resolution (about 60 calls plus one block trace).
	walletTimeout = 2 * time.Minute
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Getenv)
	stop()
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("funding stopped", "error", redact.Error(err, os.Getenv("DATABASE_URL"), os.Getenv("FUNDING_RPC_URL")))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	requeue := len(args) == 1 && args[0] == "requeue-unknown"
	if len(args) > 0 && !requeue {
		return errors.New("usage: funding [requeue-unknown]")
	}
	env, err := secretenv.Lookup(getenv, "DATABASE_PASSWORD")
	if err != nil {
		return err
	}
	c, err := config.LoadFunding(env)
	if err != nil {
		return err
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	chainID := int64(c.ChainID)
	if requeue {
		return requeueUnknown(ctx, logger, c.DatabaseURL, chainID, c.DeploymentID)
	}

	lock, err := storepostgres.AcquireFundingLock(ctx, c.DatabaseURL, chainID, c.DeploymentID)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = lock.Close(closeCtx)
	}()
	pool, err := storepostgres.OpenPool(ctx, c.DatabaseURL, storepostgres.PoolOptions{MaxConns: storepostgres.MinimumPoolMaxConns})
	if err != nil {
		return err
	}
	defer pool.Close()
	client, err := gethrpc.DialOptions(ctx, c.RPCURL)
	if err != nil {
		return err
	}
	defer client.Close()

	worker := &funding.Worker{
		Store:     storepostgres.FundingStore{Pool: pool, ChainID: chainID, DeploymentID: c.DeploymentID},
		Chain:     funding.RPC{Client: client},
		BatchSize: passSize, Interval: passInterval, WalletTimeout: walletTimeout, Logger: logger,
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, 2)
	go func() { errs <- worker.Run(ctx) }()
	go func() { errs <- lock.Watch(ctx) }()
	logger.InfoContext(ctx, "funding worker started", "chain_id", chainID, "deployment_id", c.DeploymentID)
	err = <-errs
	cancel()
	<-errs
	return err
}

func requeueUnknown(ctx context.Context, logger *slog.Logger, databaseURL string, chainID int64, deploymentID string) error {
	pool, err := storepostgres.OpenPool(ctx, databaseURL, storepostgres.PoolOptions{MaxConns: storepostgres.MinimumPoolMaxConns})
	if err != nil {
		return err
	}
	defer pool.Close()
	deleted, err := storepostgres.FundingStore{Pool: pool, ChainID: chainID, DeploymentID: deploymentID}.RequeueUnknown(ctx)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "requeued unknown wallet funding", "chain_id", chainID, "deleted", deleted)
	return nil
}
