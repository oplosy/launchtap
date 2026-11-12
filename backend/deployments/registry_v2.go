package deployments

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// rawDeploymentV2 is the runtime subset of deployment-v2.schema.json. The schema already pins
// the pool key constants, compiler, toolchain and verification flags.
type rawDeploymentV2 struct {
	SchemaVersion       uint16              `json:"schemaVersion"`
	DeploymentID        string              `json:"deploymentId"`
	Name                string              `json:"name"`
	Environment         Environment         `json:"environment"`
	ChainID             uint64              `json:"chainId"`
	Factory             string              `json:"factory"`
	StartBlock          uint64              `json:"startBlock"`
	EngineVersion       uint16              `json:"engineVersion"`
	CurveImplementation string              `json:"curveImplementation"`
	PoolManager         string              `json:"poolManager"`
	PositionManager     string              `json:"positionManager"`
	Permit2             string              `json:"permit2"`
	Hook                string              `json:"hook"`
	Locker              string              `json:"locker"`
	ExplorerBase        string              `json:"explorerBase"`
	GraduationEnabled   bool                `json:"graduationEnabled"`
	DeployTransaction   string              `json:"deployTransaction"`
	BytecodeHashes      rawBytecodeHashesV2 `json:"bytecodeHashes"`
}

type rawBytecodeHashesV2 struct {
	LaunchFactory   string `json:"launchFactory"`
	BondingCurveV2  string `json:"bondingCurveV2"`
	Hook            string `json:"hook"`
	Locker          string `json:"locker"`
	PoolManager     string `json:"poolManager"`
	PositionManager string `json:"positionManager"`
	Permit2         string `json:"permit2"`
}

func parseDeploymentV2(filePath string, body []byte, schema *jsonschema.Schema) (Deployment, error) {
	if err := validateOnly(filePath, body, schema); err != nil {
		return Deployment{}, err
	}
	var raw rawDeploymentV2
	if err := json.Unmarshal(body, &raw); err != nil {
		return Deployment{}, &ValidationError{Path: filePath, Err: err}
	}
	if err := validateRawDeploymentV2(raw); err != nil {
		return Deployment{}, &ValidationError{Path: filePath, Err: err}
	}
	return Deployment{
		ChainID:             raw.ChainID,
		DeploymentID:        raw.DeploymentID,
		Name:                raw.Name,
		Environment:         raw.Environment,
		Factory:             common.HexToAddress(raw.Factory),
		StartBlock:          raw.StartBlock,
		EngineVersion:       raw.EngineVersion,
		CurveImplementation: common.HexToAddress(raw.CurveImplementation),
		BytecodeHashes:      BytecodeHashes{LaunchFactory: common.HexToHash(raw.BytecodeHashes.LaunchFactory)},
		DeployTransaction:   common.HexToHash(raw.DeployTransaction),
		ExplorerBase:        raw.ExplorerBase,
		V4: &V4Deployment{
			PoolManager:     common.HexToAddress(raw.PoolManager),
			PositionManager: common.HexToAddress(raw.PositionManager),
			Permit2:         common.HexToAddress(raw.Permit2),
			Hook:            common.HexToAddress(raw.Hook),
			Locker:          common.HexToAddress(raw.Locker),
			BytecodeHashes: V4BytecodeHashes{
				BondingCurveV2:  common.HexToHash(raw.BytecodeHashes.BondingCurveV2),
				Hook:            common.HexToHash(raw.BytecodeHashes.Hook),
				Locker:          common.HexToHash(raw.BytecodeHashes.Locker),
				PoolManager:     common.HexToHash(raw.BytecodeHashes.PoolManager),
				PositionManager: common.HexToHash(raw.BytecodeHashes.PositionManager),
				Permit2:         common.HexToHash(raw.BytecodeHashes.Permit2),
			},
		},
	}, nil
}

func validateRawDeploymentV2(raw rawDeploymentV2) error {
	if raw.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema version %d", raw.SchemaVersion)
	}
	if raw.EngineVersion != 2 {
		return fmt.Errorf("engine-2 manifest declares engine version %d", raw.EngineVersion)
	}
	if !slices.Contains([]Environment{EnvironmentLocal, EnvironmentTestnet, EnvironmentProduction}, raw.Environment) {
		return fmt.Errorf("unknown environment %q", raw.Environment)
	}
	if !raw.GraduationEnabled {
		return errors.New("graduation must be enabled")
	}
	if raw.StartBlock == 0 && raw.Environment != EnvironmentLocal {
		return errors.New("start block zero is allowed only for local deployments")
	}
	addresses := []struct{ name, value string }{
		{"factory", raw.Factory},
		{"curve implementation", raw.CurveImplementation},
		{"pool manager", raw.PoolManager},
		{"position manager", raw.PositionManager},
		{"permit2", raw.Permit2},
		{"hook", raw.Hook},
		{"locker", raw.Locker},
	}
	for _, address := range addresses {
		if err := requireAddress(address.name, address.value, true); err != nil {
			return err
		}
	}
	if common.HexToAddress(raw.Permit2) != canonicalPermit2 {
		return errors.New("permit2 is not canonical")
	}
	hashes := []struct{ name, value string }{
		{"deploy transaction", raw.DeployTransaction},
		{"launch factory bytecode", raw.BytecodeHashes.LaunchFactory},
		{"bonding curve v2 bytecode", raw.BytecodeHashes.BondingCurveV2},
		{"hook bytecode", raw.BytecodeHashes.Hook},
		{"locker bytecode", raw.BytecodeHashes.Locker},
		{"pool manager bytecode", raw.BytecodeHashes.PoolManager},
		{"position manager bytecode", raw.BytecodeHashes.PositionManager},
		{"permit2 bytecode", raw.BytecodeHashes.Permit2},
	}
	for _, hash := range hashes {
		if err := requireHash(hash.name, hash.value); err != nil {
			return err
		}
	}
	return nil
}
