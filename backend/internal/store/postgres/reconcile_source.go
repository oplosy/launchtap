package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/Contictus/launchtap/backend/internal/reconcile"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReconcileSource reads the reconciliation snapshot: the observed block and the sampled tokens'
// indexed state, all from one read-only REPEATABLE READ transaction on the shared pool.
type ReconcileSource struct {
	Pool         *pgxpool.Pool
	ChainID      int64
	DeploymentID string
}

// Snapshot implements reconcile.Store. ok is false while the deployment has no observed block.
func (s ReconcileSource) Snapshot(ctx context.Context, tokens, holders int) (reconcile.Snapshot, bool, error) {
	var snapshot reconcile.Snapshot
	err := WithReadSnapshot(ctx, s.Pool, s.ChainID, s.DeploymentID, func(ctx context.Context, adapter *Adapter, read ReadSnapshot) error {
		if read.State.ObservedNumber.Int64 < 0 || read.State.ObservedHash == nil {
			return errors.New("observed block is invalid")
		}
		snapshot.Block = uint64(read.State.ObservedNumber.Int64)
		snapshot.BlockHash = common.Hash(*read.State.ObservedHash)
		rows, err := adapter.queries.ListReconcileTokens(ctx, sqlc.ListReconcileTokensParams{ChainID: s.ChainID, RowLimit: int32(tokens)})
		if err != nil {
			return fmt.Errorf("list reconcile tokens: %w", err)
		}
		for _, row := range rows {
			token, err := s.token(ctx, adapter, row, holders)
			if err != nil {
				return err
			}
			snapshot.Tokens = append(snapshot.Tokens, token)
		}
		return nil
	})
	if errors.Is(err, pagination.ErrSnapshotUnavailable) {
		return reconcile.Snapshot{}, false, nil
	}
	if err != nil {
		return reconcile.Snapshot{}, false, err
	}
	return snapshot, true, nil
}

func (s ReconcileSource) token(ctx context.Context, adapter *Adapter, row sqlc.ListReconcileTokensRow, holders int) (reconcile.Token, error) {
	address := common.Address(row.TokenAddress)
	token := reconcile.Token{Address: address, Curve: common.Address(row.CurveAddress), Phase: row.Phase}
	if row.ReserveSource == "curve" {
		eth, err := parseAmount("eth_reserve", row.EthReserveText)
		if err != nil {
			return reconcile.Token{}, err
		}
		tokens, err := parseAmount("token_reserve", row.TokenReserveText)
		if err != nil {
			return reconcile.Token{}, err
		}
		token.CurveReserves = &reconcile.Reserves{ETH: eth, Token: tokens}
	}
	total, err := adapter.queries.SumHolderBalances(ctx, sqlc.SumHolderBalancesParams{ChainID: s.ChainID, TokenAddress: row.TokenAddress})
	if err != nil {
		return reconcile.Token{}, fmt.Errorf("sum holder balances of %s: %w", address.Hex(), err)
	}
	if token.HolderSum, err = parseAmount("holder balance sum", total); err != nil {
		return reconcile.Token{}, err
	}
	top, err := adapter.queries.ListTopHolders(ctx, sqlc.ListTopHoldersParams{ChainID: s.ChainID, TokenAddress: row.TokenAddress, RowLimit: int32(holders)})
	if err != nil {
		return reconcile.Token{}, fmt.Errorf("list top holders of %s: %w", address.Hex(), err)
	}
	for _, holder := range top {
		balance, err := parseAmount("holder balance", holder.BalanceText)
		if err != nil {
			return reconcile.Token{}, err
		}
		token.TopHolders = append(token.TopHolders, reconcile.Holder{Address: common.Address(holder.HolderAddress), Balance: balance})
	}
	return token, nil
}

// parseAmount parses a NUMERIC(78,0) read as text.
func parseAmount(field, text string) (*big.Int, error) {
	value, ok := new(big.Int).SetString(text, 10)
	if !ok || value.Sign() < 0 {
		return nil, fmt.Errorf("indexed %s %q is not a non-negative integer", field, text)
	}
	return value, nil
}
