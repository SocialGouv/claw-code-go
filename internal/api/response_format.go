package api

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Validate checks the native format envelope, not the model's JSON Schema
// subset or generated output. A nil receiver leaves formatting at its default.
func (f *ResponseFormat) Validate() error {
	if f == nil {
		return nil
	}
	invalid := func(field, reason string) error {
		return &RequestValidationError{Field: "ResponseFormat" + field, Reason: reason}
	}
	switch f.Type {
	case "json_object":
		if f.JSONSchema != nil {
			return invalid(".JSONSchema", "must be absent for json_object")
		}
	case "json_schema":
		if f.JSONSchema == nil {
			return invalid(".JSONSchema", "descriptor is required for json_schema")
		}
		if strings.TrimSpace(f.JSONSchema.Name) == "" {
			return invalid(".JSONSchema.Name", "must not be empty")
		}
		raw := bytes.TrimSpace(f.JSONSchema.Schema)
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			return invalid(".JSONSchema.Schema", "must contain a non-null JSON schema")
		}
		if !json.Valid(raw) {
			return invalid(".JSONSchema.Schema", "must be valid JSON")
		}
	default:
		return invalid(".Type", "must be json_schema or json_object")
	}
	return nil
}
