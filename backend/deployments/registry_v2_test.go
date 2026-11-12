package deployments

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

const validManifestV2 = `{
  "schemaVersion": 1,
  "deploymentId": "anvil-v2",
  "name": "Local Anvil",
  "environment": "local",
  "chainId": 31337,
  "factory": "0x0165878a594ca255338adfa4d48449f69242eb8f",
  "startBlock": 3,
  "engineVersion": 2,
  "curveImplementation": "0x5fc8d32690cc91d4c39d9d3abcbd16989f875707",
  "poolManager": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
  "positionManager": "0xe7f1725e7734ce288f8367e1bb143e90bb3f0512",
  "permit2": "0x000000000022D473030F116dDEE9F6B43aC78BA3",
  "hook": "0x8a791620dd6260079bf849dc5567adc3f2fd2000",
  "locker": "0x610178da211fef7d417bc0e6fed39f05609ad788",
  "pool": {
    "currency0": "0x0000000000000000000000000000000000000000",
    "fee": 10000,
    "tickSpacing": 200
  },
  "explorerBase": "",
  "graduationEnabled": true,
  "deployTransaction": "0xe237a00d7888e2b095e7067bd726a83c30d5d1d8a58b7d899b505d9b395a5119",
  "bytecodeHashes": {
    "launchFactory": "0x3e9a2004917efc73f12e42947f013300e409fad73305c77307ce17fa9234417c",
    "bondingCurveV2": "0x82067d3aaf6a33091b1971a469e78c4314476e73f51b125f072b433b530d5062",
    "hook": "0x8d88e396de2375f3c7851b3b0d491455ad0387eea71469ac19280019852b49dc",
    "locker": "0xf59f833f8fabcaf9d8d6959181ad6e25b67ccbdd0cf725dba3ddd7bb2e919a1f",
    "poolManager": "0xa4eefca1e248d876f0e2eac6189c0b7203d7abf195aba9dbb19577787384e92e",
    "positionManager": "0x1111111111111111111111111111111111111111111111111111111111111111",
    "permit2": "0x2222222222222222222222222222222222222222222222222222222222222222"
  },
  "compiler": {
    "solcVersion": "0.8.36",
    "optimizer": true,
    "optimizerRuns": 200,
    "viaIr": false,
    "evmVersion": "cancun"
  },
  "toolchain": { "foundryVersion": "1.8.1" },
  "governance": {
    "pauseAuthority": "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
    "timelock": "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC",
    "protocolTreasury": "0x90F79bf6EB2c4f870365E785982E1f101E93b906",
    "deployer": "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
  },
  "verification": {
    "dependenciesReviewed": true,
    "hookAddressVerified": true,
    "dependenciesWired": true,
    "noResidualDeployerAuthority": true
  }
}`

func TestLoadEngineV2ManifestWithItsOwnSchema(t *testing.T) {
	registry, err := Load(manifestFS(t, map[string]string{".generated/anvil-v2.json": validManifestV2}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	deployment, err := registry.Lookup(31337, "anvil-v2")
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if deployment.EngineVersion != 2 || deployment.V4 == nil {
		t.Fatalf("EngineVersion = %d, V4 = %+v", deployment.EngineVersion, deployment.V4)
	}
	want := V4Deployment{
		PoolManager:     common.HexToAddress("0x5fbdb2315678afecb367f032d93f642f64180aa3"),
		PositionManager: common.HexToAddress("0xe7f1725e7734ce288f8367e1bb143e90bb3f0512"),
		Permit2:         common.HexToAddress("0x000000000022D473030F116dDEE9F6B43aC78BA3"),
		Hook:            common.HexToAddress("0x8a791620dd6260079bf849dc5567adc3f2fd2000"),
		Locker:          common.HexToAddress("0x610178da211fef7d417bc0e6fed39f05609ad788"),
		BytecodeHashes: V4BytecodeHashes{
			BondingCurveV2:  common.HexToHash("0x82067d3aaf6a33091b1971a469e78c4314476e73f51b125f072b433b530d5062"),
			Hook:            common.HexToHash("0x8d88e396de2375f3c7851b3b0d491455ad0387eea71469ac19280019852b49dc"),
			Locker:          common.HexToHash("0xf59f833f8fabcaf9d8d6959181ad6e25b67ccbdd0cf725dba3ddd7bb2e919a1f"),
			PoolManager:     common.HexToHash("0xa4eefca1e248d876f0e2eac6189c0b7203d7abf195aba9dbb19577787384e92e"),
			PositionManager: common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			Permit2:         common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		},
	}
	if *deployment.V4 != want {
		t.Fatalf("V4 = %+v, want %+v", *deployment.V4, want)
	}
	if deployment.BytecodeHashes.LaunchFactory != common.HexToHash("0x3e9a2004917efc73f12e42947f013300e409fad73305c77307ce17fa9234417c") {
		t.Fatalf("launch factory hash = %s", deployment.BytecodeHashes.LaunchFactory)
	}
	if deployment.UniV2Factory != (common.Address{}) || deployment.WETH != (common.Address{}) {
		t.Fatal("engine-2 deployment carries v1 pair fields")
	}
}

func TestLoadV1ManifestHasNoV4Section(t *testing.T) {
	registry, err := Load(manifestFS(t, map[string]string{".generated/anvil-v1.json": validManifest}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	deployment, err := registry.Lookup(31337, "anvil-v1")
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if deployment.V4 != nil {
		t.Fatalf("v1 deployment V4 = %+v, want nil", deployment.V4)
	}
}

func TestLoadRejectsInvalidEngineV2Manifests(t *testing.T) {
	cases := map[string]func(map[string]any){
		"v2 pair field":         func(v map[string]any) { v["uniswapV2Factory"] = "0xe7f1725e7734ce288f8367e1bb143e90bb3f0512" },
		"zero hook":             func(v map[string]any) { v["hook"] = "0x0000000000000000000000000000000000000000" },
		"zero locker":           func(v map[string]any) { v["locker"] = "0x0000000000000000000000000000000000000000" },
		"non-canonical permit2": func(v map[string]any) { v["permit2"] = "0x1111111111111111111111111111111111111111" },
		"wrong pool fee":        func(v map[string]any) { v["pool"].(map[string]any)["fee"] = 3000 },
		"zero locker hash":      func(v map[string]any) { v["bytecodeHashes"].(map[string]any)["locker"] = "0x" + zeroHex(64) },
		"unsupported engine":    func(v map[string]any) { v["engineVersion"] = 3 },
		"missing engine":        func(v map[string]any) { delete(v, "engineVersion") },
		"testnet start zero":    func(v map[string]any) { v["environment"] = "testnet"; v["startBlock"] = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(manifestFS(t, map[string]string{".generated/anvil-v2.json": mutateManifest(t, validManifestV2, mutate)}))
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Load() error = %v, want ValidationError", err)
			}
		})
	}
}

// An engine-1 manifest must keep failing the v1 schema when it carries v2 fields.
func TestLoadRejectsV1ManifestWithV4Fields(t *testing.T) {
	manifest := mutateManifest(t, validManifest, func(v map[string]any) { v["hook"] = "0x8a791620dd6260079bf849dc5567adc3f2fd2000" })
	if _, err := Load(manifestFS(t, map[string]string{".generated/anvil-v1.json": manifest})); err == nil {
		t.Fatal("Load() error = nil for an engine-1 manifest with a hook field")
	}
}

func zeroHex(length int) string {
	result := make([]byte, length)
	for index := range result {
		result[index] = '0'
	}
	return string(result)
}
