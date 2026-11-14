package apiserver

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Contictus/launchtap/backend/internal/apicompat"
)

const baselineContract = "../../openapi/v1-baseline.json"

func generatedContract(t *testing.T) []byte {
	t.Helper()
	generated, err := GenerateOpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	return generated
}

func TestGeneratedContractIsCompatibleWithV1Baseline(t *testing.T) {
	baseline, err := os.ReadFile(baselineContract)
	if err != nil {
		t.Fatal(err)
	}
	generated := generatedContract(t)
	changes, err := apicompat.Compare(baseline, generated)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range changes {
		t.Errorf("breaking change: %s", change)
	}
	major, err := apicompat.MajorVersion(generated)
	if err != nil || major != "1" {
		t.Fatalf("info.version major = %q, %v; /v1 must stay 1.x", major, err)
	}
}

// TestCompatibilityCheckCatchesBreakingEdits mutates the real generated contract and expects the
// check to name each edit, so the gate is known to see the shapes this API actually produces.
func TestCompatibilityCheckCatchesBreakingEdits(t *testing.T) {
	baseline := generatedContract(t)
	cases := []struct {
		name     string
		mutate   func(doc map[string]any)
		fragment string
	}{
		{
			name: "response property removed",
			mutate: func(doc map[string]any) {
				delete(schemaProperties(doc, "TokenDetailDTO"), "address")
			},
			fragment: "address: response property removed",
		},
		{
			name: "required request property added",
			mutate: func(doc map[string]any) {
				name := firstRequestBodySchema(t, doc)
				schema := componentSchema(doc, name)
				schema["properties"].(map[string]any)["new_field"] = map[string]any{"type": "string"}
				schema["required"] = append(asSlice(schema["required"]), "new_field")
			},
			fragment: "new_field: new required request property",
		},
		{
			name:     "path removed",
			mutate:   func(doc map[string]any) { delete(doc["paths"].(map[string]any), "/tokens/{token}") },
			fragment: "/tokens/{token}: path removed",
		},
		{
			name: "nullable response field",
			mutate: func(doc map[string]any) {
				schemaProperties(doc, "TokenDetailDTO")["creator"] = map[string]any{"type": []any{"string", "null"}}
			},
			fragment: "creator: response type widened",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal(baseline, &doc); err != nil {
				t.Fatal(err)
			}
			tc.mutate(doc)
			current, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			changes, err := apicompat.Compare(baseline, current)
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, change := range changes {
				found = found || strings.Contains(change.String(), tc.fragment)
			}
			if !found {
				t.Fatalf("want a change containing %q, got %v", tc.fragment, changes)
			}
		})
	}
}

func componentSchema(doc map[string]any, name string) map[string]any {
	return doc["components"].(map[string]any)["schemas"].(map[string]any)[name].(map[string]any)
}

func schemaProperties(doc map[string]any, name string) map[string]any {
	return componentSchema(doc, name)["properties"].(map[string]any)
}

func asSlice(value any) []any {
	s, _ := value.([]any)
	return s
}

// firstRequestBodySchema returns the component name of the first JSON request body by path order.
func firstRequestBodySchema(t *testing.T, doc map[string]any) string {
	t.Helper()
	paths := doc["paths"].(map[string]any)
	for _, path := range []string{"/tokens/{address}/comments", "/me/watchlist", "/profile", "/me/public-profile"} {
		item, _ := paths[path].(map[string]any)
		for _, method := range []string{"post", "put"} {
			op, _ := item[method].(map[string]any)
			body, _ := op["requestBody"].(map[string]any)
			content, _ := body["content"].(map[string]any)
			media, _ := content["application/json"].(map[string]any)
			schema, _ := media["schema"].(map[string]any)
			if ref, ok := schema["$ref"].(string); ok {
				return strings.TrimPrefix(ref, "#/components/schemas/")
			}
		}
	}
	t.Fatal("no JSON request body with a component schema found")
	return ""
}
