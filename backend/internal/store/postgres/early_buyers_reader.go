package postgres

import (
	"context"
	"errors"
	"math/big"

	"github.com/Contictus/launchtap/backend/internal/earlybuyer"
	"github.com/Contictus/launchtap/backend/internal/funding"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

// ReadEarlyBuyers reads the early curve deliveries, the token transfers touching those wallets,
// the involved wallets' balances and the early wallets' funding rows in one snapshot, then groups
// them (earlybuyer.Group).
func (r MarketReader) ReadEarlyBuyers(ctx context.Context, q earlybuyer.Query) (earlybuyer.Report, error) {
	var out earlybuyer.Report
	err := withReadSnapshotBeginner(ctx, r.Pool, q.ChainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		tokenRow, err := a.queries.GetEarlyBuyerToken(ctx, sqlc.GetEarlyBuyerTokenParams{
			SnapshotBlock: s.Identity.BlockNumber,
			SnapshotHash:  s.Identity.BlockHash[:],
			WindowSeconds: earlybuyer.WindowSeconds,
			ChainID:       q.ChainID,
			TokenAddress:  sqlc.Address(q.Token),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return token.ErrNotFound
		}
		if err != nil {
			return err
		}
		deliveryRows, err := a.queries.ListEarlyCurveDeliveries(ctx, sqlc.ListEarlyCurveDeliveriesParams{
			ChainID: q.ChainID, TokenAddress: sqlc.Address(q.Token), WindowSeconds: earlybuyer.WindowSeconds,
		})
		if err != nil {
			return err
		}
		in := earlybuyer.Input{Creator: common.Address(tokenRow.Creator), Balances: map[common.Address]*big.Int{}}
		seen := map[common.Address]bool{}
		var earlyWallets [][]byte
		for _, row := range deliveryRows {
			recipient := common.Address(row.ToAddress)
			in.Deliveries = append(in.Deliveries, earlybuyer.Delivery{TxHash: common.Hash(row.TxHash), Recipient: recipient})
			if !seen[recipient] {
				seen[recipient] = true
				earlyWallets = append(earlyWallets, recipient.Bytes())
			}
		}
		if len(earlyWallets) > 0 {
			transferRows, err := a.queries.ListTokenTransfersTouching(ctx, sqlc.ListTokenTransfersTouchingParams{
				ChainID: q.ChainID, TokenAddress: sqlc.Address(q.Token), Wallets: earlyWallets,
			})
			if err != nil {
				return err
			}
			involved := append([][]byte(nil), earlyWallets...)
			for _, row := range transferRows {
				from, to := common.Address(row.FromAddress), common.Address(row.ToAddress)
				in.Transfers = append(in.Transfers, earlybuyer.Transfer{From: from, To: to})
				for _, w := range []common.Address{from, to} {
					if !seen[w] {
						seen[w] = true
						involved = append(involved, w.Bytes())
					}
				}
			}
			balanceRows, err := a.queries.ListHolderBalancesFor(ctx, sqlc.ListHolderBalancesForParams{
				ChainID: q.ChainID, TokenAddress: sqlc.Address(q.Token), Wallets: involved,
			})
			if err != nil {
				return err
			}
			for _, row := range balanceRows {
				in.Balances[common.Address(row.HolderAddress)] = row.Balance.BigInt()
			}
			// Level 2: funding rows written by cmd/funding; none exist while the worker is off.
			fundingRows, err := a.queries.ListWalletFundingFor(ctx, sqlc.ListWalletFundingForParams{ChainID: q.ChainID, Wallets: earlyWallets})
			if err != nil {
				return err
			}
			in.FundingChecked = len(fundingRows)
			in.Funders = map[common.Address]common.Address{}
			for _, row := range fundingRows {
				if row.Status == string(funding.StatusFunded) && row.Funder != nil && !row.FunderIgnored {
					in.Funders[common.Address(row.Wallet)] = common.Address(*row.Funder)
				}
			}
		}
		out.Result = earlybuyer.Group(in)
		out.WindowOpen = tokenRow.WindowOpen
		out.Snapshot = s.Identity
		out.Finality = finality(s.State, s.Identity.BlockNumber)
		return nil
	})
	return out, err
}
