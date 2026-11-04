package api

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"
)

// ownedSchemaJSON mirrors encoding/json's per-invalid-byte U+FFFD replacement.
// Each public UnmarshalJSON boundary owns exactly one copy of the received JSON.
func ownedSchemaJSON(data []byte) json.RawMessage {
	if !utf8.Valid(data) {
		return []byte(string([]rune(string(data))))
	}
	return bytes.Clone(data)
}

// propertyReadMap decodes child accessors without retaining each subtree's Raw.
// InputSchema/Property retain the complete schema only at the public boundary.
type propertyReadView Property

func (p *propertyReadView) UnmarshalJSON(data []byte) error {
	return (*Property)(p).unmarshalJSON(data, false)
}

type propertyReadMap map[string]Property

func (m *propertyReadMap) UnmarshalJSON(data []byte) error {
	var entries map[string]propertyReadView
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	if entries == nil {
		return nil
	}
	out := make(map[string]Property, len(entries))
	for key, p := range entries {
		out[key] = Property(p)
	}
	*m = out
	return nil
}

// cleanSchemaJSON is the common tool-schema wire policy for every transport.
// It only visits schema positions, never instance data (const/default/examples,
// enum members) or opaque extensions. Unchanged schemas keep their exact bytes.
func cleanSchemaJSON(raw json.RawMessage) (json.RawMessage, error) {
	if !utf8.Valid(raw) {
		raw = ownedSchemaJSON(raw)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return raw, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	changed := false
	for key, value := range fields {
		kind := schemaKeywordKind(key)
		if kind == "" {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			delete(fields, key)
			changed = true
			continue
		}
		var next json.RawMessage
		var err error
		switch kind {
		case "schema":
			next, err = cleanSchemaJSON(value)
		case "map":
			next, err = cleanSchemaChildren(value, false)
		case "array":
			next, err = cleanSchemaChildren(value, true)
		case "items":
			if v := bytes.TrimSpace(value); len(v) > 0 && v[0] == '[' {
				next, err = cleanSchemaChildren(value, true)
			} else {
				next, err = cleanSchemaJSON(value)
			}
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(next, value) {
			fields[key] = next
			changed = true
		}
	}
	if !changed {
		return raw, nil
	}
	return marshalSchemaJSON(fields)
}

func schemaKeywordKind(key string) string {
	switch key {
	case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas", "dependencies":
		return "map"
	case "additionalProperties", "additionalItems", "unevaluatedProperties", "unevaluatedItems", "propertyNames", "contains", "not", "if", "then", "else", "contentSchema":
		return "schema"
	case "allOf", "anyOf", "oneOf", "prefixItems":
		return "array"
	case "items":
		return "items"
	case "$schema", "$id", "id", "$ref", "$anchor", "$dynamicRef", "$dynamicAnchor", "$recursiveRef", "$recursiveAnchor", "$vocabulary", "$comment",
		"type", "enum", "required", "dependentRequired", "title", "description", "format", "contentEncoding", "contentMediaType",
		"multipleOf", "maximum", "exclusiveMaximum", "minimum", "exclusiveMinimum", "maxLength", "minLength", "pattern",
		"maxItems", "minItems", "uniqueItems", "maxContains", "minContains", "maxProperties", "minProperties", "readOnly", "writeOnly", "deprecated":
		return "value"
	}
	return ""
}

func cleanSchemaChildren(raw json.RawMessage, array bool) (json.RawMessage, error) {
	changed := false
	if array {
		var children []json.RawMessage
		if err := json.Unmarshal(raw, &children); err != nil {
			return nil, err
		}
		for i, value := range children {
			next, err := cleanSchemaJSON(value)
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(next, value) {
				children[i] = next
				changed = true
			}
		}
		if changed {
			return marshalSchemaJSON(children)
		}
	} else {
		var children map[string]json.RawMessage
		if err := json.Unmarshal(raw, &children); err != nil {
			return nil, err
		}
		for key, value := range children {
			next, err := cleanSchemaJSON(value)
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(next, value) {
				children[key] = next
				changed = true
			}
		}
		if changed {
			return marshalSchemaJSON(children)
		}
	}
	return raw, nil
}

func marshalSchemaJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
