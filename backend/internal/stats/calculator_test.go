package stats

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func TestComputeTokenStatsUsesCirculatingSupplyAndHolderExclusions(t *testing.T) {
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	curve, pair, owner := common.Address{1}, common.Address{2}, common.Address{3}
	got, err := ComputeTokenStats(TokenInput{
		Token:        common.Address{4},
		Curve:        curve,
		Pair:         pair,
		LaunchPrice:  big.NewInt(10),
		LaunchAt:     at,
		ReserveETH:   new(big.Int).Mul(big.NewInt(2), wad),
		ReserveToken: new(big.Int).Mul(big.NewInt(1), wad),
		TotalSupply:  new(big.Int).Mul(big.NewInt(100), wad),
		Holders: []Holder{
			{Address: curve, Balance: new(big.Int).Mul(big.NewInt(80), wad)},
			{Address: pair, Balance: new(big.Int).Mul(big.NewInt(20), wad)},
			{Address: owner, Balance: new(big.Int).Mul(big.NewInt(1), wad)},
			{Address: zeroAddress, Balance: new(big.Int).Mul(big.NewInt(1), wad)},
			{Address: deadAddress, Balance: new(big.Int).Mul(big.NewInt(1), wad)},
		},
	}, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got.ATH.Cmp(big.NewInt(10)) != 0 || got.HolderCount != 1 || got.SpotPrice.Cmp(new(big.Int).Mul(big.NewInt(2), wad)) != 0 {
		t.Fatalf("stats=%+v", got)
	}
	if got.FDV.Cmp(new(big.Int).Mul(big.NewInt(200), wad)) != 0 || got.MarketCap.Cmp(new(big.Int).Mul(big.NewInt(36), wad)) != 0 {
		t.Fatalf("supply stats=%+v", got)
	}
}

func TestComputeTokenStatsUsesBoundaryCandleForSignedChangeAndStableATH(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	got, err := ComputeTokenStats(TokenInput{
		LaunchPrice: big.NewInt(10),
		LaunchAt:    now.Add(-48 * time.Hour),
		Candles: []Candle{
			{Start: now.Add(-25 * time.Hour), High: big.NewInt(100), Close: big.NewInt(100), Volume: big.NewInt(1)},
			{Start: now.Add(-24 * time.Hour), High: big.NewInt(200), Close: big.NewInt(80), Volume: big.NewInt(2)},
			{Start: now.Add(-time.Hour), High: big.NewInt(200), Close: big.NewInt(60), Volume: big.NewInt(3)},
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ATH.Cmp(big.NewInt(200)) != 0 || !got.ATHAt.Equal(now.Add(-24*time.Hour)) || got.Volume24H.Cmp(big.NewInt(5)) != 0 || got.PriceChange24hBPS != -2500 {
		t.Fatalf("stats=%+v", got)
	}
}

func TestComputeTokenStatsNegativePriceChangeTruncatesTowardZero(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	got, err := ComputeTokenStats(TokenInput{Candles: []Candle{
		{Start: now.Add(-25 * time.Hour), Close: big.NewInt(3)},
		{Start: now.Add(-time.Hour), Close: big.NewInt(2)},
	}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.PriceChange24hBPS != -3333 {
		t.Fatalf("price change = %d bps, want -3333", got.PriceChange24hBPS)
	}
}

func TestComputeTokenStatsKeepsPriceChangeBeyondPostgresIntegerRange(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	got, err := ComputeTokenStats(TokenInput{Candles: []Candle{
		{Start: now.Add(-25 * time.Hour), Close: big.NewInt(1)},
		{Start: now.Add(-time.Hour), Close: big.NewInt(214_750)},
	}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.PriceChange24hBPS != 2_147_490_000 {
		t.Fatalf("price change = %d bps, want 2147490000", got.PriceChange24hBPS)
	}
}

func TestComputeTokenStatsSaturatesPriceChangeOutsideSafeIntegerRange(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	baseline := big.NewInt(10_000)
	latest := new(big.Int).Add(baseline, big.NewInt(MaxSafePriceChange24hBPS+1))
	got, err := ComputeTokenStats(TokenInput{Candles: []Candle{
		{Start: now.Add(-25 * time.Hour), Close: baseline},
		{Start: now.Add(-time.Hour), Close: latest},
	}}, now)
	if err != nil {
		t.Fatalf("ComputeTokenStats() error = %v, want a saturated value", err)
	}
	if got.PriceChange24hBPS != MaxSafePriceChange24hBPS {
		t.Fatalf("price change = %d bps, want %d", got.PriceChange24hBPS, int64(MaxSafePriceChange24hBPS))
	}
}

func TestSaturatedPriceChange24hBPSKeepsSafeIntegerBoundaries(t *testing.T) {
	for _, want := range []int64{MinSafePriceChange24hBPS, MaxSafePriceChange24hBPS} {
		if got := saturatedPriceChange24hBPS(big.NewInt(want)); got != want {
			t.Fatalf("saturatedPriceChange24hBPS(%d) = %d; want exact boundary", want, got)
		}
	}
}

func TestSaturatedPriceChange24hBPSClampsValuesOutsideSafeIntegerBoundaries(t *testing.T) {
	huge := new(big.Int).Lsh(big.NewInt(1), 200)
	cases := []struct {
		value *big.Int
		want  int64
	}{
		{big.NewInt(MinSafePriceChange24hBPS - 1), MinSafePriceChange24hBPS},
		{big.NewInt(MaxSafePriceChange24hBPS + 1), MaxSafePriceChange24hBPS},
		{new(big.Int).Neg(huge), MinSafePriceChange24hBPS},
		{huge, MaxSafePriceChange24hBPS},
	}
	for _, tc := range cases {
		if got := saturatedPriceChange24hBPS(tc.value); got != tc.want {
			t.Fatalf("saturatedPriceChange24hBPS(%s) = %d; want %d", tc.value, got, tc.want)
		}
	}
}
