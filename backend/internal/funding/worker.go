package funding

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/Contictus/launchtap/backend/internal/redact"
	"github.com/ethereum/go-ethereum/common"
)

// Store is the worker's view of the database.
type Store interface {
	// FinalizedHead is the indexer's finalized block; known is false until finality is observed.
	FinalizedHead(ctx context.Context) (block uint64, known bool, err error)
	// AwaitingWallets lists early wallets delivered at or below finalized with no funding row,
	// leaving out skip.
	AwaitingWallets(ctx context.Context, finalized uint64, limit int, skip []common.Address) ([]common.Address, error)
	// Save writes the wallet's row once; a row that already exists is kept.
	Save(ctx context.Context, wallet common.Address, r Result) error
}

// MaxBackoff caps how long a wallet whose resolution keeps failing waits between attempts.
const MaxBackoff = 6 * time.Hour

// backoff is the wait after the n-th consecutive failure: one minute, doubling, capped.
func backoff(failures int) time.Duration {
	wait := time.Minute
	for i := 1; i < failures && wait < MaxBackoff; i++ {
		wait *= 2
	}
	return min(wait, MaxBackoff)
}

type retry struct {
	failures int
	next     time.Time
}

// Worker resolves waiting early wallets in bounded passes. A wallet whose resolution fails stays
// waiting but is held back with an exponential backoff, so a wallet the RPC cannot answer never
// takes a pass slot from the others. The backoff lives in memory; a restart retries at once.
// Logged errors never carry URLs: the RPC URL holds the provider key.
type Worker struct {
	Store     Store
	Chain     Chain
	BatchSize int
	Interval  time.Duration
	// WalletTimeout bounds one wallet's resolution; zero means no bound.
	WalletTimeout time.Duration
	Logger        *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	retries map[common.Address]retry
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// heldBack lists wallets still inside their backoff, in address order.
func (w *Worker) heldBack(now time.Time) []common.Address {
	var out []common.Address
	for wallet, r := range w.retries {
		if now.Before(r.next) {
			out = append(out, wallet)
		}
	}
	slices.SortFunc(out, func(a, b common.Address) int { return bytes.Compare(a[:], b[:]) })
	return out
}

// Pass resolves up to BatchSize waiting wallets against the finalized head and returns how many
// it saved. Only a store failure is returned as an error.
func (w *Worker) Pass(ctx context.Context) (int, error) {
	head, known, err := w.Store.FinalizedHead(ctx)
	if err != nil || !known {
		return 0, err
	}
	wallets, err := w.Store.AwaitingWallets(ctx, head, w.BatchSize, w.heldBack(w.now()))
	if err != nil {
		return 0, err
	}
	resolved := 0
	for _, wallet := range wallets {
		result, err := w.resolve(ctx, wallet, head)
		if err != nil {
			if ctx.Err() != nil {
				return resolved, ctx.Err()
			}
			if w.retries == nil {
				w.retries = map[common.Address]retry{}
			}
			r := w.retries[wallet]
			r.failures++
			r.next = w.now().Add(backoff(r.failures))
			w.retries[wallet] = r
			w.Logger.WarnContext(ctx, "funding resolution failed", "wallet", wallet.Hex(), "failures", r.failures,
				"retry_at", r.next, "error", redact.Error(err))
			continue
		}
		delete(w.retries, wallet)
		if err := w.Store.Save(ctx, wallet, result); err != nil {
			return resolved, err
		}
		resolved++
	}
	return resolved, nil
}

func (w *Worker) resolve(ctx context.Context, wallet common.Address, head uint64) (Result, error) {
	if w.WalletTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, w.WalletTimeout)
		defer cancel()
	}
	return Resolve(ctx, w.Chain, wallet, head)
}

// Run makes a pass every Interval until ctx ends. Store failures are logged and retried.
func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()
	for {
		resolved, err := w.Pass(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			w.Logger.ErrorContext(ctx, "funding pass failed", "error", redact.Error(err))
		} else if resolved > 0 {
			w.Logger.InfoContext(ctx, "funding pass", "resolved", resolved)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
