package api

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSchemaInvalidUTF8Replacement(t *testing.T) {
	raw := []byte("{\"type\":\"object\",\"properties\":{},\"pattern\":\"\xff\xfe\"}")
	for _, value := range []any{&InputSchema{}, &Property{}} {
		if err := json.Unmarshal(raw, value); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.Valid(body) {
			t.Errorf("%T emitted invalid UTF-8: %q", value, body)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if got["pattern"] != "\ufffd\ufffd" {
			t.Errorf("replacement = %q", got["pattern"])
		}
	}
}
func TestSchemaNullIsNoOp(t *testing.T) {
	for _, value := range []any{&InputSchema{}, &Property{}, &InputSchema{Type: "object"}, &Property{Type: "string"}} {
		before, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(" null "), value); err != nil {
			t.Fatal(err)
		}
		after, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Errorf("%T null changed %s to %s", value, before, after)
		}
		switch v := value.(type) {
		case *InputSchema:
			if v.Raw != nil {
				t.Error("null retained Raw")
			}
		case *Property:
			if v.Raw != nil {
				t.Error("null retained Raw")
			}
		}
	}
	for _, value := range []any{&InputSchema{}, &Property{}} {
		if err := json.Unmarshal([]byte(`{"type":"object","x-custom":true}`), value); err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(value)
		if err := json.Unmarshal([]byte(`null`), value); err != nil {
			t.Fatal(err)
		}
		after, _ := json.Marshal(value)
		if !bytes.Equal(before, after) {
			t.Errorf("null overwrote existing decoded schema: %s", after)
		}
	}
}
func TestSchemaEmptyRawUsesTypedView(t *testing.T) {
	for _, value := range []any{InputSchema{Type: "object", Raw: json.RawMessage{}}, Property{Type: "string", Raw: json.RawMessage{}}} {
		got, err := json.Marshal(value)
		if err != nil || !bytes.Contains(got, []byte(`"type"`)) {
			t.Errorf("%T empty Raw: %s, %v", value, got, err)
		}
	}
}
func TestSchemaRetainsOnlyRootRaw(t *testing.T) {
	raw := []byte(`{"type":"object","$defs":{"n":{"type":"string"}},"properties":{"a":` + strings.Repeat(`{"type":"array","items":`, 100) + `{"type":"string"}` + strings.Repeat(`}`, 100) + `}}`)
	var s InputSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	retained := len(s.Raw)
	var walk func(Property)
	walk = func(p Property) {
		retained += len(p.Raw)
		if p.Items != nil {
			walk(*p.Items)
		}
		for _, v := range p.Properties {
			walk(v)
		}
	}
	for _, p := range s.Properties {
		walk(p)
	}
	for _, p := range s.Defs {
		walk(p)
	}
	if retained != len(raw) {
		t.Fatalf("retained Raw bytes = %d, want one root = %d", retained, len(raw))
	}
}

func TestSchemaPropertyNullKeywords(t *testing.T) {
	var child Property
	if err := json.Unmarshal([]byte(`{"description":null,"items":null,"enum":null,"const":null,"default":{"description":null},"examples":[null],"x-extension":null}`), &child); err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(InputSchema{Type: "object", Properties: map[string]Property{"p": child}})
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Properties map[string]map[string]json.RawMessage
	}
	if err := json.Unmarshal(got, &root); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"description", "items", "enum"} {
		if _, ok := root.Properties["p"][key]; ok {
			t.Errorf("null keyword %s survived: %s", key, got)
		}
	}
	for _, key := range []string{"const", "default", "examples", "x-extension"} {
		if _, ok := root.Properties["p"][key]; !ok {
			t.Errorf("instance data %s lost: %s", key, got)
		}
	}
}
