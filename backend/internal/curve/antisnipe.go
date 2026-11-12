package curve

import (
	"fmt"
	"math/big"
)

const (
	// MaxAntiSnipeStartBPS mirrors LaunchTypesV2.MAX_ANTI_SNIPE_START_BPS.
	MaxAntiSnipeStartBPS uint16 = 9000
	// MaxAntiSnipeDurationSeconds mirrors LaunchTypesV2.MAX_ANTI_SNIPE_DURATION_SECONDS.
	MaxAntiSnipeDurationSeconds uint32 = 300
)

// ErrInvalidAntiSnipeProfile mirrors InvalidAntiSnipeProfile.
type ErrInvalidAntiSnipeProfile struct {
	StartBPS        uint16
	DurationSeconds uint32
}

func (err ErrInvalidAntiSnipeProfile) Error() string {
	return fmt.Sprintf("curve: anti-snipe profile %d bps over %d seconds exceeds the factory caps", err.StartBPS, err.DurationSeconds)
}

// AntiSnipe is a launch's immutable anti-snipe profile (spec §4).
type AntiSnipe struct {
	startBPS        uint16
	durationSeconds uint32
}

// NewAntiSnipe validates a profile against the factory caps, as BondingCurveV2.initialize does.
func NewAntiSnipe(startBPS uint16, durationSeconds uint32) (AntiSnipe, error) {
	if startBPS > MaxAntiSnipeStartBPS || durationSeconds > MaxAntiSnipeDurationSeconds {
		return AntiSnipe{}, ErrInvalidAntiSnipeProfile{StartBPS: startBPS, DurationSeconds: durationSeconds}
	}
	return AntiSnipe{startBPS: startBPS, durationSeconds: durationSeconds}, nil
}

func (profile AntiSnipe) StartBPS() uint16        { return profile.startBPS }
func (profile AntiSnipe) DurationSeconds() uint32 { return profile.durationSeconds }

// TaxBPS mirrors AntiSnipe.taxBps: floor(startBps * remaining / durationSeconds), where
// remaining = launchTime + durationSeconds - max(now, launchTime) while positive. A zero start
// or duration disables the tax.
func TaxBPS(profile AntiSnipe, launchTime, now uint64) uint16 {
	if profile.durationSeconds == 0 || profile.startBPS == 0 {
		return 0
	}
	end := launchTime + uint64(profile.durationSeconds)
	effectiveNow := max(now, launchTime)
	if effectiveNow >= end {
		return 0
	}
	remaining := end - effectiveNow
	// remaining <= durationSeconds <= 300 and startBps <= 9000, so the product fits uint64 and
	// the quotient is at most startBps.
	return uint16(uint64(profile.startBPS) * remaining / uint64(profile.durationSeconds))
}

// TaxEndsAt is the first timestamp at which TaxBPS is zero.
func TaxEndsAt(profile AntiSnipe, launchTime uint64) uint64 {
	return launchTime + uint64(profile.durationSeconds)
}

// splitSnipeTax mirrors AntiSnipe.split: tax = floor(gross * taxBps / 10000).
func splitSnipeTax(gross *big.Int, taxBPS uint16) (*big.Int, *big.Int, error) {
	// AntiSnipeBpsTooHigh is unreachable here: TaxBPS never exceeds a capped startBps.
	if taxBPS >= bpsDenominator {
		return nil, nil, ErrInternalInvariant
	}
	tax, err := mulDiv(gross, big.NewInt(int64(taxBPS)), big.NewInt(int64(bpsDenominator)))
	if err != nil {
		return nil, nil, err
	}
	net, err := checkedSub(gross, tax)
	if err != nil {
		return nil, nil, err
	}
	return tax, net, nil
}

// grossForNetSnipeTax mirrors AntiSnipe.grossForNet: the smallest gross whose post-tax net is
// at least net, floor((net - 1) * 10000 / (10000 - taxBps)) + 1 for net > 0.
func grossForNetSnipeTax(net *big.Int, taxBPS uint16) (*big.Int, error) {
	if taxBPS >= bpsDenominator {
		return nil, ErrInternalInvariant
	}
	if net.Sign() == 0 {
		return new(big.Int), nil
	}
	grossMinusOne, err := mulDiv(
		new(big.Int).Sub(net, big.NewInt(1)),
		big.NewInt(int64(bpsDenominator)),
		big.NewInt(int64(bpsDenominator-taxBPS)),
	)
	if err != nil {
		return nil, err
	}
	return checkedAdd(grossMinusOne, big.NewInt(1))
}

// BuyQuoteV2 is a v1 curve quote over the post-tax ETH plus the outer anti-snipe layer.
type BuyQuoteV2 struct {
	curve        BuyQuote
	ethGrossUsed *big.Int
	snipeTax     *big.Int
	refund       *big.Int
	taxBPS       uint16
	taxEndsAt    uint64
}

func (quote BuyQuoteV2) ETHGrossUsed() *big.Int { return copyBig(quote.ethGrossUsed) }
func (quote BuyQuoteV2) TokensOut() *big.Int    { return quote.curve.TokensOut() }
func (quote BuyQuoteV2) ProtocolFee() *big.Int  { return quote.curve.ProtocolFee() }
func (quote BuyQuoteV2) CreatorFee() *big.Int   { return quote.curve.CreatorFee() }
func (quote BuyQuoteV2) SnipeTax() *big.Int     { return copyBig(quote.snipeTax) }
func (quote BuyQuoteV2) Refund() *big.Int       { return copyBig(quote.refund) }
func (quote BuyQuoteV2) Graduates() bool        { return quote.curve.Graduates() }
func (quote BuyQuoteV2) NextState() State       { return quote.curve.NextState() }
func (quote BuyQuoteV2) TaxBPS() uint16         { return quote.taxBPS }
func (quote BuyQuoteV2) TaxEndsAt() uint64      { return quote.taxEndsAt }

// BuyV2 mirrors BondingCurveV2._quoteBuy at `now`: the tax is split off the supplied gross and
// the post-tax net is quoted by the v1 curve. On a final buy the consumed gross is
// grossForNet(v1 gross used), rounded up so the protocol is never short, and the refunded
// excess is never taxed.
func BuyV2(state State, parameters Parameters, profile AntiSnipe, launchTime, now uint64, suppliedGross *big.Int) (BuyQuoteV2, error) {
	gross, err := copyUint256("suppliedGross", suppliedGross)
	if err != nil {
		return BuyQuoteV2{}, err
	}
	taxBPS := TaxBPS(profile, launchTime, now)
	tax, net, err := splitSnipeTax(gross, taxBPS)
	if err != nil {
		return BuyQuoteV2{}, err
	}
	curveQuote, err := Buy(state, parameters, net)
	if err != nil {
		return BuyQuoteV2{}, err
	}
	quote := BuyQuoteV2{
		curve:        curveQuote,
		ethGrossUsed: gross,
		snipeTax:     tax,
		refund:       new(big.Int),
		taxBPS:       taxBPS,
		taxEndsAt:    TaxEndsAt(profile, launchTime),
	}
	if !curveQuote.Graduates() {
		return quote, nil
	}
	curveGross := curveQuote.ETHGrossUsed()
	quote.ethGrossUsed, err = grossForNetSnipeTax(curveGross, taxBPS)
	if err != nil {
		return BuyQuoteV2{}, err
	}
	if quote.snipeTax, err = checkedSub(quote.ethGrossUsed, curveGross); err != nil {
		return BuyQuoteV2{}, err
	}
	if quote.refund, err = checkedSub(gross, quote.ethGrossUsed); err != nil {
		return BuyQuoteV2{}, err
	}
	return quote, nil
}
