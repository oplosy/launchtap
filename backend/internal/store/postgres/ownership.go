package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/jackc/pgx/v5"
)

var ErrOwnershipLost = indexer.ErrOwnershipLost
var ErrOwnershipBusy = errors.New("indexer ownership already held")

const OwnershipProbeInterval = time.Second
const ownershipProbeTimeout = 2 * time.Second
const ownedTransactionTimeout = 5 * time.Minute

// Ownership is a session lock and its only write connection. Serializing all
// writers on this connection fences a dead owner at the server, even before its
// idle heartbeat notices the loss. A hash collision only reduces concurrency.
type Ownership struct {
	mu   sync.Mutex
	conn *pgx.Conn
	dead error
	pid  uint32
}

func AcquireOwnership(ctx context.Context, databaseURL string, chainID int64, deploymentID string) (*Ownership, error) {
	if chainID <= 0 || deploymentID == "" {
		return nil, errors.New("ownership scope is required")
	}
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open ownership connection: %w", err)
	}
	// The lock is chain-scoped, not deployment-scoped: the ledger and reorg deletes are keyed by
	// chain, so two deployments must never write one chain at the same time (pre-flight B2).
	// deploymentID is still required so every owner states which deployment it serves.
	scope := sha256.Sum256(fmt.Appendf(nil, "launchpad/indexer/chain/%d", chainID))
	key := int64(binary.BigEndian.Uint64(scope[:8]))
	var acquired bool
	err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&acquired)
	if err != nil || !acquired {
		closeCtx, cancel := context.WithTimeout(context.Background(), ownershipProbeTimeout)
		defer cancel()
		closeErr := conn.Close(closeCtx)
		if err == nil {
			err = ErrOwnershipBusy
		}
		return nil, errors.Join(err, closeErr)
	}
	return &Ownership{conn: conn, pid: conn.PgConn().PID()}, nil
}

func (o *Ownership) BackendPID() uint32 { return o.pid }

func (o *Ownership) Beginner() TransactionBeginner { return o.conn }

func (o *Ownership) DBTX() DBTX { return o.conn }

func (o *Ownership) WithinTx(ctx context.Context, fn func(context.Context, *Adapter) error) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.dead != nil {
		return o.dead
	}
	ctx, cancel := context.WithTimeout(ctx, ownedTransactionTimeout)
	defer cancel()
	err := WithinTx(ctx, o.conn, fn)
	var outcome *CommitOutcomeError
	if o.conn.IsClosed() || errors.As(err, &outcome) {
		o.dead = errors.Join(ErrOwnershipLost, err)
	}
	return err
}

func (o *Ownership) Probe(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.dead != nil {
		return o.dead
	}
	ctx, cancel := context.WithTimeout(ctx, ownershipProbeTimeout)
	defer cancel()
	if err := o.conn.Ping(ctx); err != nil {
		o.dead = errors.Join(ErrOwnershipLost, err)
	}
	return o.dead
}

func (o *Ownership) Watch(ctx context.Context) error {
	ticker := time.NewTicker(OwnershipProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := o.Probe(ctx); err != nil {
				return err
			}
		}
	}
}

func (o *Ownership) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), ownershipProbeTimeout)
	defer cancel()
	o.dead = ErrOwnershipLost
	return o.conn.Close(ctx)
}
