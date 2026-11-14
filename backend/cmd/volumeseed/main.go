// Command volumeseed fills an empty, migrated database with a reproducible synthetic history at
// production-like volume, so read latency can be measured against realistic table sizes
// (docs/specs/2026-11-14-production-volume-measurement.md).
//
// It writes the event ledger (blocks, launches, curve trades, transfers, graduations and pair
// events of engine-1 tokens), rebuilds every token's projections with the same SQL function a
// re-index uses, and drains the token-stats and wallet-position queues with the indexer's own
// workers. It refuses a database that already holds chain data: it is for scratch databases only.
//
//	DATABASE_URL=postgres://... go run ./cmd/volumeseed -confirm-synthetic -deployment local-dev
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/Contictus/launchtap/backend/internal/stats"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "volumeseed:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	pr := &printer{w: out}
	flags := flag.NewFlagSet("volumeseed", flag.ContinueOnError)
	p := Params{End: time.Now().UTC().Truncate(time.Second)}
	flags.Int64Var(&p.ChainID, "chain-id", 31337, "chain id of the synthetic history")
	flags.IntVar(&p.Tokens, "tokens", 10_000, "token launches")
	flags.IntVar(&p.Wallets, "wallets", 50_000, "distinct trading wallets")
	flags.IntVar(&p.Positions, "positions", 500_000, "buys; about 40 % are later sold")
	flags.Uint64Var(&p.Seed, "seed", 20261114, "random seed")
	flags.DurationVar(&p.Span, "span", 30*24*time.Hour, "history length, ending now")
	deployment := flags.String("deployment", "", "deployment id written to sync_state (must match the API's DEPLOYMENT_ID)")
	workers := flags.Int("workers", 8, "concurrent projection and position workers")
	confirm := flags.Bool("confirm-synthetic", false, "required: confirms the target is a scratch database")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*confirm {
		return errors.New("-confirm-synthetic is required; this writes synthetic chain data")
	}
	if *deployment == "" {
		return errors.New("-deployment is required")
	}
	if *workers < 1 || *workers > 64 {
		return errors.New("-workers must be between 1 and 64")
	}
	if err := p.validate(); err != nil {
		return err
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := storepostgres.OpenPool(ctx, databaseURL, storepostgres.PoolOptions{MaxConns: int32(max(*workers+2, 4))})
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := requireEmpty(ctx, pool); err != nil {
		return err
	}

	started := time.Now()
	summary, err := writeLedger(ctx, pool, p, *deployment)
	if err != nil {
		return err
	}
	pr.printf("ledger: %d blocks, %d launches, %d curve trades, %d pool swaps, %d graduations, %d transfers (%s)\n",
		summary.Blocks, summary.Launches, summary.CurveTrades, summary.PoolSwaps, summary.Graduations, summary.Transfers, time.Since(started).Round(time.Second))

	started = time.Now()
	if err := rebuildProjections(ctx, pool, p.ChainID, *workers); err != nil {
		return err
	}
	pr.printf("projections rebuilt (%s)\n", time.Since(started).Round(time.Second))

	started = time.Now()
	if err := drainQueues(ctx, pool, p.ChainID, *workers); err != nil {
		return err
	}
	pr.printf("token stats and wallet positions computed (%s)\n", time.Since(started).Round(time.Second))
	if err := printSizes(ctx, pool, pr, summary); err != nil {
		return err
	}
	return pr.err
}

func requireEmpty(ctx context.Context, pool *pgxpool.Pool) error {
	var used bool
	err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sync_state) OR EXISTS (SELECT 1 FROM indexed_blocks) OR EXISTS (SELECT 1 FROM token_launches)`).Scan(&used)
	if err != nil {
		return fmt.Errorf("inspect target database (run migrate up first): %w", err)
	}
	if used {
		return errors.New("the target database already holds chain data; volumeseed only writes to an empty, migrated database")
	}
	return nil
}

func writeLedger(ctx context.Context, pool *pgxpool.Pool, p Params, deployment string) (Summary, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sink := newCopySink(ctx, tx, p.ChainID)
	summary, err := generate(p, sink)
	if err != nil {
		return Summary{}, err
	}
	if err := sink.flush(); err != nil {
		return Summary{}, err
	}
	last := summary.LastBlock
	if _, err := tx.Exec(ctx, `
		INSERT INTO sync_state (chain_id, deployment_id, observed_number, observed_hash, observed_at,
			safe_number, safe_hash, safe_at, finalized_number, finalized_hash, finalized_at)
		VALUES ($1, $2, $3, $4, $5, $3, $4, $5, $3, $4, $5)`,
		p.ChainID, deployment, last.Number, last.Hash[:], last.Time); err != nil {
		return Summary{}, fmt.Errorf("write sync state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("commit synthetic ledger: %w", err)
	}
	return summary, nil
}

// rebuildProjections runs rebuild_token_projections, the function a re-index uses, for every token.
func rebuildProjections(ctx context.Context, pool *pgxpool.Pool, chainID int64, workers int) error {
	rows, err := pool.Query(ctx, `SELECT token_address FROM token_launches WHERE chain_id = $1 ORDER BY block_number`, chainID)
	if err != nil {
		return err
	}
	var tokens [][]byte
	for rows.Next() {
		var token []byte
		if err := rows.Scan(&token); err != nil {
			rows.Close()
			return err
		}
		tokens = append(tokens, token)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	work := make(chan []byte)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for token := range work {
				if _, err := pool.Exec(ctx, `SELECT rebuild_token_projections($1, $2)`, chainID, token); err != nil {
					errs <- fmt.Errorf("rebuild projections of %x: %w", token, err)
					for range work {
					}
					return
				}
			}
		})
	}
	for _, token := range tokens {
		work <- token
	}
	close(work)
	wg.Wait()
	close(errs)
	return <-errs
}

// drainQueues marks every holder position dirty and runs the indexer's aggregation and position
// workers until both dirty queues are empty.
func drainQueues(ctx context.Context, pool *pgxpool.Pool, chainID int64, workers int) error {
	if _, err := pool.Exec(ctx, `
		INSERT INTO position_dirty (chain_id, wallet, token_address, generation)
		SELECT $1::bigint, side.wallet, side.token_address, nextval('position_dirty_generation_seq')
		FROM (
			SELECT token_address, to_address AS wallet FROM transfers WHERE chain_id = $1
			UNION
			SELECT token_address, from_address FROM transfers WHERE chain_id = $1
		) AS side
		WHERE side.wallet <> decode(repeat('00', 20), 'hex')
		  AND NOT EXISTS (
			SELECT 1 FROM tokens AS excluded
			WHERE excluded.chain_id = $1 AND (excluded.curve_address = side.wallet OR excluded.lp_pair = side.wallet)
		  )`, chainID); err != nil {
		return fmt.Errorf("mark positions dirty: %w", err)
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	fail := func(kind string) func(stats.Claim, error) {
		return func(claim stats.Claim, err error) {
			cancel(fmt.Errorf("%s compute for %x failed: %w", kind, claim.Token, err))
		}
	}
	adapter := storepostgres.NewAdapter(pool)
	runners := []stats.Worker{{
		Source: storepostgres.AggregationSource{Adapter: adapter, ChainID: chainID}, WorkerID: "volumeseed-stats",
		PollInterval: time.Second, BatchSize: 32, DrainFullBatches: true, OnError: fail("token stats"),
	}}
	for i := range workers {
		runners = append(runners, stats.Worker{
			Source: storepostgres.PositionSource{Adapter: adapter, ChainID: chainID}, WorkerID: fmt.Sprintf("volumeseed-positions-%d", i),
			PollInterval: time.Second, BatchSize: 64, DrainFullBatches: true, OnError: fail("position"),
		})
	}
	var wg sync.WaitGroup
	for _, worker := range runners {
		wg.Go(func() {
			if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				cancel(err)
			}
		})
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return context.Cause(ctx)
		case <-ticker.C:
		}
		var remaining int64
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM aggregation_dirty WHERE chain_id = $1) + (SELECT count(*) FROM position_dirty WHERE chain_id = $1)`, chainID).Scan(&remaining); err != nil {
			cancel(err)
			continue
		}
		slog.Info("draining dirty queues", "remaining", remaining)
		if remaining == 0 {
			cancel(nil)
			wg.Wait()
			return nil
		}
	}
}

func printSizes(ctx context.Context, pool *pgxpool.Pool, pr *printer, summary Summary) error {
	pr.printf("table rows:\n")
	for _, table := range []string{"tokens", "trades", "pool_swaps", "transfers", "holder_balances", "candles", "token_stats", "wallet_positions", "wallet_realized_legs"} {
		var rows int64
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&rows); err != nil {
			return fmt.Errorf("count %s: %w", table, err)
		}
		pr.printf("  %-22s %d\n", table, rows)
	}
	pr.printf("probe targets:\n  hottest token      0x%x\n  quiet curve token  0x%x\n  busiest wallet     0x%x\n",
		summary.HottestToken, summary.QuietCurveToken, summary.BusiestWallet)
	return nil
}

// printer writes progress lines and keeps the first write error.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) printf(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, args...)
	}
}
