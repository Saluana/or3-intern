# Schema Sanitizer

The schema sanitizer cleans up tool definitions before sending them to AI providers. Some providers reject schemas that contain unsupported JSON Schema keywords. The sanitizer is in `internal/providers/schema_sanitizer.go`.

## SchemaSanitizer

```go
type SchemaSanitizer struct {
    Profile ProviderProfile
}
```

## SanitizeToolDef

`SanitizeToolDef(def)` processes a single `ToolDef`:

### 1. Description Truncation

The tool description is truncated to the profile's `MaxDescriptionRunes` limit (1200 for OpenAI, 2000 for local). Rune count is used (not byte count) to handle multi-byte characters.

### 2. Object Root Fixup

If `RequireObjectRoot` is true:
- Missing `type` field is set to `"object"`
- Missing `properties` field is set to `{}`
- If parameters is not a map, it is replaced with `{type: "object", properties: {}}`

### 3. Keyword Removal

The `sanitizeSchemaNode` function (`internal/providers/schema_sanitizer.go:67-91`) recursively walks the parameter schema and removes keys listed in `DropUnsupportedKeywords`:

**OpenAI profile drops:** `$schema`, `examples`, `default`

**OpenRouter profile drops (additional):** `nullable`, `readOnly`, `writeOnly`, `deprecated`, `$defs`, `oneOf`, `anyOf`, `allOf`

**Local profile drops:** `$schema`

The walk handles nested maps and arrays. String arrays are skipped as leaf values.

### 4. Report

The sanitizer returns a `SchemaSanitizationReport`:

```go
type SchemaSanitizationReport struct {
    ToolName              string
    RemovedKeywords       []string
    TruncatedDescriptions int
    Warnings              []string
}
```

## Batch Sanitization

`SanitizeToolDefs(defs, profile)` processes all tool definitions for a request. Only tools that had changes (removals, truncations, or warnings) appear in the report.

## Clone

Schema values are deep-cloned via `cloneJSONValue` (`internal/providers/schema_sanitizer.go:107-134`) before modification. This prevents the sanitizer from mutating the original tool definitions stored in the registry.
