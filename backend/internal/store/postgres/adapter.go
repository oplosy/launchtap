package postgres

import (
	"fmt"
	"slices"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
)

// InvariantConflictError reports that an immutable database identity was
// replayed with a different payload.
type InvariantConflictError struct {
	Entity string
	Key    string
	Cause  error
}

type refreshDeploymentContextKey struct{}

// NegativeBalanceError identifies a canonical transfer that cannot be folded.
type NegativeBalanceError struct {
	Token, Holder string
	Key           string
}

func (err *NegativeBalanceError) Error() string {
	return fmt.Sprintf("holder balance would become negative for token=%s holder=%s at %s", err.Token, err.Holder, err.Key)
}

func (err *InvariantConflictError) Error() string {
	return fmt.Sprintf("%s invariant conflict at %s", err.Entity, err.Key)
}

func (err *InvariantConflictError) Unwrap() error {
	return err.Cause
}

// Adapter adds persistence invariants around generated sqlc queries. Most
// methods use the supplied database handle as-is; protocol aggregate refreshes
// open a transaction for a pool-backed adapter and reuse an existing tx.
type Adapter struct {
	queries  *sqlc.Queries
	beginner TransactionBeginner
	// factories are excluded from wallet position tracking (R4), one per launch engine the
	// indexer ingests; the zero address is always excluded too (excludedFactories).
	factories []common.Address
	// marks remembers the dirty marks written in this transaction; nil marks every time
	// (WithDirtyMarkDedupe).
	marks map[dirtyMark]struct{}
}

func NewAdapter(db DBTX) *Adapter {
	beginner, _ := db.(TransactionBeginner)
	return &Adapter{queries: sqlc.New(db), beginner: beginner}
}

// WithFactories returns a copy of the adapter that excludes the configured factories from
// wallet position dirty marking.
func (adapter *Adapter) WithFactories(factories []common.Address) *Adapter {
	clone := *adapter
	clone.factories = slices.Clone(factories)
	return &clone
}

// excludedFactories is the query parameter of every position dirty mark: the configured
// factories plus the zero address, so the list is never empty.
func (adapter *Adapter) excludedFactories() [][]byte {
	excluded := make([][]byte, 0, len(adapter.factories)+1)
	excluded = append(excluded, make([]byte, common.AddressLength))
	for _, factory := range adapter.factories {
		excluded = append(excluded, factory.Bytes())
	}
	return excluded
}

func invariantConflict(entity, key string, cause error) error {
	return &InvariantConflictError{Entity: entity, Key: key, Cause: cause}
}

func eventKey(chainID int64, txHash sqlc.Hash, logIndex int32) string {
	return fmt.Sprintf("chain_id=%d tx_hash=%x log_index=%d", chainID, txHash, logIndex)
}
