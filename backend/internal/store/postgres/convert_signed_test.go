package postgres

import (
	"math/big"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestSignedNumericRoundTripAtBounds(t *testing.T) {
	limit := new(big.Int).Sub(new(big.Int).Exp(big.NewInt(10), big.NewInt(78), nil), big.NewInt(1))
	for _, value := range []*big.Int{limit, new(big.Int).Neg(limit), big.NewInt(0), big.NewInt(-1), big.NewInt(1)} {
		numeric, err := signedNumeric("pnl", value)
		if err != nil {
			t.Fatalf("signedNumeric(%s): %v", value, err)
		}
		back, err := signedBigInt("pnl", numeric)
		if err != nil {
			t.Fatalf("signedBigInt(%s): %v", value, err)
		}
		if back.Cmp(value) != 0 {
			t.Fatalf("round trip %s -> %s", value, back)
		}
	}
}

func TestSignedNumericRejectsOutOfRange(t *testing.T) {
	over := new(big.Int).Exp(big.NewInt(10), big.NewInt(79), nil)
	for _, value := range []*big.Int{over, new(big.Int).Neg(over), nil} {
		if _, err := signedNumeric("pnl", value); err == nil {
			t.Fatalf("signedNumeric(%v) accepted", value)
		}
	}
}

func TestSignedBigIntScalesExponent(t *testing.T) {
	got, err := signedBigInt("pnl", pgtype.Numeric{Int: big.NewInt(-12), Exp: 3, Valid: true})
	if err != nil || got.Cmp(big.NewInt(-12000)) != 0 {
		t.Fatalf("got %v, %v; want -12000", got, err)
	}
	got, err = signedBigInt("pnl", pgtype.Numeric{Int: big.NewInt(-1200), Exp: -2, Valid: true})
	if err != nil || got.Cmp(big.NewInt(-12)) != 0 {
		t.Fatalf("got %v, %v; want -12", got, err)
	}
	for _, bad := range []pgtype.Numeric{
		{Valid: false},
		{Int: big.NewInt(5), Exp: -1, Valid: true},
		{NaN: true, Valid: true},
		{Int: big.NewInt(1), Exp: 79, Valid: true},
	} {
		if _, err := signedBigInt("pnl", bad); err == nil {
			t.Fatalf("signedBigInt(%+v) accepted", bad)
		}
	}
}
