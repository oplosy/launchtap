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

// ErrWorkerBusy means another worker of the same kind already serves the deployment.
var ErrWorkerBusy = errors.New("worker already running for this deployment")

// WorkerLock is a session advisory lock that keeps one optional worker of a kind per chain and
// deployment: two Telegram workers would send duplicates and conflict on getUpdates; two funding
// workers would spend the RPC budget twice.
type WorkerLock struct {
	conn *pgx.Conn
	kind string
}

// AcquireTelegramLock holds the cmd/telegram singleton lock.
func AcquireTelegramLock(ctx context.Context, databaseURL string, chainID int64, deploymentID string) (*WorkerLock, error) {
	return acquireWorkerLock(ctx, databaseURL, "telegram", chainID, deploymentID)
}

// AcquireFundingLock holds the cmd/funding singleton lock.
func AcquireFundingLock(ctx context.Context, databaseURL string, chainID int64, deploymentID string) (*WorkerLock, error) {
	return acquireWorkerLock(ctx, databaseURL, "funding", chainID, deploymentID)
}

func acquireWorkerLock(ctx context.Context, databaseURL, kind string, chainID int64, deploymentID string) (*WorkerLock, error) {
	if chainID <= 0 || deploymentID == "" {
		return nil, fmt.Errorf("%s lock scope is required", kind)
	}
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open %s lock connection: %w", kind, err)
	}
	scope := sha256.Sum256(fmt.Appendf(nil, "launchpad/%s/%d/%s", kind, chainID, deploymentID))
	var acquired bool
	err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, int64(binary.BigEndian.Uint64(scope[:8]))).Scan(&acquired)
	if err != nil || !acquired {
		closeCtx, cancel := context.WithTimeout(context.Background(), ownershipProbeTimeout)
		defer cancel()
		closeErr := conn.Close(closeCtx)
		if err == nil {
			err = fmt.Errorf("%s: %w", kind, ErrWorkerBusy)
		}
		return nil, errors.Join(err, closeErr)
	}
	return &WorkerLock{conn: conn, kind: kind}, nil
}

// Watch pings the lock connection every second and returns when it is lost or ctx ends.
func (l *WorkerLock) Watch(ctx context.Context) error {
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
				return fmt.Errorf("%s lock connection lost: %w", l.kind, err)
			}
		}
	}
}

// Close releases the lock by closing its session.
func (l *WorkerLock) Close(ctx context.Context) error {
	return l.conn.Close(ctx)
}
