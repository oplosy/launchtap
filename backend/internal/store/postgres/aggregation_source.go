package postgres

import (
	"context"
	"fmt"
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
	computedByChain := make(map[int64][]int, len(claims))
	for i, claim := range claims {
		if err := s.Adapter.RecomputeTokenStats(ctx, claim.ChainID, common.Address(claim.Token)); err != nil {
			results[i] = err
			continue
		}
		if _, exists := computedByChain[claim.ChainID]; !exists {
			chains = append(chains, claim.ChainID)
		}
		computedByChain[claim.ChainID] = append(computedByChain[claim.ChainID], i)
	}
	for _, chainID := range chains {
		if err := s.Adapter.RecomputeProtocolAggregates(ctx, chainID); err != nil {
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
