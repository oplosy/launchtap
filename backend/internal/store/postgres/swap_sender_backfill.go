package postgres

import (
	"context"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
)

// UnattributedSwapTx is one transaction that still has a pool swap without a known sender.
type UnattributedSwapTx struct {
	TxHash      common.Hash
	BlockNumber int64
}

// ListUnattributedSwapTxs returns up to limit distinct transactions with a NULL tx_from swap,
// ordered by (block_number, tx_hash) and strictly after the (afterBlock, afterHash) cursor.
func (adapter *Adapter) ListUnattributedSwapTxs(ctx context.Context, chainID, afterBlock int64, afterHash common.Hash, limit int32) ([]UnattributedSwapTx, error) {
	rows, err := adapter.queries.ListUnattributedPoolSwaps(ctx, sqlc.ListUnattributedPoolSwapsParams{
		ChainID: chainID, AfterBlock: afterBlock, AfterHash: afterHash[:], RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list unattributed pool swaps: %w", err)
	}
	result := make([]UnattributedSwapTx, len(rows))
	for index, row := range rows {
		result[index] = UnattributedSwapTx{TxHash: common.Hash(row.TxHash), BlockNumber: row.BlockNumber}
	}
	return result, nil
}

// AttributeSwapSender fills tx_from on every still-unattributed swap of txHash and marks the
// affected (sender, token) positions dirty. It returns the number of swaps updated; a swap that
// already has a sender is left alone, so a rerun updates nothing.
func (adapter *Adapter) AttributeSwapSender(ctx context.Context, chainID int64, txHash common.Hash, sender common.Address) (int, error) {
	pairs, err := adapter.queries.SetPoolSwapTxFrom(ctx, sqlc.SetPoolSwapTxFromParams{ChainID: chainID, TxHash: txHash[:], TxFrom: sender[:]})
	if err != nil {
		return 0, fmt.Errorf("set pool swap sender: %w", err)
	}
	marked := make(map[sqlc.Address]struct{}, len(pairs))
	for _, pair := range pairs {
		if _, done := marked[pair]; done {
			continue
		}
		marked[pair] = struct{}{}
		if err := adapter.queries.MarkPoolSwapPositionDirty(ctx, sqlc.MarkPoolSwapPositionDirtyParams{ChainID: chainID, Pair: pair[:], TxFrom: sender[:], Factory: adapter.factory[:]}); err != nil {
			return 0, fmt.Errorf("mark pool swap position dirty: %w", err)
		}
	}
	return len(pairs), nil
}
