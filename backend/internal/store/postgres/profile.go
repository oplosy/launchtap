package postgres

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Contictus/launchtap/backend/internal/profile"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgtype"
)

type ProfileReader struct {
	Pool         PoolReadBeginner
	DeploymentID string
}

func (r ProfileReader) List(ctx context.Context, chainID int64, wallets []common.Address) (profile.Page, error) {
	var page profile.Page
	err := withReadSnapshotBeginner(ctx, r.Pool, chainID, r.DeploymentID, func(ctx context.Context, adapter *Adapter, snapshot ReadSnapshot) error {
		page.Snapshot = snapshot.Identity
		page.Finality = finality(snapshot.State, snapshot.Identity.BlockNumber)
		// A wallet-less account has no curve action; the snapshot still reports the indexed head.
		if len(wallets) == 0 {
			return nil
		}
		walletBytes := make([][]byte, len(wallets))
		for i, wallet := range wallets {
			walletBytes[i] = append([]byte(nil), wallet[:]...)
		}
		rows, err := adapter.queries.ListProfileActions(ctx, sqlc.ListProfileActionsParams{ChainID: chainID, Wallets: walletBytes})
		if err != nil {
			return err
		}
		for _, row := range rows {
			creatorFees, err := profileNumeric(row.CreatorFeeAmount)
			if err != nil {
				return fmt.Errorf("decode profile creator fees for token %s: %w", common.Address(row.TokenAddress).Hex(), err)
			}
			refund, err := profileNumeric(row.RefundAmount)
			if err != nil {
				return fmt.Errorf("decode profile refund for token %s: %w", common.Address(row.TokenAddress).Hex(), err)
			}
			page.Items = append(page.Items, profile.Action{
				Token: common.Address(row.TokenAddress), Curve: common.Address(row.CurveAddress), Name: row.Name, Symbol: row.Symbol, Phase: row.Phase,
				CreatorFees: creatorFees, Refund: refund,
			})
		}
		return nil
	})
	return page, err
}

func profileNumeric(value pgtype.Numeric) (*big.Int, error) {
	return numericBigExact(value)
}
