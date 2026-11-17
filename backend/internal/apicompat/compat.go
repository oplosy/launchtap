// Package apicompat reports backward-incompatible changes between two OpenAPI 3.1 documents.
//
// The rules are the ones locked in docs/specs/2026-11-14-public-api-contract-preflight.md: a
// response may only narrow what clients receive, a request may only widen what the server
// accepts, and enum values or oneOf variants may be added but never removed.
package apicompat

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Change is one backward-incompatible difference between the baseline and the current contract.
type Change struct {
	Location string
	Message  string
}

func (c Change) String() string { return c.Location + ": " + c.Message }

type direction int

const (
	request direction = iota
	response
)

var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

type object = map[string]any

type document struct{ root object }

type comparison struct {
	baseline, current document
	changes           []Change
	visited           map[string]bool
}

// Compare returns every breaking change from baseline to current, sorted by location.
func Compare(baseline, current []byte) ([]Change, error) {
	var oldRoot, newRoot object
	if err := json.Unmarshal(baseline, &oldRoot); err != nil {
		return nil, fmt.Errorf("decode baseline contract: %w", err)
	}
	if err := json.Unmarshal(current, &newRoot); err != nil {
		return nil, fmt.Errorf("decode current contract: %w", err)
	}
	c := &comparison{
		baseline: document{oldRoot},
		current:  document{newRoot},
		visited:  map[string]bool{},
	}
	c.paths()
	sort.SliceStable(c.changes, func(i, j int) bool {
		return c.changes[i].Location < c.changes[j].Location
	})
	return c.changes, nil
}

// MajorVersion returns the major component of info.version ("1.2.0" → "1").
func MajorVersion(contract []byte) (string, error) {
	var doc struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.Unmarshal(contract, &doc); err != nil {
		return "", fmt.Errorf("decode contract: %w", err)
	}
	if doc.Info.Version == "" {
		return "", fmt.Errorf("contract has no info.version")
	}
	return strings.SplitN(doc.Info.Version, ".", 2)[0], nil
}

func (c *comparison) report(location, format string, args ...any) {
	c.changes = append(c.changes, Change{Location: location, Message: fmt.Sprintf(format, args...)})
}

func (c *comparison) paths() {
	oldPaths := asObject(c.baseline.root["paths"])
	newPaths := asObject(c.current.root["paths"])
	for _, path := range sortedKeys(oldPaths) {
		newItem, ok := newPaths[path].(object)
		if !ok {
			c.report(path, "path removed")
			continue
		}
		oldItem := asObject(oldPaths[path])
		for _, method := range methods {
			oldOp, ok := oldItem[method].(object)
			if !ok {
				continue
			}
			location := strings.ToUpper(method) + " " + path
			newOp, ok := newItem[method].(object)
			if !ok {
				c.report(location, "operation removed")
				continue
			}
			c.operation(location, oldOp, newOp)
		}
	}
}

func (c *comparison) operation(location string, oldOp, newOp object) {
	c.parameters(location, oldOp, newOp)
	c.requestBody(location, asObject(oldOp["requestBody"]), asObject(newOp["requestBody"]))
	oldResponses := asObject(oldOp["responses"])
	newResponses := asObject(newOp["responses"])
	for _, status := range sortedKeys(oldResponses) {
		at := location + " response " + status
		newResponse, ok := newResponses[status].(object)
		if !ok {
			c.report(at, "response status removed")
			continue
		}
		c.content(at, asObject(asObject(oldResponses[status])["content"]), asObject(newResponse["content"]), response)
	}
}

func (c *comparison) parameters(location string, oldOp, newOp object) {
	key := func(p object) string { return fmt.Sprint(p["in"]) + ":" + fmt.Sprint(p["name"]) }
	oldParams := map[string]object{}
	for _, p := range asArray(oldOp["parameters"]) {
		param := asObject(p)
		oldParams[key(param)] = param
	}
	newParams := map[string]object{}
	for _, p := range asArray(newOp["parameters"]) {
		param := asObject(p)
		newParams[key(param)] = param
	}
	for _, k := range sortedKeys(oldParams) {
		at := location + " parameter " + k
		newParam, ok := newParams[k]
		if !ok {
			c.report(at, "parameter removed")
			continue
		}
		if isTrue(newParam["required"]) && !isTrue(oldParams[k]["required"]) {
			c.report(at, "parameter became required")
		}
		c.schema(at, asObject(oldParams[k]["schema"]), asObject(newParam["schema"]), request)
	}
	for _, k := range sortedKeys(newParams) {
		if _, existed := oldParams[k]; !existed && isTrue(newParams[k]["required"]) {
			c.report(location+" parameter "+k, "new required parameter")
		}
	}
}

func (c *comparison) requestBody(location string, oldBody, newBody object) {
	at := location + " request body"
	if isTrue(newBody["required"]) && !isTrue(oldBody["required"]) {
		c.report(at, "request body became required")
	}
	if len(oldBody) == 0 {
		return
	}
	if len(newBody) == 0 {
		c.report(at, "request body removed")
		return
	}
	c.content(at, asObject(oldBody["content"]), asObject(newBody["content"]), request)
}

func (c *comparison) content(location string, oldContent, newContent object, dir direction) {
	for _, mediaType := range sortedKeys(oldContent) {
		at := location + " " + mediaType
		newMedia, ok := newContent[mediaType].(object)
		if !ok {
			c.report(at, "content type removed")
			continue
		}
		c.schema(at, asObject(asObject(oldContent[mediaType])["schema"]), asObject(newMedia["schema"]), dir)
	}
}

func asObject(value any) object {
	if o, ok := value.(object); ok {
		return o
	}
	return object{}
}

func asArray(value any) []any {
	if a, ok := value.([]any); ok {
		return a
	}
	return nil
}

func isTrue(value any) bool {
	b, ok := value.(bool)
	return ok && b
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
