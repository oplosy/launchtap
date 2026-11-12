package curve

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const embeddedVectorV2Path = "testdata/v2/curve-v2.json"

//go:embed testdata/v2/curve-v2.json
var embeddedVectorsV2 []byte

// VectorArtifactV2 is the validated engine v2 Solidity curve-vector artifact. It extends the
// v1 shape with the anti-snipe profile, the per-case clock and the graduation block.
type VectorArtifactV2 struct {
	Schema         string
	SchemaVersion  int
	EngineVersion  int
	AmountEncoding string
	Parameters     VectorParametersV2
	Cases          []VectorCaseV2
}

type VectorParametersV2 struct {
	VectorParameters
	AntiSnipeStartBPS        int
	AntiSnipeDurationSeconds int
	LPCreatorShareBPS        int
}

type VectorStateV2 struct {
	VectorState
	SnipeTax string
}

type VectorOutputV2 struct {
	VectorOutput
	SnipeTax string
	TaxBPS   int
}

type VectorGraduation struct {
	ETHToPool, TokensToPool, Liquidity, SqrtPriceX96, ETHRemainder, TokenRemainder string
}

type VectorCaseV2 struct {
	ID                 string
	Operation          string
	SecondsSinceLaunch int
	InitialState       VectorStateV2
	Input              VectorInput
	Output             *VectorOutputV2
	NextState          VectorStateV2
	Graduation         *VectorGraduation
	ExpectedRevert     *VectorRevert
}

// LoadEmbeddedVectorsV2 loads the committed backend copy of the v2 Solidity vectors.
func LoadEmbeddedVectorsV2() (VectorArtifactV2, error) {
	artifact, err := LoadVectorsV2(bytes.NewReader(embeddedVectorsV2))
	if err != nil {
		return VectorArtifactV2{}, fmt.Errorf("load embedded v2 curve vectors %s: %w", embeddedVectorV2Path, err)
	}
	return artifact, nil
}

// LoadVectorsV2 decodes and validates one v2 curve-vector document against the locked v2
// schema (`contracts/vectors/v2/curve.schema.json`) using only the standard library.
func LoadVectorsV2(reader io.Reader) (VectorArtifactV2, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	var raw rawArtifactV2
	if err := decoder.Decode(&raw); err != nil {
		return VectorArtifactV2{}, fmt.Errorf("decode v2 curve vectors: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return VectorArtifactV2{}, errors.New("decode v2 curve vectors: trailing JSON value")
		}
		return VectorArtifactV2{}, fmt.Errorf("decode v2 curve vectors trailing content: %w", err)
	}
	artifact, err := raw.validate()
	if err != nil {
		return VectorArtifactV2{}, fmt.Errorf("validate v2 curve vectors: %w", err)
	}
	return artifact, nil
}

type rawArtifactV2 struct {
	Schema         *string          `json:"$schema"`
	SchemaVersion  *int             `json:"schemaVersion"`
	EngineVersion  *int             `json:"engineVersion"`
	AmountEncoding *string          `json:"amountEncoding"`
	Parameters     *rawParametersV2 `json:"parameters"`
	Cases          *[]rawCaseV2     `json:"cases"`
}

type rawParametersV2 struct {
	rawParameters
	AntiSnipeStartBPS        *int `json:"antiSnipeStartBps"`
	AntiSnipeDurationSeconds *int `json:"antiSnipeDurationSeconds"`
	LPCreatorShareBPS        *int `json:"lpCreatorShareBps"`
}

type rawStateV2 struct {
	rawState
	SnipeTax *string `json:"snipeTax"`
}

type rawOutputV2 struct {
	rawOutput
	SnipeTax *string `json:"snipeTax"`
	TaxBPS   *int    `json:"taxBps"`
}

type rawGraduation struct {
	ETHToPool      *string `json:"ethToPool"`
	TokensToPool   *string `json:"tokensToPool"`
	Liquidity      *string `json:"liquidity"`
	SqrtPriceX96   *string `json:"sqrtPriceX96"`
	ETHRemainder   *string `json:"ethRemainder"`
	TokenRemainder *string `json:"tokenRemainder"`
}

type rawCaseV2 struct {
	ID                 *string         `json:"id"`
	Operation          *string         `json:"operation"`
	SecondsSinceLaunch *int            `json:"secondsSinceLaunch"`
	InitialState       *rawStateV2     `json:"initialState"`
	Input              *rawInput       `json:"input"`
	Output             json.RawMessage `json:"output"`
	NextState          *rawStateV2     `json:"nextState"`
	Graduation         json.RawMessage `json:"graduation"`
	ExpectedRevert     json.RawMessage `json:"expectedRevert"`
}

func (raw rawArtifactV2) validate() (VectorArtifactV2, error) {
	if err := requireConst("$schema", raw.Schema, "./curve.schema.json"); err != nil {
		return VectorArtifactV2{}, err
	}
	if err := requireIntConst("schemaVersion", raw.SchemaVersion, 1); err != nil {
		return VectorArtifactV2{}, err
	}
	if err := requireIntConst("engineVersion", raw.EngineVersion, 2); err != nil {
		return VectorArtifactV2{}, err
	}
	if err := requireConst("amountEncoding", raw.AmountEncoding, "uint256-decimal-string"); err != nil {
		return VectorArtifactV2{}, err
	}
	if raw.Parameters == nil {
		return VectorArtifactV2{}, errors.New("parameters is required and must be an object")
	}
	parameters, err := raw.Parameters.validate()
	if err != nil {
		return VectorArtifactV2{}, fmt.Errorf("parameters: %w", err)
	}
	if raw.Cases == nil {
		return VectorArtifactV2{}, errors.New("cases is required and must be an array")
	}
	if len(*raw.Cases) < 10 {
		return VectorArtifactV2{}, fmt.Errorf("cases must contain at least 10 entries, got %d", len(*raw.Cases))
	}
	cases := make([]VectorCaseV2, len(*raw.Cases))
	for index, rawCase := range *raw.Cases {
		parsed, parseErr := rawCase.validate()
		if parseErr != nil {
			return VectorArtifactV2{}, fmt.Errorf("cases[%d]: %w", index, parseErr)
		}
		cases[index] = parsed
	}
	return VectorArtifactV2{
		Schema:         *raw.Schema,
		SchemaVersion:  *raw.SchemaVersion,
		EngineVersion:  *raw.EngineVersion,
		AmountEncoding: *raw.AmountEncoding,
		Parameters:     parameters,
		Cases:          cases,
	}, nil
}

func (raw rawParametersV2) validate() (VectorParametersV2, error) {
	base, err := raw.rawParameters.validate()
	if err != nil {
		return VectorParametersV2{}, err
	}
	// The vectors come from a deployed curve, so they sit inside the factory caps.
	if err := requireRange("antiSnipeStartBps", raw.AntiSnipeStartBPS, 0, int(MaxAntiSnipeStartBPS)); err != nil {
		return VectorParametersV2{}, err
	}
	if err := requireRange("antiSnipeDurationSeconds", raw.AntiSnipeDurationSeconds, 0, int(MaxAntiSnipeDurationSeconds)); err != nil {
		return VectorParametersV2{}, err
	}
	if err := requireRange("lpCreatorShareBps", raw.LPCreatorShareBPS, 0, 10000); err != nil {
		return VectorParametersV2{}, err
	}
	return VectorParametersV2{
		VectorParameters:         base,
		AntiSnipeStartBPS:        *raw.AntiSnipeStartBPS,
		AntiSnipeDurationSeconds: *raw.AntiSnipeDurationSeconds,
		LPCreatorShareBPS:        *raw.LPCreatorShareBPS,
	}, nil
}

func (raw rawStateV2) validate(name string) (VectorStateV2, error) {
	base, err := raw.rawState.validate(name)
	if err != nil {
		return VectorStateV2{}, err
	}
	if err := requirePattern(name+".snipeTax", raw.SnipeTax, amountPattern); err != nil {
		return VectorStateV2{}, err
	}
	return VectorStateV2{VectorState: base, SnipeTax: *raw.SnipeTax}, nil
}

func (raw rawOutputV2) validate() (VectorOutputV2, error) {
	base, err := raw.rawOutput.validate()
	if err != nil {
		return VectorOutputV2{}, err
	}
	if err := requirePattern("output.snipeTax", raw.SnipeTax, amountPattern); err != nil {
		return VectorOutputV2{}, err
	}
	if err := requireRange("output.taxBps", raw.TaxBPS, 0, int(MaxAntiSnipeStartBPS)); err != nil {
		return VectorOutputV2{}, err
	}
	return VectorOutputV2{VectorOutput: base, SnipeTax: *raw.SnipeTax, TaxBPS: *raw.TaxBPS}, nil
}

func (raw rawGraduation) validate() (VectorGraduation, error) {
	amounts := []struct {
		name  string
		value *string
	}{
		{"ethToPool", raw.ETHToPool},
		{"tokensToPool", raw.TokensToPool},
		{"liquidity", raw.Liquidity},
		{"sqrtPriceX96", raw.SqrtPriceX96},
		{"ethRemainder", raw.ETHRemainder},
		{"tokenRemainder", raw.TokenRemainder},
	}
	for _, amount := range amounts {
		if err := requirePattern("graduation."+amount.name, amount.value, amountPattern); err != nil {
			return VectorGraduation{}, err
		}
	}
	return VectorGraduation{
		ETHToPool:      *raw.ETHToPool,
		TokensToPool:   *raw.TokensToPool,
		Liquidity:      *raw.Liquidity,
		SqrtPriceX96:   *raw.SqrtPriceX96,
		ETHRemainder:   *raw.ETHRemainder,
		TokenRemainder: *raw.TokenRemainder,
	}, nil
}

func (raw rawCaseV2) validate() (VectorCaseV2, error) {
	if err := requirePattern("id", raw.ID, caseIDPattern); err != nil {
		return VectorCaseV2{}, err
	}
	if err := requireEnum("operation", raw.Operation, "buy", "sell"); err != nil {
		return VectorCaseV2{}, err
	}
	if raw.SecondsSinceLaunch == nil || *raw.SecondsSinceLaunch < 0 {
		return VectorCaseV2{}, errors.New("secondsSinceLaunch is required and must be a non-negative integer")
	}
	if raw.InitialState == nil {
		return VectorCaseV2{}, errors.New("initialState is required and must be an object")
	}
	initialState, err := raw.InitialState.validate("initialState")
	if err != nil {
		return VectorCaseV2{}, err
	}
	if raw.Input == nil {
		return VectorCaseV2{}, errors.New("input is required and must be an object")
	}
	input, err := raw.Input.validate()
	if err != nil {
		return VectorCaseV2{}, err
	}
	output, err := decodeNullable[rawOutputV2, VectorOutputV2]("output", raw.Output, rawOutputV2.validate)
	if err != nil {
		return VectorCaseV2{}, err
	}
	if raw.NextState == nil {
		return VectorCaseV2{}, errors.New("nextState is required and must be an object")
	}
	nextState, err := raw.NextState.validate("nextState")
	if err != nil {
		return VectorCaseV2{}, err
	}
	graduation, err := decodeNullable[rawGraduation, VectorGraduation]("graduation", raw.Graduation, rawGraduation.validate)
	if err != nil {
		return VectorCaseV2{}, err
	}
	expectedRevert, err := decodeNullable[rawRevert, VectorRevert]("expectedRevert", raw.ExpectedRevert, rawRevert.validate)
	if err != nil {
		return VectorCaseV2{}, err
	}
	return VectorCaseV2{
		ID:                 *raw.ID,
		Operation:          *raw.Operation,
		SecondsSinceLaunch: *raw.SecondsSinceLaunch,
		InitialState:       initialState,
		Input:              input,
		Output:             output,
		NextState:          nextState,
		Graduation:         graduation,
		ExpectedRevert:     expectedRevert,
	}, nil
}
