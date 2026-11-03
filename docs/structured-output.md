# Structured output transport

`pkg/api.InputSchema` and `pkg/api.Property` retain the JSON received by
`json.Unmarshal` in `Raw`. Marshaling a decoded schema emits that JSON; the
OpenAI chat, Responses and Foundry requests preserve it byte for byte except
for whitespace. This includes unknown keywords, nested constraints, numeric
precision, key order and string escapes.

The existing typed fields remain read accessors. When `Raw` is set, it takes
precedence over edits to those fields. Decode a replacement to update both
views, or edit `Raw` directly if no typed view is needed. Clearing `Raw`
explicitly returns to typed serialization at that node and discards keywords
outside the typed view. Go-built schemas keep the previous OpenAI defaults:
empty type becomes `object`, nil properties become `{}`, empty required is
omitted.

Native response formatting is an optional request capability. The caller
chooses it only for a route/model that supports the requested format:

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

`Strict: false` is also sent explicitly. For legacy JSON mode, use
`&api.ResponseFormat{Type: "json_object"}`. Leave `ResponseFormat` nil to
retain the existing tool-based behavior; no native format or synthetic tool
is added automatically.

OpenAI Chat Completions and Foundry send `response_format`. The OpenAI
Responses path maps the same option to `text.format`, flattening the
`json_schema` envelope as specified in the
[OpenAI migration guide](https://developers.openai.com/api/docs/guides/migrate-to-responses).
Anthropic-wire requests ignore this OpenAI-specific option and keep their
existing envelope. The transport does not validate generated output or
select the capability on behalf of an orchestrator.
