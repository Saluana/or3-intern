# Provider Profiles

Provider profiles tune the client behavior for different API providers. Defined in `internal/providers/profile.go`.

## ProviderProfile

```go
type ProviderProfile struct {
    Name       string
    ToolSchema ToolSchemaPolicy
    Streaming  StreamPolicy
    Retry      ProviderRetryPolicy
}
```

## Three Built-In Profiles

### OpenAI Compatible (`OpenAICompatibleProfile()`)

The default profile. Used for standard OpenAI-compatible APIs.

**Tool Schema:**
- `AllowAdditionalProperties: true`
- Drops: `$schema`, `examples`, `default`
- Requires object root with `type: "object"` and `properties: {}`
- Max description: 1200 runes

**Streaming:**
- Text mode: `snapshot_or_delta` (detects whether each chunk is a delta or full snapshot)
- Tool call mode: `openai_indexed` (uses index-based accumulation)
- Retry on malformed: yes

**Retry:**
- Retry empty stream: yes
- Retry malformed before output: yes
- Fallback to non-stream: yes

### OpenRouter Compatible (`OpenRouterCompatibleProfile()`)

For OpenRouter API which is stricter about schema keywords.

**Tool Schema (differences from OpenAI):**
- `AllowAdditionalProperties: false`
- Drops additional keywords: `nullable`, `readOnly`, `writeOnly`, `deprecated`, `$defs`, `oneOf`, `anyOf`, `allOf`

### Local Compatible (`LocalCompatibleProfile()`)

For local LLM servers (Ollama, LM Studio, etc.) that are less reliable with streaming.

**Tool Schema (differences from OpenAI):**
- Drops only: `$schema`
- Max description: 2000 runes

**Streaming:**
- Retry on malformed: no

**Retry:**
- Retry malformed before output: no
- Fallback to non-stream: no

## Profile Selection

`SelectProviderProfile(providerName, apiBase, model)` (`internal/providers/profile.go:90-102`) chooses a profile by checking the combined lowercase string of provider name, API base URL, and model name:

- Contains "openrouter" → OpenRouter profile
- Contains "ollama", "lmstudio", or "local" → Local profile
- Otherwise → OpenAI profile

## Per-Client Profile

`Client.ProviderProfile(model)` calls `SelectProviderProfile` with the client's `APIBase` and the given model name. This profile is used by the stream assembler and schema sanitizer.
