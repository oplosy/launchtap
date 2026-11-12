package main

import (
	"testing"

	"github.com/Contictus/launchtap/backend/deployments"
	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/ethereum/go-ethereum/common"
)

func TestNewDiscoveryFollowsTheDeploymentEngine(t *testing.T) {
	v4 := &deployments.V4Deployment{PoolManager: common.HexToAddress("0x4001"), Locker: common.HexToAddress("0x4002"), Hook: common.HexToAddress("0x4003")}
	factory := common.HexToAddress("0xfac7")
	for _, tc := range []struct {
		name       string
		engine     uint16
		v4         *deployments.V4Deployment
		wantErr    bool
		decoderFor uint16
	}{
		{"engine 1", 1, nil, false, 1},
		{"engine 2", 2, v4, false, 2},
		{"engine 2 without a V4 section", 2, nil, true, 2},
		{"engine 2 with an engine-1 decoder", 2, v4, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decoder, err := chain.NewDecoder(tc.decoderFor)
			if err != nil {
				t.Fatal(err)
			}
			deployment := deployments.Deployment{Factory: factory, EngineVersion: tc.engine, V4: tc.v4}
			discovery, err := newDiscovery(nil, decoder, deployment, 10)
			if tc.wantErr != (err != nil) || (err == nil && discovery == nil) {
				t.Fatalf("newDiscovery() = %v, %v; want error %t", discovery, err, tc.wantErr)
			}
		})
	}
}
