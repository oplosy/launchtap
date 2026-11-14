package apicompat

import (
	"encoding/json"
	"strings"
	"testing"
)

// spec builds a one-operation contract: GET /items with the given parameters, an optional
// request body schema and a 200 JSON response schema; components hold shared schemas.
func spec(t *testing.T, parameters []any, body, result object, components object) []byte {
	t.Helper()
	operation := object{
		"parameters": parameters,
		"responses": object{
			"200": object{"content": object{"application/json": object{"schema": result}}},
		},
	}
	if body != nil {
		operation["requestBody"] = object{
			"required": true,
			"content":  object{"application/json": object{"schema": body}},
		}
	}
	doc := object{
		"openapi":    "3.1.0",
		"info":       object{"title": "t", "version": "1.0.0"},
		"paths":      object{"/items": object{"post": operation}},
		"components": object{"schemas": components},
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func obj(required []any, props object) object {
	return object{"type": "object", "additionalProperties": false, "required": required, "properties": props}
}

var str = object{"type": "string"}

func messages(t *testing.T, baseline, current []byte) []string {
	t.Helper()
	changes, err := Compare(baseline, current)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(changes))
	for _, change := range changes {
		out = append(out, change.String())
	}
	return out
}

func expectOne(t *testing.T, got []string, fragment string) {
	t.Helper()
	if len(got) != 1 || !strings.Contains(got[0], fragment) {
		t.Fatalf("want one change containing %q, got %q", fragment, got)
	}
}

func expectNone(t *testing.T, got []string) {
	t.Helper()
	if len(got) != 0 {
		t.Fatalf("want no breaking change, got %q", got)
	}
}

func TestIdenticalContractsAreCompatible(t *testing.T) {
	doc := spec(t, nil, obj([]any{"a"}, object{"a": str}), obj([]any{"a"}, object{"a": str}), nil)
	expectNone(t, messages(t, doc, doc))
}

func TestResponseRules(t *testing.T) {
	base := obj([]any{"a"}, object{"a": str, "b": object{"type": "integer"}})
	cases := []struct {
		name     string
		current  object
		fragment string
	}{
		{"removed property", obj([]any{"a"}, object{"a": str}), "response property removed"},
		{"required became optional", obj(nil, object{"a": str, "b": object{"type": "integer"}}), "became optional"},
		{"null added", obj([]any{"a"}, object{"a": object{"type": []any{"string", "null"}}, "b": object{"type": "integer"}}), "type widened"},
		{"format changed", obj([]any{"a"}, object{"a": object{"type": "string", "format": "uri"}, "b": object{"type": "integer"}}), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := messages(t, spec(t, nil, nil, base, nil), spec(t, nil, nil, tc.current, nil))
			if tc.fragment == "" {
				expectNone(t, got) // a format added to a response only narrows it
				return
			}
			expectOne(t, got, tc.fragment)
		})
	}
}

func TestResponseAdditionsAreCompatible(t *testing.T) {
	base := obj([]any{"a"}, object{"a": str})
	current := obj([]any{"a", "c"}, object{"a": str, "c": object{"type": "boolean"}})
	expectNone(t, messages(t, spec(t, nil, nil, base, nil), spec(t, nil, nil, current, nil)))
}

func TestRequestRules(t *testing.T) {
	base := obj([]any{"a"}, object{"a": object{"type": "string", "maxLength": 100.0}, "b": str})
	cases := []struct {
		name     string
		current  object
		fragment string
	}{
		{"new required property", obj([]any{"a", "b"}, object{"a": object{"type": "string", "maxLength": 100.0}, "b": str}), "new required request property"},
		{"removed property while closed", obj([]any{"a"}, object{"a": object{"type": "string", "maxLength": 100.0}}), "unknown properties are rejected"},
		{"maxLength tightened", obj([]any{"a"}, object{"a": object{"type": "string", "maxLength": 50.0}, "b": str}), "maxLength tightened"},
		{"type narrowed", obj([]any{"a"}, object{"a": object{"type": "string", "maxLength": 100.0}, "b": object{"type": "integer"}}), "type narrowed"},
		{"pattern added", obj([]any{"a"}, object{"a": object{"type": "string", "maxLength": 100.0, "pattern": "^x$"}, "b": str}), "pattern changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := messages(t, spec(t, nil, base, str, nil), spec(t, nil, tc.current, str, nil))
			expectOne(t, got, tc.fragment)
		})
	}
}

func TestRequestWideningIsCompatible(t *testing.T) {
	base := obj([]any{"a"}, object{"a": object{"type": "string", "maxLength": 100.0}})
	current := obj(nil, object{"a": object{"type": []any{"string", "null"}, "maxLength": 200.0}, "c": str})
	expectNone(t, messages(t, spec(t, nil, base, str, nil), spec(t, nil, current, str, nil)))
}

func TestParameterRules(t *testing.T) {
	limit := object{"in": "query", "name": "limit", "schema": object{"type": "integer", "maximum": 100.0}}
	cursor := object{"in": "query", "name": "cursor", "schema": str}
	base := spec(t, []any{limit, cursor}, nil, str, nil)

	expectOne(t, messages(t, base, spec(t, []any{limit}, nil, str, nil)), "parameter removed")

	required := object{"in": "query", "name": "cursor", "required": true, "schema": str}
	expectOne(t, messages(t, base, spec(t, []any{limit, required}, nil, str, nil)), "parameter became required")

	added := object{"in": "header", "name": "X-Key", "required": true, "schema": str}
	expectOne(t, messages(t, base, spec(t, []any{limit, cursor, added}, nil, str, nil)), "new required parameter")

	tighter := object{"in": "query", "name": "limit", "schema": object{"type": "integer", "maximum": 50.0}}
	expectOne(t, messages(t, base, spec(t, []any{tighter, cursor}, nil, str, nil)), "maximum tightened")

	optional := object{"in": "query", "name": "sort", "schema": str}
	expectNone(t, messages(t, base, spec(t, []any{limit, cursor, optional}, nil, str, nil)))
}

func TestEnumValuesMayBeAddedButNotRemoved(t *testing.T) {
	enum := func(values ...any) object { return object{"type": "string", "enum": values} }
	base := spec(t, nil, nil, enum("buy", "sell"), nil)
	expectNone(t, messages(t, base, spec(t, nil, nil, enum("buy", "sell", "swap"), nil)))
	expectOne(t, messages(t, base, spec(t, nil, nil, enum("buy"), nil)), `enum value "sell" removed`)
}

func TestOneOfVariantsMatchByReference(t *testing.T) {
	components := object{"King": obj([]any{"a"}, object{"a": str})}
	nullable := object{"oneOf": []any{object{"$ref": "#/components/schemas/King"}, object{"type": "null"}}}
	base := spec(t, nil, nil, nullable, components)

	reordered := object{"oneOf": []any{object{"type": "null"}, object{"$ref": "#/components/schemas/King"}}}
	expectNone(t, messages(t, base, spec(t, nil, nil, reordered, components)))

	notNull := object{"$ref": "#/components/schemas/King"}
	expectOne(t, messages(t, base, spec(t, nil, nil, notNull, components)), "oneOf variant type null removed")

	changed := object{"King": obj(nil, object{"a": str})}
	expectOne(t, messages(t, base, spec(t, nil, nil, nullable, changed)), "response property became optional")
}

func TestRecursiveSchemasTerminate(t *testing.T) {
	components := object{"Node": obj([]any{"a"}, object{"a": str, "next": object{"$ref": "#/components/schemas/Node"}})}
	doc := spec(t, nil, nil, object{"$ref": "#/components/schemas/Node"}, components)
	expectNone(t, messages(t, doc, doc))
}

func TestRemovedPathAndStatus(t *testing.T) {
	base := spec(t, nil, nil, str, nil)
	var doc object
	if err := json.Unmarshal(base, &doc); err != nil {
		t.Fatal(err)
	}
	doc["paths"] = object{"/other": object{}}
	noPath, _ := json.Marshal(doc)
	expectOne(t, messages(t, base, noPath), "path removed")

	doc["paths"] = object{"/items": object{"post": object{"responses": object{"201": object{}}}}}
	noStatus, _ := json.Marshal(doc)
	expectOne(t, messages(t, base, noStatus), "response status removed")
}

func TestMajorVersion(t *testing.T) {
	major, err := MajorVersion([]byte(`{"info":{"version":"1.4.0"}}`))
	if err != nil || major != "1" {
		t.Fatalf("got %q, %v", major, err)
	}
	if _, err := MajorVersion([]byte(`{"info":{}}`)); err == nil {
		t.Fatal("want an error for a missing version")
	}
}
