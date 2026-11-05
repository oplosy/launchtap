package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// HighlightsReader serves trending tokens and the king of the hill from one snapshot-bound
// read so both lists, the snapshot identity and the finality label agree with each other.
type HighlightsReader struct {
	Pool         PoolReadBeginner
	DeploymentID string
}

func (r HighlightsReader) Highlights(ctx context.Context, chainID int64, windowStart time.Time, limit int) (token.Highlights, error) {
	if limit < 1 || limit > 100 {
		return token.Highlights{}, fmt.Errorf("highlights limit must be between 1 and 100")
	}
	var out token.Highlights
	err := withReadSnapshotBeginner(ctx, r.Pool, chainID, r.DeploymentID, func(ctx context.Context, adapter *Adapter, snapshot ReadSnapshot) error {
		rows, err := adapter.queries.ListTrendingTokens(ctx, sqlc.ListTrendingTokensParams{
			ChainID: chainID, RowLimit: int32(limit), WindowStart: pgtype.Timestamptz{Time: windowStart.UTC(), Valid: true},
		})
		if err != nil {
			return err
		}
		out.Trending = make([]token.TrendingToken, 0, len(rows))
		for _, row := range rows {
			out.Trending = append(out.Trending, token.TrendingToken{
				Summary: token.Summary{
					Address: common.Address(row.TokenAddress), Name: row.Name, Symbol: row.Symbol, Phase: row.Phase,
					LaunchTime: row.LaunchBlockTime.Time, LaunchBlock: row.LaunchBlockNumber,
					TotalSupply: row.TotalSupply.BigInt(), MarketCapETH: row.MarketCapEthWad.BigInt(),
					Volume24hETH: row.Volume24hEthWad.BigInt(), HolderCount: row.HolderCount,
				},
				WindowVolumeETH: row.WindowVolumeEthWad.BigInt(),
				DistinctBuyers:  row.DistinctBuyers,
			})
		}
		king, err := adapter.queries.GetKingOfTheHill(ctx, chainID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		default:
			if len(king.LaunchBlockHash) != 32 {
				return fmt.Errorf("king of the hill launch block hash has %d bytes, want 32", len(king.LaunchBlockHash))
			}
			out.King = &token.KingOfTheHill{
				Summary: token.Summary{
					Address: common.Address(king.TokenAddress), Name: king.Name, Symbol: king.Symbol, Phase: king.Phase,
					LaunchTime: king.LaunchBlockTime.Time, LaunchBlock: king.LaunchBlockNumber,
					TotalSupply: king.TotalSupply.BigInt(), MarketCapETH: king.MarketCapEthWad.BigInt(),
					Volume24hETH: king.Volume24hEthWad.BigInt(), HolderCount: king.HolderCount,
				},
				GraduationProgressBPS: king.GraduationProgressBps,
				RemainingETH:          king.RemainingEthWad.BigInt(),
			}
		}
		out.WindowStart = windowStart.UTC()
		out.Snapshot = snapshot.Identity
		out.Finality = finality(snapshot.State, snapshot.Identity.BlockNumber)
		return nil
	})
	return out, err
}
