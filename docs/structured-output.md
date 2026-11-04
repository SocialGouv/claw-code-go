# Structured output transport

`pkg/api.InputSchema` and `pkg/api.Property` normalize received schemas at the
public `json.Unmarshal` boundary and store the result in `Raw`. A single
`UseNumber` decode feeds both the shared cleanup and the typed read accessors.
Only the public root retains raw bytes. Marshal reuses the cleaned bytes when
an immutable snapshot matches `Raw`; replacing or editing `Raw` in place
triggers the same cleanup. Decode, retained memory and marshal scale with the
input size, rather than size times nesting depth. Invalid tool-schema UTF-8
bytes become individual U+FFFD characters at this shared boundary. Decoding
`null` remains a no-op, including on an existing value.

The offline guarantee test validates the **serialized tool schema** on
Anthropic, Bedrock and Vertex against the embedded JSON Schema draft 2020-12
meta-schema using `github.com/santhosh-tekuri/jsonschema/v6` (test imports only).
It covers legacy forms, every schema position, a cross-product of standard
keyword shapes, and actual FastMCP/OpenAPI 3.0, zod/openApi3, TypeBox,
`google/jsonschema-go` and Playwright MCP output. Expected trees and accepted
and rejected instances guard the meaning of conversions; fuzzing exercises
the same meta-schema oracle. See the [fixture provenance and reproduction
scripts](../internal/testutil/schematest/testdata/README.md).

The shared helper in `internal/api/schema_json.go` performs these conversions
for every tool transport, and for native response schemas:

| Received form | Output and semantics |
| --- | --- |
| Boolean `exclusiveMinimum` / `exclusiveMaximum` | `true` plus a numeric inclusive bound moves that exact number to the exclusive keyword and removes the inclusive one. `false` disappears, keeping the inclusive bound. An orphan boolean disappears: it supplies no bound. Numeric exclusive bounds are unchanged. |
| Tuple `items: [...]` plus `additionalItems` | `prefixItems` plus `items`, preserving positional and tail constraints. An empty tuple becomes only its tail constraint. Mixed modern constraints are intersected through `allOf`. |
| `dependencies` arrays / schemas | `dependentRequired` / `dependentSchemas`. Colliding modern declarations are conjoined through `allOf`, preserving both constraints. |
| Local JSON Pointer references into migrated positions | Rewritten to the new tuple/dependency position, with pointer escaping and local `$id` resource scopes preserved. External resources are never loaded or expanded. |
| `definitions` | Kept and recursively cleaned, preserving existing `#/definitions/...` references. The 2020-12 meta-schema permits it. |
| Old standard `$schema` declaration | Replaced with `https://json-schema.org/draft/2020-12/schema` after conversion. Explicit draft-04/06/07 `$ref` validation siblings remain ignored as in the source dialect. |
| Draft-04 `id` | Becomes `$id`; representable fragments become `$anchor`. Existing modern identifiers take precedence. |
| OpenAPI 3.0 `nullable: true` | Adds `null` to an explicit scalar `type`. It does **not** bypass `enum`, `const` or other constraints. Without an explicit type it has no effect. `nullable: false` disappears. |
| Null child in a schema position | Becomes `{}` in maps, arrays and single-schema keywords, including `not`, conditional branches and unevaluated keywords. |
| Wrong container / keyword type | Retained when permitted by 2020-12, otherwise removed. Invalid schema members become `{}`. Empty `anyOf`/`oneOf` remain unsatisfiable via `not: {}`; empty `allOf`/`prefixItems` disappear. |
| Non-array `examples`, including null | Removed; this loses an annotation, not a validation constraint. Array elements remain instance data. |

**Loss policy:** malformed constraints have no unambiguous valid meaning.
Omitting an invalid keyword or replacing an invalid child with `{}` discards
its intended constraint; it can widen or narrow acceptance depending on the
surrounding applicator (especially `not`/`oneOf`). Invalid bounds, types,
required lists, dependent-required members, lengths/counts, anchors and
container shapes follow that policy. Non-representable `id` fragments lose
their reference target; invalid modern `$id` fragments are omitted. Unsupported
legacy vocabulary semantics (for example a boolean `$recursiveAnchor`) are
not a complete dialect migration and may be lost when they violate 2020-12.
These losses are documented beside the cleanup in production code as well.
The guarantee is meta-schema conformance, not automatic acceptance by every
endpoint's supported subset, resolution of external references, or validation
of generated output.

The dialect decision follows the role of [`$schema` as the dialect identifier
and meta-schema URI](https://json-schema.org/draft/2020-12/json-schema-core#name-the-schema-keyword).
The [TypeScript SDK report](https://github.com/modelcontextprotocol/typescript-sdk/issues/745)
is evidence of the compatibility seam, not proof that a draft-07 URI alone
causes rejection. Re-labeling is paired with conversions and instance tests.
The nullable rule follows [OpenAPI 3.0.3 §4.7.24.2](https://spec.openapis.org/oas/v3.0.3.html#fixed-fields-20).

Instance data in `const`, `default`, array-valued `examples` and `enum`, and
unknown extensions, is untouched by schema traversal. Number lexemes retain
precision and exponent spelling. An unchanged valid schema keeps its bytes;
rewriting may reorder keys and re-encode string escapes without changing their
values. With nonempty `Raw`, edits to typed fields do not change serialization.
Decode a replacement to update both views, or edit `Raw` if no typed view is
needed. Clearing `Raw` opts into the typed view and discards keywords outside
that view throughout the decoded tree. A separately decoded `Property` owns
its own normalized `Raw`, including inside a Go-built parent.

OpenAI chat, Responses and Foundry additionally supply missing tool-root
`type: object` and `properties: {}`. Other non-object roots return a local
`*api.RequestValidationError`. Shared cleanup has already handled null-valued
keywords; the OpenAI adapter has no duplicate `required: null` branch.
Explicit empty required arrays survive. Go-built schemas keep the historical
OpenAI defaults (empty required omitted).

Native response formatting is optional. The caller chooses a format supported
by the endpoint/model:

```go
req.ResponseFormat = &api.ResponseFormat{
    Type: "json_schema",
    JSONSchema: &api.ResponseFormatJSONSchema{
        Name: "analysis",
        Strict: true,
        Schema: json.RawMessage(`{"type":"object","properties":{"score":{"type":"integer","minimum":0,"maximum":100}},"required":["score"],"additionalProperties":false}`),
    },
}
```

`Strict: false` is sent explicitly. For legacy JSON mode, use
`&api.ResponseFormat{Type: "json_object"}`; explicit text mode is
`&api.ResponseFormat{Type: "text"}`. A nil format leaves the existing behavior
unchanged and adds no synthetic tool.

OpenAI Chat Completions and Foundry send `response_format`; Responses flattens
it into `text.format`. All three use `ResponseFormat.Normalized`, which copies
the descriptor, validates it and applies the shared schema cleanup without
mutating the caller. `Validate` accepts `text`, `json_object` and `json_schema`.
The first two forbid a descriptor; `json_schema` requires a descriptor with a
nonempty name and non-null, syntactically valid **UTF-8** JSON. Invalid UTF-8 is
rejected here, rather than repaired as for received tools. Envelope errors
are local, non-retryable `*api.RequestValidationError` values identifying the
field. The endpoint/model still determines strict-mode and schema-subset rules.

Anthropic, Bedrock, Vertex, z.ai and Moonshot refuse non-nil `ResponseFormat`
before sending HTTP. Their Anthropic-wire API has no native `response_format`
field and claw does not translate this option to the API's structured-output
mechanism. This is the precise transport limitation, not a claim that those
services cannot provide structured output.
