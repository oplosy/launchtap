package chain

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/Contictus/launchtap/backend/deployments"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// MinProductionTimelockDelay is the shortest governance delay a production deployment accepts.
// The factory only checks msg.sender == timelock, so the delay exists only if that address is a
// timelock contract that enforces one (docs/specs/2026-11-16-audit-report-2-fixes-preflight.md).
const MinProductionTimelockDelay = 48 * time.Hour

// GovernanceVerifier reads the factory's timelock and the timelock's code and delay.
type GovernanceVerifier interface {
	CodeReader
	ContractCaller
}

// VerifyGovernanceTimelock fails closed unless a production deployment's factory timelock is a
// contract whose getMinDelay() is at least MinProductionTimelockDelay. Local and testnet
// deployments may use an EOA timelock (instant authority, documented in the testnet runbook),
// so they are not checked.
func VerifyGovernanceTimelock(ctx context.Context, reader GovernanceVerifier, deployment deployments.Deployment) error {
	if deployment.Environment != deployments.EnvironmentProduction {
		return nil
	}
	timelock, err := callAddress(ctx, reader, deployment.Factory, "timelock()")
	if err != nil {
		return fmt.Errorf("read factory timelock: %w", err)
	}
	code, err := reader.CodeAt(ctx, timelock)
	if err != nil {
		return fmt.Errorf("read timelock bytecode at %s: %w", timelock.Hex(), err)
	}
	if len(code) == 0 {
		return fmt.Errorf("%w: timelock %s has no code, so governance changes are instant", ErrTimelockPolicy, timelock.Hex())
	}
	delay, err := callUint(ctx, reader, timelock, "getMinDelay()")
	if err != nil {
		return fmt.Errorf("read timelock %s minimum delay: %w", timelock.Hex(), err)
	}
	minimum := big.NewInt(int64(MinProductionTimelockDelay / time.Second))
	if delay.Cmp(minimum) < 0 {
		return fmt.Errorf("%w: timelock %s minimum delay is %ss, want at least %ss", ErrTimelockPolicy, timelock.Hex(), delay, minimum)
	}
	return nil
}

func callWord(ctx context.Context, caller ContractCaller, to common.Address, signature string) ([]byte, error) {
	result, err := caller.CallContract(ctx, ethereum.CallMsg{To: &to, Data: crypto.Keccak256([]byte(signature))[:4]})
	if err != nil {
		return nil, err
	}
	if len(result) != 32 {
		return nil, fmt.Errorf("%w: %s returned %d bytes", ErrTimelockPolicy, signature, len(result))
	}
	return result, nil
}

func callAddress(ctx context.Context, caller ContractCaller, to common.Address, signature string) (common.Address, error) {
	word, err := callWord(ctx, caller, to, signature)
	if err != nil {
		return common.Address{}, err
	}
	for _, value := range word[:12] {
		if value != 0 {
			return common.Address{}, fmt.Errorf("%w: %s returned a non-padded address", ErrTimelockPolicy, signature)
		}
	}
	return common.BytesToAddress(word[12:]), nil
}

func callUint(ctx context.Context, caller ContractCaller, to common.Address, signature string) (*big.Int, error) {
	word, err := callWord(ctx, caller, to, signature)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(word), nil
}
