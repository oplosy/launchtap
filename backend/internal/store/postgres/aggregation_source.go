package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Contictus/launchtap/backend/internal/stats"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgtype"
)

type AggregationSource struct {
	Adapter *Adapter
	ChainID int64
}

func (s AggregationSource) Claim(ctx context.Context, worker string, batch int32) ([]stats.Claim, error) {
	var claims []DirtyClaim
	err := func() error {
		var err error
		claims, err = s.Adapter.ClaimAggregationDirty(ctx, worker, batch)
		return err
	}()
	if err != nil {
		return nil, err
	}
	result := make([]stats.Claim, 0, len(claims))
	for _, claim := range claims {
		result = append(result, stats.Claim{ChainID: claim.ChainID, Token: [20]byte(claim.TokenAddress), Generation: claim.ClaimedGeneration})
	}
	return result, nil
}
func (s AggregationSource) Compute(ctx context.Context, claim stats.Claim) error {
	return s.ComputeBatch(ctx, []stats.Claim{claim})[0]
}

func (s AggregationSource) ComputeBatch(ctx context.Context, claims []stats.Claim) []error {
	results := make([]error, len(claims))
	chains := make([]int64, 0, len(claims))
	claimsByChain := make(map[int64][]int, len(claims))
	for i, claim := range claims {
		if _, exists := claimsByChain[claim.ChainID]; !exists {
			chains = append(chains, claim.ChainID)
		}
		claimsByChain[claim.ChainID] = append(claimsByChain[claim.ChainID], i)
	}
	computedByChain := make(map[int64][]int, len(chains))
	for _, chainID := range chains {
		indexes := claimsByChain[chainID]
		tokens := make([]common.Address, len(indexes))
		for n, i := range indexes {
			tokens[n] = common.Address(claims[i].Token)
		}
		// One statement per chain; if it fails, recompute one token at a time so a bad token
		// fails only its own claim.
		if len(indexes) > 1 {
			err := s.Adapter.RecomputeTokensStats(ctx, chainID, tokens)
			if err == nil {
				computedByChain[chainID] = indexes
				continue
			}
			slog.WarnContext(ctx, "batch token stats recompute failed; retrying per token", "chain_id", chainID, "tokens", len(tokens), "error", err)
		}
		for n, i := range indexes {
			if err := s.Adapter.RecomputeTokenStats(ctx, chainID, tokens[n]); err != nil {
				results[i] = err
				continue
			}
			computedByChain[chainID] = append(computedByChain[chainID], i)
		}
	}
	for _, chainID := range chains {
		if len(computedByChain[chainID]) == 0 {
			continue
		}
		if err := s.Adapter.RefreshProtocolAggregates(ctx, chainID); err != nil {
			for _, i := range computedByChain[chainID] {
				results[i] = err
			}
		}
	}
	return results
}
func (s AggregationSource) Complete(ctx context.Context, claim stats.Claim, worker string) (bool, error) {
	var completed bool
	err := func() error {
		var err error
		completed, err = s.Adapter.CompleteAggregationDirty(ctx, DirtyClaim{ChainID: claim.ChainID, TokenAddress: Address(claim.Token), ClaimedGeneration: claim.Generation}, worker)
		return err
	}()
	return completed, err
}

// MarkExpiringWindow marks every token with a canonical trade or DEX swap in
// [from, to) dirty so its rolling-window stats are recomputed.
func (s AggregationSource) MarkExpiringWindow(ctx context.Context, chainID int64, from, to time.Time) (int64, error) {
	marked, err := s.Adapter.queries.MarkTokensDirtyInWindow(ctx, sqlc.MarkTokensDirtyInWindowParams{
		ChainID:    chainID,
		WindowFrom: pgtype.Timestamptz{Time: from, Valid: true},
		WindowTo:   pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("mark expiring window: %w", err)
	}
	return marked, nil
}

// MarkAllWithVolume marks every token with a non-zero 24h volume dirty.
func (s AggregationSource) MarkAllWithVolume(ctx context.Context, chainID int64) (int64, error) {
	marked, err := s.Adapter.queries.MarkTokensWithVolumeDirty(ctx, chainID)
	if err != nil {
		return 0, fmt.Errorf("mark tokens with volume dirty: %w", err)
	}
	return marked, nil
}

// DatabaseNow returns the database clock, which the stats recompute uses for its 24h window.
func (s AggregationSource) DatabaseNow(ctx context.Context) (time.Time, error) {
	now, err := s.Adapter.queries.DatabaseNow(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("read database clock: %w", err)
	}
	return now.Time, nil
}
