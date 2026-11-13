package postgres

import (
	"fmt"

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
	// factory is excluded from wallet position tracking (R4). The zero value is already excluded.
	factory common.Address
	// marks remembers the dirty marks written in this transaction; nil marks every time
	// (WithDirtyMarkDedupe).
	marks map[dirtyMark]struct{}
}

func NewAdapter(db DBTX) *Adapter {
	beginner, _ := db.(TransactionBeginner)
	return &Adapter{queries: sqlc.New(db), beginner: beginner}
}

// WithFactory returns a copy of the adapter that excludes the configured factory from wallet
// position dirty marking.
func (adapter *Adapter) WithFactory(factory common.Address) *Adapter {
	clone := *adapter
	clone.factory = factory
	return &clone
}

func invariantConflict(entity, key string, cause error) error {
	return &InvariantConflictError{Entity: entity, Key: key, Cause: cause}
}

func eventKey(chainID int64, txHash sqlc.Hash, logIndex int32) string {
	return fmt.Sprintf("chain_id=%d tx_hash=%x log_index=%d", chainID, txHash, logIndex)
}
