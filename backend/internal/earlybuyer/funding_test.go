package earlybuyer

import (
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestSharedFunderLinksEarlyWallets(t *testing.T) {
	got := Group(Input{
		Deliveries:     []Delivery{{tx(1), addr(1)}, {tx(2), addr(2)}, {tx(3), addr(3)}},
		Balances:       balances(1, 30, 2, 20, 3, 10),
		Funders:        map[common.Address]common.Address{addr(1): addr(900), addr(2): addr(900), addr(3): addr(901)},
		FundingChecked: 3,
	})
	if got.FundingChecked != 3 || len(got.Clusters) != 1 {
		t.Fatalf("result = %+v", got)
	}
	c := got.Clusters[0]
	// The funder links the wallets but is not a member: it never held the token.
	if !reflect.DeepEqual(addresses(c), []common.Address{addr(1), addr(2)}) ||
		!reflect.DeepEqual(c.Links, []string{LinkFundingSource}) {
		t.Fatalf("cluster = %+v", c)
	}
}

func TestEarlyFunderLinksTheWalletsItFunded(t *testing.T) {
	got := Group(Input{
		Deliveries: []Delivery{{tx(1), addr(1)}, {tx(2), addr(2)}},
		Balances:   balances(1, 30, 2, 20),
		Funders:    map[common.Address]common.Address{addr(2): addr(1)},
	})
	if len(got.Clusters) != 1 || got.Clusters[0].WalletCount != 2 ||
		!reflect.DeepEqual(got.Clusters[0].Links, []string{LinkFundingSource}) {
		t.Fatalf("clusters = %+v", got.Clusters)
	}
}

func TestFundingLinksJoinOtherLinkKindsInOrder(t *testing.T) {
	got := Group(Input{
		Deliveries: []Delivery{{tx(1), addr(1)}, {tx(1), addr(2)}, {tx(2), addr(3)}},
		Balances:   balances(1, 30, 2, 20, 3, 10),
		Funders:    map[common.Address]common.Address{addr(2): addr(900), addr(3): addr(900)},
	})
	if len(got.Clusters) != 1 || got.Clusters[0].WalletCount != 3 ||
		!reflect.DeepEqual(got.Clusters[0].Links, []string{LinkSameTransaction, LinkFundingSource}) {
		t.Fatalf("clusters = %+v", got.Clusters)
	}
}

func TestHubFunderIsDropped(t *testing.T) {
	var deliveries []Delivery
	funders := map[common.Address]common.Address{}
	for i := range HubLimit + 1 {
		deliveries = append(deliveries, Delivery{tx(i + 1), addr(i + 1)})
		funders[addr(i+1)] = addr(1000)
	}
	if got := Group(Input{Deliveries: deliveries, Funders: funders}); len(got.Clusters) != 0 {
		t.Fatalf("clusters = %+v, want the hub funder dropped", got.Clusters)
	}
	delete(funders, addr(1))
	if got := Group(Input{Deliveries: deliveries, Funders: funders}); len(got.Clusters) != 1 || got.Clusters[0].WalletCount != HubLimit {
		t.Fatalf("clusters = %+v, want one of %d", got.Clusters, HubLimit)
	}
}
