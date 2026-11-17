package curve

import "math/big"

func splitFees(gross *big.Int, feeBPS uint16, protocolShareBPS uint16) (*big.Int, *big.Int, *big.Int, error) {
	if feeBPS >= bpsDenominator {
		return nil, nil, nil, ErrInvalidTradeFeeBPS{TradeFeeBPS: feeBPS}
	}
	if protocolShareBPS > bpsDenominator {
		return nil, nil, nil, ErrInvalidProtocolShareBPS{ProtocolShareBPS: protocolShareBPS}
	}
	totalFee, err := mulDiv(gross, new(big.Int).SetUint64(uint64(feeBPS)), new(big.Int).SetUint64(uint64(bpsDenominator)))
	if err != nil {
		return nil, nil, nil, err
	}
	protocolFee, err := mulDiv(
		totalFee,
		new(big.Int).SetUint64(uint64(protocolShareBPS)),
		new(big.Int).SetUint64(uint64(bpsDenominator)),
	)
	if err != nil {
		return nil, nil, nil, err
	}
	creatorFee, err := checkedSub(totalFee, protocolFee)
	if err != nil {
		return nil, nil, nil, err
	}
	return totalFee, protocolFee, creatorFee, nil
}

func exactGrossForNet(net *big.Int, feeBPS uint16) (*big.Int, error) {
	if net.Sign() == 0 {
		return nil, ErrZeroInput
	}
	if feeBPS >= bpsDenominator {
		return nil, ErrInvalidTradeFeeBPS{TradeFeeBPS: feeBPS}
	}
	netMinusOne, err := checkedSub(net, big.NewInt(1))
	if err != nil {
		return nil, err
	}
	grossMinusOne, err := mulDiv(
		netMinusOne,
		new(big.Int).SetUint64(uint64(bpsDenominator)),
		new(big.Int).SetUint64(uint64(bpsDenominator-feeBPS)),
	)
	if err != nil {
		return nil, err
	}
	return checkedAdd(grossMinusOne, big.NewInt(1))
}

func mulDiv(x *big.Int, y *big.Int, denominator *big.Int) (*big.Int, error) {
	if denominator.Sign() == 0 {
		return nil, ErrDivisionByZero
	}
	product := new(big.Int).Mul(x, y)
	result := new(big.Int).Quo(product, denominator)
	if result.Cmp(maxUint256) > 0 {
		return nil, ErrArithmeticOverflow
	}
	return result, nil
}

func ceilDiv(numerator *big.Int, denominator *big.Int) (*big.Int, error) {
	if denominator.Sign() == 0 {
		return nil, ErrDivisionByZero
	}
	if numerator.Sign() == 0 {
		return new(big.Int), nil
	}
	adjusted := new(big.Int).Sub(numerator, big.NewInt(1))
	return new(big.Int).Add(new(big.Int).Quo(adjusted, denominator), big.NewInt(1)), nil
}

func checkedAdd(left *big.Int, right *big.Int) (*big.Int, error) {
	result := new(big.Int).Add(left, right)
	if result.Cmp(maxUint256) > 0 {
		return nil, ErrArithmeticOverflow
	}
	return result, nil
}

func checkedSub(left *big.Int, right *big.Int) (*big.Int, error) {
	if left.Cmp(right) < 0 {
		return nil, ErrArithmeticOverflow
	}
	return new(big.Int).Sub(left, right), nil
}

func checkedMul(left *big.Int, right *big.Int) (*big.Int, error) {
	result := new(big.Int).Mul(left, right)
	if result.Cmp(maxUint256) > 0 {
		return nil, ErrArithmeticOverflow
	}
	return result, nil
}

func copyUint256(field string, value *big.Int) (*big.Int, error) {
	if value == nil || value.Sign() < 0 || value.Cmp(maxUint256) > 0 {
		return nil, ErrInvalidAmount{Field: field}
	}
	return copyBig(value), nil
}

func validateParameters(parameters Parameters) error {
	values := []struct {
		name  string
		value *big.Int
	}{
		{"totalSupply", parameters.totalSupply},
		{"curveTokens", parameters.curveTokens},
		{"lpTokens", parameters.lpTokens},
		{"graduationETH", parameters.graduationETH},
		{"initialVirtualETH", parameters.initialVirtualETH},
		{"initialVirtualToken", parameters.initialVirtualToken},
		{"invariant", parameters.invariant},
		{"finalVirtualToken", parameters.finalVirtualToken},
		{"finalVirtualETH", parameters.finalVirtualETH},
	}
	for _, value := range values {
		if _, err := copyUint256(value.name, value.value); err != nil {
			return err
		}
	}
	return nil
}

func validateState(state State) error {
	values := []struct {
		name  string
		value *big.Int
	}{
		{"virtualETH", state.virtualETH},
		{"virtualToken", state.virtualToken},
		{"protocolFees", state.protocolFees},
		{"creatorFees", state.creatorFees},
	}
	for _, value := range values {
		if _, err := copyUint256(value.name, value.value); err != nil {
			return err
		}
	}
	return nil
}

func copyState(state State) State {
	return State{
		phase:        state.phase,
		virtualETH:   copyBig(state.virtualETH),
		virtualToken: copyBig(state.virtualToken),
		protocolFees: copyBig(state.protocolFees),
		creatorFees:  copyBig(state.creatorFees),
	}
}

func copyBig(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}
	return new(big.Int).Set(value)
}
