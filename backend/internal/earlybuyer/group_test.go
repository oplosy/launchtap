package earlybuyer

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func addr(n int) common.Address { return common.BigToAddress(big.NewInt(int64(n))) }
func tx(n int) common.Hash      { return common.BigToHash(big.NewInt(int64(n))) }

func balances(pairs ...int) map[common.Address]*big.Int {
	out := map[common.Address]*big.Int{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out[addr(pairs[i])] = big.NewInt(int64(pairs[i+1]))
	}
	return out
}

func addresses(c Cluster) []common.Address {
	out := make([]common.Address, 0, len(c.Wallets))
	for _, w := range c.Wallets {
		out = append(out, w.Address)
	}
	return out
}

func TestSameTransactionLinksItsRecipients(t *testing.T) {
	got := Group(Input{
		Deliveries: []Delivery{{tx(1), addr(1)}, {tx(1), addr(2)}, {tx(2), addr(3)}},
		Balances:   balances(1, 50, 2, 30, 3, 10),
	})
	if got.EarlyWalletCount != 3 || got.EarlyBalance.Cmp(big.NewInt(90)) != 0 {
		t.Fatalf("early = %d / %s, want 3 / 90", got.EarlyWalletCount, got.EarlyBalance)
	}
	if len(got.Clusters) != 1 {
		t.Fatalf("clusters = %+v, want one", got.Clusters)
	}
	c := got.Clusters[0]
	if !reflect.DeepEqual(addresses(c), []common.Address{addr(1), addr(2)}) || c.WalletCount != 2 ||
		c.Balance.Cmp(big.NewInt(80)) != 0 || !reflect.DeepEqual(c.Links, []string{LinkSameTransaction}) {
		t.Fatalf("cluster = %+v", c)
	}
}

func TestTransferBetweenEarlyWalletsLinksThem(t *testing.T) {
	got := Group(Input{
		Deliveries: []Delivery{{tx(1), addr(1)}, {tx(2), addr(2)}},
		Transfers:  []Transfer{{addr(2), addr(1)}},
		Balances:   balances(1, 40),
	})
	if len(got.Clusters) != 1 || got.Clusters[0].WalletCount != 2 ||
		!reflect.DeepEqual(got.Clusters[0].Links, []string{LinkTokenTransfer}) {
		t.Fatalf("clusters = %+v", got.Clusters)
	}
	// A wallet that sold out still appears, with a zero balance, after the holders.
	if w := got.Clusters[0].Wallets; w[0].Address != addr(1) || w[1].Balance.Sign() != 0 {
		t.Fatalf("wallets = %+v", w)
	}
}

func TestConsolidationIntoAHoldingOutsideWallet(t *testing.T) {
	got := Group(Input{
		Deliveries: []Delivery{{tx(1), addr(1)}, {tx(2), addr(2)}},
		Transfers:  []Transfer{{addr(1), addr(9)}, {addr(2), addr(9)}},
		Balances:   balances(9, 100),
	})
	if len(got.Clusters) != 1 {
		t.Fatalf("clusters = %+v", got.Clusters)
	}
	c := got.Clusters[0]
	if c.WalletCount != 3 || c.Wallets[0].Address != addr(9) || c.Balance.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("cluster = %+v", c)
	}
	// The outside wallet is grouped but is not an early wallet.
	if got.EarlyWalletCount != 2 || got.EarlyBalance.Sign() != 0 {
		t.Fatalf("early = %d / %s", got.EarlyWalletCount, got.EarlyBalance)
	}
}

func TestOutsideWalletWithoutBalanceDoesNotLink(t *testing.T) {
	// A router or aggregator ends every swap at zero, so it never joins two sellers.
	got := Group(Input{
		Deliveries: []Delivery{{tx(1), addr(1)}, {tx(2), addr(2)}},
		Transfers:  []Transfer{{addr(1), addr(9)}, {addr(2), addr(9)}},
		Balances:   balances(1, 5, 2, 5),
	})
	if len(got.Clusters) != 0 {
		t.Fatalf("clusters = %+v, want none", got.Clusters)
	}
}

func TestHubOutsideWalletIsDropped(t *testing.T) {
	var deliveries []Delivery
	var transfers []Transfer
	for i := range HubLimit + 1 {
		deliveries = append(deliveries, Delivery{tx(i + 1), addr(i + 1)})
		transfers = append(transfers, Transfer{addr(i + 1), addr(1000)})
	}
	got := Group(Input{Deliveries: deliveries, Transfers: transfers, Balances: balances(1000, 1)})
	if len(got.Clusters) != 0 {
		t.Fatalf("clusters = %+v, want the hub dropped", got.Clusters)
	}
	// At the limit the wallet still links.
	got = Group(Input{Deliveries: deliveries[:HubLimit], Transfers: transfers[:HubLimit], Balances: balances(1000, 1)})
	if len(got.Clusters) != 1 || got.Clusters[0].WalletCount != HubLimit+1 {
		t.Fatalf("clusters = %+v, want one of %d", got.Clusters, HubLimit+1)
	}
}

func TestCreatorFlagLinksAndOrdering(t *testing.T) {
	got := Group(Input{
		Deliveries: []Delivery{
			{tx(1), addr(1)}, {tx(1), addr(2)}, // small cluster
			{tx(2), addr(3)}, {tx(2), addr(4)}, // large cluster, linked twice
			{tx(3), addr(5)},
		},
		Transfers: []Transfer{{addr(3), addr(4)}, {addr(4), addr(4)}},
		Balances:  balances(1, 1, 2, 1, 3, 10, 4, 20),
		Creator:   addr(4),
	})
	if len(got.Clusters) != 2 {
		t.Fatalf("clusters = %+v", got.Clusters)
	}
	first, second := got.Clusters[0], got.Clusters[1]
	if !first.IncludesCreator || second.IncludesCreator {
		t.Fatalf("creator flags = %v %v", first.IncludesCreator, second.IncludesCreator)
	}
	if !reflect.DeepEqual(first.Links, []string{LinkSameTransaction, LinkTokenTransfer}) {
		t.Fatalf("links = %v", first.Links)
	}
	if !reflect.DeepEqual(addresses(first), []common.Address{addr(4), addr(3)}) {
		t.Fatalf("largest balance first, got %v", addresses(first))
	}
}

func TestCapsKeepExactCounts(t *testing.T) {
	var deliveries []Delivery
	for i := range MaxWalletsPerCluster + 5 {
		deliveries = append(deliveries, Delivery{tx(1), addr(i + 1)})
	}
	for c := range MaxClusters + 3 {
		deliveries = append(deliveries, Delivery{tx(100 + c), addr(500 + 2*c)}, Delivery{tx(100 + c), addr(501 + 2*c)})
	}
	got := Group(Input{Deliveries: deliveries, Balances: balances(1, 1000)})
	if len(got.Clusters) != MaxClusters {
		t.Fatalf("clusters = %d, want %d", len(got.Clusters), MaxClusters)
	}
	if got.ClusterCount != MaxClusters+4 {
		t.Fatalf("cluster count = %d, want %d", got.ClusterCount, MaxClusters+4)
	}
	big := got.Clusters[0]
	if len(big.Wallets) != MaxWalletsPerCluster || big.WalletCount != MaxWalletsPerCluster+5 {
		t.Fatalf("wallets listed %d of %d", len(big.Wallets), big.WalletCount)
	}
}

func TestOneEarlyWalletMovingTokensIsNotAGroup(t *testing.T) {
	got := Group(Input{
		Deliveries: []Delivery{{tx(1), addr(1)}},
		Transfers:  []Transfer{{addr(1), addr(9)}},
		Balances:   balances(9, 100),
	})
	if len(got.Clusters) != 0 || got.ClusterCount != 0 {
		t.Fatalf("clusters = %+v, want none", got.Clusters)
	}
}

func TestNoDeliveriesGivesAnEmptyResult(t *testing.T) {
	got := Group(Input{Transfers: []Transfer{{addr(1), addr(2)}}})
	if got.EarlyWalletCount != 0 || got.EarlyBalance.Sign() != 0 || got.Clusters == nil || len(got.Clusters) != 0 {
		t.Fatalf("got %+v", got)
	}
}
