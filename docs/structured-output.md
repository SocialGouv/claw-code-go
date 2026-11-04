# Structured output transport

`pkg/api.InputSchema` and `pkg/api.Property` retain received JSON in `Raw`
at the public `json.Unmarshal` boundary. Child fields are typed read accessors;
they do not retain additional copies of each subtree. Invalid UTF-8 bytes are
replaced individually with U+FFFD, matching `encoding/json`. Decoding `null`
is a no-op, including on an existing value.

Every tool transport removes null-valued JSON Schema keywords recursively at
schema positions. Instance data in `const`, `default`, `examples` and `enum`
members, and unknown extensions, is preserved. OpenAI chat, Responses and
Foundry also repair only the tool root: `null` becomes an empty object, missing,
null or empty `type` becomes `object`, missing/null `properties` becomes `{}`,
and null `required` is omitted. Other non-object roots return a local
`*api.RequestValidationError`. Explicit empty `required` arrays are preserved.
A conforming received schema is unchanged by normalization; HTTP serialization
may compact whitespace (and Anthropic-style envelopes escape HTML).

Unknown keywords, nested constraints, numeric precision and string escapes
survive these repairs. When nonempty, `Raw` wins over edits to typed fields.
Decode a replacement to update both views, or edit `Raw` directly if no typed
view is needed. Clearing `Raw` (nil or zero length) opts into the typed view and
discards keywords outside that view throughout the decoded tree. A separately
decoded `Property` owns its own root `Raw`, including inside a Go-built parent.
Go-built schemas retain the historical OpenAI defaults: empty type becomes
`object`, nil properties become `{}`, and empty required is omitted.

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
