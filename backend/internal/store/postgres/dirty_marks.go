package postgres

import (
	"context"
	"fmt"

	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
)

// dirtyMarkKind names what a dirty mark targets.
type dirtyMarkKind uint8

const (
	markToken            dirtyMarkKind = iota + 1 // aggregation_dirty for a token
	markPairToken                                 // aggregation_dirty for the token of an LP pair
	markV4PoolToken                               // aggregation_dirty for the token of a V4 pool
	markPosition                                  // position_dirty for (wallet, token)
	markPairSwapPosition                          // position_dirty for (tx sender, token of an LP pair)
	markV4SwapPosition                            // position_dirty for (tx sender, token of a V4 pool)
)

// dirtyMark identifies one dirty mark: subject is the token, pair or pool, wallet the position
// holder or swap sender.
type dirtyMark struct {
	kind    dirtyMarkKind
	chainID int64
	subject common.Hash
	wallet  common.Address
}

func addressSubject(address common.Address) common.Hash { return common.BytesToHash(address[:]) }

// WithDirtyMarkDedupe returns a copy of the adapter that writes each dirty mark at most once until
// forgetDirtyMarks (pre-flight 2026-11-13-indexer-dirty-mark-dedupe). A mark only bumps a row's
// generation and workers read it after the commit, so it is meant for one transaction: the
// indexer's unit of work. A plain adapter marks every time.
func (adapter *Adapter) WithDirtyMarkDedupe() *Adapter {
	clone := *adapter
	clone.marks = make(map[dirtyMark]struct{})
	return &clone
}

// markOnce runs write unless the same mark was already written, and remembers it once written.
func (adapter *Adapter) markOnce(mark dirtyMark, write func() error) error {
	if adapter.marks == nil {
		return write()
	}
	if _, done := adapter.marks[mark]; done {
		return nil
	}
	if err := write(); err != nil {
		return err
	}
	adapter.marks[mark] = struct{}{}
	return nil
}

// marked reports whether mark was already written; always false without de-duplication.
func (adapter *Adapter) marked(mark dirtyMark) bool {
	_, done := adapter.marks[mark]
	return done
}

// remember records mark as written; a no-op without de-duplication.
func (adapter *Adapter) remember(marks ...dirtyMark) {
	if adapter.marks == nil {
		return
	}
	for _, mark := range marks {
		adapter.marks[mark] = struct{}{}
	}
}

// forgetDirtyMarks drops the remembered marks. Launch, graduation and the reorg rebuild call it:
// they change the tokens, pairs, pools and exclusions the marks resolve against, and a launch
// deletes the launch-mint position marks.
func (adapter *Adapter) forgetDirtyMarks() {
	clear(adapter.marks)
}

func (adapter *Adapter) markPositionDirty(ctx context.Context, chainID int64, wallet, token common.Address) error {
	return adapter.markOnce(dirtyMark{kind: markPosition, chainID: chainID, subject: addressSubject(token), wallet: wallet}, func() error {
		if err := adapter.queries.MarkPositionDirty(ctx, sqlc.MarkPositionDirtyParams{ChainID: chainID, Wallet: wallet[:], TokenAddress: token[:], Factory: adapter.factory[:]}); err != nil {
			return fmt.Errorf("mark position dirty: %w", err)
		}
		return nil
	})
}

func (adapter *Adapter) markTokenDirty(ctx context.Context, chainID int64, token common.Address) error {
	return adapter.markOnce(dirtyMark{kind: markToken, chainID: chainID, subject: addressSubject(token)}, func() error {
		if err := adapter.queries.MarkTokenDirty(ctx, sqlc.MarkTokenDirtyParams{ChainID: chainID, TokenAddress: sqlc.Address(token)}); err != nil {
			return fmt.Errorf("mark token dirty: %w", err)
		}
		return nil
	})
}

func (adapter *Adapter) markPairTokenDirty(ctx context.Context, chainID int64, pair common.Address) error {
	return adapter.markOnce(dirtyMark{kind: markPairToken, chainID: chainID, subject: addressSubject(pair)}, func() error {
		if err := adapter.queries.MarkPairTokenDirty(ctx, sqlc.MarkPairTokenDirtyParams{ChainID: chainID, LpPair: addressPointer(pair)}); err != nil {
			return fmt.Errorf("mark pair token dirty: %w", err)
		}
		return nil
	})
}

// markTransferPositionsDirty marks both sides of a transfer. With de-duplication a side already
// marked is skipped; MarkPositionDirty applies the same exclusions as MarkTransferPositionsDirty.
func (adapter *Adapter) markTransferPositionsDirty(ctx context.Context, event ledger.Transfer) error {
	from := dirtyMark{kind: markPosition, chainID: event.ChainID, subject: addressSubject(event.Token), wallet: event.From}
	to := dirtyMark{kind: markPosition, chainID: event.ChainID, subject: addressSubject(event.Token), wallet: event.To}
	fromDone, toDone := adapter.marked(from), adapter.marked(to)
	switch {
	case fromDone && toDone:
		return nil
	case fromDone:
		return adapter.markPositionDirty(ctx, event.ChainID, event.To, event.Token)
	case toDone:
		return adapter.markPositionDirty(ctx, event.ChainID, event.From, event.Token)
	}
	if err := adapter.queries.MarkTransferPositionsDirty(ctx, sqlc.MarkTransferPositionsDirtyParams{ChainID: event.ChainID, TokenAddress: event.Token[:], FromAddress: event.From[:], ToAddress: event.To[:], Factory: adapter.factory[:]}); err != nil {
		return fmt.Errorf("mark transfer positions dirty: %w", err)
	}
	adapter.remember(from, to)
	return nil
}
