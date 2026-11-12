// Package earlybuyer groups the wallets that received tokens from the curve right after launch by
// on-chain facts already in the ledger (docs/specs/2026-11-12-early-buyer-clusters-design.md).
// Every link is a listed fact a reader can check on an explorer; nothing is inferred.
package earlybuyer

import (
	"bytes"
	"context"
	"math/big"
	"sort"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/ethereum/go-ethereum/common"
)

const (
	// WindowSeconds is how long after the launch block's time a curve delivery counts as early.
	WindowSeconds = 60
	// HubLimit drops an outside wallet linked to more early wallets than this: it is far more
	// likely a contract or service than one person, and dropping it can only under-group.
	HubLimit = 25
	// MaxClusters and MaxWalletsPerCluster bound the response; counts stay exact.
	MaxClusters          = 10
	MaxWalletsPerCluster = 20

	LinkSameTransaction = "same_transaction"
	LinkTokenTransfer   = "token_transfer"
)

// Delivery is one early curve delivery: a Transfer from the curve to Recipient in TxHash.
type Delivery struct {
	TxHash    common.Hash
	Recipient common.Address
}

// Transfer is a transfer of the token between two non-infrastructure wallets.
type Transfer struct {
	From, To common.Address
}

type Input struct {
	Deliveries []Delivery
	Transfers  []Transfer
	// Balances holds current balances; a missing wallet holds zero.
	Balances map[common.Address]*big.Int
	Creator  common.Address
}

type Wallet struct {
	Address common.Address
	Balance *big.Int
}

type Cluster struct {
	// Wallets lists at most MaxWalletsPerCluster wallets, largest balance first.
	Wallets         []Wallet
	WalletCount     int
	Balance         *big.Int
	Links           []string
	IncludesCreator bool
}

type Result struct {
	EarlyWalletCount int
	// EarlyBalance is what every early wallet holds now.
	EarlyBalance *big.Int
	// Clusters lists at most MaxClusters clusters, largest combined balance first.
	Clusters     []Cluster
	ClusterCount int
}

type Query struct {
	ChainID int64
	Token   common.Address
}

type Report struct {
	Result
	// WindowOpen is true while the snapshot block is still inside the early window.
	WindowOpen bool
	Snapshot   pagination.Snapshot
	Finality   string
}

type Reader interface {
	ReadEarlyBuyers(context.Context, Query) (Report, error)
}

type unionFind map[common.Address]common.Address

func (u unionFind) find(a common.Address) common.Address {
	if _, ok := u[a]; !ok {
		u[a] = a
	}
	for u[a] != a {
		u[a] = u[u[a]]
		a = u[a]
	}
	return a
}

func (u unionFind) union(a, b common.Address) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	// Deterministic root: the smaller address.
	if bytes.Compare(ra[:], rb[:]) < 0 {
		u[rb] = ra
	} else {
		u[ra] = rb
	}
}

type edge struct {
	a, b common.Address
	link string
}

// Group links early wallets by same-transaction deliveries and token transfers and returns the
// groups that contain two or more early wallets.
func Group(in Input) Result {
	balance := func(a common.Address) *big.Int {
		if b, ok := in.Balances[a]; ok && b != nil {
			return b
		}
		return new(big.Int)
	}
	early := map[common.Address]bool{}
	byTx := map[common.Hash][]common.Address{}
	for _, d := range in.Deliveries {
		early[d.Recipient] = true
		byTx[d.TxHash] = append(byTx[d.TxHash], d.Recipient)
	}
	result := Result{EarlyWalletCount: len(early), EarlyBalance: new(big.Int), Clusters: []Cluster{}}
	for a := range early {
		result.EarlyBalance.Add(result.EarlyBalance, balance(a))
	}

	var edges []edge
	for _, recipients := range byTx {
		for _, r := range recipients[1:] {
			if r != recipients[0] {
				edges = append(edges, edge{recipients[0], r, LinkSameTransaction})
			}
		}
	}
	// Outside wallets join only while they hold tokens, and only below the hub limit.
	outsideNeighbours := map[common.Address]map[common.Address]bool{}
	var outsideEdges []edge
	for _, t := range in.Transfers {
		if t.From == t.To {
			continue
		}
		switch {
		case early[t.From] && early[t.To]:
			edges = append(edges, edge{t.From, t.To, LinkTokenTransfer})
		case early[t.From] || early[t.To]:
			inside, outside := t.From, t.To
			if !early[inside] {
				inside, outside = outside, inside
			}
			if balance(outside).Sign() <= 0 {
				continue
			}
			if outsideNeighbours[outside] == nil {
				outsideNeighbours[outside] = map[common.Address]bool{}
			}
			outsideNeighbours[outside][inside] = true
			outsideEdges = append(outsideEdges, edge{inside, outside, LinkTokenTransfer})
		}
	}
	for _, e := range outsideEdges {
		if len(outsideNeighbours[e.b]) <= HubLimit {
			edges = append(edges, e)
		}
	}

	uf := unionFind{}
	for _, e := range edges {
		uf.union(e.a, e.b)
	}
	members := map[common.Address][]common.Address{}
	for a := range uf {
		root := uf.find(a)
		members[root] = append(members[root], a)
	}
	links := map[common.Address]map[string]bool{}
	for _, e := range edges {
		root := uf.find(e.a)
		if links[root] == nil {
			links[root] = map[string]bool{}
		}
		links[root][e.link] = true
	}

	for root, wallets := range members {
		// A group needs two early wallets: one early wallet moving tokens on is not a group.
		earlyMembers := 0
		for _, a := range wallets {
			if early[a] {
				earlyMembers++
			}
		}
		if earlyMembers < 2 {
			continue
		}
		c := Cluster{WalletCount: len(wallets), Balance: new(big.Int)}
		for _, a := range wallets {
			c.Wallets = append(c.Wallets, Wallet{Address: a, Balance: balance(a)})
			c.Balance.Add(c.Balance, balance(a))
			if a == in.Creator {
				c.IncludesCreator = true
			}
		}
		sort.Slice(c.Wallets, func(i, j int) bool {
			if d := c.Wallets[i].Balance.Cmp(c.Wallets[j].Balance); d != 0 {
				return d > 0
			}
			return bytes.Compare(c.Wallets[i].Address[:], c.Wallets[j].Address[:]) < 0
		})
		if len(c.Wallets) > MaxWalletsPerCluster {
			c.Wallets = c.Wallets[:MaxWalletsPerCluster]
		}
		for _, link := range []string{LinkSameTransaction, LinkTokenTransfer} {
			if links[root][link] {
				c.Links = append(c.Links, link)
			}
		}
		result.Clusters = append(result.Clusters, c)
	}
	sort.Slice(result.Clusters, func(i, j int) bool {
		a, b := result.Clusters[i], result.Clusters[j]
		if d := a.Balance.Cmp(b.Balance); d != 0 {
			return d > 0
		}
		if a.WalletCount != b.WalletCount {
			return a.WalletCount > b.WalletCount
		}
		return bytes.Compare(a.Wallets[0].Address[:], b.Wallets[0].Address[:]) < 0
	})
	result.ClusterCount = len(result.Clusters)
	if len(result.Clusters) > MaxClusters {
		result.Clusters = result.Clusters[:MaxClusters]
	}
	return result
}
