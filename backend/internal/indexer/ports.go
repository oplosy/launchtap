// Package indexer coordinates canonical ingestion through application ports.
// It never imports PostgreSQL or generated persistence types.
package indexer

import (
	"context"
	"time"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type State struct {
	ChainID                   int64
	DeploymentID              string
	Observed, Safe, Finalized *ledger.IndexedBlock
}

type TokenIdentity struct {
	Token, Curve, Pair common.Address
	EngineVersion      uint16
	Launch             ledger.EventCoordinates
	GraduationBlock    *int64
}

type UnitOfWork interface {
	ReadState(context.Context, int64, string) (State, error)
	WriteState(context.Context, State) error
	ReadBlock(context.Context, int64, int64) (ledger.IndexedBlock, bool, error)
	UpsertIndexedBlock(context.Context, ledger.IndexedBlock) (ledger.UpsertResult, error)
	PromoteBlocks(context.Context, int64, *ledger.IndexedBlock, *ledger.IndexedBlock, int64, int64) error
	TokenIdentities(context.Context, int64) ([]TokenIdentity, error)
}

type Store interface {
	Transaction(context.Context, func(context.Context, UnitOfWork) error) error
}
type Source interface {
	Heads(context.Context) (chain.Heads, error)
	HeaderByNumber(context.Context, uint64) (*types.Header, error)
}
type Discovery interface {
	Discover(context.Context, uint64, uint64, chain.Emitters) (chain.DiscoveryResult, error)
}

// SenderSource resolves transaction senders by hash. A hash it cannot resolve is absent from
// the result.
type SenderSource interface {
	TransactionSenders(context.Context, []common.Hash) (map[common.Hash]common.Address, error)
}
type Router interface {
	Apply(context.Context, UnitOfWork, []chain.DecodedLog, map[int64]ledger.IndexedBlock, []TokenIdentity) error
}

type Settings struct {
	ChainID           int64
	DeploymentID      string
	Factory           common.Address
	StartBlock        int64
	ChunkSize         int64
	PollInterval      time.Duration
	ReorgSearchDepth  uint64
	ReorgRecoveryMode bool
	OnCommitted       func(State)
	OnFailure         func(error)
	// OnHeads receives the latest block number after each successful heads read.
	OnHeads func(latest int64)
	// Senders is optional. When set, the engine attaches the transaction sender to every pool
	// swap before the write transaction; when nil, PoolSwap.TxFrom stays nil.
	Senders SenderSource
}

type ReorgRecord struct {
	ChainID, DetectedTipNumber, CommonAncestorNumber, Depth int64
	DeploymentID                                            string
	DetectedTipHash, CommonAncestorHash                     common.Hash
	DetectedAt                                              time.Time
}
