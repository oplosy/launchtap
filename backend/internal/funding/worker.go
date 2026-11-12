package funding

import (
	"context"
	"log/slog"
	"time"

	"github.com/Contictus/launchtap/backend/internal/redact"
	"github.com/ethereum/go-ethereum/common"
)

// Store is the worker's view of the database.
type Store interface {
	// FinalizedHead is the indexer's finalized block; known is false until finality is observed.
	FinalizedHead(ctx context.Context) (block uint64, known bool, err error)
	// AwaitingWallets lists early wallets delivered at or below finalized with no funding row.
	AwaitingWallets(ctx context.Context, finalized uint64, limit int) ([]common.Address, error)
	// Save writes the wallet's row once; a row that already exists is kept.
	Save(ctx context.Context, wallet common.Address, r Result) error
}

// Worker resolves waiting early wallets in bounded passes. A wallet whose resolution fails stays
// waiting and is retried on a later pass. Logged errors never carry URLs: the RPC URL holds the
// provider key.
type Worker struct {
	Store     Store
	Chain     Chain
	BatchSize int
	Interval  time.Duration
	// WalletTimeout bounds one wallet's resolution; zero means no bound.
	WalletTimeout time.Duration
	Logger        *slog.Logger
}

// Pass resolves up to BatchSize waiting wallets against the finalized head and returns how many
// it saved. Only a store failure is returned as an error.
func (w Worker) Pass(ctx context.Context) (int, error) {
	head, known, err := w.Store.FinalizedHead(ctx)
	if err != nil || !known {
		return 0, err
	}
	wallets, err := w.Store.AwaitingWallets(ctx, head, w.BatchSize)
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
			w.Logger.WarnContext(ctx, "funding resolution failed", "wallet", wallet.Hex(), "error", redact.Error(err))
			continue
		}
		if err := w.Store.Save(ctx, wallet, result); err != nil {
			return resolved, err
		}
		resolved++
	}
	return resolved, nil
}

func (w Worker) resolve(ctx context.Context, wallet common.Address, head uint64) (Result, error) {
	if w.WalletTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, w.WalletTimeout)
		defer cancel()
	}
	return Resolve(ctx, w.Chain, wallet, head)
}

// Run makes a pass every Interval until ctx ends. Store failures are logged and retried.
func (w Worker) Run(ctx context.Context) error {
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
