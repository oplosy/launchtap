// Package swapbackfill fills pool_swaps.tx_from and v4_swaps.tx_from for swaps whose sender
// lookup failed (or that predate sender attribution) and marks the affected wallet positions
// (v1) or token aggregates (V4) for recompute.
//
// It is resumable: tx_from IS NULL is the cursor, so a rerun only touches what is still missing.
package swapbackfill

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"

	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSkipped marks a run that finished but left unresolved transactions without a sender.
var ErrSkipped = errors.New("some transactions could not be resolved")

// Options are the per-run settings; the request rate is the caller's Sleep.
type Options struct {
	Batch  int
	DryRun bool
	// ThroughBlock, when set, limits the run to swaps in blocks at or below it (the indexer
	// passes its safe head so a pass never touches blocks that ingest may still reorg). nil
	// means no upper bound.
	ThroughBlock *int64
	// MaxTransactions caps the transactions one run lists; 0 means no cap.
	MaxTransactions int
	// Start, when set, resumes the walk strictly after this (block, hash) position instead of
	// at the oldest NULL-sender transaction.
	Start *Cursor
	// QuietUnresolved logs each unresolved transaction at Debug instead of Warn (the indexer's
	// scheduler retries them every pass and logs one summary line instead).
	QuietUnresolved bool
}

// Cursor is a (block, hash) keyset position in the NULL-sender transaction walk.
type Cursor struct {
	Block int64
	Hash  common.Hash
}

// SwapTx is one transaction that still has a v1 pool swap or a V4 swap without a known sender.
type SwapTx struct {
	Hash  common.Hash
	Block int64
}

// Store is the database side of the backfill.
type Store interface {
	// List returns up to limit distinct NULL-sender transactions in blocks at or below
	// throughBlock, strictly after the cursor.
	List(ctx context.Context, throughBlock, afterBlock int64, afterHash common.Hash, limit int) ([]SwapTx, error)
	// Attribute fills the senders and marks the affected positions dirty in one transaction.
	// It returns the number of swap rows updated.
	Attribute(ctx context.Context, senders map[common.Hash]common.Address) (int, error)
}

// SenderSource resolves transaction senders; unresolved hashes are absent from the result.
type SenderSource interface {
	TransactionSenders(ctx context.Context, hashes []common.Hash) (map[common.Hash]common.Address, error)
}

// Deps are the collaborators of one run.
type Deps struct {
	Store   Store
	Senders SenderSource
	Log     *slog.Logger
	// Sleep waits out the per-request rate interval; it returns early with the context error.
	Sleep func(context.Context) error
}

// Summary counts what one run did.
type Summary struct {
	Listed      int
	Attributed  int // transactions whose sender was written
	Updated     int // swap rows written
	WouldUpdate int // transactions resolved in dry-run mode
	Skipped     int
	// Cursor is the last listed position (Start, or the initial position, when nothing was
	// listed); ReachedEnd is true when the walk ran out of rows rather than hitting the cap.
	Cursor     Cursor
	ReachedEnd bool
}

// Run walks the NULL-sender transactions with a (block, hash) keyset cursor, so an unresolved
// transaction is passed over once and a single run always terminates, in dry-run mode too. It
// returns ErrSkipped (wrapped) after finishing if any transaction could not be resolved.
func Run(ctx context.Context, d Deps, o Options) (Summary, error) {
	var result Summary
	var afterBlock int64 = -1
	var afterHash common.Hash
	if o.Start != nil {
		afterBlock, afterHash = o.Start.Block, o.Start.Hash
	}
	result.Cursor = Cursor{Block: afterBlock, Hash: afterHash}
	unresolvedLevel := slog.LevelWarn
	if o.QuietUnresolved {
		unresolvedLevel = slog.LevelDebug
	}
	throughBlock := int64(math.MaxInt64)
	if o.ThroughBlock != nil {
		throughBlock = *o.ThroughBlock
	}
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		limit := o.Batch
		if o.MaxTransactions > 0 {
			remaining := o.MaxTransactions - result.Listed
			if remaining <= 0 {
				break
			}
			limit = min(limit, remaining)
		}
		batch, err := d.Store.List(ctx, throughBlock, afterBlock, afterHash, limit)
		if err != nil {
			return result, err
		}
		if len(batch) == 0 {
			result.ReachedEnd = true
			break
		}
		last := batch[len(batch)-1]
		afterBlock, afterHash = last.Block, last.Hash
		result.Cursor = Cursor{Block: last.Block, Hash: last.Hash}
		result.Listed += len(batch)

		hashes := make([]common.Hash, len(batch))
		for index, row := range batch {
			hashes[index] = row.Hash
		}
		senders, err := d.Senders.TransactionSenders(ctx, hashes)
		if err != nil {
			return result, fmt.Errorf("fetch transaction senders: %w", err)
		}
		resolved := make(map[common.Hash]common.Address, len(senders))
		for _, row := range batch {
			sender, ok := senders[row.Hash]
			if !ok {
				result.Skipped++
				d.Log.Log(ctx, unresolvedLevel, "transaction sender unresolved, skipped", "tx_hash", row.Hash.Hex(), "block_number", row.Block)
				continue
			}
			resolved[row.Hash] = sender
		}
		if o.DryRun {
			result.WouldUpdate += len(resolved)
			for _, row := range batch {
				if sender, ok := resolved[row.Hash]; ok {
					d.Log.Info("dry run: would set sender", "tx_hash", row.Hash.Hex(), "sender", sender.Hex())
				}
			}
		} else if len(resolved) > 0 {
			updated, err := d.Store.Attribute(ctx, resolved)
			if err != nil {
				return result, err
			}
			result.Attributed += len(resolved)
			result.Updated += updated
			d.Log.Info("batch attributed", "transactions", len(resolved), "swaps_updated", updated, "block_number", last.Block)
		}
		if err := d.Sleep(ctx); err != nil {
			return result, err
		}
		if len(batch) < limit {
			result.ReachedEnd = true
			break
		}
	}
	if result.Skipped > 0 {
		return result, fmt.Errorf("%w: %d skipped, their swaps keep tx_from NULL", ErrSkipped, result.Skipped)
	}
	return result, nil
}

// PostgresStore is the PostgreSQL Store for one chain.
type PostgresStore struct {
	Pool    *pgxpool.Pool
	ChainID int64
	// Factory is excluded from position marking (R4).
	Factory common.Address
	// DeploymentID selects the indexer sync state SafeBlock reads.
	DeploymentID string
}

// CountUnattributed counts the chain's v1 pool swaps and V4 swaps with a NULL sender in blocks
// at or below throughBlock.
func (s PostgresStore) CountUnattributed(ctx context.Context, throughBlock int64) (int64, error) {
	return storepostgres.NewAdapter(s.Pool).CountUnattributedSwaps(ctx, s.ChainID, throughBlock)
}

// SafeBlock returns the indexer's committed safe block for the chain and deployment; ok is
// false until the indexer has committed one.
func (s PostgresStore) SafeBlock(ctx context.Context) (int64, bool, error) {
	state, err := storepostgres.NewAdapter(s.Pool).ReadState(ctx, s.ChainID, s.DeploymentID)
	if err != nil || state.Safe == nil {
		return 0, false, err
	}
	return state.Safe.BlockNumber, true, nil
}

func (s PostgresStore) List(ctx context.Context, throughBlock, afterBlock int64, afterHash common.Hash, limit int) ([]SwapTx, error) {
	rows, err := storepostgres.NewAdapter(s.Pool).ListUnattributedSwapTxs(ctx, s.ChainID, throughBlock, afterBlock, afterHash, int32(limit))
	if err != nil {
		return nil, err
	}
	out := make([]SwapTx, len(rows))
	for index, row := range rows {
		out[index] = SwapTx{Hash: row.TxHash, Block: row.BlockNumber}
	}
	return out, nil
}

func (s PostgresStore) Attribute(ctx context.Context, senders map[common.Hash]common.Address) (int, error) {
	hashes := make([]common.Hash, 0, len(senders))
	for hash := range senders {
		hashes = append(hashes, hash)
	}
	sort.Slice(hashes, func(i, j int) bool { return string(hashes[i][:]) < string(hashes[j][:]) })
	updated := 0
	err := storepostgres.WithinTx(ctx, s.Pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
		adapter = adapter.WithFactory(s.Factory)
		for _, hash := range hashes {
			n, err := adapter.AttributeSwapSender(ctx, s.ChainID, hash, senders[hash])
			if err != nil {
				return err
			}
			updated += n
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return updated, nil
}
