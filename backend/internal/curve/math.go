package curve

import "math/big"

const (
	bpsDenominator uint16 = 10_000
)

var (
	maxUint256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	wad        = big.NewInt(1_000_000_000_000_000_000)
)

// Phase mirrors LaunchTypes.Phase.
type Phase uint8

const (
	PhaseCurve Phase = iota
	PhaseGraduated
)

// Parameters is an immutable, validated bonding-curve parameter set.
type Parameters struct {
	totalSupply         *big.Int
	curveTokens         *big.Int
	lpTokens            *big.Int
	graduationETH       *big.Int
	initialVirtualETH   *big.Int
	initialVirtualToken *big.Int
	tradeFeeBPS         uint16
	protocolShareBPS    uint16
	invariant           *big.Int
	finalVirtualToken   *big.Int
	finalVirtualETH     *big.Int
}

// NewParameters validates and copies a complete Solidity CurveParameters value.
func NewParameters(
	totalSupply *big.Int,
	curveTokens *big.Int,
	lpTokens *big.Int,
	graduationETH *big.Int,
	initialVirtualETH *big.Int,
	initialVirtualToken *big.Int,
	tradeFeeBPS uint16,
	protocolShareBPS uint16,
) (Parameters, error) {
	values := make([]*big.Int, 6)
	inputs := []struct {
		name  string
		value *big.Int
	}{
		{"totalSupply", totalSupply},
		{"curveTokens", curveTokens},
		{"lpTokens", lpTokens},
		{"graduationETH", graduationETH},
		{"initialVirtualETH", initialVirtualETH},
		{"initialVirtualToken", initialVirtualToken},
	}
	for index, input := range inputs {
		value, err := copyUint256(input.name, input.value)
		if err != nil {
			return Parameters{}, err
		}
		values[index] = value
	}

	totalSupplyCopy := values[0]
	curveTokensCopy := values[1]
	lpTokensCopy := values[2]
	graduationETHCopy := values[3]
	initialVirtualETHCopy := values[4]
	initialVirtualTokenCopy := values[5]

	allocatedSupply, err := checkedAdd(curveTokensCopy, lpTokensCopy)
	if err != nil {
		return Parameters{}, err
	}
	if totalSupplyCopy.Cmp(allocatedSupply) != 0 {
		return Parameters{}, ErrInvalidSupplyAllocation{
			TotalSupply: copyBig(totalSupplyCopy),
			CurveTokens: copyBig(curveTokensCopy),
			LPTokens:    copyBig(lpTokensCopy),
		}
	}
	if lpTokensCopy.Sign() == 0 || curveTokensCopy.Cmp(lpTokensCopy) <= 0 {
		return Parameters{}, ErrInvalidCurveAllocation{
			CurveTokens: copyBig(curveTokensCopy),
			LPTokens:    copyBig(lpTokensCopy),
		}
	}
	if graduationETHCopy.Sign() == 0 {
		return Parameters{}, ErrInvalidGraduationETH
	}
	if initialVirtualETHCopy.Sign() == 0 || initialVirtualTokenCopy.Cmp(curveTokensCopy) <= 0 {
		return Parameters{}, ErrInvalidVirtualReserves{
			InitialVirtualETH:   copyBig(initialVirtualETHCopy),
			InitialVirtualToken: copyBig(initialVirtualTokenCopy),
			CurveTokens:         copyBig(curveTokensCopy),
		}
	}
	if tradeFeeBPS >= bpsDenominator {
		return Parameters{}, ErrInvalidTradeFeeBPS{TradeFeeBPS: tradeFeeBPS}
	}
	if protocolShareBPS > bpsDenominator {
		return Parameters{}, ErrInvalidProtocolShareBPS{ProtocolShareBPS: protocolShareBPS}
	}

	invariant, err := checkedMul(initialVirtualETHCopy, initialVirtualTokenCopy)
	if err != nil {
		return Parameters{}, err
	}
	finalVirtualToken, err := checkedSub(initialVirtualTokenCopy, curveTokensCopy)
	if err != nil {
		return Parameters{}, err
	}
	finalVirtualETH, err := checkedAdd(initialVirtualETHCopy, graduationETHCopy)
	if err != nil {
		return Parameters{}, err
	}
	roundTripETH, err := ceilDiv(invariant, finalVirtualToken)
	if err != nil {
		return Parameters{}, err
	}
	roundTripToken, err := ceilDiv(invariant, finalVirtualETH)
	if err != nil {
		return Parameters{}, err
	}
	if roundTripETH.Cmp(finalVirtualETH) != 0 || roundTripToken.Cmp(finalVirtualToken) != 0 {
		return Parameters{}, ErrInvalidCurveBoundary{
			Invariant:         copyBig(invariant),
			FinalVirtualToken: copyBig(finalVirtualToken),
			FinalVirtualETH:   copyBig(finalVirtualETH),
		}
	}

	return Parameters{
		totalSupply:         totalSupplyCopy,
		curveTokens:         curveTokensCopy,
		lpTokens:            lpTokensCopy,
		graduationETH:       graduationETHCopy,
		initialVirtualETH:   initialVirtualETHCopy,
		initialVirtualToken: initialVirtualTokenCopy,
		tradeFeeBPS:         tradeFeeBPS,
		protocolShareBPS:    protocolShareBPS,
		invariant:           invariant,
		finalVirtualToken:   finalVirtualToken,
		finalVirtualETH:     finalVirtualETH,
	}, nil
}

func (parameters Parameters) TotalSupply() *big.Int   { return copyBig(parameters.totalSupply) }
func (parameters Parameters) CurveTokens() *big.Int   { return copyBig(parameters.curveTokens) }
func (parameters Parameters) LPTokens() *big.Int      { return copyBig(parameters.lpTokens) }
func (parameters Parameters) GraduationETH() *big.Int { return copyBig(parameters.graduationETH) }
func (parameters Parameters) InitialVirtualETH() *big.Int {
	return copyBig(parameters.initialVirtualETH)
}
func (parameters Parameters) InitialVirtualToken() *big.Int {
	return copyBig(parameters.initialVirtualToken)
}
func (parameters Parameters) TradeFeeBPS() uint16      { return parameters.tradeFeeBPS }
func (parameters Parameters) ProtocolShareBPS() uint16 { return parameters.protocolShareBPS }
func (parameters Parameters) Invariant() *big.Int      { return copyBig(parameters.invariant) }
func (parameters Parameters) FinalVirtualToken() *big.Int {
	return copyBig(parameters.finalVirtualToken)
}
func (parameters Parameters) FinalVirtualETH() *big.Int { return copyBig(parameters.finalVirtualETH) }

// State is an immutable snapshot of the on-chain curve state used by quotes.
type State struct {
	phase        Phase
	virtualETH   *big.Int
	virtualToken *big.Int
	protocolFees *big.Int
	creatorFees  *big.Int
}

// NewState validates and copies one curve state snapshot.
func NewState(
	phase Phase,
	virtualETH *big.Int,
	virtualToken *big.Int,
	protocolFees *big.Int,
	creatorFees *big.Int,
) (State, error) {
	virtualETHCopy, err := copyUint256("virtualETH", virtualETH)
	if err != nil {
		return State{}, err
	}
	virtualTokenCopy, err := copyUint256("virtualToken", virtualToken)
	if err != nil {
		return State{}, err
	}
	protocolFeesCopy, err := copyUint256("protocolFees", protocolFees)
	if err != nil {
		return State{}, err
	}
	creatorFeesCopy, err := copyUint256("creatorFees", creatorFees)
	if err != nil {
		return State{}, err
	}
	return State{
		phase:        phase,
		virtualETH:   virtualETHCopy,
		virtualToken: virtualTokenCopy,
		protocolFees: protocolFeesCopy,
		creatorFees:  creatorFeesCopy,
	}, nil
}

func (state State) Phase() Phase           { return state.phase }
func (state State) VirtualETH() *big.Int   { return copyBig(state.virtualETH) }
func (state State) VirtualToken() *big.Int { return copyBig(state.virtualToken) }
func (state State) ProtocolFees() *big.Int { return copyBig(state.protocolFees) }
func (state State) CreatorFees() *big.Int  { return copyBig(state.creatorFees) }

// BuyQuote contains a buy result and its immutable next state.
type BuyQuote struct {
	ethGrossUsed *big.Int
	tokensOut    *big.Int
	protocolFee  *big.Int
	creatorFee   *big.Int
	refund       *big.Int
	graduates    bool
	nextState    State
}

func (quote BuyQuote) ETHGrossUsed() *big.Int { return copyBig(quote.ethGrossUsed) }
func (quote BuyQuote) TokensOut() *big.Int    { return copyBig(quote.tokensOut) }
func (quote BuyQuote) ProtocolFee() *big.Int  { return copyBig(quote.protocolFee) }
func (quote BuyQuote) CreatorFee() *big.Int   { return copyBig(quote.creatorFee) }
func (quote BuyQuote) Refund() *big.Int       { return copyBig(quote.refund) }
func (quote BuyQuote) Graduates() bool        { return quote.graduates }
func (quote BuyQuote) NextState() State       { return copyState(quote.nextState) }

// SellQuote contains a sell result and its immutable next state.
type SellQuote struct {
	ethOut      *big.Int
	ethGross    *big.Int
	protocolFee *big.Int
	creatorFee  *big.Int
	nextState   State
}

func (quote SellQuote) ETHOut() *big.Int      { return copyBig(quote.ethOut) }
func (quote SellQuote) ETHGross() *big.Int    { return copyBig(quote.ethGross) }
func (quote SellQuote) ProtocolFee() *big.Int { return copyBig(quote.protocolFee) }
func (quote SellQuote) CreatorFee() *big.Int  { return copyBig(quote.creatorFee) }
func (quote SellQuote) NextState() State      { return copyState(quote.nextState) }

// Buy mirrors the contract's phase gate and CurveMath.quoteBuy control flow.
func Buy(state State, parameters Parameters, suppliedGross *big.Int) (BuyQuote, error) {
	if state.phase != PhaseCurve {
		return BuyQuote{}, ErrWrongPhase{Expected: PhaseCurve, Actual: state.phase}
	}
	if err := validateState(state); err != nil {
		return BuyQuote{}, err
	}
	if err := validateParameters(parameters); err != nil {
		return BuyQuote{}, err
	}
	gross, err := copyUint256("suppliedGross", suppliedGross)
	if err != nil {
		return BuyQuote{}, err
	}
	if gross.Sign() == 0 {
		return BuyQuote{}, ErrZeroInput
	}

	totalFee, protocolFee, creatorFee, err := splitFees(gross, parameters.tradeFeeBPS, parameters.protocolShareBPS)
	if err != nil {
		return BuyQuote{}, err
	}
	effectiveETH, err := checkedSub(gross, totalFee)
	if err != nil {
		return BuyQuote{}, err
	}
	candidateVirtualETH, err := checkedAdd(state.virtualETH, effectiveETH)
	if err != nil {
		return BuyQuote{}, err
	}
	candidateVirtualToken, err := ceilDiv(parameters.invariant, candidateVirtualETH)
	if err != nil {
		return BuyQuote{}, err
	}
	finalVirtualETH, err := ceilDiv(parameters.invariant, parameters.finalVirtualToken)
	if err != nil {
		return BuyQuote{}, err
	}
	if candidateVirtualToken.Cmp(state.virtualToken) >= 0 {
		return BuyQuote{}, ErrZeroOutput
	}

	ethGrossUsed := gross
	newVirtualETH := candidateVirtualETH
	newVirtualToken := candidateVirtualToken
	refund := new(big.Int)
	graduates := false
	if candidateVirtualETH.Cmp(finalVirtualETH) > 0 {
		if state.virtualETH.Cmp(finalVirtualETH) >= 0 {
			return BuyQuote{}, ErrInternalInvariant
		}
		netNeeded, subtractErr := checkedSub(finalVirtualETH, state.virtualETH)
		if subtractErr != nil || netNeeded.Sign() <= 0 {
			return BuyQuote{}, ErrInternalInvariant
		}
		ethGrossUsed, err = exactGrossForNet(netNeeded, parameters.tradeFeeBPS)
		if err != nil {
			return BuyQuote{}, err
		}
		_, protocolFee, creatorFee, err = splitFees(
			ethGrossUsed,
			parameters.tradeFeeBPS,
			parameters.protocolShareBPS,
		)
		if err != nil {
			return BuyQuote{}, err
		}
		newVirtualETH = copyBig(finalVirtualETH)
		newVirtualToken = copyBig(parameters.finalVirtualToken)
		refund, err = checkedSub(gross, ethGrossUsed)
		if err != nil {
			return BuyQuote{}, err
		}
		graduates = true
	} else {
		graduates = candidateVirtualToken.Cmp(parameters.finalVirtualToken) == 0
	}

	tokensOut, err := checkedSub(state.virtualToken, newVirtualToken)
	if err != nil {
		return BuyQuote{}, err
	}
	if tokensOut.Sign() == 0 {
		return BuyQuote{}, ErrZeroOutput
	}
	nextProtocolFees, err := checkedAdd(state.protocolFees, protocolFee)
	if err != nil {
		return BuyQuote{}, err
	}
	nextCreatorFees, err := checkedAdd(state.creatorFees, creatorFee)
	if err != nil {
		return BuyQuote{}, err
	}
	nextPhase := PhaseCurve
	if graduates {
		nextPhase = PhaseGraduated
	}
	nextState := State{
		phase:        nextPhase,
		virtualETH:   copyBig(newVirtualETH),
		virtualToken: copyBig(newVirtualToken),
		protocolFees: nextProtocolFees,
		creatorFees:  nextCreatorFees,
	}
	return BuyQuote{
		ethGrossUsed: copyBig(ethGrossUsed),
		tokensOut:    copyBig(tokensOut),
		protocolFee:  copyBig(protocolFee),
		creatorFee:   copyBig(creatorFee),
		refund:       copyBig(refund),
		graduates:    graduates,
		nextState:    nextState,
	}, nil
}

// Sell mirrors the contract's phase gate and CurveMath.quoteSell control flow.
func Sell(state State, parameters Parameters, tokensIn *big.Int) (SellQuote, error) {
	if state.phase != PhaseCurve {
		return SellQuote{}, ErrWrongPhase{Expected: PhaseCurve, Actual: state.phase}
	}
	if err := validateState(state); err != nil {
		return SellQuote{}, err
	}
	if err := validateParameters(parameters); err != nil {
		return SellQuote{}, err
	}
	tokens, err := copyUint256("tokensIn", tokensIn)
	if err != nil {
		return SellQuote{}, err
	}
	if tokens.Sign() == 0 {
		return SellQuote{}, ErrZeroInput
	}
	soldTokens, err := TokensSold(parameters, state)
	if err != nil {
		return SellQuote{}, err
	}
	if tokens.Cmp(soldTokens) > 0 {
		return SellQuote{}, ErrOversell{Attempted: copyBig(tokens), Sold: copyBig(soldTokens)}
	}

	newVirtualToken, err := checkedAdd(state.virtualToken, tokens)
	if err != nil {
		return SellQuote{}, err
	}
	newVirtualETH, err := ceilDiv(parameters.invariant, newVirtualToken)
	if err != nil {
		return SellQuote{}, err
	}
	ethGross, err := checkedSub(state.virtualETH, newVirtualETH)
	if err != nil {
		return SellQuote{}, err
	}
	if ethGross.Sign() == 0 {
		return SellQuote{}, ErrZeroOutput
	}
	totalFee, protocolFee, creatorFee, err := splitFees(
		ethGross,
		parameters.tradeFeeBPS,
		parameters.protocolShareBPS,
	)
	if err != nil {
		return SellQuote{}, err
	}
	ethOut, err := checkedSub(ethGross, totalFee)
	if err != nil {
		return SellQuote{}, err
	}
	if ethOut.Sign() == 0 {
		return SellQuote{}, ErrZeroOutput
	}
	nextProtocolFees, err := checkedAdd(state.protocolFees, protocolFee)
	if err != nil {
		return SellQuote{}, err
	}
	nextCreatorFees, err := checkedAdd(state.creatorFees, creatorFee)
	if err != nil {
		return SellQuote{}, err
	}
	nextState := State{
		phase:        PhaseCurve,
		virtualETH:   copyBig(newVirtualETH),
		virtualToken: copyBig(newVirtualToken),
		protocolFees: nextProtocolFees,
		creatorFees:  nextCreatorFees,
	}
	return SellQuote{
		ethOut:      copyBig(ethOut),
		ethGross:    copyBig(ethGross),
		protocolFee: copyBig(protocolFee),
		creatorFee:  copyBig(creatorFee),
		nextState:   nextState,
	}, nil
}

// SpotPriceWad returns floor(virtualETH * 1e18 / virtualToken).
func SpotPriceWad(state State) (*big.Int, error) {
	if err := validateState(state); err != nil {
		return nil, err
	}
	return mulDiv(state.virtualETH, wad, state.virtualToken)
}

// TokensSold returns initialVirtualToken - virtualToken.
func TokensSold(parameters Parameters, state State) (*big.Int, error) {
	if err := validateParameters(parameters); err != nil {
		return nil, err
	}
	if err := validateState(state); err != nil {
		return nil, err
	}
	return checkedSub(parameters.initialVirtualToken, state.virtualToken)
}

// RealCurveETH returns virtualETH - initialVirtualETH.
func RealCurveETH(parameters Parameters, state State) (*big.Int, error) {
	if err := validateParameters(parameters); err != nil {
		return nil, err
	}
	if err := validateState(state); err != nil {
		return nil, err
	}
	return checkedSub(state.virtualETH, parameters.initialVirtualETH)
}
