// Command telegram runs the Telegram alert bot (docs/specs/2026-11-12-telegram-alerts-design.md):
// it binds chats that press Start on a link code, unlinks on /stop, and sends each linked
// account's safe-block inbox events in chain order. One process per chain and deployment, held by
// a PostgreSQL advisory lock. Configuration: CHAIN_ID, DEPLOYMENT_ID, DATABASE_URL, LOG_LEVEL,
// TELEGRAM_BOT_TOKEN (secret), TELEGRAM_BOT_USERNAME, TELEGRAM_WEB_ORIGIN.
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

	"github.com/Contictus/launchtap/backend/internal/config"
	"github.com/Contictus/launchtap/backend/internal/redact"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/telegram"
)

// httpTimeout must exceed the 25 s getUpdates long-poll.
const httpTimeout = 35 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Getenv)
	stop()
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("telegram stopped", "error", redact.Error(err, os.Getenv("DATABASE_URL"), os.Getenv("TELEGRAM_BOT_TOKEN")))
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string) error {
	c, err := config.LoadTelegram(getenv)
	if err != nil {
		return err
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	chainID := int64(c.ChainID)

	lock, err := storepostgres.AcquireTelegramLock(ctx, c.DatabaseURL, chainID, c.DeploymentID)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = lock.Close(closeCtx)
	}()
	pool, err := storepostgres.OpenPool(ctx, c.DatabaseURL, storepostgres.PoolOptions{MaxConns: 4})
	if err != nil {
		return err
	}
	defer pool.Close()

	client := telegram.Client{BaseURL: telegram.DefaultBaseURL, Token: c.BotToken, HTTP: &http.Client{Timeout: httpTimeout}}
	worker := &telegram.Worker{
		Store: storepostgres.TelegramStore{Pool: pool}, Bot: client, ChainID: chainID, DeploymentID: c.DeploymentID,
		WebOrigin: c.WebOrigin, Logger: logger,
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, 2)
	go func() { errs <- worker.Run(ctx, client) }()
	go func() { errs <- lock.Watch(ctx) }()
	logger.InfoContext(ctx, "telegram worker started", "chain_id", chainID, "deployment_id", c.DeploymentID, "bot", c.BotUsername)
	err = <-errs
	cancel()
	<-errs
	return err
}
