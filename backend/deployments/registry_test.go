package deployments

import (
	"encoding/json"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/ethereum/go-ethereum/common"
)

const validManifest = `{
  "schemaVersion": 1,
  "deploymentId": "anvil-v1",
  "name": "Local Anvil",
  "environment": "local",
  "chainId": 31337,
  "factory": "0xcf7ed3acca5a467e9e704c703e8d87f634fb0fc9",
  "startBlock": 1,
  "engineVersion": 1,
  "curveImplementation": "0x9fe46736679d2d9a65f0992f2272de9f3c7fa6e0",
  "uniswapV2Factory": "0xe7f1725e7734ce288f8367e1bb143e90bb3f0512",
  "uniswapV2Router02": null,
  "weth": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
  "pairInitCodeHash": "0xa4eefca1e248d876f0e2eac6189c0b7203d7abf195aba9dbb19577787384e92e",
  "lpBurnAddress": "0x000000000000000000000000000000000000dEaD",
  "explorerBase": "",
  "graduationEnabled": true,
  "deployTransaction": "0xe237a00d7888e2b095e7067bd726a83c30d5d1d8a58b7d899b505d9b395a5119",
  "bytecodeHashes": {
    "launchFactory": "0x3e9a2004917efc73f12e42947f013300e409fad73305c77307ce17fa9234417c",
    "bondingCurveV1": "0x82067d3aaf6a33091b1971a469e78c4314476e73f51b125f072b433b530d5062",
    "uniswapV2Factory": "0x8d88e396de2375f3c7851b3b0d491455ad0387eea71469ac19280019852b49dc",
    "weth": "0xf59f833f8fabcaf9d8d6959181ad6e25b67ccbdd0cf725dba3ddd7bb2e919a1f"
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
    "pairInitCodeHashVerified": true,
    "noResidualDeployerAuthority": true
  }
}`

const testnetDisabledMarker = `{
  "schemaVersion": 1,
  "target": "robinhood-testnet",
  "chainId": 46630,
  "enabled": false,
  "reason": "test fixture"
}`

func TestLoadEmbeddedResolvesReviewedTestnetAndFailsClosedForMainnet(t *testing.T) {
	registry, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded() error = %v", err)
	}

	_, err = registry.Lookup(4663, "robinhood-mainnet-v1")
	var notFound *ErrDeploymentNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("Lookup(mainnet) error = %v, want ErrDeploymentNotFound", err)
	}

	testnet, err := registry.Lookup(46630, "robinhood-testnet-v1")
	if err != nil {
		t.Fatalf("Lookup(testnet) error = %v", err)
	}
	if testnet.Factory != common.HexToAddress("0xedddb61a53226ffdc6ecf7c042b803e769168d55") {
		t.Fatalf("Lookup(testnet).Factory = %s", testnet.Factory)
	}

	_, err = registry.Lookup(46630, "anvil-v1")
	if !errors.As(err, &notFound) {
		t.Fatalf("Lookup(testnet fallback probe) error = %v, want ErrDeploymentNotFound", err)
	}
}

func TestLoadGeneratedAnvilManifestAndExactLookup(t *testing.T) {
	registry, err := Load(manifestFS(t, map[string]string{
		".generated/anvil-v1.json": validManifest,
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	confirmations := uint64(12)
	got, err := registry.Resolve(31337, "anvil-v1", &confirmations)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Environment != EnvironmentLocal || got.StartBlock != 1 || got.EngineVersion != 1 {
		t.Fatalf("Lookup() = %+v", got)
	}
	if got.Factory != common.HexToAddress("0xcf7ed3acca5a467e9e704c703e8d87f634fb0fc9") {
		t.Fatalf("Factory = %s", got.Factory)
	}
	if got.LPBurnAddress != lpBurnAddress {
		t.Fatalf("LPBurnAddress = %s", got.LPBurnAddress)
	}

	_, err = registry.Lookup(4663, "anvil-v1")
	var notFound *ErrDeploymentNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("cross-chain Lookup() error = %v, want ErrDeploymentNotFound", err)
	}
}

func TestLoadRejectsInvalidDeploymentArtifacts(t *testing.T) {
	tests := map[string]func(map[string]any){
		"unknown field violates schema":   func(value map[string]any) { value["unexpected"] = true },
		"unsupported schema version":      func(value map[string]any) { value["schemaVersion"] = float64(2) },
		"graduation disabled in manifest": func(value map[string]any) { value["graduationEnabled"] = false },
		"zero factory":                    func(value map[string]any) { value["factory"] = "0x0000000000000000000000000000000000000000" },
		"zero curve": func(value map[string]any) {
			value["curveImplementation"] = "0x0000000000000000000000000000000000000000"
		},
		"wrong burn address":    func(value map[string]any) { value["lpBurnAddress"] = "0x0000000000000000000000000000000000000001" },
		"unsupported engine":    func(value map[string]any) { value["engineVersion"] = float64(2) },
		"invalid deployment id": func(value map[string]any) { value["deploymentId"] = "INVALID" },
		"non-local zero start block": func(value map[string]any) {
			value["environment"] = "production"
			value["startBlock"] = float64(0)
		},
		"malformed pair hash": func(value map[string]any) { value["pairInitCodeHash"] = "0x1234" },
		"zero pair hash": func(value map[string]any) {
			value["pairInitCodeHash"] = "0x0000000000000000000000000000000000000000000000000000000000000000"
		},
		"unknown marketLinks key": func(value map[string]any) {
			value["marketLinks"] = map[string]any{"dexscreenerChain": "robinhood", "unexpected": "x"}
		},
		"malformed marketLinks identifier": func(value map[string]any) {
			value["marketLinks"] = map[string]any{"geckoterminalNetwork": "Bad Value!"}
		},
		"zero bytecode hash": func(value map[string]any) {
			value["bytecodeHashes"].(map[string]any)["weth"] = "0x0000000000000000000000000000000000000000000000000000000000000000"
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			manifest := mutateManifest(t, validManifest, mutate)
			_, err := Load(manifestFS(t, map[string]string{"deployment.json": manifest}))
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Load() error = %v, want ValidationError", err)
			}
		})
	}
}

func TestLoadAcceptsOptionalMarketLinks(t *testing.T) {
	for name, manifest := range map[string]string{
		"absent": validManifest,
		"both": mutateManifest(t, validManifest, func(value map[string]any) {
			value["marketLinks"] = map[string]any{"dexscreenerChain": "robinhood", "geckoterminalNetwork": "robinhood-chain"}
		}),
		"empty": mutateManifest(t, validManifest, func(value map[string]any) {
			value["marketLinks"] = map[string]any{}
		}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(manifestFS(t, map[string]string{"deployment.json": manifest})); err != nil {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}
}

func TestLoadRejectsDuplicateDeploymentIDAcrossChains(t *testing.T) {
	second := mutateManifest(t, validManifest, func(value map[string]any) {
		value["chainId"] = float64(31338)
	})
	_, err := Load(manifestFS(t, map[string]string{
		"one.json": validManifest,
		"two.json": second,
	}))
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("Load() error = %v, want ValidationError", err)
	}
}

func TestLoadRejectsDeploymentOnDisabledChain(t *testing.T) {
	deployment := mutateManifest(t, validManifest, func(value map[string]any) {
		value["deploymentId"] = "robinhood-testnet-v1"
		value["environment"] = "testnet"
		value["chainId"] = float64(46630)
	})
	files := manifestFS(t, map[string]string{"robinhood-testnet-v1.json": deployment})
	files["config/robinhood-testnet.disabled.json"] = &fstest.MapFile{
		Data: []byte(testnetDisabledMarker),
	}

	_, err := Load(files)
	if err == nil {
		t.Fatal("Load() error = nil for deployment on disabled chain")
	}
}

func TestLoadSchemaValidatesDependencyAndDisabledRecords(t *testing.T) {
	files := manifestFS(t, nil)
	files["config/invalid-dependency.json"] = &fstest.MapFile{Data: []byte(`{"schemaVersion":1}`)}
	if _, err := Load(files); err == nil {
		t.Fatal("Load() error = nil for invalid dependency record")
	}

	files = manifestFS(t, nil)
	files["config/invalid.disabled.json"] = &fstest.MapFile{Data: []byte(`{"schemaVersion":1}`)}
	if _, err := Load(files); err == nil {
		t.Fatal("Load() error = nil for invalid disabled marker")
	}
}

func TestLoadRejectsZeroDependencyAddressAndHash(t *testing.T) {
	root, err := fs.Sub(embeddedArtifacts, "testdata")
	if err != nil {
		t.Fatalf("fs.Sub() error = %v", err)
	}
	body, err := fs.ReadFile(root, "config/robinhood-mainnet.json")
	if err != nil {
		t.Fatalf("read dependency fixture: %v", err)
	}

	tests := map[string]func(map[string]any){
		"zero address": func(value map[string]any) {
			value["weth"] = "0x0000000000000000000000000000000000000000"
		},
		"zero hash": func(value map[string]any) {
			value["bytecodeHashes"].(map[string]any)["uniswapV2Pair"] = "0x0000000000000000000000000000000000000000000000000000000000000000"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			invalid := mutateManifest(t, string(body), mutate)
			files := manifestFS(t, nil)
			files["config/robinhood-mainnet.json"] = &fstest.MapFile{Data: []byte(invalid)}
			_, err := Load(files)
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Load() error = %v, want ValidationError", err)
			}
		})
	}
}

func TestRequireHashRejectsInvalidHexWithoutSchemaGuard(t *testing.T) {
	invalid := "0x1234567890abcdef1234567890abcdef12345678zzzzzzzzzzzzzzzzzzzzzzzz"
	if err := requireHash("test hash", invalid); err == nil {
		t.Fatal("requireHash() error = nil for invalid hex")
	}
}

func TestReconcileConfig(t *testing.T) {
	confirmations := uint64(12)
	tests := []struct {
		name          string
		chainID       uint64
		confirmations *uint64
		deployment    Deployment
		wantError     bool
	}{
		{name: "local override accepted", chainID: 31337, confirmations: &confirmations, deployment: Deployment{ChainID: 31337, Environment: EnvironmentLocal}},
		{name: "testnet override rejected", chainID: 46630, confirmations: &confirmations, deployment: Deployment{ChainID: 46630, Environment: EnvironmentTestnet}, wantError: true},
		{name: "production override rejected", chainID: 4663, confirmations: &confirmations, deployment: Deployment{ChainID: 4663, Environment: EnvironmentProduction}, wantError: true},
		{name: "chain mismatch rejected", chainID: 4663, deployment: Deployment{ChainID: 31337, Environment: EnvironmentLocal}, wantError: true},
		{name: "production without override accepted", chainID: 4663, deployment: Deployment{ChainID: 4663, Environment: EnvironmentProduction}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ReconcileConfig(test.chainID, test.confirmations, test.deployment)
			if (err != nil) != test.wantError {
				t.Fatalf("ReconcileConfig() error = %v, wantError = %t", err, test.wantError)
			}
		})
	}
}

func manifestFS(t *testing.T, manifests map[string]string) fstest.MapFS {
	t.Helper()
	result := make(fstest.MapFS)
	root, err := fs.Sub(embeddedArtifacts, "testdata")
	if err != nil {
		t.Fatalf("fs.Sub() error = %v", err)
	}
	for _, name := range []string{deploymentSchemaPath, deploymentV2SchemaPath, dependenciesSchemaPath, disabledSchemaPath, v4SchemaPath} {
		body, err := fs.ReadFile(root, name)
		if err != nil {
			t.Fatalf("read schema %q: %v", name, err)
		}
		result[name] = &fstest.MapFile{Data: body}
	}
	for name, body := range manifests {
		result[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return result
}

func mutateManifest(t *testing.T, source string, mutate func(map[string]any)) string {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(source), &value); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	mutate(value)
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return string(body)
}

func TestLoadValidatesV4DependencyRecordWithItsOwnSchema(t *testing.T) {
	root, err := fs.Sub(embeddedArtifacts, "testdata")
	if err != nil {
		t.Fatalf("fs.Sub() error = %v", err)
	}
	if _, err := Load(root); err != nil {
		t.Fatalf("Load() error = %v for the reviewed v4 dependency record", err)
	}

	files := manifestFS(t, nil)
	files["config/invalid-v4.json"] = &fstest.MapFile{Data: []byte(`{"schemaVersion":1}`)}
	if _, err := Load(files); err == nil {
		t.Fatal("Load() error = nil for invalid v4 dependency record")
	}
}
