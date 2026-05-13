# Turns Endpoint

`POST /api/v1/turns` — send a message and get a response.

## Request

```json
{
  "session_key": "sess_abc123",
  "message": "What files are in my project?",
  "stream": false
}
```

## Response (Sync Mode)

The response waits for the agent to finish. It includes the full message and any tool calls made during the turn.

```json
{
  "session_key": "sess_abc123",
  "message": "Your project has src/, docs/, and tests/ directories.",
  "tool_calls": [
    {"tool": "list_dir", "args": {"path": "."}, "result": "..."}
  ],
  "status": "completed"
}
```

## Streaming Mode

Set `stream: true` to get SSE events. Each event has a type and data. The client reads events as they arrive.

Event types:
- `turn_start` — turn started
- `stream_chunk` — partial text output
- `tool_call` — agent called a tool
- `tool_result` — tool returned a result
- `turn_finish` — turn completed
- `error` — something went wrong

## Session Creation

If `session_key` is not provided, a new session is created. The new key is in the response.
