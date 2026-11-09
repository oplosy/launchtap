package postgres

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Contictus/launchtap/backend/internal/position"
	"github.com/Contictus/launchtap/backend/internal/stats"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// PositionSource drains position_dirty for one chain and writes the wallet_positions and
// wallet_totals projection. It implements stats.DirtySource.
type PositionSource struct {
	Adapter *Adapter
	ChainID int64
}

var _ stats.DirtySource = PositionSource{}

// Claim leases up to batch dirty (wallet, token) pairs of s.ChainID, including pairs whose
// generation advanced or whose 30s lease lapsed, mirroring ClaimAggregationDirty.
func (s PositionSource) Claim(ctx context.Context, worker string, batch int32) ([]stats.Claim, error) {
	rows, err := s.Adapter.queries.ClaimPositionDirty(ctx, sqlc.ClaimPositionDirtyParams{
		WorkerID: pgtype.Text{String: worker, Valid: true}, ChainID: s.ChainID, BatchSize: batch,
	})
	if err != nil {
		return nil, fmt.Errorf("claim dirty positions: %w", err)
	}
	claims := make([]stats.Claim, 0, len(rows))
	for _, row := range rows {
		if !row.ClaimedGeneration.Valid {
			return nil, fmt.Errorf("claim dirty positions: returned NULL claimed generation")
		}
		claims = append(claims, stats.Claim{
			ChainID: row.ChainID, Wallet: [20]byte(row.Wallet), Token: [20]byte(row.TokenAddress),
			Generation: row.ClaimedGeneration.Int64,
		})
	}
	return claims, nil
}

// Compute recomputes one (wallet, token) position from the ledger and then the wallet's totals,
// in one transaction. A pair with no legs loses its position row, and the wallet loses its totals
// row when no position remains.
func (s PositionSource) Compute(ctx context.Context, claim stats.Claim) error {
	if s.Adapter.beginner != nil {
		return WithinTx(ctx, s.Adapter.beginner, func(ctx context.Context, transaction *Adapter) error {
			return transaction.recomputePosition(ctx, claim)
		})
	}
	return s.Adapter.recomputePosition(ctx, claim)
}

// Complete removes the claimed row only while this worker still owns the claimed generation.
func (s PositionSource) Complete(ctx context.Context, claim stats.Claim, worker string) (bool, error) {
	rows, err := s.Adapter.queries.CompletePositionDirty(ctx, sqlc.CompletePositionDirtyParams{
		ChainID: claim.ChainID, Wallet: sqlc.Address(claim.Wallet), TokenAddress: sqlc.Address(claim.Token),
		ClaimedGeneration: claim.Generation, WorkerID: pgtype.Text{String: worker, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("complete dirty position: %w", err)
	}
	return rows == 1, nil
}

func (adapter *Adapter) recomputePosition(ctx context.Context, claim stats.Claim) error {
	wallet, token := claim.Wallet[:], claim.Token[:]
	if err := adapter.queries.LockWalletTotals(ctx, sqlc.LockWalletTotalsParams{ChainID: claim.ChainID, Wallet: wallet}); err != nil {
		return fmt.Errorf("lock wallet totals: %w", err)
	}
	legs, err := adapter.positionLegs(ctx, claim.ChainID, wallet, token)
	if err != nil {
		return err
	}
	key := sqlc.DeleteWalletPositionParams{ChainID: claim.ChainID, Wallet: sqlc.Address(claim.Wallet), TokenAddress: sqlc.Address(claim.Token)}
	if len(legs) == 0 {
		if err := adapter.queries.DeleteWalletPosition(ctx, key); err != nil {
			return fmt.Errorf("delete wallet position: %w", err)
		}
	} else {
		computed, err := position.Compute(legs)
		if err != nil {
			return fmt.Errorf("compute position %x/%x: %w", wallet, token, err)
		}
		arg, err := walletPositionParams(key, computed)
		if err != nil {
			return err
		}
		if err := adapter.queries.UpsertWalletPosition(ctx, arg); err != nil {
			return fmt.Errorf("upsert wallet position: %w", err)
		}
	}
	if err := adapter.queries.RecomputeWalletTotals(ctx, sqlc.RecomputeWalletTotalsParams{ChainID: claim.ChainID, Wallet: wallet}); err != nil {
		return fmt.Errorf("recompute wallet totals: %w", err)
	}
	return nil
}

// positionLegs loads the per-transaction legs of ListPositionLegs. Sign checks are left to
// position.Compute (ErrInvalidLeg), which rejects a negative ETH value.
func (adapter *Adapter) positionLegs(ctx context.Context, chainID int64, wallet, token []byte) ([]position.Leg, error) {
	rows, err := adapter.queries.ListPositionLegs(ctx, sqlc.ListPositionLegsParams{ChainID: chainID, TokenAddress: token, Wallet: wallet})
	if err != nil {
		return nil, fmt.Errorf("list position legs: %w", err)
	}
	legs := make([]position.Leg, len(rows))
	for i, row := range rows {
		delta, err := signedBigInt("position leg token delta", row.LegTokenDelta)
		if err != nil {
			return nil, err
		}
		paid, err := signedBigInt("position leg ETH paid", row.LegEthPaid)
		if err != nil {
			return nil, err
		}
		received, err := signedBigInt("position leg ETH received", row.LegEthReceived)
		if err != nil {
			return nil, err
		}
		legs[i] = position.Leg{Block: row.LegBlockNumber, TxIndex: row.LegTransactionIndex, TokenDelta: delta, ETHPaid: paid, ETHReceived: received}
	}
	return legs, nil
}

func walletPositionParams(key sqlc.DeleteWalletPositionParams, p position.Position) (sqlc.UpsertWalletPositionParams, error) {
	unsigned := []struct {
		name  string
		value *big.Int
	}{
		{"position balance", p.Balance}, {"position remaining cost", p.RemainingCost}, {"position transferred in", p.TransferredIn},
		{"position ETH paid", p.ETHPaid}, {"position ETH received", p.ETHReceived},
	}
	converted := make([]sqlc.Uint256, len(unsigned))
	for i, field := range unsigned {
		var err error
		if converted[i], err = uint256(field.name, field.value); err != nil {
			return sqlc.UpsertWalletPositionParams{}, err
		}
	}
	realized, err := signedNumeric("position realized PnL", p.RealizedPnL)
	if err != nil {
		return sqlc.UpsertWalletPositionParams{}, err
	}
	return sqlc.UpsertWalletPositionParams{
		ChainID: key.ChainID, Wallet: key.Wallet, TokenAddress: key.TokenAddress,
		Balance: converted[0], RemainingCost: converted[1], RealizedPnl: realized, TransferredIn: converted[2],
		EthPaid: converted[3], EthReceived: converted[4], TradeCount: p.TradeCount,
		FirstBlockNumber: p.FirstBlock, LastBlockNumber: p.LastBlock,
	}, nil
}
