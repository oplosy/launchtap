package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/earlybuyer"
	"github.com/Contictus/launchtap/backend/internal/funding"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FundingStore is cmd/funding's store (docs/specs/2026-11-12-funding-source-clusters-design.md).
type FundingStore struct {
	Pool         *pgxpool.Pool
	ChainID      int64
	DeploymentID string
}

var _ funding.Store = FundingStore{}

func (s FundingStore) FinalizedHead(ctx context.Context) (uint64, bool, error) {
	head, err := sqlc.New(s.Pool).GetFundingFinalizedHead(ctx, sqlc.GetFundingFinalizedHeadParams{ChainID: s.ChainID, DeploymentID: s.DeploymentID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !head.Valid) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read finalized head: %w", err)
	}
	if head.Int64 < 0 {
		return 0, false, fmt.Errorf("finalized head %d is negative", head.Int64)
	}
	return uint64(head.Int64), true, nil
}

func (s FundingStore) AwaitingWallets(ctx context.Context, finalized uint64, limit int, skip []common.Address) ([]common.Address, error) {
	skipped := make([][]byte, len(skip))
	for i, w := range skip {
		skipped[i] = w.Bytes()
	}
	rows, err := sqlc.New(s.Pool).ListWalletsAwaitingFunding(ctx, sqlc.ListWalletsAwaitingFundingParams{
		ChainID: s.ChainID, FinalizedBlock: int64(finalized), WindowSeconds: earlybuyer.WindowSeconds, PageSize: int32(limit),
		Skip: skipped,
	})
	if err != nil {
		return nil, fmt.Errorf("list wallets awaiting funding: %w", err)
	}
	out := make([]common.Address, len(rows))
	for i, row := range rows {
		out[i] = common.Address(row)
	}
	return out, nil
}

func (s FundingStore) Save(ctx context.Context, wallet common.Address, r funding.Result) error {
	params := sqlc.InsertWalletFundingParams{
		ChainID: s.ChainID, Wallet: sqlc.Address(wallet), Status: string(r.Status), CheckedBlock: int64(r.CheckedBlock),
	}
	switch r.Status {
	case funding.StatusFunded:
		funder, tx := sqlc.Address(r.Funder), sqlc.Hash(r.TxHash)
		params.Funder, params.FundingTx = &funder, &tx
		params.FundedBlock = pgtype.Int8{Int64: int64(r.Block), Valid: true}
	case funding.StatusUnknown:
		if r.Block > 0 {
			params.FundedBlock = pgtype.Int8{Int64: int64(r.Block), Valid: true}
		}
	}
	if err := sqlc.New(s.Pool).InsertWalletFunding(ctx, params); err != nil {
		return fmt.Errorf("save wallet funding: %w", err)
	}
	return nil
}
