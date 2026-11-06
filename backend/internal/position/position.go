// Package position computes a wallet's per-token cost basis and realized PnL
// from its ordered token legs. It is stdlib-only and uses math/big for every
// amount; floats are never involved.
package position

import (
	"errors"
	"math/big"
)

// Leg is the net effect of one transaction on a wallet's balance of one token.
type Leg struct {
	Block       int64
	TxIndex     int32
	TokenDelta  *big.Int // signed net token transfer for the wallet in this tx
	ETHPaid     *big.Int // >= 0, attributed
	ETHReceived *big.Int // >= 0, attributed
}

// Position is the result of folding legs into a cost basis.
type Position struct {
	Balance, RemainingCost, RealizedPnL, TransferredIn *big.Int
	ETHPaid, ETHReceived                               *big.Int
	TradeCount                                         int64 // legs with ETHPaid>0 or ETHReceived>0
	FirstBlock, LastBlock                              int64
}

var (
	// ErrUnordered means legs do not ascend strictly by (Block, TxIndex).
	ErrUnordered = errors.New("position: legs not strictly ordered by (block, tx index)")
	// ErrNegativeBalance means a leg sells more tokens than the wallet holds.
	ErrNegativeBalance = errors.New("position: sell exceeds balance")
	// ErrInvalidLeg means a leg has a nil field or a negative ETH value.
	ErrInvalidLeg = errors.New("position: invalid leg")
)

// Compute folds legs into a Position using average-cost accounting. Legs must
// ascend strictly by (Block, TxIndex). Inputs are copied, never mutated or
// aliased. An empty input yields zero values with FirstBlock = LastBlock = 0.
//
// Let attributed = ETHPaid>0 || ETHReceived>0 and net = ETHPaid - ETHReceived.
//
//   - TokenDelta > 0, attributed: Balance += d; RemainingCost += max(net, 0);
//     RealizedPnL += max(-net, 0).
//   - TokenDelta > 0, not attributed (a transfer in): Balance += d;
//     TransferredIn += d.
//   - TokenDelta < 0 with s = -d: removed = RemainingCost when s == Balance,
//     else floor(RemainingCost*s/Balance); Balance -= s;
//     RemainingCost -= removed; when attributed,
//     RealizedPnL += ETHReceived - ETHPaid - removed.
//   - TokenDelta == 0: RealizedPnL += ETHReceived - ETHPaid.
//
// Errors: ErrInvalidLeg (nil field or negative ETH value), ErrUnordered, and
// ErrNegativeBalance (s > Balance).
func Compute(legs []Leg) (Position, error) {
	p := Position{
		Balance:       new(big.Int),
		RemainingCost: new(big.Int),
		RealizedPnL:   new(big.Int),
		TransferredIn: new(big.Int),
		ETHPaid:       new(big.Int),
		ETHReceived:   new(big.Int),
	}
	for i, l := range legs {
		if l.TokenDelta == nil || l.ETHPaid == nil || l.ETHReceived == nil ||
			l.ETHPaid.Sign() < 0 || l.ETHReceived.Sign() < 0 {
			return Position{}, ErrInvalidLeg
		}
		if i > 0 {
			prev := legs[i-1]
			if l.Block < prev.Block || (l.Block == prev.Block && l.TxIndex <= prev.TxIndex) {
				return Position{}, ErrUnordered
			}
		}
		if i == 0 {
			p.FirstBlock = l.Block
		}
		p.LastBlock = l.Block

		attributed := l.ETHPaid.Sign() > 0 || l.ETHReceived.Sign() > 0
		net := new(big.Int).Sub(l.ETHPaid, l.ETHReceived)
		if attributed {
			p.TradeCount++
			p.ETHPaid.Add(p.ETHPaid, l.ETHPaid)
			p.ETHReceived.Add(p.ETHReceived, l.ETHReceived)
		}

		switch d := l.TokenDelta; d.Sign() {
		case 1:
			p.Balance.Add(p.Balance, d)
			switch {
			case !attributed:
				p.TransferredIn.Add(p.TransferredIn, d)
			case net.Sign() > 0:
				p.RemainingCost.Add(p.RemainingCost, net)
			default:
				p.RealizedPnL.Sub(p.RealizedPnL, net)
			}
		case -1:
			s := new(big.Int).Neg(d)
			if s.Cmp(p.Balance) > 0 {
				return Position{}, ErrNegativeBalance
			}
			removed := new(big.Int)
			if s.Cmp(p.Balance) == 0 {
				removed.Set(p.RemainingCost)
			} else {
				removed.Mul(p.RemainingCost, s)
				removed.Quo(removed, p.Balance) // operands are non-negative: Quo floors
			}
			p.Balance.Sub(p.Balance, s)
			p.RemainingCost.Sub(p.RemainingCost, removed)
			if attributed {
				p.RealizedPnL.Sub(p.RealizedPnL, net)
				p.RealizedPnL.Sub(p.RealizedPnL, removed)
			}
		default:
			p.RealizedPnL.Sub(p.RealizedPnL, net)
		}
	}
	return p, nil
}
