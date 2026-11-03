package api

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestResponseFormatDoesNotChangeAnthropicWire(t *testing.T) {
	req := CreateMessageRequest{Model: "claude-sonnet-4-20250514", MaxTokens: 1024}
	before, err := marshalAnthropicRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []*ResponseFormat{
		{Type: "json_object"},
		{Type: "json_schema", JSONSchema: &ResponseFormatJSONSchema{
			Name: "result", Strict: true, Schema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
		}},
	} {
		req.ResponseFormat = format
		after, err := marshalAnthropicRequest(req)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatalf("OpenAI-only option changes Anthropic body:\ngot %s\nwant %s", after, before)
		}
	}
}
