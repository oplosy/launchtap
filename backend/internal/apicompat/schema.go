package apicompat

import (
	"encoding/json"
	"fmt"
	"strings"
)

const schemaRefPrefix = "#/components/schemas/"

// resolve follows a local schema $ref; name is the referenced schema, or "" for an inline schema.
func (d document) resolve(schema object) (resolved object, name string) {
	ref, ok := schema["$ref"].(string)
	if !ok {
		return schema, ""
	}
	name = strings.TrimPrefix(ref, schemaRefPrefix)
	return asObject(asObject(asObject(d.root["components"])["schemas"])[name]), name
}

func (c *comparison) schema(location string, oldRaw, newRaw object, dir direction) {
	oldSchema, oldName := c.baseline.resolve(oldRaw)
	newSchema, newName := c.current.resolve(newRaw)
	if oldName != "" && newName != "" {
		// A recursive or shared schema is compared once per direction.
		key := fmt.Sprintf("%s|%s|%d", oldName, newName, dir)
		if c.visited[key] {
			return
		}
		c.visited[key] = true
		location = location + " (" + newName + ")"
	}
	if c.variants(location, oldSchema, newSchema, newRaw, dir) {
		return
	}
	c.types(location, oldSchema, newSchema, dir)
	c.format(location, oldSchema, newSchema, dir)
	c.enum(location, oldSchema, newSchema, dir)
	c.properties(location, oldSchema, newSchema, dir)
	if dir == request {
		c.constraints(location, oldSchema, newSchema)
	}
	if oldItems, ok := oldSchema["items"].(object); ok {
		c.schema(location+"[]", oldItems, asObject(newSchema["items"]), dir)
	}
}

// variants compares oneOf schemas; it reports true when it handled the comparison. newRaw is the
// unresolved current schema, so a oneOf collapsed to a single $ref still matches by name.
func (c *comparison) variants(location string, oldSchema, newSchema, newRaw object, dir direction) bool {
	oldVariants, oldHas := oldSchema["oneOf"].([]any)
	newVariants, newHas := newSchema["oneOf"].([]any)
	if !oldHas && !newHas {
		return false
	}
	if !oldHas {
		if dir == response {
			c.report(location, "response became a oneOf of several schemas")
		}
		return true
	}
	if !newHas {
		newVariants = []any{newRaw}
	}
	byKey := map[string]object{}
	for i, v := range newVariants {
		byKey[variantKey(asObject(v), i)] = asObject(v)
	}
	for i, v := range oldVariants {
		key := variantKey(asObject(v), i)
		match, ok := byKey[key]
		if !ok {
			c.report(location, "oneOf variant %s removed", key)
			continue
		}
		c.schema(location+" oneOf "+key, asObject(v), match, dir)
	}
	return true
}

func variantKey(variant object, index int) string {
	if ref, ok := variant["$ref"].(string); ok {
		return strings.TrimPrefix(ref, schemaRefPrefix)
	}
	if title, ok := variant["title"].(string); ok {
		return title
	}
	if types := typeSet(variant); len(types) > 0 {
		return "type " + strings.Join(sortedKeys(types), "|")
	}
	return fmt.Sprintf("#%d", index)
}

func typeSet(schema object) map[string]bool {
	set := map[string]bool{}
	switch t := schema["type"].(type) {
	case string:
		set[t] = true
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok {
				set[s] = true
			}
		}
	}
	return set
}

// covers reports whether every type in inner is allowed by outer; an empty set allows any type.
func covers(outer, inner map[string]bool) bool {
	if len(outer) == 0 {
		return true
	}
	if len(inner) == 0 {
		return false
	}
	for t := range inner {
		if !outer[t] && (t != "integer" || !outer["number"]) {
			return false
		}
	}
	return true
}

func (c *comparison) types(location string, oldSchema, newSchema object, dir direction) {
	oldTypes, newTypes := typeSet(oldSchema), typeSet(newSchema)
	if dir == response && !covers(oldTypes, newTypes) {
		c.report(location, "response type widened from %s to %s", typeLabel(oldTypes), typeLabel(newTypes))
	}
	if dir == request && !covers(newTypes, oldTypes) {
		c.report(location, "request type narrowed from %s to %s", typeLabel(oldTypes), typeLabel(newTypes))
	}
}

func typeLabel(types map[string]bool) string {
	if len(types) == 0 {
		return "any"
	}
	return strings.Join(sortedKeys(types), "|")
}

func (c *comparison) format(location string, oldSchema, newSchema object, dir direction) {
	oldFormat, _ := oldSchema["format"].(string)
	newFormat, _ := newSchema["format"].(string)
	switch {
	case oldFormat != "" && newFormat != oldFormat:
		c.report(location, "format changed from %q to %q", oldFormat, newFormat)
	case oldFormat == "" && newFormat != "" && dir == request:
		c.report(location, "request format %q added", newFormat)
	}
}

// enumValues returns the allowed values of enum or const, or nil when the schema has neither.
func enumValues(schema object) map[string]bool {
	var values []any
	if e, ok := schema["enum"].([]any); ok {
		values = e
	} else if v, ok := schema["const"]; ok {
		values = []any{v}
	} else {
		return nil
	}
	set := map[string]bool{}
	for _, v := range values {
		encoded, _ := json.Marshal(v)
		set[string(encoded)] = true
	}
	return set
}

func (c *comparison) enum(location string, oldSchema, newSchema object, dir direction) {
	oldValues, newValues := enumValues(oldSchema), enumValues(newSchema)
	if oldValues == nil {
		if newValues != nil && dir == request {
			c.report(location, "request values restricted to an enum")
		}
		return
	}
	if newValues == nil {
		return
	}
	for _, v := range sortedKeys(oldValues) {
		if !newValues[v] {
			c.report(location, "enum value %s removed", v)
		}
	}
}

func requiredSet(schema object) map[string]bool {
	set := map[string]bool{}
	for _, v := range asArray(schema["required"]) {
		if s, ok := v.(string); ok {
			set[s] = true
		}
	}
	return set
}

func (c *comparison) properties(location string, oldSchema, newSchema object, dir direction) {
	oldProps, newProps := asObject(oldSchema["properties"]), asObject(newSchema["properties"])
	oldRequired, newRequired := requiredSet(oldSchema), requiredSet(newSchema)
	closed := newSchema["additionalProperties"] == false
	for _, name := range sortedKeys(oldProps) {
		at := location + "." + name
		newProp, ok := newProps[name].(object)
		switch {
		case !ok && dir == response:
			c.report(at, "response property removed")
		case !ok && closed:
			c.report(at, "request property removed while unknown properties are rejected")
		case ok:
			if dir == response && oldRequired[name] && !newRequired[name] {
				c.report(at, "response property became optional")
			}
			c.schema(at, asObject(oldProps[name]), newProp, dir)
		}
	}
	if dir != request {
		return
	}
	for _, name := range sortedKeys(newRequired) {
		if !oldRequired[name] {
			c.report(location+"."+name, "new required request property")
		}
	}
	if closed && oldSchema["additionalProperties"] != false {
		c.report(location, "request no longer accepts unknown properties")
	}
}

// constraints reports a request bound that rejects a value the baseline accepted.
func (c *comparison) constraints(location string, oldSchema, newSchema object) {
	for _, key := range []string{"maxLength", "maxItems", "maximum"} {
		if tightened(oldSchema[key], newSchema[key], func(o, n float64) bool { return n < o }) {
			c.report(location, "%s tightened", key)
		}
	}
	for _, key := range []string{"minLength", "minItems", "minimum"} {
		if tightened(oldSchema[key], newSchema[key], func(o, n float64) bool { return n > o }) {
			c.report(location, "%s tightened", key)
		}
	}
	if newPattern, ok := newSchema["pattern"].(string); ok && newPattern != oldSchema["pattern"] {
		c.report(location, "pattern changed to %q", newPattern)
	}
}

func tightened(oldValue, newValue any, stricter func(o, n float64) bool) bool {
	n, hasNew := newValue.(float64)
	if !hasNew {
		return false
	}
	o, hasOld := oldValue.(float64)
	return !hasOld || stricter(o, n)
}
