package openaiwire

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/SocialGouv/claw-code-go/internal/api"
)

// NormalizedParameters is the single normaliser both OpenAI tool converters
// (chat + responses) route through. It must never emit the three null shapes
// OpenAI's function validator rejects.
func TestNormalizedParameters(t *testing.T) {
	t.Run("nil required is omitted, not null", func(t *testing.T) {
		p, err := NormalizedParameters(api.InputSchema{
			Type:       "object",
			Properties: map[string]api.Property{"pages": {Type: "array", Items: &api.Property{Type: "string"}}},
			Required:   nil,
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(p), "null") {
			t.Errorf("emitted null: %s", p)
		}
		if strings.Contains(string(p), "\"required\"") {
			t.Errorf("required must be omitted when empty: %s", p)
		}
		if !strings.Contains(string(p), "\"items\"") {
			t.Errorf("array items dropped: %s", p)
		}
	})

	t.Run("empty type defaults to object, nil properties to {}", func(t *testing.T) {
		p, err := NormalizedParameters(api.InputSchema{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(p), "\"type\":\"object\"") {
			t.Errorf("type not defaulted to object: %s", p)
		}
		if !strings.Contains(string(p), "\"properties\":{}") {
			t.Errorf("properties not defaulted to {}: %s", p)
		}
	})

	t.Run("present required survives", func(t *testing.T) {
		p, err := NormalizedParameters(api.InputSchema{
			Type:       "object",
			Properties: map[string]api.Property{"url": {Type: "string"}},
			Required:   []string{"url"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(p), "\"required\":[\"url\"]") {
			t.Errorf("required lost: %s", p)
		}
	})
}

// Received schemas own their shape, including omitted keywords and explicit
// empty collections. Only Go-built schemas use the historical defaults.
func TestNormalizedParameters_ReceivedSchemaIsVerbatim(t *testing.T) {
	for _, input := range []string{`{}`, `{"properties":{},"required":[]}`, `{"type":"object","additionalProperties":false}`, `false`} {
		var schema api.InputSchema
		if err := json.Unmarshal([]byte(input), &schema); err != nil {
			t.Fatal(err)
		}
		got, err := NormalizedParameters(schema)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != input {
			t.Errorf("received schema changed: got %s, want %s", got, input)
		}
	}
}
