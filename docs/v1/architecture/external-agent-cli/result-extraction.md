# Result Extraction

After a runner process completes, OR3 extracts the final text result from the structured output. This is handled by `internal/agentcli/result_extract.go`.

## finalTextExtractor

The `finalTextExtractor` (`internal/agentcli/result_extract.go:8-13`) watches structured events as they stream and keeps track of the best candidate for the final result.

```go
type finalTextExtractor struct {
    runnerID      RunnerID
    bestScore     int
    bestSequence  int
    bestCandidate string
}
```

Each structured payload is passed to `Consider()` (`internal/agentcli/result_extract.go:19-37`). The function decodes the JSON, calls `extractFinalTextCandidate` to score each candidate, and keeps the highest-scoring one. When two candidates have the same score, the one seen later wins (higher sequence).

## Runner-Specific Extraction

`extractFinalTextCandidate` (`internal/agentcli/result_extract.go:46-101`) has per-runner logic:

### OpenCode (score 100)
- `type: "text"` with a `part.text` field
- `type: "assistant_message"` or `"assistant"` with a `message` or `content` field

### Claude (score 100)
- `type: "result"` with `subtype: "success"` — uses `result` field
- `type: "assistant"` — extracts text from message content blocks (via `extractClaudeAssistantText`)

### Codex (score 100)
- `type: "item.completed"` with an `item.type: "agent_message"` — uses `text` or `content`

### Gemini (score 100)
- Top-level `response` field
- `type: "result"` with `response` or `result`

## Generic Fallback

If no runner-specific extraction matches, `extractGenericFinalText` (`internal/agentcli/result_extract.go:106-168`) tries generic patterns:

- `type: "assistant_message"` / `"assistant"` → `message` (score 90) or `content` (score 85)
- `type: "text"` → `part.text` (score 84)
- `type: "message"` with assistant/model role (score 75-80)
- `type: "result"` → `response` (score 92) or `result` (score 88)
- Top-level `response` (score 70), `result` (score 68), `message` (score 60), `text` (score 55)

Scores determine priority. Higher scores win. If extraction produces nothing, `FinalTextPreview` falls back to the full stdout preview.

## Machine-Oriented Text Filtering

The `looksMachineOriented` function (`internal/agentcli/result_extract.go:233-239`) filters out text that starts with `{` or `[` because these are likely raw JSON output, not human-readable results. This prevents the extractor from picking up raw tool call payloads instead of assistant messages.

## Claude Assistant Text

`extractClaudeAssistantText` (`internal/agentcli/result_extract.go:170-193`) handles Claude's nested message structure. Claude wraps content in a list of blocks with types. The function walks `message.content[]`, finds blocks with `type: "text"`, and joins their text values.
