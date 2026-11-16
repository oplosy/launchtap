package chain

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/Contictus/launchtap/backend/deployments"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type governanceChain struct {
	code     map[common.Address][]byte
	timelock common.Address
	delay    *big.Int
}

func (g governanceChain) CodeAt(_ context.Context, address common.Address) ([]byte, error) {
	return g.code[address], nil
}

func (g governanceChain) CallContract(_ context.Context, message ethereum.CallMsg) ([]byte, error) {
	switch {
	case bytes.Equal(message.Data, crypto.Keccak256([]byte("timelock()"))[:4]):
		return common.LeftPadBytes(g.timelock.Bytes(), 32), nil
	case bytes.Equal(message.Data, crypto.Keccak256([]byte("getMinDelay()"))[:4]) && *message.To == g.timelock:
		return common.LeftPadBytes(g.delay.Bytes(), 32), nil
	default:
		return nil, errors.New("execution reverted")
	}
}

func TestVerifyGovernanceTimelock(t *testing.T) {
	t.Parallel()
	factory, timelock := common.HexToAddress("0xf1"), common.HexToAddress("0x71")
	enough := big.NewInt(int64(MinProductionTimelockDelay.Seconds()))
	contract := map[common.Address][]byte{timelock: {0x60}}
	eoa := map[common.Address][]byte{}
	production := deployments.Deployment{Environment: deployments.EnvironmentProduction, Factory: factory}
	testnet := deployments.Deployment{Environment: deployments.EnvironmentTestnet, Factory: factory}
	for _, test := range []struct {
		name       string
		deployment deployments.Deployment
		reader     GovernanceVerifier
		wantErr    bool
		wantPolicy bool
	}{
		{"production timelock contract with enough delay", production, governanceChain{contract, timelock, enough}, false, false},
		{"production EOA timelock", production, governanceChain{eoa, timelock, enough}, true, true},
		{"production delay one second short", production, governanceChain{contract, timelock, new(big.Int).Sub(enough, big.NewInt(1))}, true, true},
		{"production contract without getMinDelay", production, revertingDelay{governanceChain{contract, timelock, enough}}, true, false},
		{"testnet EOA timelock is allowed", testnet, governanceChain{eoa, timelock, big.NewInt(0)}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := VerifyGovernanceTimelock(context.Background(), test.reader, test.deployment)
			if (err != nil) != test.wantErr {
				t.Fatalf("VerifyGovernanceTimelock() error = %v, want error %v", err, test.wantErr)
			}
			if test.wantPolicy && !errors.Is(err, ErrTimelockPolicy) {
				t.Fatalf("VerifyGovernanceTimelock() error = %v, want ErrTimelockPolicy", err)
			}
		})
	}
}

// revertingDelay answers timelock() but reverts getMinDelay(), as an arbitrary contract would.
type revertingDelay struct{ governanceChain }

func (r revertingDelay) CallContract(ctx context.Context, message ethereum.CallMsg) ([]byte, error) {
	if bytes.Equal(message.Data, crypto.Keccak256([]byte("getMinDelay()"))[:4]) {
		return nil, errors.New("execution reverted")
	}
	return r.governanceChain.CallContract(ctx, message)
}
