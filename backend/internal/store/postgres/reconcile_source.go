package postgres

import (
	"context"
	"crypto/rand"
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
	// RotationStart is optional; tests set it. It picks where each pass's rotating sample over
	// every token starts; by default a random address.
	RotationStart func() common.Address
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
		rows, err := adapter.queries.ListReconcileTokens(ctx, sqlc.ListReconcileTokensParams{ChainID: s.ChainID, RowLimit: int32(tokens), RotationStart: s.rotationStart().Bytes()})
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
	token := reconcile.Token{Address: address, Curve: common.Address(row.CurveAddress), Phase: row.Phase, ReserveSource: row.ReserveSource}
	switch row.ReserveSource {
	case reconcile.SourceCurve, reconcile.SourcePair:
		eth, err := parseAmount("eth_reserve", row.EthReserveText)
		if err != nil {
			return reconcile.Token{}, err
		}
		tokens, err := parseAmount("token_reserve", row.TokenReserveText)
		if err != nil {
			return reconcile.Token{}, err
		}
		if row.ReserveSource == reconcile.SourceCurve {
			token.CurveReserves = &reconcile.Reserves{ETH: eth, Token: tokens}
		} else if row.LpPair != nil && row.Weth != nil {
			// Without a pair or WETH the pass reports the missing coordinates as drift.
			token.Pair = &reconcile.PairState{Pair: common.Address(*row.LpPair), WETH: common.Address(*row.Weth), ETH: eth, Token: tokens}
		}
	case reconcile.SourceV4:
		price, err := parseAmount("sqrt_price_x96", row.SqrtPriceX96Text)
		if err != nil {
			return reconcile.Token{}, err
		}
		switch {
		case len(row.V4PoolID) == 0 || len(row.V4PoolManager) == 0:
			// No pool registry or PoolManager row: the pass reports the missing coordinates as drift.
		case len(row.V4PoolID) != common.HashLength || len(row.V4PoolManager) != common.AddressLength:
			return reconcile.Token{}, fmt.Errorf("indexed v4 pool of %s has a %d-byte id and a %d-byte PoolManager", address.Hex(), len(row.V4PoolID), len(row.V4PoolManager))
		default:
			token.V4 = &reconcile.V4State{PoolManager: common.BytesToAddress(row.V4PoolManager), PoolID: common.BytesToHash(row.V4PoolID), SqrtPriceX96: price}
		}
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

func (s ReconcileSource) rotationStart() common.Address {
	if s.RotationStart != nil {
		return s.RotationStart()
	}
	var start common.Address
	_, _ = rand.Read(start[:]) // crypto/rand.Read never fails
	return start
}

// parseAmount parses a NUMERIC(78,0) read as text.
func parseAmount(field, text string) (*big.Int, error) {
	value, ok := new(big.Int).SetString(text, 10)
	if !ok || value.Sign() < 0 {
		return nil, fmt.Errorf("indexed %s %q is not a non-negative integer", field, text)
	}
	return value, nil
}
