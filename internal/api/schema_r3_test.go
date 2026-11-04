package api

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSchemaHugeIntegerExponentPreserved(t *testing.T) {
	// The meta-schema compiler materializes numbers; test the wire lexeme
	// directly so this witness neither allocates an enormous integer nor
	// silently loses a mathematically integral count after ParseInt overflows.
	raw := []byte(`{"maxLength":1e9223372036854775808,"required":null}`)
	var schema InputSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	out, err := schema.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, []byte(`{"maxLength":1e9223372036854775808}`)) {
		t.Fatalf("integral count was lost: %s", out)
	}
}

func TestSchemaOrderThroughMigration(t *testing.T) {
	for _, raw := range []string{
		`{"description":null,"default":{"z":1,"a":2},"enum":[{"z":1,"a":2}],"x-extension":{"z":1,"a":2}}`,
		`{"items":[{"properties":{"z":{},"a":{}}}],"prefixItems":[{"properties":{"z":{},"a":{}}}],"additionalItems":{"properties":{"z":{},"a":{}}}}`,
		`{"dependencies":{"x":{"properties":{"z":{},"a":{}}}},"dependentSchemas":{"x":{"properties":{"z":{},"a":{}}}}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var schema InputSchema
			if err := json.Unmarshal([]byte(raw), &schema); err != nil {
				t.Fatal(err)
			}
			out, err := schema.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if got, want := bytes.Count(out, []byte(`"z":`)), bytes.Count([]byte(raw), []byte(`"z":`)); got != want {
				t.Fatalf("objects lost: %s", out)
			}
			for rest := out; ; {
				z, a := bytes.Index(rest, []byte(`"z":`)), bytes.Index(rest, []byte(`"a":`))
				if z < 0 && a < 0 {
					break
				}
				if z < 0 || a < z {
					t.Fatalf("object order changed: %s", out)
				}
				rest = rest[a+4:]
			}
			var again InputSchema
			if err := json.Unmarshal(out, &again); err != nil {
				t.Fatal(err)
			}
			twice, err := again.MarshalJSON()
			if err != nil || !bytes.Equal(out, twice) {
				t.Fatalf("normalization is not idempotent: %s -> %s (%v)", out, twice, err)
			}
		})
	}
}
