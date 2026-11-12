package quote

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/Contictus/launchtap/backend/internal/curve"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/ethereum/go-ethereum/common"
)

type Service struct {
	Reader  token.QuoteReader
	ChainID int64
}

type Result struct {
	Input, Output, ProtocolFee, CreatorFee, Refund *big.Int
	Graduates                                      bool
	NextVirtualETH, NextVirtualToken               *big.Int
	AsOfBlock                                      int64
	Finality                                       string
	ReserveSourceBlock                             int64
	ReserveSourceHash                              common.Hash
	// The engine-2 anti-snipe tax at the indexed head's block time. SnipeTax is charged on buys
	// only; TaxBPS is the rate in force for either side, and TaxEndsAt is zero once it is.
	SnipeTax  *big.Int
	TaxBPS    uint16
	TaxEndsAt time.Time
}
type Provider interface {
	Quote(context.Context, common.Address, bool, *big.Int) (Result, error)
}

func (s Service) Quote(ctx context.Context, address common.Address, buy bool, amount *big.Int) (Result, error) {
	state, err := s.Reader.ReadQuoteState(ctx, s.ChainID, address)
	if err != nil {
		return Result{}, err
	}
	d := state.Detail
	p, err := curve.NewParameters(d.TotalSupply, d.CurveTokens, d.LPTokens, d.GraduationETH, d.InitialVirtualETH, d.InitialVirtualToken, d.TradeFeeBPS, d.ProtocolShareBPS)
	if err != nil {
		return Result{}, err
	}
	phase := curve.PhaseCurve
	if d.Phase == "graduated" {
		phase = curve.PhaseGraduated
	}
	cs, err := curve.NewState(phase, d.ETHReserve, d.TokenReserve, state.ProtocolFees, state.CreatorFees)
	if err != nil {
		return Result{}, err
	}
	result := Result{Input: new(big.Int).Set(amount), AsOfBlock: d.Snapshot.BlockNumber, Finality: d.Finality, ReserveSourceBlock: d.ReserveBlock, ReserveSourceHash: d.ReserveHash, SnipeTax: new(big.Int)}
	if d.EngineVersion == 2 {
		return quoteV2(state, cs, p, buy, amount, result)
	}
	if buy {
		q, e := curve.Buy(cs, p, amount)
		if e != nil {
			return result, e
		}
		n := q.NextState()
		result.Output = q.TokensOut()
		result.ProtocolFee = q.ProtocolFee()
		result.CreatorFee = q.CreatorFee()
		result.Refund = q.Refund()
		result.Graduates = q.Graduates()
		result.NextVirtualETH = n.VirtualETH()
		result.NextVirtualToken = n.VirtualToken()
		return result, nil
	}
	q, e := curve.Sell(cs, p, amount)
	if e != nil {
		return result, e
	}
	n := q.NextState()
	result.Output = q.ETHOut()
	result.ProtocolFee = q.ProtocolFee()
	result.CreatorFee = q.CreatorFee()
	result.Refund = new(big.Int)
	result.NextVirtualETH = n.VirtualETH()
	result.NextVirtualToken = n.VirtualToken()
	return result, nil
}

// quoteV2 quotes an engine-2 curve at the indexed head's block time (pre-flight B5). A buy is
// curve.BuyV2, which mirrors BondingCurveV2._quoteBuy; a sell is untaxed.
func quoteV2(state token.QuoteState, cs curve.State, p curve.Parameters, buy bool, amount *big.Int, result Result) (Result, error) {
	profile, err := curve.NewAntiSnipe(state.AntiSnipeStartBPS, state.AntiSnipeDurationSeconds)
	if err != nil {
		return result, err
	}
	launch, err := unixSeconds(state.Detail.LaunchTime)
	if err != nil {
		return result, err
	}
	now, err := unixSeconds(state.HeadTime)
	if err != nil {
		return result, err
	}
	result.TaxBPS = curve.TaxBPS(profile, launch, now)
	if result.TaxBPS > 0 {
		result.TaxEndsAt = time.Unix(int64(curve.TaxEndsAt(profile, launch)), 0).UTC()
	}
	if !buy {
		q, e := curve.Sell(cs, p, amount)
		if e != nil {
			return result, e
		}
		n := q.NextState()
		result.Output = q.ETHOut()
		result.ProtocolFee = q.ProtocolFee()
		result.CreatorFee = q.CreatorFee()
		result.Refund = new(big.Int)
		result.NextVirtualETH = n.VirtualETH()
		result.NextVirtualToken = n.VirtualToken()
		return result, nil
	}
	q, e := curve.BuyV2(cs, p, profile, launch, now, amount)
	if e != nil {
		return result, e
	}
	n := q.NextState()
	result.Output = q.TokensOut()
	result.ProtocolFee = q.ProtocolFee()
	result.CreatorFee = q.CreatorFee()
	result.SnipeTax = q.SnipeTax()
	result.Refund = q.Refund()
	result.Graduates = q.Graduates()
	result.NextVirtualETH = n.VirtualETH()
	result.NextVirtualToken = n.VirtualToken()
	return result, nil
}

var errPreEpochTime = errors.New("quote: block time before the Unix epoch")

func unixSeconds(at time.Time) (uint64, error) {
	if at.Unix() < 0 {
		return 0, errPreEpochTime
	}
	return uint64(at.Unix()), nil
}
