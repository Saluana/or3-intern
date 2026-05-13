# Request and Response Models

The API uses JSON for requests and responses. Here are the common patterns.

## Common Request Fields

```json
{
  "session_key": "sess_abc123",
  "message": "Hello, agent!",
  "stream": false
}
```

- `session_key` — identifies the conversation (optional, creates new session if missing)
- `message` — the user's message text
- `stream` — if true, response is SSE streamed (optional, defaults to false)

## Common Response Fields

```json
{
  "session_key": "sess_abc123",
  "message": "Hello! How can I help?",
  "tool_calls": [],
  "status": "completed",
  "events": []
}
```

- `session_key` — the session this response belongs to
- `message` — the agent's response text
- `tool_calls` — list of tools called during the turn
- `status` — completed, pending, or error
- `events` — event log for this turn

## Error Response

```json
{
  "error": "invalid_api_key",
  "message": "The API key is not valid. Check your config.",
  "status_code": 401
}
```

## Pagination

List endpoints use cursor-based pagination:

```json
{
  "items": [...],
  "next_cursor": "cursor_xyz",
  "has_more": true
}
```
