// Package stats computes rebuildable aggregate values from projection snapshots.
package stats

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

var (
	wad         = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	zeroAddress common.Address
	deadAddress = common.HexToAddress("0x000000000000000000000000000000000000dEaD")
)

const (
	// MaxSafePriceChange24hBPS is the largest integer JavaScript Number represents exactly.
	MaxSafePriceChange24hBPS int64 = 1<<53 - 1
	// MinSafePriceChange24hBPS is the smallest integer JavaScript Number represents exactly.
	MinSafePriceChange24hBPS int64 = -MaxSafePriceChange24hBPS
)

type Holder struct {
	Address common.Address
	Balance *big.Int
}
type Candle struct {
	Start                          time.Time
	Open, High, Low, Close, Volume *big.Int
	Trades                         int64
}
type TokenInput struct {
	Token, Curve, Pair       common.Address
	LaunchPrice              *big.Int
	LaunchAt                 time.Time
	ReserveETH, ReserveToken *big.Int
	TotalSupply              *big.Int
	Candles                  []Candle
	Holders                  []Holder
	PreviousATH              *big.Int
	PreviousATHAt            time.Time
}
type TokenStats struct {
	Token                                                common.Address
	SpotPrice, MarketCap, FDV, Liquidity, ATH, Volume24H *big.Int
	ATHAt                                                time.Time
	PriceChange24hBPS                                    int64
	HolderCount                                          int64
}

// ComputeTokenStats mirrors RecomputeTokenStats. Candles is the '1m' series only; the
// coarser intervals repeat the same trades. PreviousATH is supplied for
// ordinary aggregation so ATH remains monotonic; rollback callers omit it
// after deleting the invalidated token_stats row. It returns an error when the
// computed price change cannot be represented exactly by API clients.
func ComputeTokenStats(input TokenInput, now time.Time) (TokenStats, error) {
	result := TokenStats{Token: input.Token, SpotPrice: new(big.Int), MarketCap: new(big.Int), FDV: new(big.Int), Liquidity: new(big.Int), ATH: nonNegativeCopy(input.LaunchPrice), ATHAt: input.LaunchAt, Volume24H: new(big.Int)}
	if input.PreviousATH != nil && input.PreviousATH.Sign() >= 0 {
		result.ATH.Set(input.PreviousATH)
		result.ATHAt = input.PreviousATHAt
	}
	if positive(input.ReserveETH) && positive(input.ReserveToken) {
		result.SpotPrice.Mul(input.ReserveETH, wad)
		result.SpotPrice.Div(result.SpotPrice, input.ReserveToken)
	}
	if input.TotalSupply != nil && input.TotalSupply.Sign() >= 0 {
		result.FDV.Mul(result.SpotPrice, input.TotalSupply)
		result.FDV.Div(result.FDV, wad)
		circulating := new(big.Int).Set(input.TotalSupply)
		for _, holder := range input.Holders {
			if !positive(holder.Balance) {
				continue
			}
			if isSupplyExcluded(holder.Address, input.Curve) {
				circulating.Sub(circulating, holder.Balance)
			}
			if !isHolderExcluded(holder.Address, input.Curve, input.Pair) {
				result.HolderCount++
			}
		}
		if circulating.Sign() > 0 {
			result.MarketCap.Mul(result.SpotPrice, circulating)
			result.MarketCap.Div(result.MarketCap, wad)
		}
	}
	if positive(input.ReserveETH) {
		result.Liquidity.Set(input.ReserveETH)
	}

	cutoff := now.Add(-24 * time.Hour)
	var baseline, latest *Candle
	athFromCandle := false
	for index := range input.Candles {
		candle := &input.Candles[index]
		if positive(candle.High) && (candle.High.Cmp(result.ATH) > 0 || (athFromCandle && candle.High.Cmp(result.ATH) == 0 && candle.Start.Before(result.ATHAt))) {
			result.ATH.Set(candle.High)
			result.ATHAt = candle.Start
			athFromCandle = true
		}
		if !candle.Start.Before(cutoff) && positive(candle.Volume) {
			result.Volume24H.Add(result.Volume24H, candle.Volume)
		}
		if candle.Close != nil && candle.Start.Compare(cutoff) <= 0 && (baseline == nil || candle.Start.After(baseline.Start)) {
			baseline = candle
		}
		if candle.Close != nil && (latest == nil || candle.Start.After(latest.Start)) {
			latest = candle
		}
	}
	if baseline != nil && positive(baseline.Close) && latest != nil {
		delta := new(big.Int).Sub(latest.Close, baseline.Close)
		delta.Mul(delta, big.NewInt(10_000))
		priceChange := delta.Quo(delta, baseline.Close)
		result.PriceChange24hBPS = saturatedPriceChange24hBPS(priceChange)
	}
	return result, nil
}

// saturatedPriceChange24hBPS clamps to the JavaScript safe-integer range the API publishes. A dust
// baseline makes the change unbounded; saturating keeps the token's stats computable instead of
// failing its recompute forever. Mirrors the LEAST/GREATEST in RecomputeTokenStats.
func saturatedPriceChange24hBPS(value *big.Int) int64 {
	if value.Cmp(big.NewInt(MaxSafePriceChange24hBPS)) > 0 {
		return MaxSafePriceChange24hBPS
	}
	if value.Cmp(big.NewInt(MinSafePriceChange24hBPS)) < 0 {
		return MinSafePriceChange24hBPS
	}
	return value.Int64()
}

func positive(value *big.Int) bool { return value != nil && value.Sign() > 0 }

func nonNegativeCopy(value *big.Int) *big.Int {
	if value == nil || value.Sign() < 0 {
		return new(big.Int)
	}
	return new(big.Int).Set(value)
}

func isSupplyExcluded(address, curve common.Address) bool {
	return address == zeroAddress || address == deadAddress || address == curve
}

func isHolderExcluded(address, curve, pair common.Address) bool {
	return isSupplyExcluded(address, curve) || address == pair
}
