package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Contictus/launchtap/backend/internal/config"
	"github.com/Contictus/launchtap/backend/internal/secretenv"
	"github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/migrations"
)

type migrationExecutor func(context.Context, string, migrations.Command) ([]migrations.Result, error)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr, executeMigrations))
}

func run(
	ctx context.Context,
	args []string,
	getenv func(string) string,
	stdout io.Writer,
	stderr io.Writer,
	execute migrationExecutor,
) int {
	if len(args) != 1 {
		return writeFailure(stderr, 2, "usage: migrate <up|down|status>")
	}
	command, err := migrations.ParseCommand(args[0])
	if err != nil {
		return writeFailure(stderr, 2, "%v", err)
	}
	env, err := secretenv.Lookup(getenv, "DATABASE_PASSWORD")
	if err != nil {
		return writeFailure(stderr, 1, "load database configuration: %v", err)
	}
	databaseConfig, err := config.LoadDatabase(env)
	if err != nil {
		return writeFailure(stderr, 1, "load database configuration: %v", err)
	}
	results, err := execute(ctx, databaseConfig.DatabaseURL, command)
	if err != nil {
		return writeFailure(stderr, 1, "migration failed: %v", err)
	}
	if err := writeResults(stdout, command, results); err != nil {
		return writeFailure(stderr, 1, "write migration output: %v", err)
	}
	return 0
}

func executeMigrations(
	ctx context.Context,
	databaseURL string,
	command migrations.Command,
) (results []migrations.Result, err error) {
	database, err := postgres.Open(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	defer func() {
		err = errors.Join(err, database.Close())
	}()

	if err := database.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return migrations.Run(ctx, database, command)
}

func writeResults(output io.Writer, command migrations.Command, results []migrations.Result) error {
	if len(results) == 0 {
		_, err := fmt.Fprintf(output, "%s: no changes\n", command)
		return err
	}
	for _, result := range results {
		if command == migrations.CommandStatus {
			if _, err := fmt.Fprintf(output, "%05d %-7s %s\n", result.Version, result.State, result.Source); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(output, "%05d %s %s\n", result.Version, result.Direction, result.Source); err != nil {
			return err
		}
	}
	return nil
}

func writeFailure(output io.Writer, exitCode int, format string, args ...any) int {
	if _, err := fmt.Fprintf(output, format+"\n", args...); err != nil {
		return 1
	}
	return exitCode
}
