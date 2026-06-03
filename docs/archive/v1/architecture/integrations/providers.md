# Provider Integration

The provider client wraps OpenAI-compatible chat completion and embedding APIs. It is defined in `internal/providers/openai.go`.

## Client

```go
type Client struct {
    APIBase         string
    APIKey          string
    HTTP            *http.Client
    EmbedDimensions int
    HostPolicy      security.HostPolicy
    Fallbacks       []Fallback
}
```

Created with `New(apiBase, apiKey, timeout)`.

## Chat Completions

### Non-Streaming Chat

`Client.Chat(ctx, req)` sends a `ChatCompletionRequest` to `POST /chat/completions`:

```go
type ChatCompletionRequest struct {
    Model       string
    Messages    []ChatMessage
    Tools       []ToolDef
    ToolChoice  any
    Temperature float64
}
```

The client retries transient errors through fallback models. Each fallback is a separate `Client` with an optional model override. Transient errors include HTTP 408, 429, >=500, context timeouts, and connection resets.

A single attempt retries on JSON decode errors (empty body, syntax errors) once.

### Streaming Chat

`Client.ChatStream(ctx, req, onDelta)` sends `stream: true`:

- Reads SSE events line-by-line with a scanner
- Parses each `data: ` line as a `ChatStreamChunk`
- Applies chunks through a `StreamAssembler`
- Calls `onDelta(text)` for each text delta
- Returns the fully assembled `ChatCompletionResponse`

If the stream fails before any visible output, the client can fall back to non-streaming mode (controlled by provider profile).

If the response is not `text/event-stream`, it is treated as a non-streaming response.

## Embeddings

`Client.Embed(ctx, model, input)` sends a `POST /embeddings` request:

```go
type EmbeddingRequest struct {
    Model      string
    Input      string
    Dimensions int
}
```

Returns a `[]float32` embedding vector. Falls back through the same fallback chain on transient errors.

## Fallback Chain

Both chat and embedding requests use the same fallback mechanism:

1. Try the primary client
2. If error is not transient, return immediately
3. For each fallback in order:
   - Try the fallback client (with model override if set)
   - If success, return
   - If error is not transient, stop trying fallbacks

## Prompt Cache Support

For Anthropic-compatible endpoints, `SupportsExplicitPromptCache()` returns true when the API base URL contains "anthropic" or "claude".

`BuildCacheAwareSystemContent(stable, volatile)` splits system messages into cached (stable) and non-cached (volatile) parts. The stable part gets `cache_control: {type: "ephemeral"}` markers.

## HTTP Client

`Client.do(req)` wraps the HTTP client with host policy enforcement when `HostPolicy.EnabledPolicy()` is true. The transport is wrapped via `security.WrapHTTPClient`.

## Embedding Fingerprint

`EmbeddingFingerprint(apiBase, model, dimensions)` creates a stable identifier for the embedding space. It changes when the provider endpoint, model, or dimensions change. Used to detect when stored vectors need re-embedding.
