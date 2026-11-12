package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrTelegramWorkerBusy means another Telegram worker already serves the deployment.
var ErrTelegramWorkerBusy = errors.New("telegram worker already running for this deployment")

// TelegramLock is the session advisory lock that keeps one Telegram worker per chain and
// deployment: two workers would send duplicates and conflict on getUpdates.
type TelegramLock struct {
	conn *pgx.Conn
}

func AcquireTelegramLock(ctx context.Context, databaseURL string, chainID int64, deploymentID string) (*TelegramLock, error) {
	if chainID <= 0 || deploymentID == "" {
		return nil, errors.New("telegram lock scope is required")
	}
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open telegram lock connection: %w", err)
	}
	scope := sha256.Sum256(fmt.Appendf(nil, "launchpad/telegram/%d/%s", chainID, deploymentID))
	var acquired bool
	err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, int64(binary.BigEndian.Uint64(scope[:8]))).Scan(&acquired)
	if err != nil || !acquired {
		closeCtx, cancel := context.WithTimeout(context.Background(), ownershipProbeTimeout)
		defer cancel()
		closeErr := conn.Close(closeCtx)
		if err == nil {
			err = ErrTelegramWorkerBusy
		}
		return nil, errors.Join(err, closeErr)
	}
	return &TelegramLock{conn: conn}, nil
}

// Watch pings the lock connection every second and returns when it is lost or ctx ends.
func (l *TelegramLock) Watch(ctx context.Context) error {
	ticker := time.NewTicker(OwnershipProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			probeCtx, cancel := context.WithTimeout(ctx, ownershipProbeTimeout)
			err := l.conn.Ping(probeCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				return fmt.Errorf("telegram lock connection lost: %w", err)
			}
		}
	}
}

// Close releases the lock by closing its session.
func (l *TelegramLock) Close(ctx context.Context) error {
	return l.conn.Close(ctx)
}
