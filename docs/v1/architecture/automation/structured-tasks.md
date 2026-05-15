# Structured Tasks

Structured tasks allow trigger payloads to specify tool calls the agent should execute. They are embedded in webhook bodies, file contents, or heartbeat tasks.

## Format

Structured tasks use a JSON envelope:

```json
{
  "version": 1,
  "tasks": [
    {"tool": "exec", "params": {"program": "git", "args": ["status"]}},
    {"tool": "send_message", "params": {"text": "Backup complete"}}
  ]
}
```

Or the shorter inline format:
```json
[
  {"tool": "exec", "params": {"program": "ls"}}
]
```

Source: `internal/triggers/structured_tasks.go:11-19` (StructuredToolCall, StructuredTaskEnvelope)

## Parsing sources

Structured tasks can be parsed from:

1. **JSON text** - `ParseStructuredTasksJSON` parses raw JSON bytes
2. **Free text** - `ParseStructuredTasksText` first tries parsing as JSON, then looks for code-fenced blocks labeled `or3-tasks`, `structured-tasks`, or `autonomous-tasks`

Source: `internal/triggers/structured_tasks.go:62-84`

## Metadata integration

Tasks are stored in event metadata under the key `structured_tasks`. The `StructuredTasksMap` function converts a `StructuredTaskEnvelope` to a metadata-compatible map.

Source: `internal/triggers/structured_tasks.go:21-49`

## Extraction from metadata

`StructuredTasksFromMeta` retrieves and normalizes structured tasks from event metadata. It handles nested `structured_tasks` keys.

Source: `internal/triggers/structured_tasks.go:51-60`

## Normalization

`normalizeStructuredTasks` handles multiple input formats:
- Objects with `tasks` array
- Objects with nested `structured_tasks` key
- Bare task arrays
- Map-based task lists (converted from `[]map[string]any`)

Source: `internal/triggers/structured_tasks.go:85-113`

## Tool call validation

Each tool call must have a non-empty `tool` name. Params are optional. Invalid entries cause the entire task set to be rejected.

Source: `internal/triggers/structured_tasks.go:115-168` (normalizeStructuredTaskList)
