# Stream Assembler

The stream assembler processes SSE chunks from streaming chat completions and assembles them into a complete assistant message. It is in `internal/providers/stream_assembler.go`.

## StreamAssembler

```go
type StreamAssembler struct {
    Profile          ProviderProfile
    content          strings.Builder
    previousSnapshot string
    toolCalls        toolCallAccumulator
    sawData          bool
    sawVisibleOutput bool
    warnings         []ProviderStreamWarning
}
```

## Chunk Processing

`ApplyChunk(chunk)` processes each `ChatStreamChunk`:

1. Sets `sawData = true`
2. For each choice, processes the delta:

### Text Deltas

When `delta.Content` is non-empty, `textDelta()` converts it based on the profile's `TextMode`:

**Delta mode** (`StreamTextModeDelta`): returns the content as-is. Providers that send pure deltas use this.

**Snapshot or Delta mode** (`StreamTextModeSnapshotOrDelta`): detects whether the content is a delta or a full snapshot. This is the default for OpenAI-compatible APIs which can send either format:

1. First chunk: always emitted
2. If content equals previous snapshot → delta is empty (duplicate)
3. If content starts with previous snapshot → return the suffix
4. If content starts with the accumulated full content → return the suffix
5. Otherwise, check for suffix/prefix overlap and return non-overlapping part

### Tool Call Deltas

Tool calls arrive incrementally over the stream. The `toolCallAccumulator` (`internal/providers/stream_assembler.go:189-233`) accumulates them:

1. Each delta has an `Index` indicating which tool call slot it belongs to
2. Slots are created as needed (growing the slice)
3. `Function.Name` and `Function.Arguments` are concatenated (SSE sends partial JSON fragments)
4. `ID` and `Type` are set from the first delta that provides them

## Malformed Chunk Handling

`RecordMalformed(raw)` is called for lines that fail to parse as valid JSON:

- Records a warning with code `"malformed_chunk"`
- The raw input is truncated to 240 runes in the preview
- Warnings are accumulated but processing continues

## Finalization

`Finalize()` returns the assembled `ProviderAssistantMessage`:

### Empty Stream
If no data events were seen at all, returns `ProviderStreamError{Code: "empty_stream"}`. Retryable if the profile says so.

### Malformed Before Output
If warnings were recorded but no visible text or tool calls appeared, returns `ProviderStreamError{Code: "malformed_stream_before_output"}`. Retryable if the profile says so.

### Incomplete Tool Arguments
Tool calls with non-emptiable JSON arguments are checked. If any argument string fails to parse as JSON, a warning is recorded. If no visible output was seen, returns `ProviderStreamError{Code: "incomplete_tool_arguments"}`.

### Success
Returns the full content string, final tool calls, and any accumulated warnings.

## toolCallAccumulator

Tracks tool calls by index:

- `Apply(delta)` adds/updates tool calls from SSE deltas
- `Finalize()` returns the completed tool calls with auto-generated IDs (`call_1`, `call_2`, ...) if none were provided
- All calls get `Type: "function"` by default
